package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"vigil/internal/policy"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type CommitRequest struct {
	CommandID    string   `json:"command_id"`
	PlanID       string   `json:"plan_id"`
	RepositoryID string   `json:"repository_id"`
	TaskID       string   `json:"task_id"`
	Paths        []string `json:"paths"`
	Message      string   `json:"message"`
	AuthorName   string   `json:"author_name"`
	AuthorEmail  string   `json:"author_email"`
}

type CommitIntent struct {
	PlanID             string   `json:"plan_id"`
	RepositoryID       string   `json:"repository_id"`
	RepositoryRevision int      `json:"repository_revision"`
	TaskID             string   `json:"task_id"`
	TaskRevision       int      `json:"task_revision"`
	AcceptanceID       string   `json:"acceptance_id"`
	ScopeID            string   `json:"scope_id"`
	ParentOID          string   `json:"parent_oid"`
	ExpectedRefOID     string   `json:"expected_ref_oid,omitempty"`
	TreeOID            string   `json:"tree_oid"`
	TargetRef          string   `json:"target_ref"`
	Paths              []string `json:"paths"`
	Message            string   `json:"message"`
	AuthorName         string   `json:"author_name"`
	AuthorEmail        string   `json:"author_email"`
	Timestamp          int64    `json:"timestamp"`
}

type PreparedCommit struct {
	OperationID string       `json:"operation_id"`
	RequestID   string       `json:"request_id"`
	Intent      CommitIntent `json:"intent"`
}

type CommitResult struct {
	OperationID string `json:"operation_id"`
	DeliveryID  string `json:"delivery_id"`
	CommitOID   string `json:"commit_oid"`
	TargetRef   string `json:"target_ref"`
	State       string `json:"state"`
}

func (e *Engine) deliveryEnvelope(ctx context.Context, commandID, kind string, payload any) (Envelope, error) {
	var revision int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", e.ProjectID).Scan(&revision); err != nil {
		return Envelope{}, err
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{CommandID: commandID, ExpectedRevision: revision, Kind: kind, Payload: b}, nil
}

func validCommitIdentity(name, email, message string) bool {
	if len(name) < 1 || len(name) > 128 || len(email) < 3 || len(email) > 254 || len(message) < 1 || len(message) > 4096 {
		return false
	}
	if strings.TrimSpace(name) != name || strings.TrimSpace(email) != email || strings.TrimSpace(message) == "" ||
		strings.ContainsAny(name+email+message, "\x00\r") || strings.ContainsAny(name+email, "\n<>\x00") || !strings.Contains(email, "@") {
		return false
	}
	return true
}

func scopeCoversPath(scopes []string, name string) bool {
	for _, scope := range scopes {
		if scope == name || scope == "**" || strings.HasSuffix(scope, "/**") && strings.HasPrefix(name, strings.TrimSuffix(scope, "/**")+"/") {
			return true
		}
		if ok, _ := path.Match(scope, name); ok {
			return true
		}
	}
	return false
}

