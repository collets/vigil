package boundary

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vigil/internal/coordinator"
	"vigil/internal/workspace"
)

func TestBoundaryControllerHelper(t *testing.T) {
	if os.Getenv("VIGIL_BOUNDARY_CONTROLLER") != "1" {
		t.Skip("disposable controller helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	base := os.Getenv("VIGIL_CRASH_BASE")
	c, err := coordinator.Open(ctx, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.DB.Close()
	owner, err := c.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	root, err := workspace.Inspect(ctx, filepath.Join(base, "work"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Claim(ctx, "fixture", "workspace", []workspace.Identity{root}); err != nil {
		t.Fatal(err)
	}
	ticket, err := owner.Enqueue(ctx, "capacity", "fixture", "crash", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Reserve(ctx, ticket); err != nil {
		t.Fatal(err)
	}
	renew := func() error {
		b, _ := json.Marshal(Lease{RunID: "crash", ExpiresAt: time.Now().Add(9 * time.Second).UnixMilli()})
		if err := os.WriteFile(filepath.Join(base, "control/next"), b, 0444); err != nil {
			return err
		}
		return os.Rename(filepath.Join(base, "control/next"), filepath.Join(base, "control/lease.json"))
	}
	if err := renew(); err != nil {
		t.Fatal(err)
	}
	mode := os.Getenv("VIGIL_CRASH_PHASE")
	if mode != "before_start" {
		if b, err := exec.CommandContext(ctx, "docker", "start", os.Getenv("VIGIL_CRASH_CONTAINER")).CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
		if mode == "after_result" {
			if b, err := exec.CommandContext(ctx, "docker", "wait", os.Getenv("VIGIL_CRASH_CONTAINER")).CombinedOutput(); err != nil || strings.TrimSpace(string(b)) != "0" {
				t.Fatal("fixture result not observed", err, string(b))
			}
		}
		until := time.Now().Add(5 * time.Second)
		for {
			if b, err := os.ReadFile(filepath.Join(base, "work/heartbeat")); err == nil && len(b) > 0 {
				break
			}
			if time.Now().After(until) {
				t.Fatal("writer did not start")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	fmt.Println(owner.ID)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := renew(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// The parent owns only disposable fixture cleanup. The killed child owns the
// actual OS lock, coordinator reservations, Docker start and lease renewal.
func TestDockerControllerCrashQuarantines(t *testing.T) {
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" {
		t.Skip("explicit Docker opt-in required")
	}
	const image = "alpine@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0"
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		b, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v: %s", args[0], err, b)
		}
		return strings.TrimSpace(string(b))
	}
	binary := filepath.Join(t.TempDir(), "guardian")
	arch := docker("version", "--format", "{{.Server.Arch}}")
	if arch != "amd64" && arch != "arm64" {
		t.Fatal("unsupported engine architecture")
	}
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, "../../cmd/vigil-guardian")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	for _, phase := range []string{"before_start", "after_start", "after_result"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			base := t.TempDir()
			for _, name := range []string{"control", "work"} {
				if err := os.Mkdir(filepath.Join(base, name), 0755); err != nil {
					t.Fatal(err)
				}
			}
			c, err := coordinator.Open(ctx, filepath.Join(base, "state"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.DB.Close()
			if err := c.Endpoint(ctx, "fixture-model", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
				t.Fatal(err)
			}
			script := `setsid sh -c 'while :; do echo tick >> /work/heartbeat; sleep 0.1; done' </dev/null >/dev/null 2>&1 &
sleep 30`
			if phase == "after_result" {
				script = strings.TrimSuffix(script, "sleep 30") + "sleep 0.3; echo result > /work/result"
			}
			id := docker("create", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--cap-add=SETUID", "--cap-add=SETGID", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=128m", "--cpus=0.5", "--restart=no", "--mount", "type=bind,src="+binary+",dst=/guardian,readonly", "--mount", "type=bind,src="+filepath.Join(base, "control")+",dst=/control,readonly", "--mount", "type=bind,src="+filepath.Join(base, "work")+",dst=/work", image, "/guardian", "--run-id=crash", "--uid", fmt.Sprint(os.Getuid()), "--gid", fmt.Sprint(os.Getgid()), "--limit=25s", "--", "sh", "-c", script)
			t.Cleanup(func() { docker("rm", "-f", id) })
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBoundaryControllerHelper$")
			child.Env = append(os.Environ(), "VIGIL_BOUNDARY_CONTROLLER=1", "VIGIL_CRASH_BASE="+base, "VIGIL_CRASH_PHASE="+phase, "VIGIL_CRASH_CONTAINER="+id)
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			child.Stderr = os.Stderr
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					child.Process.Kill()
					child.Wait()
				}
			}()
			ready := make(chan string, 1)
			go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
			var owner string
			select {
			case owner = <-ready:
			case <-time.After(10 * time.Second):
				t.Fatal("fixture controller not ready")
			}
			if len(owner) != 32 {
				t.Fatal("invalid fixture controller identity")
			}
			if err = child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			child.Wait()
			waited = true
			if err = c.Reap(ctx); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"workspace_claims", "endpoint_slots"} {
				var state string
				if err = c.DB.SQL.QueryRowContext(ctx, "SELECT state FROM "+table).Scan(&state); err != nil || state != "quarantined" {
					t.Fatal("lost quarantine", table, state, err)
				}
			}
			if phase != "before_start" {
				docker("wait", id)
			}
			if state := docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", id); state != "false 0" {
				t.Fatal("boundary survived", state)
			}
			if phase == "after_start" && !strings.Contains(docker("logs", id), "controller lease lost") {
				t.Fatal("missing actual controller-loss shutdown")
			}
			heartbeat := filepath.Join(base, "work/heartbeat")
			before, err := os.ReadFile(heartbeat)
			if phase == "before_start" {
				if !os.IsNotExist(err) {
					t.Fatal("unstarted container wrote fixture")
				}
			} else {
				if err != nil || len(before) == 0 {
					t.Fatal("heartbeat missing", err)
				}
				time.Sleep(300 * time.Millisecond)
				after, err := os.ReadFile(heartbeat)
				if err != nil || string(after) != string(before) {
					t.Fatal("writer survived controller crash", err)
				}
			}
			// Even proven container cleanup must not implicitly free unknown inference.
			next, err := c.Register(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			root, err := workspace.Inspect(ctx, filepath.Join(base, "work"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = next.Claim(ctx, "next", "new-claim", []workspace.Identity{root}); !errors.Is(err, coordinator.ErrBusy) {
				t.Fatal("new owner bypassed quarantine", err)
			}
			if err := c.Reconcile(ctx, owner, "Disposable probe: engine reports stopped namespace, heartbeat stable, no model request was made"); err != nil {
				t.Fatal(err)
			}
			if _, err = next.Claim(ctx, "next", "new-claim", []workspace.Identity{root}); err != nil {
				t.Fatal("explicit reconciliation failed", err)
			}
		})
	}
}
