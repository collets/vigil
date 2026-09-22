package quality

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"vigil/internal/artifacts"
	"vigil/internal/core"
	"vigil/internal/store"
)

type CheckGate struct {
	CheckID      string `json:"check_id"`
	ResultID     string `json:"result_id"`
	ResultDigest string `json:"result_digest"`
	Status       string `json:"status"`
	BaselineID   string `json:"baseline_exception_id,omitempty"`
	Satisfied    bool   `json:"satisfied"`
	Reason       string `json:"reason,omitempty"`
}

type CheckGates struct {
	ScopeID   string      `json:"scope_id"`
	Satisfied bool        `json:"satisfied"`
	Checks    []CheckGate `json:"checks"`
}

func stringSlice(raw string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	sort.Strings(values)
	return values, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// EvaluateChecks verifies both database bindings and artifact bytes. A missing
// or corrupt artifact can never be hidden by a pass row.
func EvaluateChecks(ctx context.Context, engine *core.Engine, scope Scope) (CheckGates, error) {
	result := CheckGates{ScopeID: scope.ID, Satisfied: true, Checks: []CheckGate{}}
	artifactsRepository, err := artifacts.New(engine.DB)
	if err != nil {
		return result, err
	}
	compatible, err := CompatibleScopeIDs(ctx, engine, scope)
	if err != nil {
		return result, err
	}
	for _, definition := range scope.RequiredChecks {
		definitionRaw, _ := json.Marshal(definition)
		definitionRaw, _ = store.Canonical(definitionRaw)
		definitionDigest := store.Digest(definitionRaw)
		gate := CheckGate{CheckID: definition.ID}
		var status, failuresRaw, artifactID, artifactDigest, observedDigest string
		var latest int64 = -1
		err := sql.ErrNoRows
		for _, scopeID := range compatible {
			var candidate CheckGate
			var candidateStatus, candidateFailures, candidateArtifact, candidateArtifactDigest, candidateObserved string
			var ended int64
			queryErr := engine.DB.SQL.QueryRowContext(ctx, `SELECT id,status,failure_identities_json,output_artifact_id,output_artifact_digest,observed_repository_set_digest,result_digest,ended_at FROM check_results_v2 WHERE scope_id=? AND check_id=? AND definition_digest=? ORDER BY ended_at DESC,id DESC LIMIT 1`, scopeID, definition.ID, definitionDigest).Scan(&candidate.ResultID, &candidateStatus, &candidateFailures, &candidateArtifact, &candidateArtifactDigest, &candidateObserved, &candidate.ResultDigest, &ended)
			if errors.Is(queryErr, sql.ErrNoRows) {
				continue
			}
			if queryErr != nil {
				return result, queryErr
			}
			if ended > latest || ended == latest && candidate.ResultID > gate.ResultID {
				latest = ended
				gate.ResultID, gate.ResultDigest = candidate.ResultID, candidate.ResultDigest
				status, failuresRaw, artifactID, artifactDigest, observedDigest = candidateStatus, candidateFailures, candidateArtifact, candidateArtifactDigest, candidateObserved
				err = nil
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			gate.Reason = "missing current check result"
			result.Satisfied = false
			result.Checks = append(result.Checks, gate)
			continue
		}
		if err != nil {
			return result, err
		}
		gate.Status = status
		if observedDigest != scope.RepositorySetDigest {
			gate.Reason = "check observed a different repository set"
			result.Satisfied = false
			result.Checks = append(result.Checks, gate)
			continue
		}
		if err := artifactsRepository.Verify(ctx, artifactID, artifactDigest, "check-output"); err != nil {
			gate.Reason = "check artifact missing or corrupt"
			result.Satisfied = false
			_ = RecordStaleness(ctx, engine, "check", gate.ResultID, scope, scope, []string{"artifact_missing_or_corrupt"})
			result.Checks = append(result.Checks, gate)
			continue
		}
		if status == "pass" {
			gate.Satisfied = true
			result.Checks = append(result.Checks, gate)
			continue
		}
		if status == "fail" {
			failures, parseErr := stringSlice(failuresRaw)
			if parseErr != nil {
				return result, parseErr
			}
			rows, queryErr := engine.DB.SQL.QueryContext(ctx, `SELECT id,failure_identities_json FROM baseline_exceptions_v2 WHERE check_id=? AND check_definition_digest=? AND base_repository_set_digest=? ORDER BY approved_at DESC,id`, definition.ID, definitionDigest, scope.RepositorySetDigest)
			if queryErr != nil {
				return result, queryErr
			}
			for rows.Next() {
				var id, authorizedRaw string
				if err := rows.Scan(&id, &authorizedRaw); err != nil {
					rows.Close()
					return result, err
				}
				authorized, err := stringSlice(authorizedRaw)
				if err != nil {
					rows.Close()
					return result, err
				}
				if sameStrings(failures, authorized) {
					gate.BaselineID, gate.Satisfied, gate.Status = id, true, "accepted_baseline"
					break
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return result, err
			}
			rows.Close()
		}
		if !gate.Satisfied {
			gate.Reason = "check result is blocking"
			result.Satisfied = false
		}
		result.Checks = append(result.Checks, gate)
	}
	return result, nil
}

type BaselineRequest struct {
	CommandID         string   `json:"command_id"`
	Target            Target   `json:"target"`
	CheckID           string   `json:"check_id"`
	FailureIdentities []string `json:"failure_identities"`
	Paths             []string `json:"paths"`
	Rationale         string   `json:"rationale"`
	Actor             string   `json:"actor"`
}

func fixtureScope(scope Scope) bool {
	for _, repository := range scope.Repositories {
		info, err := os.Lstat(filepath.Join(repository.Identity.Root, ".vigil-disposable-fixture"))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func sortedUnique(values []string, path bool) ([]string, error) {
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if value == "" || index > 0 && result[index-1] == value {
			return nil, errors.New("values must be nonempty and unique")
		}
		if path && (filepath.IsAbs(value) || filepath.Clean(value) != filepath.FromSlash(value) || value == "..") {
			return nil, errors.New("baseline paths must be canonical and relative")
		}
	}
	return result, nil
}

func AuthorizeBaseline(ctx context.Context, engine *core.Engine, request BaselineRequest) (string, error) {
	if engine == nil || engine.DB == nil || !store.SafeID(request.CommandID) || request.Rationale == "" || (request.Actor != "human" && request.Actor != "fixture_human") {
		return "", errors.New("explicit human baseline authority required")
	}
	scope, err := Observe(ctx, engine, request.Target)
	if err != nil {
		return "", err
	}
	if request.Actor == "fixture_human" && !fixtureScope(scope) {
		return "", errors.New("fixture human authority requires disposable repositories")
	}
	definition, definitionDigest, err := func() (any, string, error) {
		for _, value := range scope.RequiredChecks {
			if value.ID == request.CheckID {
				raw, _ := json.Marshal(value)
				raw, _ = store.Canonical(raw)
				return value, store.Digest(raw), nil
			}
		}
		return nil, "", errors.New("baseline check is not required")
	}()
	_ = definition
	if err != nil {
		return "", err
	}
	request.FailureIdentities, err = sortedUnique(request.FailureIdentities, false)
	if err != nil || len(request.FailureIdentities) == 0 {
		return "", errors.New("baseline requires exact failure identities")
	}
	request.Paths, err = sortedUnique(request.Paths, true)
	if err != nil {
		return "", err
	}
	if err := Persist(ctx, engine, scope); err != nil {
		return "", err
	}
	args, _ := json.Marshal(request)
	receipt, err := engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: request.Actor, Kind: "quality.baseline.authorize", Args: args}, func(tx *store.Tx) (any, error) {
		id := store.ID()
		failures, _ := json.Marshal(request.FailureIdentities)
		paths, _ := json.Marshal(request.Paths)
		_, err := tx.ExecContext(ctx, `INSERT INTO baseline_exceptions_v2(id,check_id,check_definition_digest,base_repository_set_digest,failure_identities_json,paths_json,rationale,actor,command_id,approved_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, request.CheckID, definitionDigest, scope.RepositorySetDigest, string(failures), string(paths), request.Rationale, request.Actor, request.CommandID, store.Now())
		return map[string]string{"id": id}, err
	})
	if err != nil {
		return "", err
	}
	var result map[string]string
	if err = json.Unmarshal(receipt, &result); err != nil {
		return "", err
	}
	// A narrowly authorized baseline resolves the check failure that moved the
	// task to needs_repair. It may restore the checking workflow only when no
	// already-observed current check remains blocking; missing checks must still
	// run and no project-required definition is removed.
	if scope.Target.Kind == "task" {
		gates, gateErr := EvaluateChecks(ctx, engine, scope)
		if gateErr != nil {
			return "", gateErr
		}
		blockingObserved := false
		for _, gate := range gates.Checks {
			if gate.ResultID != "" && !gate.Satisfied {
				blockingObserved = true
			}
		}
		if !blockingObserved {
			if err = engine.DB.Write(ctx, func(tx *store.Tx) error {
				_, updateErr := tx.ExecContext(ctx, "UPDATE tasks SET state='checking',block_reason=NULL WHERE id=? AND revision=? AND state='needs_repair'", scope.Target.TaskID, scope.TaskRevision)
				return updateErr
			}); err != nil {
				return "", err
			}
		}
	}
	return result["id"], nil
}