func (e *Engine) acceptedCommitContext(ctx context.Context, intent CommitIntent) (RepositoryRecord, workspace.Baseline, error) {
	var state, rawDefinition, acceptanceID, scopeID, scopeManifest string
	var taskRevision int
	err := e.DB.SQL.QueryRowContext(ctx, `SELECT t.state,t.revision,tr.definition_json,a.id,a.scope_id,s.repository_manifest_json
		FROM tasks t JOIN task_revisions tr ON tr.task_id=t.id AND tr.revision=t.revision
		JOIN quality_acceptances_v2 a ON a.task_id=t.id AND a.invalidated_at IS NULL
		JOIN quality_scopes_v2 s ON s.id=a.scope_id WHERE t.id=? AND t.plan_id=? AND t.kind='implementation'`, intent.TaskID, intent.PlanID).
		Scan(&state, &taskRevision, &rawDefinition, &acceptanceID, &scopeID, &scopeManifest)
	if err != nil || state != "accepted" || taskRevision != intent.TaskRevision || acceptanceID != intent.AcceptanceID || scopeID != intent.ScopeID {
		return RepositoryRecord{}, workspace.Baseline{}, errors.New("current accepted task and exact scope are required")
	}
	var definition policy.Task
	if err := json.Unmarshal([]byte(rawDefinition), &definition); err != nil {
		return RepositoryRecord{}, workspace.Baseline{}, err
	}
	if len(intent.Paths) == 0 || len(intent.Paths) > 1000 {
		return RepositoryRecord{}, workspace.Baseline{}, errors.New("bounded commit path set required")
	}
	for _, name := range intent.Paths {
		if !exactCommitPath(name) || !scopeCoversPath(definition.Scope, name) {
			return RepositoryRecord{}, workspace.Baseline{}, fmt.Errorf("commit path %q is outside accepted task scope", name)
		}
	}
	record, err := e.Repository(ctx, intent.RepositoryID)
	if err != nil || record.PlanID != intent.PlanID || record.Revision != intent.RepositoryRevision {
		return RepositoryRecord{}, workspace.Baseline{}, errors.New("enrolled repository revision changed")
	}
	baseline, err := acceptedBaselineForRepository(ctx, scopeManifest, record)
	if err != nil {
		return RepositoryRecord{}, workspace.Baseline{}, err
	}
	for _, name := range intent.Paths {
		if !policy.Contains(baseline.DirtyPaths, name) {
			return RepositoryRecord{}, workspace.Baseline{}, fmt.Errorf("commit path %q is not in the accepted change set", name)
		}
		if policy.Contains(record.Baseline.DirtyPaths, name) && !policy.Contains(record.IncludedPaths, name) {
			return RepositoryRecord{}, workspace.Baseline{}, fmt.Errorf("pre-existing user path %q was not explicitly included", name)
		}
		for _, boundary := range record.NestedBoundaries {
			if name == boundary || strings.HasPrefix(name, boundary+"/") {
				return RepositoryRecord{}, workspace.Baseline{}, errors.New("nested repository content cannot enter parent commit")
			}
		}
	}
	return record, baseline, nil
}

