// Package coordinator manages cooperative host ownership, capacity and quarantine.
// It does not contain hostile workers or arbitrate inference on other hosts.
package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"vigil/internal/store"
	"vigil/internal/workspace"
)

var ErrBusy = errors.New("resource owned or quarantined")
var ErrWaiting = errors.New("waiting for earlier ticket or endpoint capacity")

type Coordinator struct {
	DB  *store.DB
	Dir string
}
type Owner struct {
	Coordinator *Coordinator
	ID          string
	lock        *os.File
}
type Claim struct {
	ID         string             `json:"id"`
	Generation int64              `json:"generation"`
	Identity   workspace.Identity `json:"identity"`
}
type Ticket struct {
	Sequence   int64  `json:"sequence"`
	Generation int64  `json:"generation"`
	Endpoint   string `json:"endpoint"`
	State      string `json:"state"`
}

func Open(ctx context.Context, dir string) (*Coordinator, error) {
	if err := store.PrivateDir(dir); err != nil {
		return nil, err
	}
	if err := store.PrivateDir(filepath.Join(dir, "locks")); err != nil {
		return nil, err
	}
	db, err := store.Open(ctx, filepath.Join(dir, "coordination.sqlite"), "coordination")
	if err != nil {
		return nil, err
	}
	return &Coordinator{DB: db, Dir: dir}, nil
}
func Host() string { h, _ := os.Hostname(); return store.Digest([]byte(h)) }
func processIdentity(ctx context.Context) (string, string, error) {
	if runtime.GOOS == "linux" {
		boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
		if err != nil {
			return "", "", err
		}
		stat, err := os.ReadFile("/proc/self/stat")
		if err != nil {
			return "", "", err
		}
		i := strings.LastIndex(string(stat), ") ")
		if i < 0 {
			return "", "", errors.New("invalid process identity")
		}
		fields := strings.Fields(string(stat)[i+2:])
		if len(fields) < 20 {
			return "", "", errors.New("invalid process start")
		}
		return strings.TrimSpace(string(boot)), fields[19], nil
	}
	boot, err := exec.CommandContext(ctx, "sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return "", "", err
	}
	start, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(os.Getpid()), "-o", "lstart=").Output()
	return strings.TrimSpace(string(boot)), strings.TrimSpace(string(start)), err
}
func (c *Coordinator) Register(ctx context.Context) (*Owner, error) {
	boot, start, err := processIdentity(ctx)
	if err != nil {
		return nil, err
	}
	id := store.ID()
	path := filepath.Join(c.Dir, "locks", id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	err = c.DB.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO instances VALUES(?,?,?,?,?,?,?)", id, Host(), boot, os.Getpid(), start, path, store.Now())
		return err
	})
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Owner{Coordinator: c, ID: id, lock: f}, nil
}
func (o *Owner) live() error {
	if o.lock == nil {
		return errors.New("owner already closed")
	}
	return nil
}

