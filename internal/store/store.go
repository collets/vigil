// Package store owns private SQLite databases and durable command transactions.
package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const MaxDocument = 65536

var ErrConflict = errors.New("command ID already used with different authority or arguments")

type DB struct {
	SQL  *sql.DB
	Path string
	Kind string
}
type Tx struct{ *sql.Conn }

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Now() int64             { return time.Now().UTC().UnixMilli() }

// Canonical preserves JSON numbers exactly, rejects trailing values and bounds inputs.
func Canonical(b []byte) ([]byte, error) {
	if err := Decode(b, new(any)); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}
	return json.Marshal(v)
}

func Open(ctx context.Context, path, kind string) (*DB, error) {
	if kind != "project" && kind != "coordination" {
		return nil, errors.New("unknown database kind")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = PrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("database must be a private regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		f.Close()
	} else if !os.IsExist(err) {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "synchronous(FULL)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &DB{SQL: db, Path: path, Kind: kind}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		db.Close()
		return nil, fmt.Errorf("database integrity check failed: %v (%s)", err, integrity)
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		db.Close()
		return nil, err
	}
	invalid := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil || invalid {
		db.Close()
		return nil, errors.New("database foreign key integrity check failed")
	}
	var journal string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journal); err != nil || journal != "wal" {
		db.Close()
		return nil, fmt.Errorf("WAL unavailable: %s (%v)", journal, err)
	}
	return s, nil
}
func (s *DB) Close() error { return s.SQL.Close() }

func (s *DB) Write(ctx context.Context, fn func(*Tx) error) error {
	c, err := s.SQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err = c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer c.ExecContext(context.Background(), "ROLLBACK")
	if err = fn(&Tx{c}); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "COMMIT")
	return err
}

func (s *DB) migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	type migration struct {
		version int
		body    []byte
	}
	var available []migration
	prefix := s.Kind + "-"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, parseErr := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".sql"))
		if parseErr != nil || version < 1 {
			return fmt.Errorf("invalid migration name %q", name)
		}
		body, readErr := migrations.ReadFile("migrations/" + name)
		if readErr != nil {
			return readErr
		}
		available = append(available, migration{version: version, body: body})
	}
	sort.Slice(available, func(i, j int) bool { return available[i].version < available[j].version })
	if len(available) == 0 {
		return errors.New("no migrations for database kind")
	}
	for n, migration := range available {
		if migration.version != n+1 {
			return errors.New("migration history has a version gap")
		}
	}
	return s.Write(ctx, func(tx *Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			var tables int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
				return err
			}
			if tables != 0 {
				return errors.New("refusing unrecognized database")
			}
			for _, migration := range available {
				if _, err := tx.ExecContext(ctx, string(migration.body)); err != nil {
					return fmt.Errorf("apply migration %d: %w", migration.version, err)
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,?)", migration.version, Digest(migration.body), Now()); err != nil {
					return err
				}
			}
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT version,digest FROM schema_migrations ORDER BY version")
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var version int
			var digest string
			if err := rows.Scan(&version, &digest); err != nil {
				return err
			}
			count++
			if version != count || version > len(available) {
				return errors.New("unsupported or newer schema version")
			}
			if digest != Digest(available[version-1].body) {
				return errors.New("historical migration digest mismatch (or wrong database kind)")
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if count == 0 {
			return errors.New("missing migration history")
		}
		for _, migration := range available[count:] {
			if _, err := tx.ExecContext(ctx, string(migration.body)); err != nil {
				return fmt.Errorf("apply migration %d: %w", migration.version, err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,?)", migration.version, Digest(migration.body), Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

type Command struct {
	ID    string          `json:"command_id"`
	Actor string          `json:"actor"`
	Kind  string          `json:"kind"`
	Args  json.RawMessage `json:"args"`
}

func (s *DB) Command(ctx context.Context, cmd Command, fn func(*Tx) (any, error)) (json.RawMessage, error) {
	if s.Kind != "project" || cmd.ID == "" || len(cmd.ID) > 128 || cmd.Kind == "" || len(cmd.Kind) > 128 || cmd.Actor == "" || len(cmd.Actor) > 128 {
		return nil, errors.New("invalid command envelope")
	}
	args, err := Canonical(cmd.Args)
	if err != nil {
		return nil, err
	}
	envelope, _ := json.Marshal([]string{cmd.Kind, cmd.Actor, string(args)})
	digest := Digest(envelope)
	var result json.RawMessage
	err = s.Write(ctx, func(tx *Tx) error {
		var prior, actor, value string
		err := tx.QueryRowContext(ctx, "SELECT args_digest,actor,result_json FROM command_receipts WHERE id=?", cmd.ID).Scan(&prior, &actor, &value)
		if err == nil {
			if prior != digest || actor != cmd.Actor {
				return ErrConflict
			}
			result = json.RawMessage(value)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		v, err := fn(tx)
		if err != nil {
			return err
		}
		result, err = json.Marshal(v)
		if err != nil {
			return err
		}
		if len(result) > MaxDocument {
			return errors.New("command result exceeds 64 KiB")
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO command_receipts VALUES(?,?,?,?,?)", cmd.ID, cmd.Actor, digest, string(result), Now()); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"command_kind": cmd.Kind, "actor": cmd.Actor})
		_, err = tx.ExecContext(ctx, "INSERT INTO events(schema_version,command_id,kind,occurred_at,payload_json) VALUES(1,?,'command_applied',?,?)", cmd.ID, Now(), string(payload))
		return err
	})
	return result, err
}

// Decode is the closed-schema JSON boundary used by CLI and future model handlers.
func Decode(b []byte, target any) error {
	if len(b) > MaxDocument {
		return errors.New("input exceeds 64 KiB")
	}
	// Reject duplicate object keys, which otherwise permit ambiguous authority inputs.
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			if delim == '{' {
				seen := map[string]bool{}
				for d.More() {
					k, err := d.Token()
					if err != nil {
						return err
					}
					key := k.(string)
					if seen[key] {
						return fmt.Errorf("duplicate JSON field: %s", key)
					}
					seen[key] = true
					if err := walk(); err != nil {
						return err
					}
				}
			} else if delim == '[' {
				for d.More() {
					if err := walk(); err != nil {
						return err
					}
				}
			} else {
				return errors.New("invalid JSON delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(target)
}

func PrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("state path must be a private directory")
	}
	return nil
}
func SafeID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && !strings.ContainsAny(id, "/\\\x00") && id != "." && id != ".."
}
