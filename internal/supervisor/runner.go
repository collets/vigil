package supervisor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/boundary"
	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type Runner struct {
	Engine             *core.Engine
	Driver             Driver
	Checkout           *boundary.Checkout
	StartCommandID     string
	ReconcileCommandID string
	Fault              func(string) error
}

var productionRecoveryClasses = []string{
	"intent_commit", "workspace_reservation", "endpoint_slot", "runtime_create", "runtime_start",
	"runtime_attach", "native_create", "submit_write", "submit_ack", "terminal_observe",
	"artifact_publish", "outcome_commit", "persistence_failure",
}

type checkpointFailure struct{ error }

func (e checkpointFailure) Unwrap() error { return e.error }

func (r *Runner) checkpoint(name string) error {
	if r.Fault != nil {
		if err := r.Fault(name); err != nil {
			return checkpointFailure{err}
		}
	}
	return nil
}

func isCheckpointFailure(err error) bool {
	var failure checkpointFailure
	return errors.As(err, &failure)
}

func (r *Runner) effect(ctx context.Context, prepared PreparedRun, ordinal int, kind string, intent any) (string, error) {
	id := store.Digest([]byte(prepared.GenerationID + "\x00" + kind))
	raw, err := json.Marshal(intent)
	if err != nil {
		return "", err
	}
	err = r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO execution_effects(id,run_id,generation_id,ordinal,kind,state,stable_identity,intent_json,prepared_at) VALUES(?,?,?,?,?,'prepared',?,?,?) ON CONFLICT(id) DO NOTHING`, id, prepared.RunID, prepared.GenerationID, ordinal, kind, prepared.RuntimeResourceID, string(raw), store.Now())
		if err != nil {
			return err
		}
		var existingKind, identity, existingIntent string
		if err := tx.QueryRowContext(ctx, "SELECT kind,stable_identity,intent_json FROM execution_effects WHERE id=?", id).Scan(&existingKind, &identity, &existingIntent); err != nil {
			return err
		}
		if existingKind != kind || identity != prepared.RuntimeResourceID || store.Digest([]byte(existingIntent)) != store.Digest(raw) {
			return store.ErrConflict
		}
		return nil
	})
	return id, err
}

func (r *Runner) effectState(ctx context.Context, id string) (string, error) {
	var state string
	err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM execution_effects WHERE id=?", id).Scan(&state)
	return state, err
}

func (r *Runner) observeEffect(ctx context.Context, id, state string, observation any) error {
	if state != "observed" && state != "reconciled" && state != "uncertain" && state != "cancelled" {
		return errors.New("invalid effect observation state")
	}
	raw, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var prior string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM execution_effects WHERE id=?", id).Scan(&prior); err != nil {
			return err
		}
		if prior == "observed" || prior == "reconciled" {
			return nil
		}
		if prior != "prepared" && prior != "executing" && prior != "uncertain" {
			return errors.New("effect is not observable")
		}
		_, err := tx.ExecContext(ctx, "UPDATE execution_effects SET state=?,observation_json=?,observed_at=? WHERE id=?", state, string(raw), store.Now(), id)
		return err
	})
}

func phaseObserved(kind string, observation Observation) bool {
	switch kind {
	case "runtime_create":
		return observation.Exists
	case "runtime_start":
		return observation.Started
	case "runtime_attach":
		return observation.Attached
	case "native_create":
		return observation.NativeSessionID != ""
	case "prompt_write", "prompt_ack":
		return observation.SubmissionState == "delivered"
	case "terminal_observe":
		return observation.Terminal
	}
	return false
}

func (r *Runner) ensureDriverEffect(ctx context.Context, prepared PreparedRun, ordinal int, kind string, call func() error) error {
	id, err := r.effect(ctx, prepared, ordinal, kind, map[string]any{"run_id": prepared.RunID, "generation_id": prepared.GenerationID, "kind": kind})
	if err != nil {
		return err
	}
	state, err := r.effectState(ctx, id)
	if err != nil {
		return err
	}
	if state == "observed" || state == "reconciled" {
		return nil
	}
	observation, inspectErr := r.Driver.Inspect(ctx, prepared)
	if inspectErr == nil && phaseObserved(kind, observation) {
		return r.observeEffect(ctx, id, "reconciled", observation)
	}
	if state == "uncertain" {
		return fmt.Errorf("%s remains uncertain; reconcile before any retry", kind)
	}
	if err := r.checkpointSegment(ctx, prepared); err != nil {
		return err
	}
	if err := r.checkpoint("before_" + kind); err != nil {
		return err
	}
	if err := call(); err != nil {
		observed, observedErr := r.Driver.Inspect(ctx, prepared)
		_ = r.observeEffect(context.Background(), id, "uncertain", map[string]any{"driver_error": err.Error(), "inspection_available": observedErr == nil, "observation": observed})
		return err
	}
	if err := r.checkpoint("after_" + kind); err != nil {
		return err
	}
	observation, err = r.Driver.Inspect(ctx, prepared)
	if err != nil {
		return err
	}
	if !phaseObserved(kind, observation) {
		return fmt.Errorf("%s completed without required observation", kind)
	}
	return r.observeEffect(ctx, id, "observed", observation)
}

func (r *Runner) validateStart(ctx context.Context, prepared PreparedRun, reservation core.Reservation) error {
	if r.Engine == nil || r.Engine.DB == nil || r.Driver == nil {
		return errors.New("runner is incomplete")
	}
	if reservation.RunID != prepared.RunID || reservation.Phase != "reserved" || len(reservation.Claims) != len(prepared.Repositories) {
		return errors.New("all participating repository and endpoint reservations are required")
	}
	var runState string
	var projectRevision int
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM runs WHERE id=?", prepared.RunID).Scan(&runState); err != nil {
		return err
	}
	if runState != "prepared" && runState != "starting" && runState != "active" {
		return errors.New("run is not startable")
	}
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", r.Engine.ProjectID).Scan(&projectRevision); err != nil {
		return err
	}
	if projectRevision != prepared.ExpectedRevision {
		return errors.New("project revision changed after run preparation")
	}
	for _, repository := range prepared.Repositories {
		if err := repository.Identity.Validate(); err != nil {
			return err
		}
		current, err := workspace.Fingerprint(ctx, repository.Root, withoutGit(repository.Baseline.Exclusions))
		if err != nil {
			return err
		}
		if current.Dirty || current.HeadOID != repository.BaseOID || current.HeadRef != "refs/heads/"+repository.PlanBranch || current.IndexDigest != repository.Baseline.IndexDigest || current.ContentDigest != repository.Baseline.ContentDigest {
			return fmt.Errorf("repository %s baseline no longer matches", repository.ID)
		}
	}
	var charged, unknown, limit int64
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&charged, &unknown, &limit); err != nil {
		return err
	}
	if charged+unknown >= limit || charged+unknown >= prepared.ActiveLimitMS {
		return errors.New("execution budget exhausted before dispatch")
	}
	if prepared.RuntimeKind != "synthetic" {
		if prepared.Eligibility == nil {
			return errors.New("missing exact production qualification")
		}
		for _, claim := range []string{boundary.ClaimBoundaryExecution, boundary.ClaimProviderIdle, boundary.ClaimProductionLaunch} {
			if !stringSetContains(prepared.Eligibility.RequiredClaims, claim) {
				return fmt.Errorf("production qualification request omits required claim %s", claim)
			}
		}
		for _, recovery := range productionRecoveryClasses {
			if !stringSetContains(prepared.Eligibility.RequiredRecoveryClasses, recovery) {
				return fmt.Errorf("production qualification request omits recovery class %s", recovery)
			}
		}
		eligibility, err := boundary.QueryEligibility(ctx, r.Engine.DB, *prepared.Eligibility)
		if err != nil {
			return err
		}
		if eligibility.Status != "supported" {
			return fmt.Errorf("production dispatch disabled: %v", eligibility.Reasons)
		}
		if r.Checkout == nil {
			return errors.New("qualified checkout plan required")
		}
		if err := r.Checkout.Validate(ctx); err != nil {
			return err
		}
	}
	return nil
}

func stringSetContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (r *Runner) Run(ctx context.Context, prepared PreparedRun, reservation core.Reservation, prompt string) (Result, error) {
	var result Result
	if prompt == "" || len(prompt) > 65536 {
		return result, errors.New("bounded nonempty prompt required")
	}
	commandID := r.StartCommandID
	if commandID == "" {
		commandID = "execution-start:" + prepared.RunID
	}
	if err := r.recordControlCommand(ctx, commandID, "execution.start", prepared, map[string]string{"prompt_digest": store.Digest([]byte(prompt)), "reservation_operation_id": reservation.OperationID}); err != nil {
		return result, err
	}
	if err := r.validateStart(ctx, prepared, reservation); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(prepared.WallLimitMS)*time.Millisecond)
	defer cancel()
	if err := r.startSegment(ctx, prepared); err != nil {
		return result, err
	}
	skipClose := false
	defer func() {
		if !skipClose {
			_ = r.closeSegment(context.Background(), prepared)
		}
	}()
	contain := func(cause error) (Result, error) {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		observation, stopErr := r.Driver.Stop(stopCtx, prepared)
		_ = r.recordContainment(context.Background(), prepared, observation, stopErr)
		if stopErr != nil {
			return result, fmt.Errorf("%w; containment failed: %v", cause, stopErr)
		}
		return result, cause
	}
	fail := func(cause error) (Result, error) {
		if isCheckpointFailure(cause) {
			skipClose = true
			return result, cause
		}
		return contain(cause)
	}
	if err := r.ensureDriverEffect(ctx, prepared, 1, "runtime_create", func() error { return r.Driver.Create(ctx, prepared) }); err != nil {
		return fail(err)
	}
	if prepared.RuntimeKind != "synthetic" {
		observation, err := r.Driver.Inspect(ctx, prepared)
		if err != nil {
			return contain(err)
		}
		if err := r.Checkout.ValidateMountsForRole(ctx, "implementation", observation.Mounts); err != nil {
			return contain(err)
		}
	}
	if err := r.ensureDriverEffect(ctx, prepared, 2, "runtime_start", func() error { return r.Driver.Start(ctx, prepared) }); err != nil {
		return fail(err)
	}
	if err := r.ensureDriverEffect(ctx, prepared, 3, "runtime_attach", func() error { return r.Driver.Attach(ctx, prepared) }); err != nil {
		return fail(err)
	}
	if err := r.ensureDriverEffect(ctx, prepared, 4, "native_create", func() error {
		id, err := r.Driver.CreateNative(ctx, prepared)
		if err == nil {
			err = r.persistNativeSession(ctx, prepared, id)
		}
		return err
	}); err != nil {
		return fail(err)
	}
	if err := r.submit(ctx, prepared, prompt); err != nil {
		return fail(err)
	}
	if err := r.Driver.RenewLease(ctx, prepared); err != nil {
		return contain(err)
	}
	terminalID, err := r.effect(ctx, prepared, 7, "terminal_observe", map[string]string{"generation_id": prepared.GenerationID})
	if err != nil {
		return contain(err)
	}
	observation, err := r.awaitWithLease(ctx, prepared)
	if err != nil {
		return contain(err)
	}
	if err := r.checkpoint("after_terminal_observe"); err != nil {
		skipClose = true
		return result, err
	}
	if !observation.Terminal {
		return contain(errors.New("native execution ended without terminal observation"))
	}
	if err := r.observeEffect(ctx, terminalID, "observed", observation); err != nil {
		return contain(err)
	}
	if err := r.ingest(ctx, prepared, observation); err != nil {
		return contain(err)
	}
	if err := r.checkpoint("before_result_persist"); err != nil {
		skipClose = true
		return result, err
	}
	result, err = r.persistResult(ctx, prepared, observation)
	if err != nil {
		return fail(err)
	}
	return result, nil
}

func (r *Runner) recordControlCommand(ctx context.Context, commandID, kind string, prepared PreparedRun, detail map[string]string) error {
	if !store.SafeID(commandID) {
		return errors.New("valid control command ID required")
	}
	args, _ := json.Marshal(map[string]any{"run_id": prepared.RunID, "generation_id": prepared.GenerationID, "detail": detail})
	_, err := r.Engine.DB.Command(ctx, store.Command{ID: commandID, Actor: string(core.Core), Kind: kind, Args: args}, func(tx *store.Tx) (any, error) {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM run_generations WHERE id=? AND run_id=?", prepared.GenerationID, prepared.RunID).Scan(&count); err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, errors.New("run generation changed")
		}
		return map[string]string{"run_id": prepared.RunID, "generation_id": prepared.GenerationID, "state": "intent_recorded"}, nil
	})
	return err
}

func (r *Runner) persistNativeSession(ctx context.Context, prepared PreparedRun, nativeID string) error {
	if nativeID == "" || len(nativeID) > 256 {
		return errors.New("native session identity missing")
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE run_generations SET native_session_id=?,state='created' WHERE id=?", nativeID, prepared.GenerationID); err != nil {
			return err
		}
		caps := `{"persisted":true}`
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,run_id,generation,harness,durable_id,runtime_id,native_home_ref,workspace_identity,profile_digest,capabilities_json) VALUES(?,?,?,?,?,?,?, ?,?,?) ON CONFLICT(id) DO NOTHING`, store.Digest([]byte(prepared.GenerationID+"\x00session")), prepared.RunID, prepared.TransportGeneration, prepared.ProfileID, nativeID, nativeID, "private:"+prepared.GenerationID, prepared.Repositories[0].Identity.Key, store.Digest([]byte(prepared.ProfileID)), caps)
		return err
	})
}

