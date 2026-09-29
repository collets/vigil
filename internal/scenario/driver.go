package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bounds are the limits the manifest records *before* the walkthrough starts.
// They are enforced, not advisory: an over-long or over-large step is a failure
// of the scenario rather than something quietly truncated.
type Bounds struct {
	MaxSteps           int   `json:"max_steps"`
	MaxStepDurationMS  int64 `json:"max_step_duration_ms"`
	MaxStepOutputBytes int   `json:"max_step_output_bytes"`
	MaxLiveModelTurns  int   `json:"max_live_model_turns"`
	MaxWallClockMS     int64 `json:"max_wall_clock_ms"`
}

// DefaultBounds are the pre-registered Stage 5.7 bounds. They are small because
// the walkthrough is a bounded rehearsal, not a soak test.
func DefaultBounds() Bounds {
	return Bounds{
		MaxSteps:           220,
		MaxStepDurationMS:  120000,
		MaxStepOutputBytes: 4 << 20,
		MaxLiveModelTurns:  4,
		MaxWallClockMS:     45 * 60 * 1000,
	}
}

func (b Bounds) validate() error {
	if b.MaxSteps < 1 || b.MaxStepDurationMS < 1 || b.MaxStepOutputBytes < 1024 || b.MaxWallClockMS < 1 {
		return errors.New("scenario bounds must be positive")
	}
	if b.MaxLiveModelTurns < 0 {
		return errors.New("live model turn bound cannot be negative")
	}
	return nil
}

// StepResult is one recorded production-command invocation. It is the unit of
// evidence in the manifest: what was run, on which binary, with what exit
// status, and the digest of what came back.
type StepResult struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Args       []string        `json:"args"`
	ExitCode   int             `json:"exit_code"`
	DurationMS int64           `json:"duration_ms"`
	OutputSize int             `json:"output_bytes"`
	OutputSHA  string          `json:"output_sha256"`
	Error      string          `json:"error,omitempty"`
	JSON       json.RawMessage `json:"json,omitempty"`
}

// Driver runs the real production binary as a subprocess. Every step is a
// separate operating-system process, so the walkthrough exercises the actual
// command surface, the real per-invocation open/close cycle, and real receipt
// and revision checks rather than an in-process approximation.
type Driver struct {
	Binary     string
	StateDir   string
	ProjectID  string
	ProjectRev int

	bounds    Bounds
	started   time.Time
	steps     []StepResult
	liveTurns int
	WorkDir   string
	Env       []string
}

