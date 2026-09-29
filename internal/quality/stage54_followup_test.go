package quality_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"vigil/internal/policy"
	"vigil/internal/quality"
	"vigil/internal/store"
)

func TestStage54FollowupHelper(t *testing.T) {
	if os.Getenv("VIGIL_FOLLOWUP_CHILD") != "1" {
		return
	}
	child := exec.Command("/bin/sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("VIGIL_PID_PATH"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		_ = child.Process.Kill()
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestStage54FollowupEscapedSession(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := setupQuality(t, "pass", func(d *policy.CheckDefinition) {
		d.Argv = []string{executable, "-test.run=^TestStage54FollowupHelper$"}
		d.Environment = []policy.EnvironmentVariable{{Name: "VIGIL_FOLLOWUP_CHILD", Value: "1"}, {Name: "VIGIL_PID_PATH", Value: pidfile}}
		d.RequiredOutputs = nil
	})
	result, runErr := f.runCheck(context.Background(), nil)
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err, runErr)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	alive := syscall.Kill(pid, 0) == nil
	_ = syscall.Kill(pid, syscall.SIGKILL)
	if alive && runErr == nil {
		t.Fatalf("detached descendant alive after terminal %s (error %v)", result.Status, runErr)
	}
	if runErr != nil {
		var effectState, segmentState string
		if err := f.engine.DB.SQL.QueryRow(`SELECT e.state,b.state FROM quality_effects_v2 e JOIN quality_budget_segments_v2 b ON b.effect_id=e.id WHERE e.kind='check' ORDER BY e.prepared_at DESC LIMIT 1`).Scan(&effectState, &segmentState); err != nil {
			t.Fatal(err)
		}
		if effectState != "uncertain" || segmentState != "uncertain" {
			t.Fatalf("unavailable containment did not remain uncertain: effect=%s segment=%s", effectState, segmentState)
		}
	}
}

func TestStage54FollowupRegularPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0644, 0755} {
		t.Run(mode.String(), func(t *testing.T) {
			f := setupQuality(t, "pass", nil)
			if err := os.Chmod(filepath.Join(f.root, "src/input.txt"), mode); err != nil {
				t.Fatal(err)
			}
			result, err := f.runCheck(context.Background(), nil)
			if err != nil || result.Status != "pass" {
				t.Fatalf("ordinary %04o source cannot pass check: %s %v", mode, result.Status, err)
			}
		})
	}
}

func TestStage54FollowupPostprocessBudget(t *testing.T) {
	f := setupQuality(t, "pass", func(d *policy.CheckDefinition) {
		d.Argv = []string{"/bin/true"}
		d.RequiredOutputs = nil
	})
	if _, err := f.engine.DB.SQL.Exec("UPDATE budget_ledgers SET active_limit_ms=charged_ms+unknown_ms+500 WHERE scope='task'"); err != nil {
		t.Fatal(err)
	}
	result, err := f.runCheck(context.Background(), func(point string) error {
		if point == "before_source_recheck" {
			time.Sleep(800 * time.Millisecond)
		}
		return nil
	})
	var remaining int64
	if e := f.engine.DB.SQL.QueryRow("SELECT active_limit_ms-charged_ms-unknown_ms FROM budget_ledgers WHERE scope='task'").Scan(&remaining); e != nil {
		t.Fatal(e)
	}
	if err == nil && result.Status == "pass" {
		t.Fatalf("late check passed after 800ms observation delay with %dms remaining from 500ms allowance; charged duration %d", remaining, result.DurationMS)
	}
}

