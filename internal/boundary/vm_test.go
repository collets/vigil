package boundary

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Invoked only in the disposable upstream container. It performs no inference.
func TestVMProviderFixture(t *testing.T) {
	if os.Getenv("VIGIL_VM_PROVIDER_FIXTURE") != "1" {
		t.Skip("container helper")
	}
	var calls atomic.Int32
	server := &http.Server{Addr: ":8080", ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/calls" {
			fmt.Fprint(w, calls.Load())
			return
		}
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer vm-private-fixture-key" {
			http.Error(w, "denied", 403)
			return
		}
		io.Copy(io.Discard, io.LimitReader(r.Body, 8192))
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"vm-relay-ok"}}]}`)
	})}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(90 * time.Second):
		}
		server.Close()
	}()
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

// This topology avoids mounting a macOS socket into Linux. Provider and relay
// share an internal Docker network; the worker shares only a read-only volume.
func TestDockerVMRelay(t *testing.T) {
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" {
		t.Skip("explicit Docker opt-in required")
	}
	const image = "alpine@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		c, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		b, err := exec.CommandContext(c, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v: %s", args[0], err, b)
		}
		return strings.TrimSpace(string(b))
	}
	arch := docker("version", "--format", "{{.Server.Arch}}")
	if arch != "amd64" && arch != "arm64" {
		t.Fatal("unsupported engine architecture")
	}
	base := t.TempDir()
	for _, name := range []string{"worker", "relay", "guardian", "fixture"} {
		args := []string{"build", "-o", filepath.Join(base, name), "../../cmd/vigil-" + name}
		if name == "fixture" {
			args = []string{"test", "-c", "-o", filepath.Join(base, name), "."}
		}
		build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
		if b, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v: %s", name, err, b)
		}
	}
	uid, gid := os.Getuid(), os.Getgid()
	if uid == 0 || gid == 0 {
		t.Fatal("non-root test owner required")
	}
	user := fmt.Sprintf("%d:%d", uid, gid)
	volume := docker("volume", "create", "--label=vigil.probe=stage5")
	t.Cleanup(func() { docker("volume", "rm", volume) })
	network := "vigil-probe-" + filepath.Base(base) + fmt.Sprint(time.Now().UnixNano())
	docker("network", "create", "--internal", "--label=vigil.probe=stage5", network)
	t.Cleanup(func() { docker("network", "rm", network) })
	docker("run", "--rm", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--cap-add=CHOWN", "--security-opt=no-new-privileges", "--mount", "type=volume,src="+volume+",dst=/relay", image, "chown", user, "/relay")
	common := []string{"create", "--pull=never", "--label=vigil.probe=stage5", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=128m", "--cpus=0.5", "--restart=no"}
	create := func(args ...string) string {
		t.Helper()
		id := docker(append(append([]string{}, common...), args...)...)
		t.Cleanup(func() { docker("rm", "-f", id) })
		return id
	}
	bind := func(name string) string {
		return "type=bind,src=" + filepath.Join(base, name) + ",dst=/" + name + ",readonly"
	}
	provider := create("--network", network, "--network-alias=provider", "--user", user, "--env=VIGIL_VM_PROVIDER_FIXTURE=1", "--mount", bind("fixture"), image, "/fixture", "-test.run=^TestVMProviderFixture$")
	docker("start", provider)
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := exec.CommandContext(ctx, "docker", "exec", provider, "wget", "-q", "-T", "1", "-O", "-", "http://127.0.0.1:8080/calls").CombinedOutput()
		if err == nil && string(b) == "0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("synthetic provider did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	control := filepath.Join(base, "control")
	// Root guardian has no DAC override; lease is public within this private
	// fixture, mounted read-only, and carries no provider credential.
	if err := os.Mkdir(control, 0755); err != nil {
		t.Fatal(err)
	}
	lease := func() {
		b, _ := json.Marshal(Lease{RunID: "vm-probe", ExpiresAt: time.Now().Add(9 * time.Second).UnixMilli()})
		if err := os.WriteFile(filepath.Join(control, "next"), b, 0444); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(control, "next"), filepath.Join(control, "lease.json")); err != nil {
			t.Fatal(err)
		}
	}
	lease()
	relay := create("--interactive", "--network", network, "--cap-add=SETUID", "--cap-add=SETGID", "--mount", bind("relay"), "--mount", bind("guardian"), "--mount", "type=bind,src="+control+",dst=/control,readonly", "--mount", "type=volume,src="+volume+",dst=/relay-volume", image, "/guardian", "--run-id=vm-probe", "--uid", fmt.Sprint(uid), "--gid", fmt.Sprint(gid), "--limit=60s", "--", "/relay", "--socket=/relay-volume/provider.sock", "--limit=60s")
	bootstrap, _ := json.Marshal(map[string]string{"upstream": "http://provider:8080/v1", "model": "selected", "run_token": relayTestToken, "provider_key": "vm-private-fixture-key"})
	attach := exec.CommandContext(ctx, "docker", "start", "-a", "-i", relay)
	attach.Stdin = bytes.NewReader(bootstrap)
	stdout, err := attach.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	attach.Stderr = &stderr
	if err := attach.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		ready <- scanner.Scan() && scanner.Text() == `{"ready":true}`
		io.Copy(io.Discard, stdout)
	}()
	waited := false
	t.Cleanup(func() {
		if !waited {
			exec.Command("docker", "kill", relay).Run()
			attach.Wait()
		}
	})
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("relay did not become ready")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay startup timed out")
	}
	lease()
	metadata := docker("inspect", relay)
	if strings.Contains(metadata, "vm-private-fixture-key") {
		t.Fatal("credential leaked into container metadata")
	}
	script := `set -eu
