package boundary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnixProviderBridge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.sock")
	l, err := ListenRelaySocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if duplicate, err := ListenRelaySocket(path); err == nil {
		duplicate.Close()
		t.Fatal("existing socket replaced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := NewRelay(ctx, "http://127.0.0.1:1/v1", "selected", relayTestToken, "private-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	finished := make(chan error, 1)
	go func() { finished <- ServeProvider(ctx, l, p) }()
	bridge := httptest.NewServer(ProviderBridge(path))
	defer bridge.Close()
	req, _ := http.NewRequest("GET", bridge.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+relayTestToken)
	response, err := bridge.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), "selected") {
		t.Fatal(response.StatusCode, string(body), err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("socket server did not stop")
	}
}

func TestDockerWorkerUnixRelay(t *testing.T) {
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" {
		t.Skip("explicit VIGIL_TEST_DOCKER=1 required")
	}
	const image = "alpine@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer host-only-fixture-key" {
			t.Error("provider credential missing")
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"relay-ok"}}]}`)
	}))
	defer upstream.Close()
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
	binary := filepath.Join(base, "worker")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, "../../cmd/vigil-worker")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build worker: %v: %s", err, b)
	}
	socket := filepath.Join(base, "provider.sock")
	var l net.Listener
	var err error
	if runtime.GOOS == "darwin" {
		l, err = net.Listen("tcp4", "127.0.0.1:0")
	} else {
		l, err = ListenRelaySocket(socket)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := NewRelay(ctx, upstream.URL+"/v1", "selected", relayTestToken, "host-only-fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	done := make(chan error, 1)
	go func() { done <- ServeProvider(ctx, l, p) }()
	socketMount := "type=bind,src=" + socket + ",dst=/relay/provider.sock,readonly"
	if runtime.GOOS == "darwin" {
		control := filepath.Join(base, "control")
		if err := os.Mkdir(control, 0755); err != nil {
			t.Fatal(err)
		}
		lease, _ := json.Marshal(Lease{RunID: "host-route", ExpiresAt: time.Now().Add(9 * time.Second).UnixMilli()})
		if err := os.WriteFile(filepath.Join(control, "lease.json"), lease, 0444); err != nil {
			t.Fatal(err)
		}
		socketMount = vmHostRelayMount(t, ctx, control, "host-route", "30s", "selected", relayTestToken, l.Addr().String())
	}
	script := `set -eu
test "$(ls /sys/class/net)" = lo
wget -q -T 5 -O - --header="Authorization: Bearer $OPENAI_API_KEY" --post-data='{"model":"selected","messages":[]}' "$OPENAI_BASE_URL/chat/completions"
if wget -q -T 5 -O /dev/null --header="Authorization: Bearer $OPENAI_API_KEY" --post-data='{"model":"other"}' "$OPENAI_BASE_URL/chat/completions" 2>/dev/null; then exit 50; fi`
	id := docker("create", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=128m", "--cpus=0.5", "--restart=no", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--env", "OPENAI_API_KEY="+relayTestToken, "--mount", "type=bind,src="+binary+",dst=/worker,readonly", "--mount", socketMount, image, "/worker", "--socket", "/relay/provider.sock", "--", "sh", "-c", script)
	t.Cleanup(func() { docker("rm", "-f", id) })
	out := docker("start", "-a", id)
	if code := docker("inspect", "--format", "{{.State.ExitCode}}", id); code != "0" {
		t.Fatal("worker failed", code, out)
	}
	if !strings.Contains(out, "relay-ok") || strings.Contains(out, "host-only-fixture-key") || calls.Load() != 1 {
		t.Fatal("worker relay boundary failed", out, calls.Load())
	}
	p.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay server not stopped")
	}
}