func (r *Runner) submit(ctx context.Context, prepared PreparedRun, prompt string) error {
	writeID, err := r.effect(ctx, prepared, 5, "prompt_write", map[string]string{"prompt_digest": store.Digest([]byte(prompt))})
	if err != nil {
		return err
	}
	ackID, err := r.effect(ctx, prepared, 6, "prompt_ack", map[string]string{"prompt_write_id": writeID})
	if err != nil {
		return err
	}
	observation, inspectErr := r.Driver.Inspect(ctx, prepared)
	if inspectErr == nil && observation.SubmissionState == "delivered" {
		if err := r.observeEffect(ctx, writeID, "reconciled", observation); err != nil {
			return err
		}
		if err := r.observeEffect(ctx, ackID, "reconciled", observation); err != nil {
			return err
		}
		return r.setSubmission(ctx, prepared, "delivered", observation.NativeTurnID)
	}
	var generationState string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT submission_state FROM run_generations WHERE id=?", prepared.GenerationID).Scan(&generationState); err != nil {
		return err
	}
	if generationState == "uncertain" {
		return errors.New("native submission is uncertain and cannot be replayed")
	}
	if generationState == "writing" && (inspectErr != nil || observation.SubmissionState != "not_attempted") {
		return errors.New("native submission delivery is unresolved and cannot be replayed")
	}
	if err := r.checkpoint("before_prompt_write"); err != nil {
		return err
	}
	if err := r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE runs SET state='starting',started_at=coalesce(started_at,?) WHERE id=?", store.Now(), prepared.RunID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE run_generations SET state='starting',submission_state='writing' WHERE id=?", prepared.GenerationID)
		return err
	}); err != nil {
		return err
	}
	state, nativeTurn, submitErr := r.Driver.Submit(ctx, prepared, prompt)
	if submitErr != nil || state != "delivered" {
		if state != "proven_not_delivered" {
			state = "uncertain"
		}
		_ = r.setSubmission(context.Background(), prepared, state, nativeTurn)
		_ = r.observeEffect(context.Background(), writeID, "uncertain", map[string]any{"submission_state": state, "error": errorString(submitErr)})
		return fmt.Errorf("native submission %s; automatic replay disabled: %w", state, submitErr)
	}
	if err := r.checkpoint("after_prompt_write"); err != nil {
		return err
	}
	observation, err = r.Driver.Inspect(ctx, prepared)
	if err != nil {
		return err
	}
	if err := r.observeEffect(ctx, writeID, "observed", observation); err != nil {
		return err
	}
	if err := r.checkpoint("before_prompt_ack"); err != nil {
		return err
	}
	if err := r.observeEffect(ctx, ackID, "observed", observation); err != nil {
		return err
	}
	if err := r.checkpoint("after_prompt_ack"); err != nil {
		return err
	}
	return r.setSubmission(ctx, prepared, "delivered", nativeTurn)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (r *Runner) setSubmission(ctx context.Context, prepared PreparedRun, state, nativeTurn string) error {
	if state != "delivered" && state != "uncertain" && state != "proven_not_delivered" {
		return errors.New("invalid submission observation")
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		runState := "active"
		if state == "uncertain" {
			runState = "unknown"
		}
		if state == "proven_not_delivered" {
			runState = "failed"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE run_generations SET submission_state=?,native_turn_id=?,state=CASE WHEN state='terminal' THEN state ELSE ? END WHERE id=?", state, nullableString(nativeTurn), map[bool]string{true: "active", false: "unknown"}[state == "delivered"], prepared.GenerationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE runs SET state=CASE WHEN state='completed' THEN state ELSE ? END WHERE id=?", runState, prepared.RunID)
		return err
	})
}

