package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

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
	command.Actor = "model"
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
	if _, err = db.SQL.Exec("INSERT INTO schema_migrations VALUES(2,'future',1)"); err != nil {
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