func (o *Owner) Claim(ctx context.Context, project, operation string, roots []workspace.Identity) ([]Claim, error) {
	if err := o.live(); err != nil {
		return nil, err
	}
	if !store.SafeID(project) || !store.SafeID(operation) || len(roots) == 0 || len(roots) > 128 {
		return nil, errors.New("invalid claim")
	}
	unique := map[string]bool{}
	for _, root := range roots {
		if unique[root.Key] {
			return nil, errors.New("duplicate physical root")
		}
		unique[root.Key] = true
	}
	claims := []Claim{}
	err := o.Coordinator.DB.Write(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id,operation_id,instance_id,identity_json,generation,project_id,state FROM workspace_claims")
		if err != nil {
			return err
		}
		type existing struct {
			id, op, owner  string
			project, state string
			identity       workspace.Identity
			generation     int64
		}
		var prior []existing
		for rows.Next() {
			var e existing
			var raw string
			if err := rows.Scan(&e.id, &e.op, &e.owner, &raw, &e.generation, &e.project, &e.state); err != nil {
				rows.Close()
				return err
			}
			if err := json.Unmarshal([]byte(raw), &e.identity); err != nil {
				rows.Close()
				return err
			}
			prior = append(prior, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, root := range roots {
			if err := root.Validate(); err != nil {
				return err
			}
			op := operation + ":" + store.Digest([]byte(root.Key))[:16]
			reused := false
			for _, p := range prior {
				if p.op == op && p.owner == o.ID {
					if p.project != project || p.state != "active" || p.identity.Key != root.Key {
						return store.ErrConflict
					}
					if err := p.identity.Validate(); err != nil {
						return err
					}
					claims = append(claims, Claim{p.id, p.generation, p.identity})
					reused = true
					break
				}
			}
			if reused {
				continue
			}
			for _, p := range prior {
				if workspace.Overlap(root, p.identity) {
					return ErrBusy
				}
			}
			r, err := tx.ExecContext(ctx, "INSERT INTO fencing_tokens DEFAULT VALUES")
			if err != nil {
				return err
			}
			generation, _ := r.LastInsertId()
			claim := Claim{store.ID(), generation, root}
			raw, _ := json.Marshal(root)
			_, err = tx.ExecContext(ctx, "INSERT INTO workspace_claims(id,operation_id,project_id,instance_id,canonical_root,filesystem_identity,identity_json,common_git_identity,generation,state,acquired_at) VALUES(?,?,?,?,?,?,?,?,?,'active',?)", claim.ID, op, project, o.ID, root.Root, root.Key, string(raw), root.CommonGit, generation, store.Now())
			if err != nil {
				return err
			}
			claims = append(claims, claim)
		}
		return nil
	})
	return claims, err
}

func quarantine(ctx context.Context, tx *store.Tx, owner, reason string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE workspace_claims SET state='quarantined',quarantine_reason=? WHERE instance_id=?", reason, owner); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE endpoint_slots SET state='quarantined' WHERE ticket IN(SELECT sequence FROM queue_tickets WHERE instance_id=? AND state IN('reserved','quarantined'))", owner); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE queue_tickets SET state=CASE WHEN state='waiting' THEN 'cancelled' ELSE 'quarantined' END WHERE instance_id=? AND state IN('waiting','reserved','quarantined')", owner)
	return err
}
func (o *Owner) Close() error {
	if o.lock == nil {
		return nil
	}
	err := o.Coordinator.DB.Write(context.Background(), func(tx *store.Tx) error {
		return quarantine(context.Background(), tx, o.ID, "controller closed without verified release")
	})
	closeErr := o.lock.Close()
	o.lock = nil
	if err != nil {
		return err
	}
	return closeErr
}
func (c *Coordinator) ownerAlive(id string) (bool, error) {
	if !store.SafeID(id) {
		return false, errors.New("invalid owner identity")
	}
	path := filepath.Join(c.Dir, "locks", id)
	i, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !i.Mode().IsRegular() {
		return false, errors.New("invalid owner lock")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

// Reap quarantines dead owners. It never releases their workspace or inference slot.
func (c *Coordinator) Reap(ctx context.Context) error {
	return c.DB.Write(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM instances WHERE host_identity=?", Host())
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			alive, err := c.ownerAlive(id)
			if err != nil {
				return err
			}
			if !alive {
				if err := quarantine(ctx, tx, id, "owner lock released; writer state unknown"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Release is for a trusted supervisor observation, never a worker-supplied claim.
// Manual recovery of a dead owner uses Reconcile with human-observed evidence.
func (o *Owner) Release(ctx context.Context, claim Claim, proof string) error {
	if err := o.live(); err != nil {
		return err
	}
	if proof != "contained_stopped" && proof != "never_started" {
		return errors.New("writer/inference shutdown proof required")
	}
	return o.Coordinator.DB.Write(ctx, func(tx *store.Tx) error {
		var slots int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM queue_tickets WHERE instance_id=? AND state IN('waiting','reserved','quarantined')", o.ID).Scan(&slots); err != nil {
			return err
		}
		if slots != 0 {
			return errors.New("release endpoint reservations before workspace")
		}
		r, err := tx.ExecContext(ctx, "DELETE FROM workspace_claims WHERE id=? AND instance_id=? AND generation=? AND state='active'", claim.ID, o.ID, claim.Generation)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return errors.New("stale claim")
		}
		return nil
	})
}
func CanonicalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid credential-free endpoint URL")
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		h = "127.0.0.1"
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	u.Host = net.JoinHostPort(h, port)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}
func (c *Coordinator) Endpoint(ctx context.Context, id string, aliases []string, capacity int, authority string) error {
	if !store.SafeID(id) || len(aliases) == 0 || capacity < 1 || capacity > 16 {
		return errors.New("invalid endpoint")
	}
	if authority != Host() {
		return errors.New("cross-host capacity unsupported; select one explicit host authority")
	}
	var urls []string
	for _, alias := range aliases {
		u, err := CanonicalURL(alias)
		if err != nil {
			return err
		}
		urls = append(urls, u)
	}
	return c.DB.Write(ctx, func(tx *store.Tx) error {
		var existing int
		err := tx.QueryRowContext(ctx, "SELECT capacity FROM endpoints WHERE id=?", id).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = tx.ExecContext(ctx, "INSERT INTO endpoints VALUES(?,?,?,1)", id, urls[0], capacity); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existing != capacity {
			return errors.New("endpoint capacity change requires reconciliation")
		}
		for _, alias := range urls {
			var prior string
			err := tx.QueryRowContext(ctx, "SELECT endpoint_id FROM endpoint_aliases WHERE alias=?", alias).Scan(&prior)
			if err == nil && prior != id {
				return errors.New("endpoint alias already belongs to another physical resource")
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if errors.Is(err, sql.ErrNoRows) {
				if _, err = tx.ExecContext(ctx, "INSERT INTO endpoint_aliases VALUES(?,?)", alias, id); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (o *Owner) Enqueue(ctx context.Context, operation, project, run, endpoint string) (Ticket, error) {
	var ticket Ticket
	for _, id := range []string{operation, project, run, endpoint} {
		if !store.SafeID(id) {
			return ticket, errors.New("invalid queue identity")
		}
	}
	if err := o.live(); err != nil {
		return ticket, err
	}
	err := o.Coordinator.DB.Write(ctx, func(tx *store.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM workspace_claims WHERE project_id=? AND instance_id=? AND state='active'", project, o.ID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return errors.New("workspace ownership required before endpoint queue")
		}
		var owner, priorProject, priorRun string
		err := tx.QueryRowContext(ctx, "SELECT sequence,endpoint_id,state,instance_id,project_id,run_id FROM queue_tickets WHERE operation_id=?", operation).Scan(&ticket.Sequence, &ticket.Endpoint, &ticket.State, &owner, &priorProject, &priorRun)
		if err == nil {
			if owner != o.ID || priorProject != project || priorRun != run || ticket.Endpoint != endpoint {
				return store.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		r, err := tx.ExecContext(ctx, "INSERT INTO queue_tickets(operation_id,endpoint_id,instance_id,project_id,run_id,enqueued_at,state) VALUES(?,?,?,?,?,?,'waiting')", operation, endpoint, o.ID, project, run, store.Now())
		if err != nil {
			return err
		}
		ticket.Sequence, _ = r.LastInsertId()
		ticket.Endpoint = endpoint
		ticket.State = "waiting"
		return nil
	})
	return ticket, err
}
func (o *Owner) Reserve(ctx context.Context, ticket Ticket) (Ticket, error) {
	if err := o.live(); err != nil {
		return ticket, err
	}
	err := o.Coordinator.DB.Write(ctx, func(tx *store.Tx) error {
		var state, owner, endpoint, project string
		if err := tx.QueryRowContext(ctx, "SELECT state,instance_id,endpoint_id,project_id FROM queue_tickets WHERE sequence=?", ticket.Sequence).Scan(&state, &owner, &endpoint, &project); err != nil {
			return err
		}
		if owner != o.ID || endpoint != ticket.Endpoint || state != "waiting" {
			return errors.New("stale or foreign ticket")
		}
		var owned int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM workspace_claims WHERE instance_id=? AND project_id=? AND state='active'", o.ID, project).Scan(&owned); err != nil {
			return err
		}
		if owned == 0 {
			return ErrBusy
		}
		var first int64
		if err := tx.QueryRowContext(ctx, "SELECT min(sequence) FROM queue_tickets WHERE endpoint_id=? AND state='waiting'", endpoint).Scan(&first); err != nil {
			return err
		}
		if first != ticket.Sequence {
			return ErrWaiting
		}
		var capacity int
		if err := tx.QueryRowContext(ctx, "SELECT capacity FROM endpoints WHERE id=?", endpoint).Scan(&capacity); err != nil {
			return err
		}
		slot := -1
		for n := 0; n < capacity; n++ {
			var used int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM endpoint_slots WHERE endpoint_id=? AND slot_number=?", endpoint, n).Scan(&used); err != nil {
				return err
			}
			if used == 0 {
				slot = n
				break
			}
		}
		if slot < 0 {
			return ErrWaiting
		}
		r, err := tx.ExecContext(ctx, "INSERT INTO fencing_tokens DEFAULT VALUES")
		if err != nil {
			return err
		}
		ticket.Generation, _ = r.LastInsertId()
		if _, err = tx.ExecContext(ctx, "INSERT INTO endpoint_slots VALUES(?,?,?,?,'reserved')", endpoint, slot, ticket.Sequence, ticket.Generation); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE queue_tickets SET state='reserved' WHERE sequence=?", ticket.Sequence)
		ticket.State = "reserved"
		return err
	})
	return ticket, err
}
func (o *Owner) FinishTicket(ctx context.Context, ticket Ticket, proof string) error {
	if err := o.live(); err != nil {
		return err
	}
	if proof != "contained_stopped" && proof != "never_started" {
		return errors.New("inference shutdown proof required")
	}
	return o.Coordinator.DB.Write(ctx, func(tx *store.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM queue_tickets WHERE sequence=? AND instance_id=? AND endpoint_id=?", ticket.Sequence, o.ID, ticket.Endpoint).Scan(&state); err != nil {
			return err
		}
		if state == "waiting" {
			_, err := tx.ExecContext(ctx, "UPDATE queue_tickets SET state='cancelled' WHERE sequence=?", ticket.Sequence)
			return err
		}
		if state != "reserved" {
			return errors.New("ticket not releasable")
		}
		r, err := tx.ExecContext(ctx, "DELETE FROM endpoint_slots WHERE ticket=? AND generation=? AND state!='quarantined'", ticket.Sequence, ticket.Generation)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return errors.New("stale slot generation")
		}
		_, err = tx.ExecContext(ctx, "UPDATE queue_tickets SET state='released' WHERE sequence=?", ticket.Sequence)
		return err
	})
}
func (c *Coordinator) Reconcile(ctx context.Context, owner, observation string) error {
	if len(strings.TrimSpace(observation)) < 12 || len(observation) > 4096 {
		return errors.New("explicit human observation of stopped writers and inference required")
	}
	alive, err := c.ownerAlive(owner)
	if err != nil {
		return err
	}
	if alive {
		return errors.New("owner is still active")
	}
	return c.DB.Write(ctx, func(tx *store.Tx) error {
		// A generated instance ID is never reused; reacquisition needs a new fencing generation.
		if _, err := tx.ExecContext(ctx, "DELETE FROM endpoint_slots WHERE ticket IN(SELECT sequence FROM queue_tickets WHERE instance_id=?)", owner); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE queue_tickets SET state='released' WHERE instance_id=? AND state IN('waiting','reserved','quarantined')", owner); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_claims WHERE instance_id=?", owner); err != nil {
			return err
		}
		raw, _ := json.Marshal(map[string]string{"actor": "human", "observation": observation})
		_, err := tx.ExecContext(ctx, "INSERT INTO reconciliation_log(operation_id,instance_id,observed_at,evidence_json) VALUES(?,?,?,?)", store.ID(), owner, store.Now(), string(raw))
		return err
	})
}
func (c *Coordinator) Status(ctx context.Context) (map[string]any, error) {
	result := map[string]any{"host_authority": Host(), "cross_host_capacity": false}
	tx, err := c.DB.SQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, table := range []string{"workspace_claims", "queue_tickets", "endpoint_slots"} {
		rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT state,count(*) FROM %s GROUP BY state", table))
		if err != nil {
			return nil, err
		}
		counts := map[string]int{}
		for rows.Next() {
			var state string
			var n int
			if err = rows.Scan(&state, &n); err != nil {
				rows.Close()
				return nil, err
			}
			counts[state] = n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		result[table] = counts
	}
	// Bounded identifiers make quarantine actionable without exposing raw state.
	rows, err := tx.QueryContext(ctx, "SELECT id,instance_id,project_id,canonical_root,generation,state,coalesce(quarantine_reason,'') FROM workspace_claims ORDER BY acquired_at,id LIMIT 101")
	if err != nil {
		return nil, err
	}
	claims := []map[string]any{}
	for rows.Next() {
		var id, owner, project, root, state, reason string
		var generation int64
		if err := rows.Scan(&id, &owner, &project, &root, &generation, &state, &reason); err != nil {
			rows.Close()
			return nil, err
		}
		claims = append(claims, map[string]any{"id": id, "owner_id": owner, "project_id": project, "root": root, "generation": generation, "state": state, "reason": reason})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result["claims_truncated"] = len(claims) > 100
	if len(claims) > 100 {
		claims = claims[:100]
	}
	result["claims"] = claims
	return result, nil
}
