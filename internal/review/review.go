// Package review runs fresh, closed-schema, read-only quality reviews. It
// records findings but has no code-writing, publishing or acceptance authority.
package review

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/quality"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

var ErrInvalidReview = errors.New("review output is malformed or non-passing")

type Finding struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	RepositoryID   string `json:"repository_id"`
	Path           string `json:"path,omitempty"`
	Line           int    `json:"line,omitempty"`
	Evidence       string `json:"evidence"`
	Recommendation string `json:"recommendation"`
}

type Document struct {
	SchemaVersion int       `json:"schema_version"`
	Decision      string    `json:"decision"`
	Summary       string    `json:"summary"`
	Findings      []Finding `json:"findings"`
}

type Manifest struct {
	Scope          quality.Scope      `json:"scope"`
	Checks         quality.CheckGates `json:"checks"`
	Definition     json.RawMessage    `json:"definition"`
	RepositoryDiff string             `json:"repository_diff_digest"`
}

type Reviewer interface {
	Review(context.Context, Manifest) ([]byte, error)
}

type Request struct {
	CommandID      string         `json:"command_id"`
	Target         quality.Target `json:"target"`
	Actor          string         `json:"actor"`
	SessionID      string         `json:"session_id"`
	NativeIdentity string         `json:"native_identity"`
}

type RecordedFinding struct {
	Finding
	Blocking bool `json:"blocking"`
}

type Result struct {
	ID                     string            `json:"id"`
	EffectID               string            `json:"effect_id"`
	ScopeID                string            `json:"scope_id"`
	Status                 string            `json:"status"`
	ReviewerSessionID      string            `json:"reviewer_session_id"`
	ReviewerNativeIdentity string            `json:"reviewer_native_identity"`
	ReadOnlyVerified       bool              `json:"read_only_verified"`
	EvidenceManifestDigest string            `json:"evidence_manifest_digest"`
	ResultArtifactID       string            `json:"result_artifact_id"`
	ResultArtifactDigest   string            `json:"result_artifact_digest"`
	ResultDigest           string            `json:"result_digest"`
	StartedAt              int64             `json:"started_at"`
	EndedAt                int64             `json:"ended_at"`
	Findings               []RecordedFinding `json:"findings"`
}

type Runner struct {
	Engine   *core.Engine
	Owner    *coordinator.Owner
	Reviewer Reviewer
	Hook     func(string) error
}

func blockingThreshold(value string) int {
	switch value {
	case "critical":
		return 4
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 3
	}
}
func severityRank(value string) int {
	switch value {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	case "suggestion":
		return 0
	}
	return -1
}

func validateDocument(raw []byte, scope quality.Scope) (Document, error) {
	var document Document
	if err := store.Decode(raw, &document); err != nil {
		return document, err
	}
	if document.SchemaVersion != 1 || (document.Decision != "pass" && document.Decision != "request_changes") || strings.TrimSpace(document.Summary) == "" || len(document.Summary) > 8192 || len(document.Findings) > 100 {
		return document, errors.New("invalid closed review result")
	}
	repositories := map[string]bool{}
	for _, repository := range scope.Repositories {
		repositories[repository.ID] = true
	}
	seen := map[string]bool{}
	for _, finding := range document.Findings {
		if !store.SafeID(finding.ID) || seen[finding.ID] || severityRank(finding.Severity) < 0 || !repositories[finding.RepositoryID] || strings.TrimSpace(finding.Evidence) == "" || strings.TrimSpace(finding.Recommendation) == "" || len(finding.Evidence) > 8192 || len(finding.Recommendation) > 8192 || finding.Line < 0 {
			return document, errors.New("invalid review finding")
		}
		if finding.Path != "" && (filepath.IsAbs(finding.Path) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(finding.Path))) != finding.Path || finding.Path == ".." || strings.HasPrefix(finding.Path, "../")) {
			return document, errors.New("review finding path escapes repository")
		}
		seen[finding.ID] = true
	}
	return document, nil
}