func (e *Engine) commitParent(ctx context.Context, record RepositoryRecord) (string, string, string, error) {
	target := "refs/heads/" + record.PlanBranch
	checkedOut, _ := deliveryGit(ctx, record.Root, nil, nil, "symbolic-ref", "--quiet", "HEAD")
	if checkedOut == target {
		return "", "", "", errors.New("plan ref is checked out; commit would disturb the user's HEAD")
	}
	var last sql.NullString
	err := e.DB.SQL.QueryRowContext(ctx, `SELECT head_oid FROM deliveries WHERE plan_id=? AND repository_id=? AND kind='commit' AND state='succeeded' ORDER BY rowid DESC LIMIT 1`, record.PlanID, record.ID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", "", err
	}
	parent := record.BaseOID
	if last.Valid {
		parent = last.String
	}
	if !gitOID(parent) {
		return "", "", "", errors.New("invalid enrolled or prior commit parent")
	}
	current, err := deliveryGit(ctx, record.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", target+"^{commit}")
	if err != nil {
		if last.Valid {
			return "", "", "", errors.New("previously committed plan ref disappeared")
		}
		return parent, "", target, nil
	}
	if current != parent {
		return "", "", "", errors.New("plan ref moved outside the recorded commit chain")
	}
	return parent, current, target, nil
}

// PrepareCommit is a read-only exact-tree preview followed by a human approval
// request. Preview objects live only in a private temp object database.
func (e *Engine) PrepareCommit(ctx context.Context, request CommitRequest) (PreparedCommit, error) {
	var prepared PreparedCommit
	if !store.SafeID(request.CommandID) || !store.SafeID(request.PlanID) || !store.SafeID(request.RepositoryID) || !store.SafeID(request.TaskID) || !validCommitIdentity(request.AuthorName, request.AuthorEmail, request.Message) {
		return prepared, errors.New("exact commit identity, message and command required")
	}
	if len(request.Paths) == 0 || len(request.Paths) > 1000 {
		return prepared, errors.New("bounded exact commit paths required")
	}
	paths := append([]string(nil), request.Paths...)
	sort.Strings(paths)
	for i, name := range paths {
		if !exactCommitPath(name) || i > 0 && name == paths[i-1] {
			return prepared, errors.New("unique exact commit paths required")
		}
	}
	// Reconstruct the original exact intent before taking a fresh timestamp or
	// observing a ref that might have moved after a prior successful prepare.
	var priorRaw string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT result_json FROM command_receipts WHERE id=? AND actor='human'", request.CommandID).Scan(&priorRaw); err == nil {
		var prior struct {
			Result struct {
				OperationID string `json:"operation_id"`
				RequestID   string `json:"request_id"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(priorRaw), &prior); err != nil || prior.Result.OperationID == "" {
			return prepared, errors.New("command ID already belongs to another action")
		}
		intent, _, err := e.loadCommitIntent(ctx, prior.Result.OperationID)
		if err != nil || intent.PlanID != request.PlanID || intent.RepositoryID != request.RepositoryID || intent.TaskID != request.TaskID ||
			intent.Message != request.Message || intent.AuthorName != request.AuthorName || intent.AuthorEmail != request.AuthorEmail || !reflect.DeepEqual(intent.Paths, paths) {
			return prepared, errors.New("commit prepare command ID reused with different arguments")
		}
		return PreparedCommit{OperationID: prior.Result.OperationID, RequestID: prior.Result.RequestID, Intent: intent}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return prepared, err
	}
	var revision int
	var acceptanceID, scopeID string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT t.revision,a.id,a.scope_id FROM tasks t JOIN quality_acceptances_v2 a ON a.task_id=t.id AND a.invalidated_at IS NULL WHERE t.id=? AND t.plan_id=?`, request.TaskID, request.PlanID).Scan(&revision, &acceptanceID, &scopeID); err != nil {
		return prepared, errors.New("current task acceptance required")
	}
	record, err := e.Repository(ctx, request.RepositoryID)
	if err != nil {
		return prepared, err
	}
	parent, expected, target, err := e.commitParent(ctx, record)
	if err != nil {
		return prepared, err
	}
	intent := CommitIntent{PlanID: request.PlanID, RepositoryID: request.RepositoryID, RepositoryRevision: record.Revision,
		TaskID: request.TaskID, TaskRevision: revision, AcceptanceID: acceptanceID, ScopeID: scopeID,
		ParentOID: parent, ExpectedRefOID: expected, TargetRef: target, Paths: paths,
		Message: request.Message, AuthorName: request.AuthorName, AuthorEmail: request.AuthorEmail, Timestamp: store.Now() / 1000}
	if _, _, err := e.acceptedCommitContext(ctx, intent); err != nil {
		return prepared, err
	}
	intent.TreeOID, err = buildCommitTree(ctx, record, parent, paths, false)
	if err != nil {
		return prepared, err
	}
	if _, _, err := e.acceptedCommitContext(ctx, intent); err != nil {
		return prepared, err
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		return prepared, err
	}
	resource, _ := json.Marshal(map[string]any{"repository_id": record.ID, "revision": record.Revision, "identity": record.Identity, "target_ref": target})
	op := OperationRequest{Category: "commit", ResourceDigest: store.Digest(resource), ArgumentsDigest: store.Digest(canonical),
		PlanID: request.PlanID, TaskID: request.TaskID, TaskRevision: revision, IntentJSON: string(canonical)}
	envelope, err := e.deliveryEnvelope(ctx, request.CommandID, "operation.request", op)
	if err != nil {
		return prepared, err
	}
	result, err := e.Apply(ctx, Human, envelope)
	if err != nil {
		return prepared, err
	}
	var parsed struct {
		Result struct {
			OperationID string `json:"operation_id"`
			RequestID   string `json:"request_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return prepared, err
	}
	return PreparedCommit{OperationID: parsed.Result.OperationID, RequestID: parsed.Result.RequestID, Intent: intent}, nil
}

func (e *Engine) loadCommitIntent(ctx context.Context, operationID string) (CommitIntent, string, error) {
	var raw, state, kind string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT evidence_json,state,kind FROM operations WHERE id=?", operationID).Scan(&raw, &state, &kind); err != nil {
		return CommitIntent{}, "", err
	}
	if kind != "commit" {
		return CommitIntent{}, "", errors.New("operation is not a commit intent")
	}
	var op OperationRequest
	if err := json.Unmarshal([]byte(raw), &op); err != nil || op.IntentJSON == "" || store.Digest([]byte(op.IntentJSON)) != op.ArgumentsDigest {
		return CommitIntent{}, "", errors.New("commit intent is absent or corrupt")
	}
	var intent CommitIntent
	if err := json.Unmarshal([]byte(op.IntentJSON), &intent); err != nil {
		return CommitIntent{}, "", err
	}
	if intent.PlanID != op.PlanID || intent.TaskID != op.TaskID || intent.TaskRevision != op.TaskRevision {
		return CommitIntent{}, "", errors.New("commit operation context differs from approved intent")
	}
	return intent, state, nil
}

func commitDeliveryID(operationID string) string {
	return store.Digest([]byte("delivery.commit\x00" + operationID))
}

// ExecuteCommit can resume after object creation or a successful ref update.
// Only a prepared operation needs grant consumption; executing is an already
// consumed operation and never starts a new effect identity.
func (e *Engine) ExecuteCommit(ctx context.Context, operationID, grantID string) (CommitResult, error) {
	result := CommitResult{OperationID: operationID, DeliveryID: commitDeliveryID(operationID)}
	if !store.SafeID(operationID) {
		return result, errors.New("operation ID required")
	}
	intent, state, err := e.loadCommitIntent(ctx, operationID)
	if err != nil {
		return result, err
	}
	result.TargetRef = intent.TargetRef
	if state == "observed" || state == "reconciled" {
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT head_oid,state FROM deliveries WHERE id=?", result.DeliveryID).Scan(&result.CommitOID, &result.State); err != nil || result.State != "succeeded" {
			return CommitResult{}, errors.New("observed commit lacks succeeded delivery")
		}
		return result, nil
	}
	if state != "prepared" && state != "executing" && state != "uncertain" {
		return result, errors.New("commit operation is not executable")
	}
	record, _, err := e.acceptedCommitContext(ctx, intent)
	if err != nil {
		return result, err
	}
	parent, expected, target, err := e.commitParent(ctx, record)
	if err != nil || parent != intent.ParentOID || expected != intent.ExpectedRefOID || target != intent.TargetRef {
		// A moved target might already be our exact commit; reconciliation below
		// handles that only after deterministic reconstruction.
		if state == "prepared" {
			return result, errors.New("approved commit parent/ref changed")
		}
	}
	if state == "prepared" {
		if !store.SafeID(grantID) {
			return result, errors.New("exact grant ID required before commit effect")
		}
		envelope, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.start\x00"+operationID)), "operation.start", map[string]string{"operation_id": operationID, "grant_id": grantID})
		if err != nil {
			return result, err
		}
		if _, err := e.Apply(ctx, Core, envelope); err != nil {
			return result, err
		}
	}
	if _, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.pending\x00" + operationID)), Actor: "core", Kind: "delivery.commit.pending", Args: json.RawMessage(`{"operation_id":"` + operationID + `"}`)}, func(tx *store.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, "INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,head_oid) VALUES(?,?,?,?,'commit','pending',?)", result.DeliveryID, intent.PlanID, intent.RepositoryID, operationID, intent.ParentOID)
		return map[string]string{"delivery_id": result.DeliveryID}, err
	}); err != nil {
		return result, err
	}
	tree, err := buildCommitTree(ctx, record, intent.ParentOID, intent.Paths, true)
	if err != nil || tree != intent.TreeOID {
		return result, errors.New("commit tree differs from exact approved tree")
	}
	if _, _, err := e.acceptedCommitContext(ctx, intent); err != nil {
		return result, errors.New("accepted repository changed during exact commit construction")
	}
	date := fmt.Sprintf("%d +0000", intent.Timestamp)
	env := []string{"GIT_AUTHOR_NAME=" + intent.AuthorName, "GIT_AUTHOR_EMAIL=" + intent.AuthorEmail,
		"GIT_COMMITTER_NAME=" + intent.AuthorName, "GIT_COMMITTER_EMAIL=" + intent.AuthorEmail,
		"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	commitOID, err := deliveryGit(ctx, record.Root, env, nil, "commit-tree", tree, "-p", intent.ParentOID, "-m", intent.Message)
	if err != nil || !gitOID(commitOID) {
		return result, errors.New("controlled commit object creation failed")
	}
	result.CommitOID = commitOID
	objectArgs, _ := json.Marshal(map[string]string{"operation_id": operationID, "commit_oid": commitOID})
	if _, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.object\x00" + operationID)), Actor: "core", Kind: "delivery.commit.object", Args: objectArgs}, func(tx *store.Tx) (any, error) {
		updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET head_oid=? WHERE id=? AND state='pending' AND head_oid IN (?,?)", commitOID, result.DeliveryID, intent.ParentOID, commitOID)
		if err != nil {
			return nil, err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return nil, errors.New("commit object journal changed before observation")
		}
		return map[string]string{"commit_oid": commitOID}, err
	}); err != nil {
		return result, err
	}
	current, refErr := deliveryGit(ctx, record.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", intent.TargetRef+"^{commit}")
	if refErr != nil {
		current = ""
	}
	if current != commitOID {
		if current != intent.ExpectedRefOID {
			return result, errors.New("plan ref differs from approved predecessor; commit requires reconciliation")
		}
		old := current
		if old == "" {
			old = strings.Repeat("0", len(commitOID))
		}
		if _, err := deliveryGit(ctx, record.Root, nil, nil, "update-ref", "-m", "vigil exact task commit", intent.TargetRef, commitOID, old); err != nil {
			_ = e.markCommitUncertain(ctx, operationID)
			return result, fmt.Errorf("commit ref update uncertain; inspect exact ref: %w", err)
		}
	}
	_, err = e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.observed\x00" + operationID)), Actor: "core", Kind: "delivery.commit.observed", Args: objectArgs}, func(tx *store.Tx) (any, error) {
		updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='succeeded',head_oid=? WHERE id=? AND state='pending'", commitOID, result.DeliveryID)
		if err != nil {
			return nil, err
		}
		n, _ := updated.RowsAffected()
		if n != 1 {
			return nil, errors.New("commit delivery state changed before observation")
		}
		updated, err = tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=? AND state IN ('executing','uncertain')", operationID)
		if err != nil {
			return nil, err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return nil, errors.New("commit operation changed before observation")
		}
		return result, err
	})
	if err != nil {
		return result, err
	}
	result.State = "succeeded"
	return result, nil
}

func (e *Engine) markCommitUncertain(ctx context.Context, operationID string) error {
	args, _ := json.Marshal(map[string]string{"operation_id": operationID})
	_, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.uncertain\x00" + operationID)), Actor: "core", Kind: "delivery.commit.uncertain", Args: args}, func(tx *store.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=? AND state='executing'", operationID)
		return map[string]string{"state": "uncertain"}, err
	})
	return err
}
