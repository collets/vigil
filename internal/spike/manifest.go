// Package spike is a development-only fixture runner, not the workflow engine.
package spike

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vigil/internal/harness"
)

type Limits struct {
	Concurrency int   `json:"managed_concurrency"`
	Turns       int   `json:"turns_per_harness"`
	Repairs     int   `json:"repair_attempts"`
	Replays     int   `json:"infrastructure_replays"`
	Startup     int   `json:"startup_seconds"`
	RPC         int   `json:"rpc_seconds"`
	Active      int   `json:"attempt_active_seconds"`
	Task        int   `json:"task_active_seconds"`
	Wait        int   `json:"request_wait_seconds"`
	Wall        int   `json:"absolute_wall_seconds"`
	Interrupt   int   `json:"interrupt_grace_seconds"`
	Terminate   int   `json:"terminate_grace_seconds"`
	Frame       int   `json:"max_frame_bytes"`
	Evidence    int64 `json:"max_evidence_bytes"`
	Pending     int   `json:"max_pending_requests"`
	Queue       int   `json:"max_queued_events"`
}
type Launch struct {
	Argv          []string          `json:"argv"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"env"`
	AuthFile      string            `json:"auth_file_reference"`
	SecretEnv     string            `json:"secret_env_reference"`
	ThreadStart   map[string]any    `json:"thread_start"`
	SessionCreate map[string]any    `json:"session_create"`
}
type Manifest struct {
	Version   int    `json:"schema_version"`
	Stage     string `json:"stage"`
	Workspace string `json:"workspace"`
	Baseline  string `json:"fixture_baseline"`
	Limits    Limits `json:"limits"`
	Codex     Launch `json:"codex"`
	Hermes    Launch `json:"hermes"`
}
type Preparation struct {
	CodexVersion string            `json:"codex_version"`
	HermesCommit string            `json:"hermes_commit"`
	ManifestHash string            `json:"manifest_sha256"`
	Configs      map[string]string `json:"config_sha256"`
	Sources      map[string]string `json:"source_sha256"`
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("file size limit exceeded")
	}
	return b, err
}
func environment(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
func command(ctx context.Context, cwd string, env []string, argv ...string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = env
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed", filepath.Base(argv[0]))
	}
	return strings.TrimSpace(out.String()), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("command output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func load(path, kind string) (Manifest, Launch, harness.Profile, Preparation, error) {
	var m Manifest
	var p Preparation
	raw, err := readBounded(path, 1<<20)
	if err != nil {
		return m, Launch{}, harness.Profile{}, p, err
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return m, Launch{}, harness.Profile{}, p, err
	}
	parent := filepath.Dir(path)
	proof, err := readBounded(filepath.Join(parent, "evidence/preparation.json"), 1<<20)
	if err != nil {
		return m, Launch{}, harness.Profile{}, p, err
	}
	if err = json.Unmarshal(proof, &p); err != nil {
		return m, Launch{}, harness.Profile{}, p, err
	}
	if m.Version != 1 || m.Stage != "prepared-no-inference" || p.ManifestHash != digest(raw) {
		return m, Launch{}, harness.Profile{}, p, errors.New("manifest missing fresh preparation proof; run prepare.py again")
	}
	canonical, err := filepath.EvalSymlinks(m.Workspace)
	if err != nil || canonical != m.Workspace || m.Workspace != filepath.Join(parent, "fixture") || filepath.Base(filepath.Dir(parent)) != "spike" {
		return m, Launch{}, harness.Profile{}, p, errors.New("workspace must be the prepared disposable fixture")
	}
	if len(p.Configs) < 1 || len(p.Configs) > 2 || p.Configs["hermes-home/config.yaml"] == "" {
		return m, Launch{}, harness.Profile{}, p, errors.New("missing configuration digests")
	}
	for name, expected := range p.Configs {
		if name != "codex-home/config.toml" && name != "hermes-home/config.yaml" {
			return m, Launch{}, harness.Profile{}, p, errors.New("unexpected configuration path")
		}
		b, err := readBounded(filepath.Join(parent, name), 1<<20)
		if err != nil || digest(b) != expected {
			return m, Launch{}, harness.Profile{}, p, errors.New("prepared native configuration changed")
		}
	}
	l := m.Limits
	if l.Concurrency != 1 || l.Turns != 1 || l.Repairs != 0 || l.Replays != 0 || l.Startup < 1 || l.RPC < 1 || l.Active < 1 || l.Task < 1 || l.Wait < 1 || l.Wall < 1 || l.Interrupt < 1 || l.Terminate < 1 || l.Frame < 128 || l.Frame > 1<<20 || l.Evidence < int64(l.Frame) || l.Evidence > 16<<20 || l.Pending < 1 || l.Pending > 8 || l.Queue < 1 || l.Queue > 256 || l.Wall > 300 || l.Startup > 30 || l.RPC > 15 || l.Active > 180 || l.Task > 180 || l.Wait > 60 || l.Interrupt > 5 || l.Terminate > 5 {
		return m, Launch{}, harness.Profile{}, p, errors.New("invalid or unbounded spike limits")
	}
	launch := m.Codex
	params := launch.ThreadStart
	provider := "openai"
	if kind == "hermes" {
		launch = m.Hermes
		params = launch.SessionCreate
		provider = "custom"
	} else if kind != "codex" {
		return m, launch, harness.Profile{}, p, errors.New("choose codex or hermes")
	}
	model, _ := params["model"].(string)
	if len(launch.Argv) == 0 || launch.Cwd != m.Workspace || model == "" || launch.Env["HOME"] != filepath.Join(parent, "user-home") {
		return m, launch, harness.Profile{}, p, errors.New("invalid prepared launch")
	}
	if kind == "codex" && (launch.Env["CODEX_HOME"] != filepath.Join(parent, "codex-home") || len(launch.Argv) != 3 || launch.Argv[1] != "app-server" || launch.Argv[2] != "--stdio") {
		return m, launch, harness.Profile{}, p, errors.New("invalid Codex launch")
	}
	if kind == "hermes" && (launch.Env["HERMES_HOME"] != filepath.Join(parent, "hermes-home") || len(launch.Argv) != 3 || launch.Argv[1] != "-m" || launch.Argv[2] != "tui_gateway.entry") {
		return m, launch, harness.Profile{}, p, errors.New("invalid Hermes launch")
	}
	return m, launch, harness.Profile{Model: model, Provider: provider, Workspace: m.Workspace, NativeParams: params}, p, nil
}

type LocalMetadata struct {
	Model   string `json:"model"`
	Context int    `json:"context_tokens"`
	Slots   int    `json:"slots"`
	Tools   bool   `json:"template_supports_tools"`
}

func localMetadata(ctx context.Context, base, key, model string) (LocalMetadata, error) {
	var meta LocalMetadata
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1" {
		return meta, errors.New("local profile requires loopback /v1 endpoint")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("endpoint redirect refused") }}
	defer client.CloseIdleConnections()
	get := func(path string, out any) error {
		endpoint := *u
		endpoint.Path = path
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
		if err != nil {
			return err
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		r, err := client.Do(req)
		if err != nil {
			return errors.New("local endpoint unavailable")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("local endpoint %s returned HTTP %d", path, r.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err = get("/health", &health); err != nil {
		return meta, err
	}
	if health.Status != "ok" {
		return meta, errors.New("llama not ready")
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = get("/v1/models", &models); err != nil {
		return meta, err
	}
	for _, m := range models.Data {
		if m.ID == model {
			meta.Model = model
		}
	}
	if meta.Model == "" {
		return meta, errors.New("configured local model not advertised")
	}
	var props struct {
		Slots    int `json:"total_slots"`
		Settings struct {
			Context int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		Caps struct {
			Tools bool `json:"supports_tools"`
			Calls bool `json:"supports_tool_calls"`
		} `json:"chat_template_caps"`
	}
	if err = get("/props", &props); err != nil {
		return meta, err
	}
	meta.Context = props.Settings.Context
	meta.Slots = props.Slots
	meta.Tools = props.Caps.Tools && props.Caps.Calls
	if meta.Context < 8192 || !meta.Tools || meta.Slots < 1 {
		return meta, errors.New("local context/tool capabilities insufficient for fixture")
	}
	return meta, nil
}