func (r *Runner) awaitWithLease(ctx context.Context, prepared PreparedRun) (Observation, error) {
	type response struct {
		observation Observation
		err         error
	}
	done := make(chan response, 1)
	awaitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { observation, err := r.Driver.Await(awaitCtx, prepared); done <- response{observation, err} }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case response := <-done:
			return response.observation, response.err
		case <-ticker.C:
			if err := r.checkpointSegment(ctx, prepared); err != nil {
				cancel()
				return Observation{}, err
			}
			if err := r.Driver.RenewLease(ctx, prepared); err != nil {
				cancel()
				return Observation{}, err
			}
			if err := r.checkpoint("lease_checkpoint"); err != nil {
				cancel()
				return Observation{}, err
			}
		case <-ctx.Done():
			cancel()
			return Observation{}, ctx.Err()
		}
	}
}

func (r *Runner) ingest(ctx context.Context, prepared PreparedRun, observation Observation) error {
	if len(observation.Events) > 1000 {
		return errors.New("normalized event batch exceeds limit")
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		for _, event := range observation.Events {
			if event.Sequence < 1 || event.Kind == "" || len(event.Kind) > 128 || len(event.Payload) > store.MaxDocument {
				return errors.New("invalid normalized event")
			}
			canonical, err := store.Canonical(event.Payload)
			if err != nil {
				return err
			}
			var prior string
			err = tx.QueryRowContext(ctx, "SELECT payload_json FROM normalized_run_events WHERE generation_id=? AND source_sequence=?", prepared.GenerationID, event.Sequence).Scan(&prior)
			if err == nil {
				if store.Digest([]byte(prior)) != store.Digest(canonical) {
					return errors.New("conflicting duplicate normalized event")
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO normalized_run_events(run_id,generation_id,source_sequence,native_turn_id,kind,occurred_at,payload_json) VALUES(?,?,?,?,?,?,?)`, prepared.RunID, prepared.GenerationID, event.Sequence, nullableString(observation.NativeTurnID), event.Kind, event.At, string(canonical)); err != nil {
				return err
			}
		}
		u := observation.Usage
		if u.Provenance != "" {
			if u.Provenance != "observed" && u.Provenance != "estimated" {
				return errors.New("invalid usage provenance")
			}
			if u.CostMicros != nil && u.Currency == "" {
				return errors.New("usage cost requires currency")
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO usage_observations(id,run_id,observed_at,scope,provenance,input_tokens,output_tokens,cached_tokens,cost_microunits,currency) VALUES(?,?,?,'turn',?,?,?,?,?,?,?)`, store.ID(), prepared.RunID, store.Now(), u.Provenance, u.InputTokens, u.OutputTokens, u.CachedTokens, u.CostMicros, nullableString(u.Currency))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func pathAllowed(scopes []string, path string) bool {
	for _, scope := range scopes {
		if strings.HasSuffix(scope, "/**") && (path == strings.TrimSuffix(scope, "/**") || strings.HasPrefix(path, strings.TrimSuffix(scope, "**"))) {
			return true
		}
		if matched, _ := filepath.Match(scope, path); matched {
			return true
		}
	}
	return false
}

func validateResult(raw json.RawMessage, prepared PreparedRun, actual []string) (Result, error) {
	var result Result
	if err := store.Decode(raw, &result); err != nil {
		return result, err
	}
	if result.SchemaVersion != 1 || result.Status != "completed" || strings.TrimSpace(result.Summary) == "" || len(result.Summary) > 4096 || len(result.ChangedPaths) == 0 || len(result.ChangedPaths) > 1000 {
		return result, errors.New("invalid completed execution result")
	}
	seen := map[string]bool{}
	for _, changed := range result.ChangedPaths {
		parts := strings.SplitN(changed, ":", 2)
		if len(parts) != 2 || !store.SafeID(parts[0]) || parts[1] == "" || filepath.IsAbs(parts[1]) || strings.Contains(parts[1], "\\") || strings.Contains(parts[1], "..") || seen[changed] || !pathAllowed(prepared.Task.Scope, parts[1]) {
			return result, fmt.Errorf("changed path is duplicate, invalid or outside scope: %s", changed)
		}
		seen[changed] = true
	}
	sort.Strings(result.ChangedPaths)
	sort.Strings(actual)
	if strings.Join(result.ChangedPaths, "\x00") != strings.Join(actual, "\x00") {
		return result, errors.New("reported changed paths do not match the checkout")
	}
	return result, nil
}

func (r *Runner) persistResult(ctx context.Context, prepared PreparedRun, observation Observation) (Result, error) {
	var result Result
	if observation.SubmissionState != "delivered" || observation.Outcome != "completed" || observation.WriterState != "contained_stopped" {
		return result, errors.New("completion lacks delivered submission and contained writer proof")
	}
	actual := []string{}
	fingerprints := map[string]workspace.Baseline{}
	for _, repository := range prepared.Repositories {
		fingerprint, err := workspace.Fingerprint(ctx, repository.Root, withoutGit(repository.Baseline.Exclusions))
		if err != nil {
			return result, err
		}
		fingerprints[repository.ID] = fingerprint
		for _, path := range fingerprint.DirtyPaths {
			actual = append(actual, repository.ID+":"+path)
		}
	}
	var err error
	result, err = validateResult(observation.Result, prepared, actual)
	if err != nil {
		return result, err
	}
	artifactEffect, err := r.effect(ctx, prepared, 8, "artifact_publish", map[string]string{"result_digest": store.Digest(observation.Result)})
	if err != nil {
		return result, err
	}
	repository, err := artifacts.New(r.Engine.DB)
	if err != nil {
		return result, err
	}
	artifact, err := repository.PutCore(ctx, "run-result:"+prepared.RunID, "execution-result", "unfinished", bytes.NewReader(observation.Result))
	if err != nil {
		return result, err
	}
	if err := r.checkpoint("after_artifact_publish"); err != nil {
		return result, err
	}
	if err := r.observeEffect(ctx, artifactEffect, "observed", artifact); err != nil {
		return result, err
	}
	outcomeEffect, err := r.effect(ctx, prepared, 9, "outcome_commit", map[string]string{"artifact_id": artifact.ID})
	if err != nil {
		return result, err
	}
	if err := r.checkpoint("before_outcome_commit"); err != nil {
		return result, err
	}
	fingerprintsJSON, _ := json.Marshal(fingerprints)
	changedJSON, _ := json.Marshal(result.ChangedPaths)
	canonical, _ := json.Marshal(result)
	err = r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO execution_results(run_id,generation_id,schema_version,status,summary,changed_paths_json,repository_fingerprints_json,artifact_id,result_digest,validated_at) VALUES(?,?,1,?,?,?,?,?,?,?)`, prepared.RunID, prepared.GenerationID, result.Status, result.Summary, string(changedJSON), string(fingerprintsJSON), artifact.ID, store.Digest(canonical), store.Now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO run_artifacts(run_id,artifact_id,purpose) VALUES(?,?,'execution_result')", prepared.RunID, artifact.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE runs SET state='completed',writer_state='contained_stopped',ended_at=? WHERE id=?", store.Now(), prepared.RunID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE run_generations SET state='terminal',terminal_at=? WHERE id=?", store.Now(), prepared.GenerationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='checking' WHERE id=? AND state='running'", prepared.TaskID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE execution_effects SET state='observed',observation_json=?,observed_at=? WHERE id=? AND state='prepared'", string(canonical), store.Now(), outcomeEffect)
		return err
	})
	if err != nil {
		return result, err
	}
	if err := r.checkpoint("after_outcome_commit"); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Runner) recordContainment(ctx context.Context, prepared PreparedRun, observation Observation, stopErr error) error {
	id, err := r.effect(ctx, prepared, 10, "containment_stop", map[string]string{"cause": errorString(stopErr)})
	if err != nil {
		return err
	}
	state := "uncertain"
	if stopErr == nil && observation.WriterState == "contained_stopped" {
		state = "observed"
	}
	if err := r.observeEffect(ctx, id, state, observation); err != nil {
		return err
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		writer, runState := "unconfirmed", "unknown"
		if state == "observed" {
			writer, runState = "contained_stopped", "interrupted"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE runs SET state=?,writer_state=?,ended_at=? WHERE id=? AND state NOT IN('completed','failed')", runState, writer, store.Now(), prepared.RunID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE run_generations SET state=? WHERE id=? AND state!='terminal'", map[bool]string{true: "contained", false: "unknown"}[state == "observed"], prepared.GenerationID)
		return err
	})
}
