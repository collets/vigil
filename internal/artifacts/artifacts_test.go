package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vigil/internal/store"
)

func TestPublicationCrashAndCorruption(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "project.sqlite")
	db, err := store.Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	a, err := r.Put(ctx, "first", "check", "durable", strings.NewReader("evidence"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.Put(ctx, "first", "check", "durable", strings.NewReader("evidence"))
	if err != nil || a.ID != again.ID {
		t.Fatal("publication replay", err)
	}
	if b, err := r.Read(ctx, a.ID); err != nil || string(b) != "evidence" {
		t.Fatal("read", err)
	}
	if err := r.Verify(ctx, a.ID, a.Digest, "check"); err != nil {
		t.Fatal("verify", err)
	}
	if err := r.Verify(ctx, a.ID, store.Digest([]byte("other")), "check"); err == nil {
		t.Fatal("accepted wrong expected digest")
	}
	if err := r.Verify(ctx, a.ID, a.Digest, "other"); err == nil {
		t.Fatal("accepted wrong artifact kind")
	}
	transcript, err := r.Put(ctx, "transcript", "check", "transcript", strings.NewReader("transcript evidence"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx, transcript.ID, transcript.Digest, "check"); err == nil {
		t.Fatal("accepted non-durable evidence")
	}
	db.Close()
	if _, err = r.Put(ctx, "interrupted", "check", "durable", strings.NewReader("orphan")); err == nil {
		t.Fatal("closed DB accepted manifest")
	}
	db, err = store.Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r.DB = db
	inspection, err := r.Inspect(ctx)
	if err != nil || len(inspection.Orphans) != 1 {
		t.Fatal("orphan reconciliation", inspection, err)
	}
	if err = os.WriteFile(filepath.Join(r.Dir, "blobs", a.Digest), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Read(ctx, a.ID); err == nil {
		t.Fatal("accepted corrupt artifact")
	}
	inspection, err = r.Inspect(ctx)
	if err != nil || len(inspection.Corrupt) != 1 {
		t.Fatal("corrupt evidence", err)
	}
	if err = os.Remove(filepath.Join(r.Dir, "blobs", a.Digest)); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(r.Dir, "blobs", inspection.Orphans[0]), filepath.Join(r.Dir, "blobs", a.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Read(ctx, a.ID); err == nil {
		t.Fatal("followed artifact symlink")
	}
}
