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

func TestProjectV4UpgradeAndRollback(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "state.sqlite")
	db, err := Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.Exec("INSERT INTO config_snapshots VALUES('existing','digest',1,'{}','{}',1)"); err != nil {
		t.Fatal(err)
	}
	for _, object := range []string{"recovery_choices", "execution_controls"} {
		if _, err = db.SQL.Exec("DROP TABLE " + object); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SQL.Exec("DELETE FROM schema_migrations WHERE version=5"); err != nil {
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
	if _, err = raw.Exec("CREATE TABLE execution_controls(id TEXT)"); err != nil {
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
	if err = raw.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 4 {
		t.Fatal("failed migration changed history", versions, err)
	}
	if err = raw.QueryRow("SELECT count(*) FROM config_snapshots WHERE id='existing'").Scan(&existing); err != nil || existing != 1 {
		t.Fatal("failed migration changed v1 data", existing, err)
	}
	if _, err = raw.Exec("DROP TABLE execution_controls"); err != nil {
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
	if err = upgraded.SQL.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 5 {
		t.Fatal("v4 database was not upgraded", versions, err)
	}
	if err = upgraded.SQL.QueryRow("SELECT count(*) FROM config_snapshots WHERE id='existing'").Scan(&existing); err != nil || existing != 1 {
		t.Fatal("upgrade lost populated data", existing, err)
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