func (r *Runner) prepare(ctx context.Context, request Request, scope quality.Scope, manifestDigest string) (string, error) {
	if r.Engine == nil || r.Engine.DB == nil || r.Owner == nil || r.Reviewer == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.SessionID) || !store.SafeID(request.NativeIdentity) || request.SessionID == request.NativeIdentity {
		return "", errors.New("fresh reviewer identities and implementation required")
	}
	if request.Actor != "fixture" {
		return "", errors.New("production review dispatch remains disabled pending exact live qualification")
	}
	if !qualityFixture(scope) {
		return "", errors.New("synthetic reviewer requires disposable fixture repositories")
	}
	effectID := store.Digest([]byte("quality.review\x00" + request.CommandID))
	args, _ := json.Marshal(request)
	receipt, err := r.Engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: "core", Kind: "quality.review.prepare", Args: args}, func(tx *store.Tx) (any, error) {
		if err := quality.EnsureTargetDispatchable(ctx, tx, scope.Target); err != nil {
			return nil, err
		}
		var projectState string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", r.Engine.ProjectID).Scan(&projectState); err != nil {
			return nil, err
		}
		if projectState != "ready" {
			return nil, errors.New("project state prevents reviewer dispatch")
		}
		var reused int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM sessions WHERE id=? OR coalesce(durable_id,'')=? OR coalesce(runtime_id,'')=?)+(SELECT count(*) FROM review_results_v2 WHERE reviewer_session_id=? OR reviewer_native_identity=?)`, request.SessionID, request.NativeIdentity, request.NativeIdentity, request.SessionID, request.NativeIdentity).Scan(&reused); err != nil {
			return nil, err
		}
		if reused != 0 {
			return nil, errors.New("reviewer identity is not fresh or is an implementation identity")
		}
		if scope.Target.Kind == "task" {
			var state string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM tasks WHERE id=?", scope.Target.TaskID).Scan(&state); err != nil {
				return nil, err
			}
			if state != "checking" && state != "reviewing" {
				return nil, errors.New("task is not ready for review")
			}
			if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='reviewing' WHERE id=?", scope.Target.TaskID); err != nil {
				return nil, err
			}
		} else {
			var state string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM plans WHERE id=?", scope.Target.PlanID).Scan(&state); err != nil {
				return nil, err
			}
			if state != "verifying" {
				return nil, errors.New("plan is not verifying")
			}
		}
		var charged, unknown, limit int64
		var budgetErr error
		if scope.Target.Kind == "task" {
			budgetErr = tx.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND plan_id=? AND task_id=?", scope.Target.PlanID, scope.Target.TaskID).Scan(&charged, &unknown, &limit)
		} else {
			budgetErr = tx.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='plan_services' AND plan_id=? AND task_id IS NULL", scope.Target.PlanID).Scan(&charged, &unknown, &limit)
		}
		if budgetErr != nil {
			return nil, budgetErr
		}
		if charged+unknown >= limit {
			return nil, errors.New("cumulative quality budget exhausted")
		}
		intent, _ := json.Marshal(map[string]any{"scope_id": scope.ID, "manifest_digest": manifestDigest, "session_id": request.SessionID, "native_identity": request.NativeIdentity})
		_, err := tx.ExecContext(ctx, `INSERT INTO quality_effects_v2(id,scope_id,kind,definition_id,definition_digest,actor,state,intent_json,prepared_at) VALUES(?,?,'review','fresh-review',?,?,'prepared',?,?)`, effectID, scope.ID, manifestDigest, request.Actor, string(intent), store.Now())
		if err != nil {
			return nil, err
		}
		return map[string]string{"effect_id": effectID}, nil
	})
	if err != nil {
		return "", err
	}
	var response map[string]string
	err = json.Unmarshal(receipt, &response)
	return response["effect_id"], err
}

func qualityFixture(scope quality.Scope) bool {
	// fixtureScope is deliberately repeated here rather than exported as policy.
	for _, repository := range scope.Repositories {
		if info, err := osLstat(filepath.Join(repository.Identity.Root, ".vigil-disposable-fixture")); err != nil || !info {
			return false
		}
	}
	return true
}

var osLstat = func(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func (r *Runner) Run(ctx context.Context, request Request) (Result, error) {
	var result Result
	scope, err := quality.Observe(ctx, r.Engine, request.Target)
	if err != nil {
		return result, err
	}
	if err = quality.Persist(ctx, r.Engine, scope); err != nil {
		return result, err
	}
	checks, err := quality.EvaluateChecks(ctx, r.Engine, scope)
	if err != nil {
		return result, err
	}
	if !checks.Satisfied {
		return result, errors.New("all current checks must pass before review")
	}
	manifest := Manifest{Scope: scope, Checks: checks, Definition: scope.DefinitionJSON, RepositoryDiff: scope.RepositorySetDigest}
	manifestRaw, _ := json.Marshal(manifest)
	manifestRaw, _ = store.Canonical(manifestRaw)
	manifestDigest := store.Digest(manifestRaw)
	effectID, err := r.prepare(ctx, request, scope, manifestDigest)
	if err != nil {
		return result, err
	}
	var existing string
	if err = r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT id FROM review_results_v2 WHERE effect_id=?", effectID).Scan(&existing); err == nil {
		return r.Load(ctx, existing)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	roots := make([]workspace.Identity, 0, len(scope.Repositories))
	for _, repository := range scope.Repositories {
		roots = append(roots, repository.Identity)
	}
	claims, err := r.Owner.Claims(ctx, r.Engine.ProjectID, roots)
	ownedForReview := false
	if err != nil {
		claims, err = r.Owner.Claim(ctx, r.Engine.ProjectID, effectID, roots)
		ownedForReview = err == nil
	}
	if err != nil {
		return result, err
	}
	releaseAllowed := true
	defer func() {
		if ownedForReview && releaseAllowed {
			for _, claim := range claims {
				_ = r.Owner.Release(context.Background(), claim, "contained_stopped")
			}
		}
	}()
	remainingMS, err := quality.RemainingBudgetMS(ctx, r.Engine, scope)
	if err != nil {
		return result, err
	}
	timeoutMS := scope.Config.AttemptLimitMS
	if remainingMS < timeoutMS {
		timeoutMS = remainingMS
	}
	reviewCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	var started int64
	var raw []byte
	var reviewErr error
	err = r.Owner.HoldClaims(ctx, r.Engine.ProjectID, roots, claims, func() error {
		if err := r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
			var state, projectState string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM quality_effects_v2 WHERE id=?", effectID).Scan(&state); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", r.Engine.ProjectID).Scan(&projectState); err != nil {
				return err
			}
			if state != "prepared" || projectState != "ready" {
				return errors.New("review dispatch became unavailable")
			}
			started = store.Now()
			if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='executing',started_at=? WHERE id=?", started, effectID); err != nil {
				return err
			}
			return quality.StartBudgetSegment(ctx, tx, scope, effectID, started)
		}); err != nil {
			return err
		}
		releaseAllowed = false
		if r.Hook != nil {
			if err := r.Hook("after_effect_start"); err != nil {
				return err
			}
		}
		raw, reviewErr = r.Reviewer.Review(reviewCtx, manifest)
		return nil
	})
	if err != nil {
		_ = quality.MarkEffectUncertain(context.Background(), r.Engine, effectID, err.Error())
		return result, err
	}
	ended := store.Now()
	status := "error"
	document := Document{}
	if reviewCtx.Err() != nil {
		status = "interrupted"
	} else if reviewErr == nil {
		document, err = validateDocument(raw, scope)
		if err != nil {
			status = "malformed"
		} else {
			status = document.Decision
		}
	}
	current, observeErr := quality.Observe(ctx, r.Engine, request.Target)
	readOnly := observeErr == nil && len(quality.StaleReasons(scope, current)) == 0
	if !readOnly {
		status = "write_denied"
	}
	result, persistErr := r.finish(ctx, effectID, request, scope, manifestDigest, raw, document, status, readOnly, started, ended)
	if persistErr != nil {
		_ = quality.MarkEffectUncertain(context.Background(), r.Engine, effectID, persistErr.Error())
		return result, persistErr
	}
	releaseAllowed = true
	if result.Status != "pass" {
		return result, fmt.Errorf("%w: %s", ErrInvalidReview, result.Status)
	}
	return result, nil
}

func (r *Runner) finish(ctx context.Context, effectID string, request Request, scope quality.Scope, manifestDigest string, raw []byte, document Document, status string, readOnly bool, started, ended int64) (Result, error) {
	var result Result
	repository, err := artifacts.New(r.Engine.DB)
	if err != nil {
		return result, err
	}
	artifact, err := repository.PutCore(ctx, store.Digest([]byte(effectID+"\x00result")), "review-result", "durable", bytes.NewReader(raw))
	if err != nil {
		return result, err
	}
	resultID := store.ID()
	threshold := blockingThreshold(scope.Config.BlockingSeverity)
	findings := make([]RecordedFinding, 0, len(document.Findings))
	for _, finding := range document.Findings {
		recorded := RecordedFinding{Finding: finding, Blocking: severityRank(finding.Severity) >= threshold}
		findings = append(findings, recorded)
	}
	err = r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		ended = store.Now()
		_, exhausted, err := quality.FinishBudgetSegment(ctx, tx, effectID, ended)
		if err != nil {
			return err
		}
		terminalStatus := status
		if exhausted && terminalStatus == "pass" {
			terminalStatus = "error"
		}
		blocking := false
		for _, finding := range findings {
			blocking = blocking || finding.Blocking
		}
		if terminalStatus == "pass" && blocking {
			terminalStatus = "request_changes"
		}
		result = Result{ID: resultID, EffectID: effectID, ScopeID: scope.ID, Status: terminalStatus, ReviewerSessionID: request.SessionID, ReviewerNativeIdentity: request.NativeIdentity, ReadOnlyVerified: readOnly, EvidenceManifestDigest: manifestDigest, ResultArtifactID: artifact.ID, ResultArtifactDigest: artifact.Digest, StartedAt: started, EndedAt: ended, Findings: findings}
		digestValue := result
		digestValue.ID, digestValue.ResultDigest = "", ""
		rawDigest, _ := json.Marshal(digestValue)
		rawDigest, _ = store.Canonical(rawDigest)
		result.ResultDigest = store.Digest(rawDigest)
		observation, _ := json.Marshal(result)
		if _, err := tx.ExecContext(ctx, `INSERT INTO review_results_v2(id,effect_id,scope_id,schema_version,status,reviewer_session_id,reviewer_native_identity,read_only_verified,evidence_manifest_digest,result_artifact_id,result_artifact_digest,result_digest,started_at,ended_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, result.ID, effectID, scope.ID, 1, result.Status, request.SessionID, request.NativeIdentity, boolInt(readOnly), manifestDigest, artifact.ID, artifact.Digest, result.ResultDigest, started, ended); err != nil {
			return err
		}
		for _, finding := range result.Findings {
			resolution := "open"
			if finding.Severity == "suggestion" {
				resolution = "accepted_suggestion"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO quality_findings_v2(review_id,finding_id,severity,blocking,repository_id,path,line,evidence,recommendation,resolution) VALUES(?,?,?,?,?,?,?,?,?,?)`, result.ID, finding.ID, finding.Severity, boolInt(finding.Blocking), finding.RepositoryID, nullable(finding.Path), nullableLine(finding.Line), finding.Evidence, finding.Recommendation, resolution); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='observed',observation_json=?,observed_at=? WHERE id=? AND state='executing'", string(observation), ended, effectID); err != nil {
			return err
		}
		if scope.Target.Kind == "task" {
			if result.Status == "request_changes" || result.Status == "write_denied" || result.Status == "malformed" || result.Status == "error" || result.Status == "interrupted" {
				_, err = tx.ExecContext(ctx, "UPDATE tasks SET state='needs_repair',block_reason=? WHERE id=? AND revision=? AND state='reviewing'", "review returned "+result.Status, scope.Target.TaskID, scope.TaskRevision)
			} else if result.Status == "pass" && (scope.HumanAcceptanceRequired || hasManual(scope.Criteria)) {
				_, err = tx.ExecContext(ctx, "UPDATE tasks SET state='awaiting_human',block_reason=NULL WHERE id=? AND revision=? AND state='reviewing'", scope.Target.TaskID, scope.TaskRevision)
			}
		}
		return err
	})
	return result, err
}

func hasManual(criteria []policy.Criterion) bool {
	for _, criterion := range criteria {
		if criterion.Manual {
			return true
		}
	}
	return false
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableLine(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func (r *Runner) Load(ctx context.Context, id string) (Result, error) {
	var result Result
	var readOnly int
	err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT id,effect_id,scope_id,status,reviewer_session_id,reviewer_native_identity,read_only_verified,evidence_manifest_digest,result_artifact_id,result_artifact_digest,result_digest,started_at,ended_at FROM review_results_v2 WHERE id=?`, id).Scan(&result.ID, &result.EffectID, &result.ScopeID, &result.Status, &result.ReviewerSessionID, &result.ReviewerNativeIdentity, &readOnly, &result.EvidenceManifestDigest, &result.ResultArtifactID, &result.ResultArtifactDigest, &result.ResultDigest, &result.StartedAt, &result.EndedAt)
	result.ReadOnlyVerified = readOnly == 1
	if err != nil {
		return result, err
	}
	rows, err := r.Engine.DB.SQL.QueryContext(ctx, `SELECT finding_id,severity,blocking,repository_id,coalesce(path,''),coalesce(line,0),evidence,recommendation FROM quality_findings_v2 WHERE review_id=? ORDER BY finding_id`, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var finding RecordedFinding
		var blocking int
		if err := rows.Scan(&finding.ID, &finding.Severity, &blocking, &finding.RepositoryID, &finding.Path, &finding.Line, &finding.Evidence, &finding.Recommendation); err != nil {
			return result, err
		}
		finding.Blocking = blocking == 1
		result.Findings = append(result.Findings, finding)
	}
	return result, rows.Err()
}
