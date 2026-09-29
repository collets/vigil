package artifacts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vigil/internal/store"
)

// ExpiryCandidate is a dry-inspected raw transcript; no durable evidence is
// eligible for this operation. Time is in UTC Unix milliseconds.
type ExpiryCandidate struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	Bytes    int64  `json:"bytes"`
	Deadline int64  `json:"deadline"`
}

type ExpiryResult struct {
	Expired []ExpiryCandidate `json:"expired"`
	Deleted []string          `json:"deleted_blob_digests"`
}

type artifactForeignKey struct{ table, column string }

// RetentionReader is the database surface the expiry candidate query and
// expiry marking run against: either the shared connection or one open write
// transaction.
type RetentionReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func quotedSQL(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// Discover every typed artifact reference, including future migrations. New
// reference tables therefore make expiry more conservative by default.
func artifactReferences(ctx context.Context, db RetentionReader) ([]artifactForeignKey, error) {
	tables, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			tables.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = tables.Err()
	tables.Close()
	if err != nil {
		return nil, err
	}
	var refs []artifactForeignKey
	for _, name := range names {
		rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_list("+quotedSQL(name)+")")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, seq int
			var table, from, to, onUpdate, onDelete, match string
			if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				rows.Close()
				return nil, err
			}
			if table == "artifacts" {
				if to != "id" {
					rows.Close()
					return nil, errors.New("unsupported artifact foreign key")
				}
				refs = append(refs, artifactForeignKey{name, from})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func (r *Repository) expiryCandidates(ctx context.Context, db RetentionReader, now int64, days int, checkBytes bool) ([]ExpiryCandidate, error) {
	if days < 1 || days > 36500 || now < 0 {
		return nil, errors.New("invalid retention clock or interval")
	}
	refs, err := artifactReferences(ctx, db)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT id,digest,byte_count FROM artifacts WHERE state='available' AND retention='transcript' ORDER BY id")
	if err != nil {
		return nil, err
	}
	var raw []ExpiryCandidate
	for rows.Next() {
		var candidate ExpiryCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Digest, &candidate.Bytes); err != nil {
			rows.Close()
			return nil, err
		}
		raw = append(raw, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	cutoff := now - int64(days)*86400000
	eligible := make([]ExpiryCandidate, 0, len(raw))
	for _, candidate := range raw {
		// Any non-run reference is durable, a checkpoint or recovery dependency.
		blocked := false
		for _, ref := range refs {
			if ref.table == "run_artifacts" {
				continue
			}
			var count int
			query := "SELECT count(*) FROM " + quotedSQL(ref.table) + " WHERE " + quotedSQL(ref.column) + "=?"
			if err := db.QueryRowContext(ctx, query, candidate.ID).Scan(&count); err != nil {
				return nil, err
			}
			if count != 0 {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		var owners, incomplete int
		var latestCompletion sql.NullInt64
		err := db.QueryRowContext(ctx, `SELECT count(*),
			coalesce(sum(CASE WHEN p.state!='completed' OR p.completed_at IS NULL OR p.completed_at>? OR
				r.state NOT IN ('completed','failed','interrupted') OR
				EXISTS(SELECT 1 FROM runs x WHERE x.plan_id=p.id AND (x.state NOT IN ('completed','failed','interrupted') OR x.writer_state='unconfirmed')) OR
				EXISTS(SELECT 1 FROM checkpoint_sets c JOIN runs cr ON cr.id=c.run_id WHERE cr.plan_id=p.id AND c.state!='restored') OR
				EXISTS(SELECT 1 FROM operations o WHERE o.plan_id=p.id AND o.state IN ('prepared','executing','uncertain')) OR
				EXISTS(SELECT 1 FROM deliveries d WHERE d.plan_id=p.id AND d.state IN ('prepared','pending','uncertain')) OR
				EXISTS(SELECT 1 FROM requests q WHERE q.plan_id=p.id AND q.state='pending' AND (q.deadline IS NULL OR q.deadline>?))
			THEN 1 ELSE 0 END),0), max(p.completed_at)
			FROM run_artifacts ra JOIN runs r ON r.id=ra.run_id JOIN plans p ON p.id=r.plan_id
			WHERE ra.artifact_id=?`, cutoff, now, candidate.ID).Scan(&owners, &incomplete, &latestCompletion)
		if err != nil {
			return nil, err
		}
		if owners == 0 || incomplete != 0 || !latestCompletion.Valid {
			continue
		}
		if checkBytes {
			if _, err := readBlob(filepath.Join(r.Dir, "blobs", candidate.Digest), candidate.Digest, candidate.Bytes); err != nil {
				// Corrupt evidence is preserved for inspection rather than silently purged.
				continue
			}
		}
		candidate.Deadline = latestCompletion.Int64 + int64(days)*86400000
		eligible = append(eligible, candidate)
	}
	return eligible, nil
}

// TranscriptExpiryCandidates computes the exact eligible candidate set. The
// caller chooses the reader (shared connection or open command transaction)
// and whether blob bytes are re-verified before relying on the result.
func (r *Repository) TranscriptExpiryCandidates(ctx context.Context, reader RetentionReader, now int64, days int, checkBytes bool) ([]ExpiryCandidate, error) {
	return r.expiryCandidates(ctx, reader, now, days, checkBytes)
}

// ExpireCandidates marks an exact eligible candidate set expired inside the
// caller's transaction. The irreversible blob deletion is deliberately a
// separate phase that runs only after this transaction commits.
func (r *Repository) ExpireCandidates(ctx context.Context, reader RetentionReader, now int64, candidates []ExpiryCandidate) error {
	for _, candidate := range candidates {
		updated, err := reader.ExecContext(ctx, "UPDATE artifacts SET state='expired',expires_at=? WHERE id=? AND state='available' AND retention='transcript'", now, candidate.ID)
		if err != nil {
			return err
		}
		n, _ := updated.RowsAffected()
		if n != 1 {
			return errors.New("retention candidate changed before expiry")
		}
	}
	return nil
}

// CleanupExpiredTranscriptBlobs removes blob bytes of expired transcripts that
// no available artifact still needs, under the blob lock so a concurrent
// publication cannot lose its bytes. A retry after a crash between marking
// and deletion cleans up the remaining blobs. No durable evidence is eligible
// for this operation.
func (r *Repository) CleanupExpiredTranscriptBlobs(ctx context.Context) ([]string, error) {
	deleted := []string{}
	err := r.withBlobLock(func() error {
		rows, err := r.DB.SQL.QueryContext(ctx, "SELECT DISTINCT digest FROM artifacts WHERE retention='transcript' AND state='expired' ORDER BY digest")
		if err != nil {
			return err
		}
		var digests []string
		for rows.Next() {
			var digest string
			if err := rows.Scan(&digest); err != nil {
				rows.Close()
				return err
			}
			digests = append(digests, digest)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, digest := range digests {
			if len(digest) != 64 || !store.SafeID(digest) {
				return fmt.Errorf("invalid expired digest %q", digest)
			}
			var live int
			if err := r.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM artifacts WHERE digest=? AND state='available'", digest).Scan(&live); err != nil {
				return err
			}
			if live != 0 {
				continue
			}
			path := filepath.Join(r.Dir, "blobs", digest)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			deleted = append(deleted, digest)
		}
		if len(deleted) != 0 {
			return syncDir(filepath.Join(r.Dir, "blobs"))
		}
		return nil
	})
	return deleted, err
}