func TestStage54FollowupPopulatedV12Assessment(t *testing.T) {
	ctx := context.Background()
	f := setupQuality(t, "pass", nil)
	if _, err := f.engine.DB.SQL.Exec("UPDATE tasks SET state='needs_repair',repair_limit=0 WHERE id='task'"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	assessor := assessorFunc(func(context.Context, quality.Scope) ([]byte, error) {
		calls++
		return []byte(`{"schema_version":1,"action":"remain_blocked","rationale":"fixture"}`), nil
	})
	request := quality.AssessmentRequest{CommandID: store.ID(), Target: f.target(), SourceKind: "repair_exhaustion", Actor: "fixture"}
	if _, err := quality.RunAssessment(ctx, f.engine, f.owner, assessor, request); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the populated v12 database by removing only later additions.
	downgradeQualityFixtureToV12(t, f)
	reopened, err := f.manager.Open(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.engine = reopened
	request.CommandID = store.ID()
	_, err = quality.RunAssessment(ctx, f.engine, f.owner, assessor, request)
	if calls != 1 {
		t.Fatalf("v12 observed exhaustion invoked assessor again after upgrade: calls=%d error=%v", calls, err)
	}
}

func TestStage54FollowupUnfinishedV12Assessment(t *testing.T) {
	for _, state := range []string{"executing", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			f := setupQuality(t, "pass", nil)
			if _, err := f.engine.DB.SQL.Exec("UPDATE tasks SET state='needs_repair',repair_limit=0 WHERE id='task'"); err != nil {
				t.Fatal(err)
			}
			scope, err := quality.Observe(ctx, f.engine, f.target())
			if err != nil {
				t.Fatal(err)
			}
			if err := quality.Persist(ctx, f.engine, scope); err != nil {
				t.Fatal(err)
			}
			effectID := store.ID()
			started := store.Now() - 50
			var observed any
			if state == "uncertain" {
				observed = started + 1
			}
			if _, err := f.engine.DB.SQL.Exec(`INSERT INTO quality_effects_v2(id,scope_id,kind,definition_id,definition_digest,actor,state,intent_json,observation_json,prepared_at,started_at,observed_at) VALUES(?,?,'supervisor_assessment',?,?,'fixture',?,'{}',?, ?,?,?)`, effectID, scope.ID, "repair_exhaustion:repair-limit-r1", store.Digest([]byte("legacy")), state, nullableJSON(state), started, started, observed); err != nil {
				t.Fatal(err)
			}
			downgradeQualityFixtureToV12(t, f)
			f.engine, err = f.manager.Open(ctx, f.project.ID)
			if err != nil {
				t.Fatal(err)
			}
			var effectState, segmentState, sourceState string
			var unknown int64
			if err := f.engine.DB.SQL.QueryRow(`SELECT e.state,b.state,b.unknown_ms,a.state FROM quality_effects_v2 e JOIN quality_budget_segments_v2 b ON b.effect_id=e.id JOIN quality_assessment_sources_v2 a ON a.effect_id=e.id WHERE e.id=?`, effectID).Scan(&effectState, &segmentState, &unknown, &sourceState); err != nil {
				t.Fatal(err)
			}
			if effectState != "uncertain" || segmentState != "uncertain" || sourceState != "uncertain" || unknown <= 0 {
				t.Fatalf("legacy %s effect was not conservatively reconciled: effect=%s segment=%s source=%s unknown=%d", state, effectState, segmentState, sourceState, unknown)
			}
			calls := 0
			assessor := assessorFunc(func(context.Context, quality.Scope) ([]byte, error) {
				calls++
				return []byte(`{"schema_version":1,"action":"remain_blocked","rationale":"fixture"}`), nil
			})
			_, _ = quality.RunAssessment(ctx, f.engine, f.owner, assessor, quality.AssessmentRequest{CommandID: store.ID(), Target: f.target(), SourceKind: "repair_exhaustion", Actor: "fixture"})
			if calls != 0 {
				t.Fatalf("reconciled v12 %s source invoked assessor again", state)
			}
		})
	}
}

func nullableJSON(state string) any {
	if state == "uncertain" {
		return `{}`
	}
	return nil
}

func downgradeQualityFixtureToV12(t *testing.T, f *fixture) {
	t.Helper()
	path := f.engine.DB.Path
	if err := f.engine.DB.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := raw.Query("SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'quality_authority_%'")
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, name)
	}
	_ = rows.Close()
	for _, name := range triggers {
		if _, err := raw.Exec("DROP TRIGGER " + name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec("DROP INDEX one_draft_delivery_per_head_base"); err != nil {
		t.Fatal(err)
	}
	// Migration 18 adds operations.closure_kind; the v12 reconstruction must
	// remove it along with the later additions it already drops.
	if _, err := raw.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("DELETE FROM schema_migrations WHERE version=18"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("ALTER TABLE operations DROP COLUMN closure_kind"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA ignore_check_constraints=OFF"); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"DROP TRIGGER planning_attempt_terminal_no_update", "DROP TRIGGER planning_attempt_no_delete", "DROP TABLE planning_budget_transfers", "DROP TABLE planning_attempts", "DROP TABLE planning_service_ledgers", "DROP INDEX one_active_tool_generation", "DROP TRIGGER blocked_observation_no_update", "DROP TRIGGER blocked_observation_no_delete", "DROP TRIGGER specification_revision_no_update", "DROP TRIGGER specification_revision_no_delete", "DROP TABLE blocked_observations", "DROP TABLE tool_sessions", "DROP TABLE planning_proposals", "DROP TABLE specification_revisions", "DROP TABLE workflow_dispatches", "DROP TABLE workflow_controls", "DROP TRIGGER quality_budget_segment_no_delete_v2", "DROP TRIGGER quality_budget_segment_update_guard_v2", "DROP TRIGGER quality_assessment_source_no_delete_v2", "DROP TRIGGER quality_assessment_source_update_guard_v2", "DROP TABLE quality_assessment_sources_v2", "DROP TABLE quality_budget_segments_v2", "DROP TABLE quality_authority_v2", "DELETE FROM schema_migrations WHERE version>=13"} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
}
