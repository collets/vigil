package coordinator

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vigil/internal/store"
	"vigil/internal/workspace"
)

func inspect(t *testing.T, path string) workspace.Identity {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	id, err := workspace.Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestOwnershipAndFIFO(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dir := filepath.Join(base, "state")
	a, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.DB.Close()
	b, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.DB.Close()
	owner1, err := a.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner1.Close()
	owner2, err := b.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner2.Close()
	root1 := inspect(t, filepath.Join(base, "a"))
	root2 := inspect(t, filepath.Join(base, "ab"))
	nested := inspect(t, filepath.Join(base, "a", "nested"))
	claims, err := owner1.Claim(ctx, "p1", "claim1", []workspace.Identity{root1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner1.Claim(ctx, "different-project", "claim1", []workspace.Identity{root1}); !errors.Is(err, store.ErrConflict) {
		t.Fatal("claim replay changed project", err)
	}
	if _, err = owner2.Claim(ctx, "p2", "overlap", []workspace.Identity{root2, nested}); !errors.Is(err, ErrBusy) {
		t.Fatal("overlap admitted", err)
	}
	var count int
	a.DB.SQL.QueryRow("SELECT count(*) FROM workspace_claims").Scan(&count)
	if count != 1 {
		t.Fatal("partial roots claimed")
	}
	claim2, err := owner2.Claim(ctx, "p2", "claim2", []workspace.Identity{root2})
	if err != nil {
		t.Fatal("prefix sibling blocked", err)
	}
	if err = a.Endpoint(ctx, "llama", []string{"http://localhost:8080/v1", "http://[::1]:8080/v1/"}, 1, Host()); err != nil {
		t.Fatal(err)
	}
	if err = b.Endpoint(ctx, "alias-bypass", []string{"http://127.0.0.1:8080/v1"}, 1, Host()); err == nil {
		t.Fatal("physical endpoint alias bypass")
	}
	t1, err := owner1.Enqueue(ctx, "op1", "p1", "r1", "llama")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := owner2.Enqueue(ctx, "op2", "p2", "r2", "llama")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner2.Reserve(ctx, t2); !errors.Is(err, ErrWaiting) {
		t.Fatal("FIFO bypass", err)
	}
	t1, err = owner1.Reserve(ctx, t1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner2.Reserve(ctx, t2); !errors.Is(err, ErrWaiting) {
		t.Fatal("capacity bypass", err)
	}
	if err = owner2.FinishTicket(ctx, t1, "never_started"); err == nil {
		t.Fatal("foreign owner release")
	}
	if err = owner1.FinishTicket(ctx, t1, "never_started"); err != nil {
		t.Fatal(err)
	}
	t2, err = owner2.Reserve(ctx, t2)
	if err != nil {
		t.Fatal(err)
	}
	stale := t2
	stale.Generation++
	if err = owner2.FinishTicket(ctx, stale, "never_started"); err == nil {
		t.Fatal("stale generation released")
	}
	if err = owner2.FinishTicket(ctx, t2, "never_started"); err != nil {
		t.Fatal(err)
	}
	if err = owner1.Release(ctx, claims[0], "native_idle"); err == nil {
		t.Fatal("native idle treated as containment")
	}
	if err = owner1.Release(ctx, claims[0], "never_started"); err != nil {
		t.Fatal(err)
	}
	if err = owner2.Release(ctx, claim2[0], "never_started"); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerCrashHelper(t *testing.T) {
	if os.Getenv("VIGIL_TEST_OWNER") != "1" {
		return
	}
	ctx := context.Background()
	c, err := Open(ctx, os.Getenv("VIGIL_TEST_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := c.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := workspace.Inspect(ctx, os.Getenv("VIGIL_TEST_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.Claim(ctx, "project", "claim", []workspace.Identity{id}); err != nil {
		t.Fatal(err)
	}
	ticket, err := o.Enqueue(ctx, "op", "project", "run", "llama")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.Reserve(ctx, ticket); err != nil {
		t.Fatal(err)
	}
	fmt.Println(o.ID)
	for {
		time.Sleep(time.Hour)
	}
}
func TestCrashRetainsQuarantine(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dir := filepath.Join(base, "state")
	root := inspect(t, filepath.Join(base, "work"))
	c, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.DB.Close()
	if err = c.Endpoint(ctx, "llama", []string{"http://localhost:8080/v1"}, 1, Host()); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestOwnerCrashHelper$")
	child.Env = append(os.Environ(), "VIGIL_TEST_OWNER=1", "VIGIL_TEST_STATE="+dir, "VIGIL_TEST_ROOT="+root.Root)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	var owner string
	select {
	case owner = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("owner startup timed out")
	}
	if len(owner) != 32 {
		t.Fatal("invalid owner output", owner)
	}
	if err = c.Reconcile(ctx, owner, "No worker was started in this fixture"); err == nil {
		t.Fatal("reconciled live owner")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	if err = c.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = c.DB.SQL.QueryRow("SELECT state FROM workspace_claims WHERE instance_id=?", owner).Scan(&state); err != nil || state != "quarantined" {
		t.Fatal("lost workspace quarantine", state, err)
	}
	if err = c.DB.SQL.QueryRow("SELECT state FROM endpoint_slots").Scan(&state); err != nil || state != "quarantined" {
		t.Fatal("lost endpoint quarantine", state, err)
	}
	next, err := c.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = next.Claim(ctx, "next", "claim-next", []workspace.Identity{root}); !errors.Is(err, ErrBusy) {
		t.Fatal("stale lock evicted writer", err)
	}
	if err = c.Reconcile(ctx, owner, "Fixture owner killed; no worker or inference was ever launched"); err != nil {
		t.Fatal(err)
	}
	claims, err := next.Claim(ctx, "next", "claim-next", []workspace.Identity{root})
	if err != nil {
		t.Fatal(err)
	}
	if claims[0].Generation <= 1 {
		t.Fatal("fencing token reused")
	}
}
