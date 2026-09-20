package boundary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This is a synthetic prerequisite probe, not harness/profile qualification.
// No host credentials, sockets, model traffic or user checkout are exposed.
func TestDockerIsolationPrerequisites(t *testing.T) {
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" {
		t.Skip("explicit VIGIL_TEST_DOCKER=1 required")
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
	root := t.TempDir()
	git := filepath.Join(root, ".git")
	if err := os.Mkdir(git, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(git, "config"), filepath.Join(root, "AGENTS.md")} {
		if err := os.WriteFile(path, []byte("protected\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"create", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=64m", "--cpus=0.5", "--restart=no", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=8m,mode=1777", "--mount", "type=bind,src=" + root + ",dst=/work,bind-recursive=disabled", "--mount", "type=bind,src=" + git + ",dst=/work/.git,readonly,bind-recursive=readonly,bind-propagation=rprivate", "--mount", "type=bind,src=" + filepath.Join(root, "AGENTS.md") + ",dst=/work/AGENTS.md,readonly", "--workdir=/work", image, "sleep", "300"}
	id := docker(args...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if b, err := exec.CommandContext(ctx, "docker", "rm", "-f", id).CombinedOutput(); err != nil {
			t.Errorf("probe cleanup failed; container %s: %v: %s", id, err, b)
		}
	})
	docker("start", id)
	probe := `set -eu
printf 'editable\n' > /work/result
if (echo bypass >> /work/.git/config) 2>/dev/null; then exit 10; fi
if rm /work/.git/config 2>/dev/null; then exit 11; fi
if mv /work/.git /work/renamed 2>/dev/null; then exit 12; fi
if (echo bypass > /work/AGENTS.md) 2>/dev/null; then exit 13; fi
if rm /work/AGENTS.md 2>/dev/null; then exit 14; fi
test ! -S /var/run/docker.sock
test ! -e /root/.ssh
test ! -e /root/.codex/auth.json
test ! -e /host
test ! -e /proc/1/root/home/scoletta
if mount -o remount,rw /work/.git 2>/dev/null; then exit 15; fi
if wget -T 2 -q -O /tmp/network http://1.1.1.1 2>/dev/null; then exit 16; fi
if wget -T 2 -q -O /tmp/network http://127.0.0.1:8080/health 2>/dev/null; then exit 17; fi
test "$(ls /sys/class/net)" = lo
echo filesystem-and-network-ok`
	if got := docker("exec", id, "sh", "-c", probe); got != "filesystem-and-network-ok" {
		t.Fatal(got)
	}
	// Detached writer survives its launching exec client; whole-container kill
	// must stop it. Controller-loss watchdog and harness tests remain separate.
	docker("exec", "-d", id, "setsid", "sh", "-c", `while :; do echo tick >> /work/heartbeat; sleep 0.1; done`)
	heartbeat := filepath.Join(root, "heartbeat")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, err := os.ReadFile(heartbeat); err == nil && len(b) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	docker("kill", id)
	if got := docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", id); got != "false 0" {
		t.Fatal("boundary still running", got)
	}
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("detached writer survived container kill")
	}
	for _, path := range []string{filepath.Join(git, "config"), filepath.Join(root, "AGENTS.md")} {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != "protected\n" {
			t.Fatal("protected content changed", path, err)
		}
	}
	t.Log("Synthetic filesystem, network and whole-container cleanup probes passed; production eligibility remains false")
}

func TestDockerGuardianLeaseAndDeadline(t *testing.T) {
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" {
		t.Skip("explicit VIGIL_TEST_DOCKER=1 required")
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
	arch := docker("version", "--format", "{{.Server.Arch}}")
	if arch != "amd64" && arch != "arm64" {
		t.Fatal("unsupported engine architecture", arch)
	}
	base := t.TempDir()
	binary := filepath.Join(base, "guardian")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, "../../cmd/vigil-guardian")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build guardian: %v: %s", err, b)
	}
	for _, mode := range []string{"lease_expiry", "hard_deadline", "native_exit"} {
		t.Run(mode, func(t *testing.T) {
			control, work := filepath.Join(base, mode+"-control"), filepath.Join(base, mode+"-work")
			for _, p := range []string{control, work} {
				if err := os.Mkdir(p, 0755); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(Lease{RunID: mode, ExpiresAt: time.Now().Add(9 * time.Second).UnixMilli()})
			if err := os.WriteFile(filepath.Join(control, "lease.json"), raw, 0444); err != nil {
				t.Fatal(err)
			}
			limit := "20s"
			if mode == "hard_deadline" {
				limit = "2s"
			}
			script := `set -eu
test "$(id -u)" != 0
if kill -STOP 1 2>/dev/null; then exit 40; fi
if (echo forged > /control/lease.json) 2>/dev/null; then exit 41; fi
echo guarded > /work/ready
setsid sh -c 'while :; do echo tick >> /work/heartbeat; sleep 0.1; done' </dev/null >/dev/null 2>&1 &
sleep 60`
			if mode == "native_exit" {
				script = strings.TrimSuffix(script, "sleep 60") + "sleep 0.5"
			}
			id := docker("create", "--pull=never", "--label=vigil.probe=stage5", "--read-only", "--network=none", "--cap-drop=ALL", "--cap-add=SETUID", "--cap-add=SETGID", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=128m", "--cpus=0.5", "--restart=no", "--user=0:0", "--mount", "type=bind,src="+binary+",dst=/guardian,readonly", "--mount", "type=bind,src="+control+",dst=/control,readonly,bind-recursive=disabled", "--mount", "type=bind,src="+work+",dst=/work,bind-recursive=disabled", image, "/guardian", "--run-id", mode, "--uid", fmt.Sprint(os.Getuid()), "--gid", fmt.Sprint(os.Getgid()), "--limit", limit, "--", "sh", "-c", script)
			t.Cleanup(func() { docker("rm", "-f", id) })
			docker("start", id)
			code := docker("wait", id)
			logs := docker("logs", id)
			if mode == "lease_expiry" && !strings.Contains(logs, "controller lease lost") {
				t.Fatal("lease shutdown not observed", code, logs)
			}
			if mode == "hard_deadline" && !strings.Contains(logs, "worker hard deadline reached") {
				t.Fatal("deadline shutdown not observed", code, logs)
			}
			if mode == "native_exit" && code != "0" {
				t.Fatal("native completion failed", code, logs)
			}
			if b, err := os.ReadFile(filepath.Join(work, "ready")); err != nil || string(b) != "guarded\n" {
				t.Fatal("UID/lease protection failed", err, logs)
			}
			before, err := os.ReadFile(filepath.Join(work, "heartbeat"))
			if err != nil || len(before) == 0 {
				t.Fatal("writer did not start", err, logs)
			}
			if state := docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", id); state != "false 0" {
				t.Fatal("nonempty boundary", state)
			}
			time.Sleep(300 * time.Millisecond)
			after, err := os.ReadFile(filepath.Join(work, "heartbeat"))
			if err != nil || string(before) != string(after) {
				t.Fatal("writer survived guardian exit", err)
			}
		})
	}
}
