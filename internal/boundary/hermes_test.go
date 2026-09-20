package boundary

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"vigil/internal/harness"
)

// Explicit image ID is required: changing the worker image invalidates this
// evidence. Metadata tests start the real gateway but submit no model prompt.
func hermesImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv("VIGIL_HERMES_IMAGE")
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" || image == "" {
		t.Skip("explicit Docker opt-in and VIGIL_HERMES_IMAGE required")
	}
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 || os.Getuid() == 0 {
		t.Fatal("immutable image ID and non-root host required")
	}
	return image
}
func TestDockerHermesMetadataAndLeaseLoss(t *testing.T) {
	image := hermesImage(t)
	for _, mode := range []string{"normal_close", "lease_loss"} {
		t.Run(mode, func(t *testing.T) { containedHermesProbe(t, image, mode) })
	}
}
func TestDockerHermesLive(t *testing.T) {
	if os.Getenv("VIGIL_TEST_LIVE_HERMES") != "1" {
		t.Skip("explicit VIGIL_TEST_LIVE_HERMES=1 required; starts one local model turn")
	}
	containedHermesProbe(t, hermesImage(t), "live")
}
func containedHermesProbe(t *testing.T, image, mode string) {
	model, upstream, key, token, limit := "selected", "http://127.0.0.1:1/v1", "unused-fixture-provider-key", relayTestToken, "45s"
	if mode == "live" {
		model = "qwen3.8-27b-local"
		upstream = os.Getenv("OPENAI_BASE_URL")
		key = os.Getenv("OPENAI_API_KEY")
		token = rand.Text() + rand.Text()
		limit = "210s"
		u, err := url.Parse(upstream)
		if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Port() != "8080" || u.Path != "/v1" || u.User != nil || u.RawQuery != "" || key == "" {
			t.Fatal("explicit existing local llama endpoint/key required")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		c, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		b, err := exec.CommandContext(c, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s failed: %v: %s", args[0], err, b)
		}
		return strings.TrimSpace(string(b))
	}
	if source := docker("image", "inspect", "--format", "{{index .Config.Labels \"vigil.hermes-source\"}}", image); source != "6a627e6eb38e28ac421d5ad8df3f676e49d0c287" {
		t.Fatal("unrecognized source image", source)
	}
	base := t.TempDir()
	if mode == "live" {
		parent, err := filepath.Abs("../../.cache/boundary")
		if err != nil {
			t.Fatal(err)
		}
		base, err = os.MkdirTemp(parent, "live-hermes-")
		if err != nil {
			t.Fatal(err)
		}
		t.Log("private live evidence: " + base)
	}
	control, work, native := filepath.Join(base, "control"), filepath.Join(base, "work"), filepath.Join(base, "native")
	for _, p := range []string{control, work, native} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(work, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "message.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	renew := func() error {
		b, _ := json.Marshal(Lease{RunID: mode, ExpiresAt: time.Now().Add(9 * time.Second).UnixMilli()})
		temp := filepath.Join(control, "next")
		if err := os.WriteFile(temp, b, 0444); err != nil {
			return err
		}
		return os.Rename(temp, filepath.Join(control, "lease.json"))
	}
	if err := renew(); err != nil {
		t.Fatal(err)
	}
	stopLease := make(chan struct{})
	leaseDone := make(chan struct{})
	var once sync.Once
	stopRenewal := func() { once.Do(func() { close(stopLease) }); <-leaseDone }
	defer stopRenewal()
	go func() {
		defer close(leaseDone)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopLease:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if renew() != nil {
					return
				}
			}
		}
	}()
	socket := filepath.Join(base, "provider.sock")
	var listener net.Listener
	var err error
	if runtime.GOOS == "darwin" {
		listener, err = net.Listen("tcp4", "127.0.0.1:0")
	} else {
		listener, err = ListenRelaySocket(socket)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	relay, err := NewRelay(ctx, upstream, model, token, key)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	go ServeProvider(ctx, listener, relay)
	socketMount := "type=bind,src=" + socket + ",dst=/relay/provider.sock,readonly"
	if runtime.GOOS == "darwin" {
		socketMount = vmHostRelayMount(t, ctx, control, mode, limit, model, token, listener.Addr().String())
	}
	template, err := filepath.Abs("../../config/spike/hermes.json")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := `import json, os, pathlib, runpy
from hermes_cli.config_defaults import DEFAULT_CONFIG
c=json.load(open('/instructions/hermes.json'))
c['model'].update(default=os.environ['VIGIL_TEST_MODEL'],provider='custom',base_url=os.environ['OPENAI_BASE_URL'],api_key=os.environ['OPENAI_API_KEY'],context_length=131072)
c['terminal']['cwd']='/work'
for name,value in DEFAULT_CONFIG['auxiliary'].items():
    if isinstance(value,dict):
        c['auxiliary'].setdefault(name,{}).update(provider='custom',model=os.environ['VIGIL_TEST_MODEL'],base_url=os.environ['OPENAI_BASE_URL'],api_key=os.environ['OPENAI_API_KEY'],fallback_chain=[],timeout=10)
home=pathlib.Path(os.environ['HERMES_HOME']);home.mkdir(parents=True,exist_ok=True)
(home/'config.yaml').write_text(json.dumps(c))
runpy.run_module('tui_gateway.entry',run_name='__main__')`
	id := docker("create", "-i", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--cap-add=SETUID", "--cap-add=SETGID", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=512m", "--cpus=1", "--restart=no", "--user=0:0", "--tmpfs", "/tmp:rw,nosuid,nodev,size=32m,mode=1777", "--mount", "type=bind,src="+control+",dst=/control,readonly,bind-recursive=disabled", "--mount", "type=bind,src="+work+",dst=/work,bind-recursive=disabled", "--mount", "type=bind,src="+filepath.Join(work, ".git")+",dst=/work/.git,readonly,bind-recursive=readonly,bind-propagation=rprivate", "--mount", "type=bind,src="+native+",dst=/native,bind-recursive=disabled", "--mount", socketMount, "--mount", "type=bind,src="+template+",dst=/instructions/hermes.json,readonly", "--env", "OPENAI_API_KEY="+token, "--env", "VIGIL_TEST_MODEL="+model, "--env", "HERMES_TUI_TOOLSETS=file,terminal,clarify", "--env", "HERMES_TUI_CHECKPOINTS=false", "--env", "HERMES_TUI_GATEWAY_SHUTDOWN_GRACE_S=1", "--env", "GIT_CONFIG_NOSYSTEM=1", "--env", "GIT_CONFIG_GLOBAL=/dev/null", image, "--run-id", mode, "--uid", fmt.Sprint(os.Getuid()), "--gid", fmt.Sprint(os.Getgid()), "--limit", limit, "--", "/opt/vigil/vigil-worker", "--", "/opt/hermes/.venv/bin/python", "-c", bootstrap)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if t.Failed() && mode != "live" {
			b, _ := exec.CommandContext(cleanup, "docker", "logs", "--tail", "1000", id).CombinedOutput()
			var diagnostics strings.Builder
			for _, line := range strings.Split(string(b), "\n") {
				if (!strings.HasPrefix(line, "{") || strings.Contains(line, `"error"`) || strings.Contains(line, "failed")) && diagnostics.Len() < 8000 {
					diagnostics.WriteString(line + "\n")
				}
			}
			t.Log("fixture startup diagnostics: " + diagnostics.String())
		}
		if b, err := exec.CommandContext(cleanup, "docker", "rm", "-f", id).CombinedOutput(); err != nil {
			t.Errorf("cleanup %s failed: %v: %s", id, err, b)
		}
	})
	transport, err := harness.Start(ctx, harness.ProcessConfig{Argv: []string{"docker", "start", "-a", "-i", id}, Env: os.Environ(), JSONRPC: true, FrameBytes: 1 << 20, TrafficBytes: 16 << 20, QueueSize: 256, PendingLimit: 8, RPCTimeout: 15 * time.Second, Grace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	session, err := harness.NewSession("hermes", transport, harness.Profile{Model: model, Provider: "custom", Workspace: "/work", NativeParams: map[string]any{"cwd": "/work", "model": model, "provider": "custom", "close_on_disconnect": true}}, "probe", "generation", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	go func() {
		for event := range session.Events() {
			if event.Request != nil && event.Kind != "request_resolved" {
				if session.Answer(ctx, *event.Request, harness.Answer{Decision: "deny"}) != nil {
					session.Interrupt(ctx)
				}
			}
		}
	}()
	if err = session.Probe(ctx); err != nil {
		t.Fatalf("contained gateway probe: %v (stderr bytes %d)", err, transport.StderrBytes())
	}
	createCtx, stopCreate := context.WithTimeout(ctx, 12*time.Second)
	defer stopCreate()
	if err = session.Create(createCtx); err != nil {
		t.Fatalf("contained session create: %v", err)
	}
	if s := session.Inspect(); s.RuntimeID == "" || s.Workspace != "/work" {
		t.Fatal("missing native identity", s)
	}
	if relay.Status().Started != 0 {
		t.Fatal("metadata unexpectedly triggered inference")
	}
	if mode == "live" {
		defer func() {
			report := map[string]any{"image_id": image, "model": model, "snapshot": session.Inspect(), "relay": relay.Status(), "test_failed": t.Failed(), "production_eligible": false}
			b, _ := json.MarshalIndent(report, "", "  ")
			os.WriteFile(filepath.Join(base, "report.json"), b, 0600)
		}()
		prompt := "In /work, use the file or terminal tool to replace message.txt with exactly the single line: contained-hermes-ok followed by a newline. Do not modify any other file. Do not run Git or install anything. Return only this JSON object after the edit: {\"summary\":\"done\",\"files\":[\"message.txt\"]}"
		if err = session.Submit(ctx, "one-live-turn", prompt, nil); err != nil {
			t.Fatal("single submit failed; no replay", err)
		}
		for {
			snapshot := session.Inspect()
			if snapshot.Outcome != "active" {
				if snapshot.Outcome != "completed" {
					t.Fatal("native turn did not complete", snapshot.Outcome)
				}
				break
			}
			select {
			case <-session.Changed():
			case <-ctx.Done():
				t.Fatal("live deadline; no replay")
			}
		}
		if err = session.Err(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(work, "message.txt"))
		if err != nil || string(b) != "contained-hermes-ok\n" {
			t.Fatal("actual fixture edit missing", err)
		}
		var result struct {
			Summary string   `json:"summary"`
			Files   []string `json:"files"`
		}
		if json.Unmarshal([]byte(session.Inspect().Output), &result) != nil || result.Summary != "done" || len(result.Files) != 1 || result.Files[0] != "message.txt" {
			t.Fatal("invalid native result")
		}
		if s := relay.Status(); s.Active || s.Completed == 0 || s.Aborted != 0 {
			t.Fatal("unreconciled provider transport", s)
		}
	}
	docker("exec", "-d", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), id, "setsid", "sh", "-c", `while :; do echo tick >> /work/heartbeat; sleep 0.1; done`)
	deadline := time.Now().Add(3 * time.Second)
	heartbeat := filepath.Join(work, "heartbeat")
	for {
		if b, err := os.ReadFile(heartbeat); err == nil && len(b) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer not started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if mode != "lease_loss" {
		session.Close()
	} else {
		stopRenewal()
	}
	docker("wait", id)
	if state := docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", id); state != "false 0" {
		t.Fatal("gateway boundary still active", state)
	}
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil || string(before) != string(after) {
		t.Fatal("writer survived gateway boundary", err)
	}
	if mode == "lease_loss" && !strings.Contains(docker("logs", id), "controller lease lost") {
		t.Fatal("lease loss was not shutdown cause")
	}
	if mode == "live" {
		t.Log("one live contained Hermes edit, structured result and writer cleanup passed")
	} else {
		t.Log("real Hermes metadata and namespace writer cleanup passed without inference")
	}
}
