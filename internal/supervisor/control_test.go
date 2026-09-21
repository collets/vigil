package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vigil/internal/core"
	"vigil/internal/store"
)

func projectRevision(t *testing.T, fixture fixture) int {
	t.Helper()
	var revision int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}

func TestPauseContinueAreDurableAndIdempotent(t *testing.T) {
	fixture := setupFixture(t)
	revision := projectRevision(t, fixture)
	paused, err := fixture.engine.Pause(context.Background(), "pause", revision)
	if err != nil || paused.State != "paused" || paused.Repeated {
		t.Fatal(paused, err)
	}
	again, err := fixture.engine.Pause(context.Background(), "pause", revision)
	if err != nil || !again.Repeated || again.Revision != paused.Revision {
		t.Fatal(again, err)
	}
	if _, err := Prepare(context.Background(), fixture.engine, PrepareRequest{CommandID: "dispatch-while-paused", ExpectedProjectRevision: paused.Revision, TaskID: fixture.prepared.TaskID, RuntimeKind: "synthetic", WallLimitMS: 1000}); err == nil {
		t.Fatal("pause allowed another dispatch")
	}
	continued, err := fixture.engine.Continue(context.Background(), "continue", paused.Revision)
	if err != nil || continued.State != "ready" {
		t.Fatal(continued, err)
	}
	again, err = fixture.engine.Continue(context.Background(), "continue", paused.Revision)
	if err != nil || !again.Repeated || again.Revision != continued.Revision {
		t.Fatal(again, err)
	}
}

func TestStopPersistsIntentRetiresRequestsAndDoesNotRepeatEffects(t *testing.T) {
	fixture := setupFixture(t)
	digest := store.Digest([]byte("late-authority"))
	contextJSON, _ := json.Marshal(core.OperationRequest{Category: "checkpoint_restore", ResourceDigest: digest, ArgumentsDigest: digest, PlanID: fixture.prepared.PlanID, TaskID: fixture.prepared.TaskID, TaskRevision: 1})
	var epoch int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT policy_epoch FROM project").Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.DB.SQL.Exec(`INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,task_id,evidence_json,created_at) VALUES('late-op','checkpoint_restore',?,?,?,'prepared',?,?,?,?)`, digest, digest, epoch, fixture.prepared.PlanID, fixture.prepared.TaskID, string(contextJSON), store.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.DB.SQL.Exec(`INSERT INTO requests(id,kind,state,plan_id,task_id,task_revision,run_id,operation_id,context_json,blocking,created_at,deadline) VALUES('late','approval','pending',?,?,1,?,'late-op',?,1,?,?)`, fixture.prepared.PlanID, fixture.prepared.TaskID, fixture.prepared.RunID, string(contextJSON), store.Now(), store.Now()+60000); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	receipt, err := runner.Stop(context.Background(), fixture.prepared, "stop", 0, time.Second)
	if err != nil || receipt.State != "observed" || receipt.WriterState != "contained_stopped" {
		t.Fatal(receipt, err)
	}
	if fixture.driver.Calls["interrupt"] != 1 || fixture.driver.Calls["stop"] != 1 {
		t.Fatal("stop sequence count", fixture.driver.Calls)
	}
	again, err := runner.Stop(context.Background(), fixture.prepared, "stop", 0, time.Second)
	if err != nil || !again.Repeated || fixture.driver.Calls["interrupt"] != 1 || fixture.driver.Calls["stop"] != 1 {
		t.Fatal(again, fixture.driver.Calls, err)
	}
	var requestState, projectState, taskState string
	if err := fixture.engine.DB.SQL.QueryRow("SELECT state FROM requests WHERE id='late'").Scan(&requestState); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.DB.SQL.QueryRow("SELECT state FROM project").Scan(&projectState); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.DB.SQL.QueryRow("SELECT state FROM tasks WHERE id=?", fixture.prepared.TaskID).Scan(&taskState); err != nil {
		t.Fatal(err)
	}
	if requestState != "cancelled" || projectState != "paused" || taskState != "stopped" {
		t.Fatal(requestState, projectState, taskState)
	}
	// The ordinary resolution path cannot revive the retired request.
	payload, _ := json.Marshal(core.GrantRequest{RequestID: "late", Scope: "once", Decision: "allow"})
	_, resolveErr := fixture.engine.Apply(context.Background(), core.Human, core.Envelope{CommandID: "late-answer", ExpectedRevision: projectRevision(t, fixture), Kind: "permission.grant", Payload: payload})
	if resolveErr == nil {
		t.Fatal("late answer authorized after stop")
	}
}

type failedStopDriver struct{ *FixtureDriver }

func (d *failedStopDriver) Stop(context.Context, PreparedRun) (Observation, error) {
	d.count("stop")
	return Observation{WriterState: "unconfirmed"}, errors.New("termination unavailable")
}

func TestFailedStopRemainsUncertainAndCannotReplay(t *testing.T) {
	fixture := setupFixture(t)
	driver := &failedStopDriver{FixtureDriver: fixture.driver}
	runner := Runner{Engine: fixture.engine, Driver: driver}
	receipt, err := runner.Shutdown(context.Background(), fixture.prepared, "shutdown", 0, 20*time.Millisecond)
	if err == nil || receipt.State != "uncertain" || receipt.WriterState != "unconfirmed" {
		t.Fatal(receipt, err)
	}
	_, err = runner.Shutdown(context.Background(), fixture.prepared, "shutdown", 0, 20*time.Millisecond)
	if err == nil || driver.Calls["stop"] != 1 {
		t.Fatal("uncertain termination replayed", driver.Calls, err)
	}
	if _, err := fixture.engine.Continue(context.Background(), "unsafe-continue", projectRevision(t, fixture)); err == nil {
		t.Fatal("continued with unconfirmed writer")
	}
}