// NewDriver builds a driver over an already-built production binary and a
// private, empty state directory. workDir is the working directory production
// invocations run in; it defaults to the binary's own directory.
func NewDriver(binary, stateDir, workDir string, bounds Bounds) (*Driver, error) {
	if err := bounds.validate(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("production binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("production binary is not a regular file")
	}
	state, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	if workDir == "" {
		workDir = filepath.Dir(absolute)
	}
	work, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	return &Driver{Binary: absolute, StateDir: state, WorkDir: work, bounds: bounds, started: time.Now()}, nil
}

// Steps returns every recorded step in order.
func (d *Driver) Steps() []StepResult { return append([]StepResult(nil), d.steps...) }

// LiveTurns returns how many live model turns the walkthrough has consumed.
func (d *Driver) LiveTurns() int { return d.liveTurns }

// ChargeLiveTurn reserves one live model turn against the pre-registered bound.
func (d *Driver) ChargeLiveTurn() error {
	if d.liveTurns >= d.bounds.MaxLiveModelTurns {
		return fmt.Errorf("live model turn bound of %d reached", d.bounds.MaxLiveModelTurns)
	}
	d.liveTurns++
	return nil
}

// Invoke runs one production command and records it. A nonzero exit is returned
// as an error together with the recorded step, so a caller can inspect the
// output before deciding how to proceed.
func (d *Driver) Invoke(ctx context.Context, name string, args ...string) (StepResult, error) {
	if len(d.steps) >= d.bounds.MaxSteps {
		return StepResult{}, fmt.Errorf("scenario step bound of %d reached", d.bounds.MaxSteps)
	}
	if elapsed := time.Since(d.started).Milliseconds(); elapsed > d.bounds.MaxWallClockMS {
		return StepResult{}, fmt.Errorf("scenario wall-clock bound of %dms reached", d.bounds.MaxWallClockMS)
	}
	resolved, err := d.resolveRevisionArgs(ctx, args)
	if err != nil {
		return StepResult{}, fmt.Errorf("step %q: %w", name, err)
	}
	args = resolved
	full := append([]string{"--state-dir", d.StateDir}, args...)
	stepCtx, cancel := context.WithTimeout(ctx, time.Duration(d.bounds.MaxStepDurationMS)*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(stepCtx, d.Binary, full...)
	command.Dir = d.WorkDir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if d.Env != nil {
		command.Env = d.Env
	}
	started := time.Now()
	runErr := command.Run()
	elapsed := time.Since(started).Milliseconds()
	combined := stdout.Bytes()
	if stderr.Len() > 0 {
		combined = append(append([]byte(nil), combined...), stderr.Bytes()...)
	}
	step := StepResult{
		Index:      len(d.steps) + 1,
		Name:       name,
		Args:       args,
		ExitCode:   command.ProcessState.ExitCode(),
		DurationMS: elapsed,
		OutputSize: len(combined),
		OutputSHA:  Digest(combined),
	}
	if step.OutputSize > d.bounds.MaxStepOutputBytes {
		d.steps = append(d.steps, step)
		return step, fmt.Errorf("step %q produced %d bytes, above the %d byte bound", name, step.OutputSize, d.bounds.MaxStepOutputBytes)
	}
	if stepCtx.Err() == context.DeadlineExceeded {
		step.Error = "step exceeded its duration bound"
		d.steps = append(d.steps, step)
		return step, errors.New(step.Error)
	}
	if json.Valid(bytes.TrimSpace(combined)) {
		step.JSON = json.RawMessage(bytes.TrimSpace(combined))
	}
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		step.Error = truncate(detail, 600)
		d.steps = append(d.steps, step)
		return step, fmt.Errorf("step %q failed: %s", name, step.Error)
	}
	d.steps = append(d.steps, step)
	return step, nil
}

// MustInvoke runs a production command and fails the scenario on any error.
func (d *Driver) MustInvoke(ctx context.Context, name string, args ...string) StepResult {
	step, err := d.Invoke(ctx, name, args...)
	if err != nil {
		panic(&ScenarioAbort{Step: name, Err: err})
	}
	return step
}

// CurrentRevision is the placeholder callers pass in place of a literal
// expected revision. Invoke resolves it by re-reading the authoritative
// revision immediately before the command runs, so a stale envelope can never be
// sent by the walkthrough itself.
const CurrentRevision = "@current"

// resolveRevisionArgs substitutes the CurrentRevision placeholder with the
// freshly read project revision. A project-scoped call with no known project is
// left untouched, because some commands legitimately take no revision.
func (d *Driver) resolveRevisionArgs(ctx context.Context, args []string) ([]string, error) {
	needed := false
	for _, arg := range args {
		if arg == CurrentRevision {
			needed = true
		}
	}
	if !needed {
		return args, nil
	}
	if d.ProjectID == "" {
		return nil, errors.New("a current-revision placeholder was used with no project in scope")
	}
	revision, err := d.RefreshRevision(ctx)
	if err != nil {
		return nil, err
	}
	resolved := make([]string, len(args))
	copy(resolved, args)
	for index, arg := range resolved {
		if arg == CurrentRevision {
			resolved[index] = strconv.Itoa(revision)
		}
	}
	return resolved, nil
}

// ScenarioAbort unwinds a failed walkthrough without masking the real failure.
type ScenarioAbort struct {
	Step string
	Err  error
}

func (a *ScenarioAbort) Error() string { return fmt.Sprintf("scenario step %q: %v", a.Step, a.Err) }
func (a *ScenarioAbort) Unwrap() error { return a.Err }

// Decode unmarshals a recorded step's JSON output into target.
func Decode(step StepResult, target any) error {
	if len(step.JSON) == 0 {
		return fmt.Errorf("step %q produced no JSON output", step.Name)
	}
	if err := json.Unmarshal(step.JSON, target); err != nil {
		return fmt.Errorf("step %q output was not the expected shape: %w", step.Name, err)
	}
	return nil
}

// ApplyResult is the envelope `project apply` returns: the applied command's own
// result, plus the new project revision.
type ApplyResult struct {
	ProjectID string `json:"project_id"`
	Result    struct {
		OperationID string `json:"operation_id"`
		RequestID   string `json:"request_id"`
		GrantID     string `json:"grant_id"`
		State       string `json:"state"`
		Decision    string `json:"decision"`
	} `json:"result"`
	Revision int `json:"revision"`
}

// DecodeApply unwraps a `project apply` result. Commands applied through the
// envelope are not bare JSON, so decoding them directly would silently produce
// empty identities.
func DecodeApply(step StepResult) (ApplyResult, error) {
	var applied ApplyResult
	if err := Decode(step, &applied); err != nil {
		return applied, err
	}
	if applied.ProjectID == "" {
		return applied, fmt.Errorf("step %q did not return a project command envelope", step.Name)
	}
	return applied, nil
}

// ProjectStatus is the subset of `project status` (Readiness) the walkthrough
// depends on. It is decoded into the production type so a shape change in the
// application surfaces here as a scenario failure rather than a silent gap.
type ProjectStatus struct {
	Project struct {
		ID       string `json:"id"`
		Root     string `json:"root"`
		Revision int    `json:"revision"`
		State    string `json:"state"`
	} `json:"project"`
	DefinitionIssues []string `json:"definition_issues"`
	RuntimeIssues    []string `json:"runtime_issues"`
	Tasks            []struct {
		ID       string   `json:"id"`
		State    string   `json:"state"`
		Revision int      `json:"revision"`
		Issues   []string `json:"issues"`
	} `json:"tasks"`
	ExecutionEligible bool `json:"execution_eligible"`
}

// RefreshRevision re-reads the authoritative project revision. The walkthrough
// never caches a revision across a mutating step, because every command
// envelope must carry the exact current value.
func (d *Driver) RefreshRevision(ctx context.Context) (int, error) {
	step, err := d.Invoke(ctx, "project status", "project", "status", d.ProjectID)
	if err != nil {
		return 0, err
	}
	var status ProjectStatus
	if err := Decode(step, &status); err != nil {
		return 0, err
	}
	if status.Project.ID != d.ProjectID {
		return 0, fmt.Errorf("project status returned project %q, want %q", status.Project.ID, d.ProjectID)
	}
	d.ProjectRev = status.Project.Revision
	return status.Project.Revision, nil
}

// Status reads and decodes the full readiness document.
func (d *Driver) Status(ctx context.Context) (ProjectStatus, StepResult, error) {
	step, err := d.Invoke(ctx, "project status", "project", "status", d.ProjectID)
	if err != nil {
		return ProjectStatus{}, step, err
	}
	var status ProjectStatus
	if err := Decode(step, &status); err != nil {
		return ProjectStatus{}, step, err
	}
	if status.Project.ID != d.ProjectID {
		return ProjectStatus{}, step, fmt.Errorf("project status returned project %q, want %q", status.Project.ID, d.ProjectID)
	}
	d.ProjectRev = status.Project.Revision
	return status, step, nil
}

// Envelope is the versioned human command the walkthrough applies.
type Envelope struct {
	CommandID        string `json:"command_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Kind             string `json:"kind"`
	Payload          any    `json:"payload"`
}

// Apply writes a command envelope through the production `project apply` path,
// refreshing the expected revision first.
func (d *Driver) Apply(ctx context.Context, name, commandID, kind string, payload any) (StepResult, error) {
	if _, err := d.RefreshRevision(ctx); err != nil {
		return StepResult{}, err
	}
	file := filepath.Join(d.StateDir, "commands", commandID+".json")
	envelope := Envelope{CommandID: commandID, ExpectedRevision: d.ProjectRev, Kind: kind, Payload: payload}
	raw, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return StepResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return StepResult{}, err
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		return StepResult{}, err
	}
	step, err := d.Invoke(ctx, name, "project", "apply", d.ProjectID, "--file", file)
	if err != nil {
		return step, err
	}
	if _, err := d.RefreshRevision(ctx); err != nil {
		return step, err
	}
	return step, nil
}

// MustApply is Apply with scenario-fatal failure.
func (d *Driver) MustApply(ctx context.Context, name, commandID, kind string, payload any) StepResult {
	step, err := d.Apply(ctx, name, commandID, kind, payload)
	if err != nil {
		panic(&ScenarioAbort{Step: name, Err: err})
	}
	return step
}

// WriteJSON persists a bounded request document the CLI reads through --file.
func (d *Driver) WriteJSON(name string, value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 {
		return "", errors.New("request document exceeds the one mebibyte scenario bound")
	}
	file := filepath.Join(d.StateDir, "requests", name+".json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		return "", err
	}
	return file, nil
}

// WriteTemp persists a bounded document outside the state directory, for
// requests that must be read by a command but are not application state.
func (d *Driver) WriteTemp(dir, name string, value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 {
		return "", errors.New("request document exceeds the one mebibyte scenario bound")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(dir, name+".json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		return "", err
	}
	return file, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…(truncated)"
}
