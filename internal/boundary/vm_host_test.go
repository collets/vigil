package boundary

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// OrbStack host route: a VM relay holds only a run token, and forwards to the
// credential-holding host relay. The actual provider key never enters Docker.
// This is an opt-in experiment, not a persisted production launcher.
func vmHostRelayMount(t *testing.T, ctx context.Context, control, runID, limit, model, token, hostAddress string) string {
	t.Helper()
	const image = "alpine@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0"
	docker := func(args ...string) string {
		t.Helper()
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		b, err := exec.CommandContext(c, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("VM host relay %s failed: %v: %s", args[0], err, b)
		}
		return strings.TrimSpace(string(b))
	}
	base := t.TempDir()
	arch := docker("version", "--format", "{{.Server.Arch}}")
	if arch != "amd64" && arch != "arm64" {
		t.Fatal("unsupported engine architecture")
	}
	for _, name := range []string{"guardian", "relay"} {
		build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", filepath.Join(base, name), "../../cmd/vigil-"+name)
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
		if b, err := build.CombinedOutput(); err != nil {
			t.Fatalf("sidecar build: %v: %s", err, b)
		}
	}
	volume := docker("volume", "create", "--label=vigil.probe=stage5")
	t.Cleanup(func() { docker("volume", "rm", volume) })
	user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	docker("run", "--rm", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--cap-add=CHOWN", "--security-opt=no-new-privileges", "--mount", "type=volume,src="+volume+",dst=/relay", image, "chown", user, "/relay")
	id := docker("create", "-i", "--pull=never", "--label=vigil.probe=stage5", "--network=bridge", "--read-only", "--cap-drop=ALL", "--cap-add=SETUID", "--cap-add=SETGID", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=128m", "--cpus=0.5", "--restart=no", "--mount", "type=bind,src="+filepath.Join(base, "guardian")+",dst=/guardian,readonly", "--mount", "type=bind,src="+filepath.Join(base, "relay")+",dst=/relay-bin,readonly", "--mount", "type=bind,src="+control+",dst=/control,readonly", "--mount", "type=volume,src="+volume+",dst=/relay", image, "/guardian", "--run-id", runID, "--uid", fmt.Sprint(os.Getuid()), "--gid", fmt.Sprint(os.Getgid()), "--limit", limit, "--", "/relay-bin", "--limit", limit)
	t.Cleanup(func() { docker("rm", "-f", id) })
	_, port, err := net.SplitHostPort(hostAddress)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, _ := json.Marshal(map[string]string{"upstream": "http://host.docker.internal:" + port + "/v1", "model": model, "run_token": token, "provider_key": token})
	attach := exec.CommandContext(ctx, "docker", "start", "-a", "-i", id)
	attach.Stdin = bytes.NewReader(bootstrap)
	stdout, err := attach.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Relay errors are fixed diagnostics; bootstrap values are never printed.
	var stderr bytes.Buffer
	attach.Stderr = &stderr
	if err := attach.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		scanner := bufio.NewScanner(stdout)
		ready <- scanner.Scan() && scanner.Text() == `{"ready":true}`
		io.Copy(io.Discard, stdout)
	}()
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		exec.CommandContext(c, "docker", "kill", id).Run()
		<-drained
		attach.Wait()
		if state := docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", id); state != "false 0" {
			t.Error("sidecar namespace still active", state)
		}
	})
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("VM relay did not become ready")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("VM relay readiness timed out")
	}
	return "type=volume,src=" + volume + ",dst=/relay,readonly"
}
