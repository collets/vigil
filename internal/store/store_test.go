package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProjectV6UpgradeAndRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.sqlite")
	db, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.Exec("INSERT INTO config_snapshots VALUES('existing','digest',1,'{}','{}',1)"); err != nil {
		t.Fatal(err)
	}
	for _, object := range []string{"quality_acceptances_v2", "quality_acceptance_attempts_v2", "evidence_staleness_v2", "human_decisions_v2", "manual_results_v2", "quality_findings_v2", "review_results_v2", "baseline_exceptions_v2", "check_results_v2", "quality_effects_v2", "quality_scopes_v2", "generation_recovery_snapshots", "budget_exhaustions", "recovery_choice_checkpoints", "recovery_attempt_links"} {
		if _, err = db.SQL.Exec("DROP TABLE " + object); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SQL.Exec("DELETE FROM schema_migrations WHERE version>=7"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	// A conflicting object injects a migration failure. The migration and its
	// history row must roll back together, preserving populated v1 data.
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec("CREATE TABLE recovery_attempt_links(id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if failed, err := Open(ctx, path, "project"); err == nil {
		failed.Close()
		t.Fatal("migration failure was accepted")
	}
	raw, err = sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	var versions, existing int
	if err = raw.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 6 {
		t.Fatal("failed migration changed history", versions, err)
	}
	if err = raw.QueryRow("SELECT count(*) FROM config_snapshots WHERE id='existing'").Scan(&existing); err != nil || existing != 1 {
		t.Fatal("failed migration changed v1 data", existing, err)
	}
	if _, err = raw.Exec("DROP TABLE recovery_attempt_links"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if err = upgraded.SQL.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 11 {
		t.Fatal("v6 database was not upgraded", versions, err)
	}
	if err = upgraded.SQL.QueryRow("SELECT count(*) FROM config_snapshots WHERE id='existing'").Scan(&existing); err != nil || existing != 1 {
		t.Fatal("upgrade lost populated data", existing, err)
	}
}

func TestProjectV8UpgradePreservesRecoverySnapshotsAndPermitsEqualDigests(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.sqlite")
	db, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	fixtureStatements := []string{
		"INSERT INTO config_snapshots VALUES('config','config-digest',1,'{}','{}',1)",
		"INSERT INTO profiles VALUES('profile',1,'config','{}')",
		"INSERT INTO plans(id,revision,queue_rank,state,service_limit_ms,created_at) VALUES('plan',1,1,'paused',1,1)",
		"INSERT INTO plan_revisions VALUES('plan',1,'spec','{}','human',1)",
		"INSERT INTO tasks(id,plan_id,revision,kind,state,rank,active_limit_ms,repair_limit,infra_limit) VALUES('task','plan',1,'implementation','blocked',1,1,0,0)",
		"INSERT INTO task_revisions VALUES('task',1,'{}','criteria','definition','human',NULL)",
		`INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at)
 VALUES('run','plan',1,'task',1,'config','profile',1,'implementation','initial','interrupted','contained_stopped',1,1,1)`,
		`INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at)
 VALUES('generation-one','run',1,'synthetic','runtime-one','transport-one','contained','delivered','{}','checkout','{}',1)`,
		`INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at)
 VALUES('generation-two','run',2,'synthetic','runtime-two','transport-two','contained','delivered','{}','checkout','{}',2)`,
		"INSERT INTO operations VALUES('operation-one','checkpoint_save','resource','args',1,'observed','plan','task','run','{}',1)",
		"INSERT INTO operations VALUES('operation-two','checkpoint_save','resource-two','args-two',1,'observed','plan','task','run','{}',2)",
		"INSERT INTO checkpoint_sets(id,run_id,operation_id,state,created_at) VALUES('checkpoint-one','run','operation-one','incomplete',1)",
		"INSERT INTO checkpoint_sets(id,run_id,operation_id,state,created_at) VALUES('checkpoint-two','run','operation-two','incomplete',2)",
	}
	for _, statement := range fixtureStatements {
		if _, err = db.SQL.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		"PRAGMA foreign_keys=OFF",
		"DROP TABLE quality_acceptances_v2",
		"DROP TABLE quality_acceptance_attempts_v2",
		"DROP TABLE evidence_staleness_v2",
		"DROP TABLE human_decisions_v2",
		"DROP TABLE manual_results_v2",
		"DROP TABLE quality_findings_v2",
		"DROP TABLE review_results_v2",
		"DROP TABLE baseline_exceptions_v2",
		"DROP TABLE check_results_v2",
		"DROP TABLE quality_effects_v2",
		"DROP TABLE quality_scopes_v2",
		"DROP TRIGGER generation_recovery_snapshot_no_update",
		"DROP TRIGGER generation_recovery_snapshot_no_delete",
		"ALTER TABLE generation_recovery_snapshots RENAME TO generation_recovery_snapshots_v9",
		`CREATE TABLE generation_recovery_snapshots (
 generation_id TEXT PRIMARY KEY REFERENCES run_generations(id),
 checkpoint_id TEXT NOT NULL REFERENCES checkpoint_sets(id),
 repository_snapshot_json TEXT NOT NULL CHECK(json_valid(repository_snapshot_json)),
 digest TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL
) STRICT`,
		"INSERT INTO generation_recovery_snapshots VALUES('generation-one','checkpoint-one','[]','same-digest',1)",
		"DROP TABLE generation_recovery_snapshots_v9",
		`CREATE TRIGGER generation_recovery_snapshot_no_update BEFORE UPDATE ON generation_recovery_snapshots
 BEGIN SELECT RAISE(ABORT,'generation recovery workspace authority is immutable'); END`,
		`CREATE TRIGGER generation_recovery_snapshot_no_delete BEFORE DELETE ON generation_recovery_snapshots
 BEGIN SELECT RAISE(ABORT,'generation recovery workspace authority is recovery evidence'); END`,
		"DELETE FROM schema_migrations WHERE version>=9",
	}
	for _, statement := range statements {
		if _, err = raw.Exec(statement); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var generation, checkpoint, snapshot, digest string
	if err = upgraded.SQL.QueryRow("SELECT generation_id,checkpoint_id,repository_snapshot_json,digest FROM generation_recovery_snapshots WHERE generation_id='generation-one'").Scan(&generation, &checkpoint, &snapshot, &digest); err != nil {
		t.Fatal("populated v8 snapshot was not preserved", err)
	}
	if generation != "generation-one" || checkpoint != "checkpoint-one" || snapshot != "[]" || digest != "same-digest" {
		t.Fatal("populated v8 snapshot changed", generation, checkpoint, snapshot, digest)
	}
	if _, err = upgraded.SQL.Exec("INSERT INTO generation_recovery_snapshots VALUES('generation-two','checkpoint-two','[]','same-digest',2)"); err != nil {
		t.Fatal("equal content digest remained globally unique", err)
	}
	if _, err = upgraded.SQL.Exec("UPDATE generation_recovery_snapshots SET checkpoint_id='checkpoint-two' WHERE generation_id='generation-one'"); err == nil {
		t.Fatal("upgraded generation recovery authority became mutable")
	}
}

func TestDurableCommandsAndMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.sqlite")
	db, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	command := Command{ID: "request", Actor: "human", Kind: "test", Args: json.RawMessage(`{"b":2,"a":1}`)}
	calls := 0
	apply := func(tx *Tx) (any, error) {
		calls++
		_, err := tx.ExecContext(ctx, "INSERT INTO config_snapshots VALUES('config','digest',1,'{}','{}',1)")
		return map[string]string{"id": "config"}, err
	}
	first, err := db.Command(ctx, command, apply)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	command.Args = json.RawMessage(`{"a":1,"b":2}`)
	second, err := db.Command(ctx, command, apply)
	if err != nil || string(first) != string(second) || calls != 1 {
		t.Fatalf("replay %s %v calls=%d", second, err, calls)
	}
	if receipt, found, err := db.Receipt(ctx, command); err != nil || !found || string(receipt) != string(first) {
		t.Fatal("receipt lookup", string(receipt), found, err)
	}
	command.Actor = "model"
	if _, _, err = db.Receipt(ctx, command); !errors.Is(err, ErrConflict) {
		t.Fatalf("receipt authority reuse: %v", err)
	}
	if _, err = db.Command(ctx, command, apply); !errors.Is(err, ErrConflict) {
		t.Fatalf("authority reuse: %v", err)
	}
	command = Command{ID: "rollback", Actor: "human", Kind: "test", Args: json.RawMessage(`{}`)}
	_, err = db.Command(ctx, command, func(tx *Tx) (any, error) {
		_, err := tx.ExecContext(ctx, "INSERT INTO config_snapshots VALUES('rolled','rolled',1,'{}','{}',1)")
		if err != nil {
			return nil, err
		}
		return nil, errors.New("injected crash before commit")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	var count int
	if err = db.SQL.QueryRow("SELECT count(*) FROM config_snapshots WHERE id='rolled'").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial commit", err)
	}
	if err = db.SQL.QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil || count != 1 {
		t.Fatal("event receipt atomicity", err, count)
	}
	var fk int
	if err = db.SQL.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatal("foreign keys", err)
	}
	if _, err = db.SQL.Exec("UPDATE schema_migrations SET digest='modified'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if bad, err := Open(ctx, path, "project"); err == nil {
		bad.Close()
		t.Fatal("accepted modified history")
	}
}

func TestRejectNewerAndWrongKind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.sqlite")
	db, err := Open(ctx, path, "coordination")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.Exec("INSERT INTO schema_migrations VALUES(3,'future',1)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if next, err := Open(ctx, path, "coordination"); err == nil {
		next.Close()
		t.Fatal("accepted newer schema")
	}
	if next, err := Open(ctx, path, "project"); err == nil {
		next.Close()
		t.Fatal("accepted wrong database kind")
	}
}

func TestClosedInputs(t *testing.T) {
	for _, raw := range []string{`{"x":1,"x":2}`, `{"unknown":1}`, `{"x":1} {}`, `{"x":`} {
		var target struct {
			X int `json:"x"`
		}
		if err := Decode([]byte(raw), &target); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestConcurrentReplayAcrossHandles(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.sqlite")
	a, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var calls atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(db *DB) {
			defer wg.Done()
			_, err := db.Command(ctx, Command{ID: "concurrent", Actor: "human", Kind: "test", Args: json.RawMessage(`{}`)}, func(tx *Tx) (any, error) { calls.Add(1); return "one effect", nil })
			errs <- err
		}([]*DB{a, b}[n%2])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("effect ran %d times", calls.Load())
	}
	if _, err := Canonical([]byte(`{"authority":"human","authority":"worker"}`)); err == nil {
		t.Fatal("ambiguous receipt arguments accepted")
	}
}
