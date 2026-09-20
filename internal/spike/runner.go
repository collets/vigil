package spike

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vigil/internal/harness"
)

type Options struct {
	Manifest, Harness, KeyFile, Scenario string
	Live                                 bool
	Output                               io.Writer
}
type Result struct {
	Summary string   `json:"summary"`
	Files   []string `json:"files"`
}
type Report struct {
	Experiment      *Experiment      `json:"experiment,omitempty"`
	Schema          int              `json:"schema_version"`
	Mode            string           `json:"mode"`
	Started         time.Time        `json:"started_at"`
	Duration        float64          `json:"duration_seconds"`
	Session         harness.Snapshot `json:"session"`
	Metadata        *LocalMetadata   `json:"local_metadata,omitempty"`
	Submitted       bool             `json:"submitted"`
	FixtureVerified bool             `json:"fixture_verified"`
	TaskAccepted    bool             `json:"task_accepted"`
	Result          *Result          `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
	StderrBytes     int64            `json:"stderr_bytes"`
	ProcessClosed   bool             `json:"owned_process_close_completed"`
	Events          map[string]int   `json:"event_counts"`
}
type journal struct {
	mu        sync.Mutex
	f         *os.File
	remaining int64
	secrets   []string
}

func (j *journal) redact(text string) string {
	for _, secret := range j.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}
func (j *journal) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	b = []byte(j.redact(string(b)))
	if int64(len(b)+1) > j.remaining {
		return errors.New("diagnostic evidence limit exceeded")
	}
	j.remaining -= int64(len(b) + 1)
	_, err = j.f.Write(append(b, '\n'))
	return err
}
func safeWord(s string) string {
	if len(s) > 160 {
		return "[oversize]"
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return "[nonprintable]"
		}
	}
	return s
}

func Run(ctx context.Context, opt Options) (report Report, runErr error) {
	if opt.Scenario != "" && (!opt.Live || !validScenario(opt.Scenario)) {
		return report, errors.New("lifecycle scenario requires --live and a supported scenario")
	}
	start := time.Now()
	report = Report{Schema: 1, Mode: "probe", Started: start.UTC(), Events: make(map[string]int)}
	if opt.Live {
		report.Mode = "live"
	}
	if opt.Output == nil {
		opt.Output = io.Discard
	}
	path, err := filepath.Abs(opt.Manifest)
	if err != nil {
		return report, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return report, err
	}
	m, launch, profile, proof, err := load(path, opt.Harness)
	if err != nil {
		return report, err
	}
	dir := filepath.Dir(path)
	evidence := filepath.Join(dir, "evidence")
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		return report, err
	}
	id := hex.EncodeToString(random[:])
	// A persistent live marker prevents accidental replay even after failure/crash.
	if opt.Live {
		f, err := os.OpenFile(filepath.Join(dir, "live-attempt.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return report, errors.New("fixture already used; prepare a fresh experiment")
		}
		_ = json.NewEncoder(f).Encode(map[string]string{"run_id": id, "harness": opt.Harness})
		f.Close()
	}
	// One operator-managed spike per checkout, including probes that share native homes.
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(dir), "runner.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return report, errors.New("spike runner lock exists; inspect the owner before removing a stale lock")
	}
	fmt.Fprintln(lock, os.Getpid())
	lock.Close()
	defer os.Remove(filepath.Join(filepath.Dir(dir), "runner.lock"))
	log, err := os.OpenFile(filepath.Join(evidence, "events-"+id+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return report, err
	}
	j := &journal{f: log, remaining: m.Limits.Evidence - 65536}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Limits.Wall)*time.Second)
	defer cancel()
	// Final report is reserved separately, so traffic saturation cannot erase outcome.
	defer func() {
		report.Duration = time.Since(start).Seconds()
		if runErr != nil {
			report.FixtureVerified = false
			if report.Experiment != nil {
				report.Experiment.Passed = false
			}
			report.Error = j.redact(runErr.Error())
		}
		log.Close()
		b, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			b = []byte(j.redact(string(b)))
			if len(b) > 65535 {
				err = errors.New("final report limit exceeded")
			} else {
				err = os.WriteFile(filepath.Join(evidence, "result-"+id+".json"), append(b, '\n'), 0600)
			}
		}
		if err != nil && runErr == nil {
			runErr = err
		}
		fmt.Fprintf(opt.Output, "Report: %s\n", filepath.Join(evidence, "result-"+id+".json"))
	}()
	initialCtx, initialCancel := context.WithTimeout(ctx, time.Duration(m.Limits.Startup)*time.Second)
	defer initialCancel()
	env := environment(launch.Env)
	if err = validateBaseline(initialCtx, m, env); err != nil {
		return report, err
	}
	if opt.Harness == "hermes" {
		installation := launch.Env["PYTHONPATH"]
		commit, err := command(initialCtx, installation, env, "git", "rev-parse", "HEAD")
		if err != nil || commit != proof.HermesCommit {
			return report, errors.New("Hermes source version changed")
		}
		for name, expected := range proof.Sources {
			if filepath.IsAbs(name) || strings.Contains(name, "..") {
				return report, errors.New("invalid source evidence path")
			}
			b, err := readBounded(filepath.Join(installation, name), 2<<20)
			if err != nil || digest(b) != expected {
				return report, errors.New("Hermes source fingerprint changed")
			}
		}
		key := os.Getenv("VIGIL_LLAMA_API_KEY")
		if opt.KeyFile != "" {
			info, err := os.Lstat(opt.KeyFile)
			if err != nil {
				return report, err
			}
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return report, errors.New("llama key file must be private (mode 600)")
			}
			b, err := readBounded(opt.KeyFile, 16384)
			if err != nil {
				return report, err
			}
			key = strings.TrimSpace(string(b))
		} else if key == "" && os.Getenv("OPENAI_API_KEY") != "" {
			if strings.TrimRight(os.Getenv("OPENAI_BASE_URL"), "/") != launch.Env["SPIKE_BASE_URL"] {
				return report, errors.New("OPENAI_BASE_URL does not match prepared local endpoint")
			}
			key = os.Getenv("OPENAI_API_KEY")
		}
		if key == "" {
			key = launch.Env[launch.SecretEnv]
		}
		if key != "local-no-auth" {
			j.secrets = append(j.secrets, key)
		}
		launch.Env[launch.SecretEnv] = key
		meta, err := localMetadata(initialCtx, launch.Env["SPIKE_BASE_URL"], key, profile.Model)
		if err != nil {
			return report, err
		}
		report.Metadata = &meta
	} else {
		version, err := command(initialCtx, m.Workspace, env, launch.Argv[0], "--version")
		if err != nil || version != proof.CodexVersion {
			return report, errors.New("Codex version changed")
		}
		b, err := readBounded(launch.AuthFile, 1<<20)
		if err != nil {
			return report, errors.New("cannot read referenced Codex authentication")
		}
		var auth struct {
			Mode   string            `json:"auth_mode"`
			Tokens map[string]string `json:"tokens"`
		}
		if json.Unmarshal(b, &auth) != nil || auth.Mode != "chatgpt" || len(auth.Tokens) == 0 {
			return report, errors.New("referenced authentication is not ChatGPT")
		}
		for _, value := range auth.Tokens {
			if len(value) > 16 {
				j.secrets = append(j.secrets, value)
			}
		}
		dest := filepath.Join(launch.Env["CODEX_HOME"], "auth.json")
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return report, errors.New("private auth file already exists or cannot be created")
		}
		defer os.Remove(dest)
		_, err = f.Write(b)
		f.Close()
		if err != nil {
			return report, err
		}
	}
	env = environment(launch.Env)
	procCtx, procCancel := context.WithCancel(context.Background())
	defer procCancel()
	spawn := func(generation string) (*harness.Transport, *harness.Session, error) {
		transport, err := harness.Start(procCtx, harness.ProcessConfig{Argv: launch.Argv, Cwd: m.Workspace, Env: env, JSONRPC: opt.Harness == "hermes", FrameBytes: m.Limits.Frame, TrafficBytes: m.Limits.Evidence, QueueSize: m.Limits.Queue, PendingLimit: m.Limits.Pending, RPCTimeout: time.Duration(m.Limits.RPC) * time.Second, Grace: time.Duration(m.Limits.Terminate) * time.Second, Record: func(direction string, message harness.Message, size int) error {
			return j.write(map[string]any{"kind": "wire", "direction": direction, "method": safeWord(message.Method), "has_id": len(message.ID) > 0, "bytes": size})
		}})
		if err != nil {
			return nil, nil, err
		}
		s, err := harness.NewSession(opt.Harness, transport, profile, harness.RunID(id), harness.Generation(generation), time.Duration(m.Limits.Wait)*time.Second)
		if err != nil {
			transport.Close()
			return nil, nil, err
		}
		return transport, s, nil
	}
	transport, s, err := spawn(id + "-1")
	if err != nil {
		return report, err
	}
	defer func() {
		if runErr != nil && report.Submitted {
			stopCtx, c := context.WithTimeout(context.Background(), time.Duration(m.Limits.Interrupt)*time.Second)
			_ = s.Interrupt(stopCtx)
			c()
		}
		s.Close()
		report.Session = s.Inspect()
		report.StderrBytes = transport.StderrBytes()
		report.ProcessClosed = true
		if s.Err() != nil && runErr == nil && opt.Scenario == "" {
			runErr = s.Err()
		}
		if transport.Err() != nil && runErr == nil && opt.Scenario == "" {
			runErr = transport.Err()
		}
		for {
			select {
			case e := <-s.Events():
				if err := recordEvent(j, opt.Output, &report, e); err != nil && runErr == nil {
					runErr = err
				}
			default:
				return
			}
		}
	}()
	if err = s.Probe(initialCtx); err != nil {
		return report, err
	}
	fmt.Fprintf(opt.Output, "%s profile verified: %s / %s; workspace %s\n", opt.Harness, profile.Provider, profile.Model, m.Workspace)
	if !opt.Live {
		return report, nil
	}
	if opt.Scenario != "" {
		report.Experiment = &Experiment{Scenario: opt.Scenario, Observations: map[string]any{}}
		err = lifecycle(ctx, opt, m, s, transport, spawn, j, &report)
		return report, err
	}
	if err = s.Create(initialCtx); err != nil {
		return report, err
	}
	initialCancel()
	active := m.Limits.Active
	if m.Limits.Task < active {
		active = m.Limits.Task
	}
	turnCtx, turnCancel := context.WithTimeout(ctx, time.Duration(active)*time.Second)
	defer turnCancel()
	prompt, err := readBounded(filepath.Join(m.Workspace, "README.md"), 65536)
	if err != nil {
		return report, err
	}
	report.Submitted = true
	if err = s.Submit(turnCtx, harness.AppTurnID(id+"-turn-1"), string(prompt), resultSchema()); err != nil {
		return report, err
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		snap := s.Inspect()
		if err = s.Err(); err != nil {
			return report, err
		}
		if snap.Outcome != "active" && snap.Outcome != "not_started" {
			if snap.Outcome != "completed" {
				return report, fmt.Errorf("native turn ended %s", snap.Outcome)
			}
			if opt.Harness == "hermes" {
				if err = s.Refresh(turnCtx); err != nil {
					return report, err
				}
				if !s.Inspect().IdleObserved {
					select {
					case <-turnCtx.Done():
						return report, turnCtx.Err()
					case <-tick.C:
					}
					continue
				}
			}
			break
		}
		select {
		case <-turnCtx.Done():
			return report, turnCtx.Err()
		case e := <-s.Events():
			if err = recordEvent(j, opt.Output, &report, e); err != nil {
				return report, err
			}
			if e.Request != nil && strings.HasSuffix(e.Kind, "requested") {
				if err = s.Answer(turnCtx, *e.Request, harness.Answer{Decision: "deny"}); err != nil {
					return report, err
				}
				if e.Kind == "input_requested" {
					return report, errors.New("fixture requested human input; cancelled without inventing an answer")
				}
			} else if e.Kind == "unsupported_request" {
				_ = s.Answer(turnCtx, *e.Request, harness.Answer{Decision: "cancel"})
				return report, errors.New("unsupported native request")
			}
		case <-s.Changed():
		case <-tick.C:
			if err = s.Expire(turnCtx); err != nil {
				return report, err
			}
		}
	}
	// Close before inspecting filesystem results. Later stages prove detached-writer boundaries.
	s.Close()
	if s.Err() != nil {
		return report, s.Err()
	}
	if transport.Err() != nil {
		return report, transport.Err()
	}
	result, err := validateResult(j.redact(s.Inspect().Output))
	if err != nil {
		return report, err
	}
	checkCtx, c := context.WithTimeout(ctx, 15*time.Second)
	defer c()
	if err = verifyFixture(checkCtx, m, env); err != nil {
		return report, err
	}
	report.Result = &result
	report.FixtureVerified = true
	fmt.Fprintln(opt.Output, "Native turn completed; fixture and result verified. Product task acceptance remains separate.")
	return report, nil
}

func recordEvent(j *journal, out io.Writer, report *Report, e harness.Event) error {
	e.Name = safeWord(e.Name)
	e.Item = safeWord(e.Item)
	e.Method = safeWord(e.Method)
	e.Status = safeWord(e.Status)
	if err := j.write(e); err != nil {
		return err
	}
	report.Events[e.Kind]++
	if e.Kind != "output" && e.Kind != "usage" {
		fmt.Fprintf(out, "%s %s %s\n", e.Kind, j.redact(e.Name), j.redact(e.Status))
	}
	return nil
}
func resultSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"summary": map[string]any{"type": "string"}, "files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"summary", "files"}, "additionalProperties": false}
}
func validateResult(text string) (Result, error) {
	var r Result
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, errors.New("invalid structured result")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 4096 || len(r.Files) != 1 || r.Files[0] != "message.txt" {
		return r, errors.New("structured result does not match fixture contract")
	}
	return r, nil
}
func validateBaseline(ctx context.Context, m Manifest, env []string) error {
	head, err := command(ctx, m.Workspace, env, "git", "rev-parse", "HEAD")
	if err != nil || head != m.Baseline {
		return errors.New("fixture baseline mismatch")
	}
	status, err := command(ctx, m.Workspace, env, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil || status != "" {
		return errors.New("fixture must start clean")
	}
	remotes, err := command(ctx, m.Workspace, env, "git", "remote")
	if err != nil || remotes != "" {
		return errors.New("fixture must have no remotes")
	}
	return nil
}
func verifyFixture(ctx context.Context, m Manifest, env []string) error {
	for _, name := range []string{"README.md", "check.sh", "message.txt"} {
		p := filepath.Join(m.Workspace, name)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("fixture artifact missing or not a regular file")
		}
		b, err := readBounded(p, 65536)
		if err != nil {
			return err
		}
		if name == "message.txt" {
			if !bytes.Equal(b, []byte("adapter spike ready\n")) {
				return errors.New("fixture content incorrect")
			}
		} else {
			baseline, err := command(ctx, m.Workspace, env, "git", "show", m.Baseline+":"+name)
			if err != nil || strings.TrimSpace(string(b)) != baseline {
				return errors.New("fixture instructions/check modified")
			}
		}
	}
	head, err := command(ctx, m.Workspace, env, "git", "rev-parse", "HEAD")
	if err != nil || head != m.Baseline {
		return errors.New("fixture HEAD changed")
	}
	remotes, err := command(ctx, m.Workspace, env, "git", "remote")
	if err != nil || remotes != "" {
		return errors.New("fixture remote added")
	}
	status, err := command(ctx, m.Workspace, env, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "M message.txt" && status != "M  message.txt" {
		return errors.New("unexpected fixture diff")
	}
	// Include ignored files, which porcelain intentionally omits.
	err = filepath.WalkDir(m.Workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(m.Workspace, path)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if rel != "README.md" && rel != "check.sh" && rel != "message.txt" {
			return errors.New("unexpected fixture path")
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = command(ctx, m.Workspace, env, "sh", "check.sh")
	return err
}
