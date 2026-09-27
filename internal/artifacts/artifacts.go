// Package artifacts publishes bounded private content-addressed evidence.
package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"vigil/internal/store"
)

const MaxBytes = 16 << 20

type Artifact struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
	Kind   string `json:"kind"`
}
type Repository struct {
	DB  *store.DB
	Dir string
}

func New(db *store.DB) (*Repository, error) {
	dir := filepath.Join(filepath.Dir(db.Path), "artifacts")
	if err := store.PrivateDir(dir); err != nil {
		return nil, err
	}
	if err := store.PrivateDir(filepath.Join(dir, "blobs")); err != nil {
		return nil, err
	}
	return &Repository{DB: db, Dir: dir}, nil
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// withBlobLock serializes publication with expiry across application processes.
// A private lock file is kept for the lifetime of the repository.
func (r *Repository) withBlobLock(fn func() error) error {
	if err := store.PrivateDir(r.Dir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(r.Dir, ".blob-lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func (r *Repository) Put(ctx context.Context, commandID, kind, retention string, reader io.Reader) (Artifact, error) {
	return r.put(ctx, commandID, "human", kind, retention, reader)
}

// PutCore publishes trusted application-generated evidence without falsely
// attributing the command receipt to a human actor.
func (r *Repository) PutCore(ctx context.Context, commandID, kind, retention string, reader io.Reader) (Artifact, error) {
	return r.put(ctx, commandID, "core", kind, retention, reader)
}

func (r *Repository) put(ctx context.Context, commandID, actor, kind, retention string, reader io.Reader) (Artifact, error) {
	if actor != "human" && actor != "core" {
		return Artifact{}, errors.New("invalid artifact authority")
	}
	if kind == "" || len(kind) > 128 || (retention != "durable" && retention != "transcript" && retention != "unfinished") {
		return Artifact{}, errors.New("invalid artifact definition")
	}
	b, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Artifact{}, err
	}
	if len(b) > MaxBytes {
		return Artifact{}, errors.New("artifact exceeds 16 MiB")
	}
	digest := store.Digest(b)
	if err := store.PrivateDir(filepath.Join(r.Dir, "blobs")); err != nil {
		return Artifact{}, err
	}
	tmp, err := os.CreateTemp(r.Dir, ".publish-")
	if err != nil {
		return Artifact{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Artifact{}, err
	}
	var result json.RawMessage
	err = r.withBlobLock(func() error {
		destination := filepath.Join(r.Dir, "blobs", digest)
		// Link publishes without replacing an existing object, including a malicious symlink.
		if linkErr := os.Link(tmp.Name(), destination); linkErr != nil && !os.IsExist(linkErr) {
			return linkErr
		}
		if _, readErr := readBlob(destination, digest, int64(len(b))); readErr != nil {
			return readErr
		}
		if syncErr := syncDir(filepath.Dir(destination)); syncErr != nil {
			return syncErr
		}
		args, _ := json.Marshal(map[string]any{"kind": kind, "retention": retention, "digest": digest, "bytes": len(b)})
		var commandErr error
		result, commandErr = r.DB.Command(ctx, store.Command{ID: commandID, Actor: actor, Kind: "artifact.publish", Args: args}, func(tx *store.Tx) (any, error) {
			a := Artifact{ID: store.ID(), Digest: digest, Bytes: int64(len(b)), Kind: kind}
			_, insertErr := tx.ExecContext(ctx, "INSERT INTO artifacts(id,kind,digest,relative_path,byte_count,state,retention,created_at) VALUES(?,?,?,?,?,'available',?,?)", a.ID, kind, digest, "blobs/"+digest, a.Bytes, retention, store.Now())
			return a, insertErr
		})
		return commandErr
	})
	var a Artifact
	if err == nil {
		err = json.Unmarshal(result, &a)
	}
	return a, err
}
func readBlob(path, digest string, size int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 || i.Size() != size || size < 0 || size > MaxBytes {
		return nil, errors.New("artifact is corrupt, non-private or not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != size || store.Digest(b) != digest {
		return nil, errors.New("artifact digest mismatch")
	}
	return b, nil
}
func (r *Repository) Read(ctx context.Context, id string) ([]byte, error) {
	var digest, path, state string
	var size int64
	err := r.DB.SQL.QueryRowContext(ctx, "SELECT digest,relative_path,byte_count,state FROM artifacts WHERE id=?", id).Scan(&digest, &path, &size, &state)
	if err != nil {
		return nil, err
	}
	if state != "available" || len(digest) != 64 || path != "blobs/"+digest || !store.SafeID(digest) {
		return nil, errors.New("artifact unavailable or invalid reference")
	}
	if err = store.PrivateDir(filepath.Join(r.Dir, "blobs")); err != nil {
		return nil, err
	}
	return readBlob(filepath.Join(r.Dir, path), digest, size)
}

// Verify resolves a durable content-addressed reference and re-reads its bytes.
// Callers must invoke it again at consumption time because retained evidence can
// disappear or become corrupt after its manifest was admitted.
func (r *Repository) Verify(ctx context.Context, id, expectedDigest, expectedKind string) error {
	if !store.SafeID(id) || len(expectedDigest) != 64 || !store.SafeID(expectedDigest) || expectedKind == "" || len(expectedKind) > 128 {
		return errors.New("invalid artifact reference")
	}
	var digest, path, state, retention, kind string
	var size int64
	err := r.DB.SQL.QueryRowContext(ctx, "SELECT digest,relative_path,byte_count,state,retention,kind FROM artifacts WHERE id=?", id).Scan(&digest, &path, &size, &state, &retention, &kind)
	if err != nil {
		return fmt.Errorf("resolve artifact: %w", err)
	}
	if state != "available" || retention != "durable" || kind != expectedKind || digest != expectedDigest || path != "blobs/"+digest || !store.SafeID(digest) {
		return errors.New("artifact reference is unavailable or does not match")
	}
	if err := store.PrivateDir(filepath.Join(r.Dir, "blobs")); err != nil {
		return err
	}
	_, err = readBlob(filepath.Join(r.Dir, path), digest, size)
	return err
}

type Reconciliation struct {
	Orphans []string `json:"orphans"`
	Corrupt []string `json:"corrupt_artifact_ids"`
}

// Inspect never deletes recovery evidence. Corruption must block any dependent use.
func (r *Repository) Inspect(ctx context.Context) (Reconciliation, error) {
	result := Reconciliation{Orphans: []string{}, Corrupt: []string{}}
	if err := store.PrivateDir(filepath.Join(r.Dir, "blobs")); err != nil {
		return result, err
	}
	rows, err := r.DB.SQL.QueryContext(ctx, "SELECT id,digest,relative_path,byte_count FROM artifacts WHERE state='available'")
	if err != nil {
		return result, err
	}
	known := map[string]bool{}
	for rows.Next() {
		var id, digest, path string
		var size int64
		if err = rows.Scan(&id, &digest, &path, &size); err != nil {
			rows.Close()
			return result, err
		}
		known[digest] = true
		if path != "blobs/"+digest || len(digest) != 64 || !store.SafeID(digest) {
			result.Corrupt = append(result.Corrupt, id)
			continue
		}
		if _, err = readBlob(filepath.Join(r.Dir, path), digest, size); err != nil {
			result.Corrupt = append(result.Corrupt, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	entries, err := os.ReadDir(filepath.Join(r.Dir, "blobs"))
	if err != nil {
		return result, fmt.Errorf("scan artifacts: %w", err)
	}
	for _, entry := range entries {
		if !known[entry.Name()] {
			result.Orphans = append(result.Orphans, entry.Name())
		}
	}
	sort.Strings(result.Orphans)
	sort.Strings(result.Corrupt)
	return result, nil
}