test "$(ls /sys/class/net)" = lo
test "$(ls -A /relay-volume)" = provider.sock
if touch /relay-volume/unwanted 2>/dev/null; then exit 21; fi
test ! -e /var/run/docker.sock
test ! -e /control/lease.json
wget -q -T 3 -O - --header="Authorization: Bearer $OPENAI_API_KEY" --post-data='{"model":"selected","messages":[]}' "$OPENAI_BASE_URL/chat/completions"
if wget -q -T 3 -O /dev/null --header="Authorization: Bearer $OPENAI_API_KEY" --post-data='{"model":"other"}' "$OPENAI_BASE_URL/chat/completions" 2>/dev/null; then exit 22; fi`
	worker := create("--network=none", "--user", user, "--env", "OPENAI_API_KEY="+relayTestToken, "--mount", bind("worker"), "--mount", "type=volume,src="+volume+",dst=/relay-volume,readonly", image, "/worker", "--socket=/relay-volume/provider.sock", "--", "sh", "-c", script)
	out := docker("start", "-a", worker)
	if !strings.Contains(out, "vm-relay-ok") || strings.Contains(out, "vm-private-fixture-key") {
		t.Fatal("worker response mismatch or credential disclosure")
	}
	if got := docker("exec", provider, "wget", "-q", "-O", "-", "http://127.0.0.1:8080/calls"); got != "1" {
		t.Fatal("wrong-model call reached provider", got)
	}
	// No further renewals: an absent controller must revoke the VM relay too.
	if err := attach.Wait(); err == nil {
		t.Fatal("expected guardian lease-loss exit")
	}
	waited = true
	if got := docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", relay); got != "false 0" {
		t.Fatal("relay namespace survived lease loss", got)
	}
	retry := exec.CommandContext(ctx, "docker", "start", "-a", worker)
	if b, err := retry.CombinedOutput(); err == nil || strings.Contains(string(b), "vm-relay-ok") {
		t.Fatal("worker reached provider after relay lease expired")
	}
	t.Log("VM socket volume, stdin credential handoff, scoped inference and lease-loss revocation passed; no real inference")
}
