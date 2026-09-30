package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Stage is one of the four Stage 5.7 work checkpoints.
type Stage string

const (
	StageA Stage = "A"
	StageB Stage = "B"
	StageC Stage = "C"
	StageD Stage = "D"
)

// Config is one scenario invocation's inputs.
type Config struct {
	// Binary is the production executable under qualification. It must already
	// be built; the scenario never builds it, so the evidence names the exact
	// candidate the reviewer validates.
	Binary string
	// Root is the agent-owned disposable scenario root. Everything the run
	// creates lives beneath it, and nothing outside it is ever removed.
	Root string
	// SourceCommit is the candidate commit the binary is expected to be from.
	SourceCommit string
	// OptIns is the operator's capability request.
	OptIns OptIns
	// Bounds are the pre-registered run limits.
	Bounds Bounds
	// Stage limits the run to one checkpoint. Empty runs all of them.
	Stage Stage
	// HarnessVersion and Model are the declared profile identities.
	HarnessVersion string
	Model          string
	// Platform is the recorded platform identity, when the caller already knows
	// it. Empty means "observe".
	Platform string
}

// Report is the durable output of one scenario run.
type Report struct {
	SchemaVersion int    `json:"schema_version"`
	Stage         Stage  `json:"stage"`
	ScenarioID    string `json:"scenario_id"`
	SourceCommit  string `json:"source_commit"`
	StartedAt     int64  `json:"started_at"`
	FinishedAt    int64  `json:"finished_at"`
	DurationMS    int64  `json:"duration_ms"`

	Binary   BinaryIdentity     `json:"binary"`
	Platform PlatformIdentity   `json:"platform"`
	Bounds   Bounds             `json:"bounds"`
	OptIns   []CapabilityRecord `json:"capabilities"`
	Fixture  Fixture            `json:"fixture"`
	Route    RouteIdentity      `json:"route"`
	Project  ProjectIdentity    `json:"project"`
	Steps    []StepResult       `json:"steps"`
	Matrix   *Matrix            `json:"matrix"`
	Checks   []Assertion        `json:"assertions"`
	LiveTurn LiveTurnEvidence   `json:"live_turns"`
	Gaps     []string           `json:"gaps"`
	Pending  []string           `json:"pending_gates"`
	Limits   []string           `json:"limitations"`
	Cleanup  CleanupReport      `json:"cleanup"`
	// Aborted records that a stage failed partway through. Every row the run did
	// not decide is undecided, never a pass, and this flag says so in the report
	// itself rather than only on stderr.
	Aborted bool   `json:"aborted"`
	Digest  string `json:"digest"`
}

// BinaryIdentity records exactly which executable produced the evidence.
type BinaryIdentity struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// PlatformIdentity records the host the run executed on.
type PlatformIdentity struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname,omitempty"`
	Kernel   string `json:"kernel,omitempty"`
	Declared string `json:"declared,omitempty"`
}

// RouteIdentity records the inference route by identity only. It never carries a
// credential value.
type RouteIdentity struct {
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	Probed    bool   `json:"probed"`
	ProbedFor string `json:"probed_for,omitempty"`
}

// ProjectIdentity records the persisted project the rehearsal ran against.
type ProjectIdentity struct {
	ID       string            `json:"id"`
	Root     string            `json:"root"`
	StateDir string            `json:"state_dir"`
	BaseOIDs map[string]string `json:"revisions,omitempty"`
}

// LiveTurnEvidence records real model usage. It is explicit so "zero live
// turns" can never be read as "live turns happened and passed".
type LiveTurnEvidence struct {
	Attempted int      `json:"attempted"`
	Completed int      `json:"completed"`
	Notes     []string `json:"notes,omitempty"`
}

// Assertion is one observed property of the run, with the value that was
// actually seen. A walkthrough that asserts nothing has proved nothing.
type Assertion struct {
	Name     string `json:"name"`
	Observed string `json:"observed"`
	Pass     bool   `json:"pass"`
	Detail   string `json:"detail,omitempty"`
}

// CleanupReport records what the run created and removed, so the operator can
// confirm no user resource was touched.
type CleanupReport struct {
	Root        string   `json:"root"`
	Removed     bool     `json:"removed"`
	Preserved   []string `json:"preserved,omitempty"`
	Notes       []string `json:"notes,omitempty"`
	ForeignRefs []string `json:"foreign_refs,omitempty"`
}

// Run executes the requested stages and returns the durable report. The report
// is returned even when a stage fails, so a partial walkthrough is recorded
// honestly rather than discarded.
func Run(ctx context.Context, config Config) (report *Report, failure error) {
	if config.Bounds.MaxSteps == 0 {
		config.Bounds = DefaultBounds()
	}
	if err := config.Bounds.validate(); err != nil {
		return nil, err
	}
	if err := requirePrivateRoot(config.Root); err != nil {
		return nil, err
	}
	binary, err := identifyBinary(config.Binary)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.Root, 0o700); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, err
	}
	route := ProbeLocalRoute()
	started := time.Now()
	report = &Report{
		SchemaVersion: 1,
		Stage:         config.Stage,
		ScenarioID:    "stage-5.7-" + started.UTC().Format("20060102T150405Z"),
		SourceCommit:  config.SourceCommit,
		StartedAt:     started.UnixMilli(),
		Binary:        binary,
		Platform:      observePlatform(config.Platform),
		Bounds:        config.Bounds,
		OptIns:        Resolve(config.OptIns, route),
		Fixture:       NewFixture(),
		Route: RouteIdentity{
			BaseURL:   route.BaseURL,
			Model:     route.Model,
			Probed:    route.Probed,
			ProbedFor: "loopback metadata read; no prompt and no inference",
		},
		Matrix: NewMatrix(),
		Checks: []Assertion{},
		Limits: []string{},
	}
	report.Pending = Pending(report.OptIns)

	walk := &walkthrough{config: config, root: root, report: report, route: route}
	defer func() {
		walk.collectSteps()
		report.FinishedAt = time.Now().UnixMilli()
		report.DurationMS = time.Now().Sub(started).Milliseconds()
		if recovered := recover(); recovered != nil {
			if abort, ok := recovered.(*ScenarioAbort); ok {
				failure = abort
				report.Aborted = true
				report.Limits = append(report.Limits, "scenario aborted at step "+abort.Step+": "+abort.Err.Error())
			} else {
				panic(recovered)
			}
		}
		// Both gap lists are derived here, in the deferred function, rather than
		// stored at the moment a stage fills them in. Deriving both at the same
		// point is what keeps a report's own gap lists from contradicting its own
		// rows: a stage added later that marked a requirement would otherwise
		// emit a `requirement_gaps` list computed before that mark.
		report.Gaps = report.Matrix.Gap()
		report.Matrix.RequirementGapList = report.Matrix.RequirementGaps()
		report.Digest = reportDigest(report)
	}()

	walk.run(ctx)
	return report, nil
}

// collectSteps records every production command invocation the walkthrough made.
// Each boundary case builds its own driver over the same binary, so the steps
// are merged in invocation order with the driver identity kept explicit.
func (w *walkthrough) collectSteps() {
	steps := []StepResult{}
	if w.driver != nil {
		steps = append(steps, w.driver.Steps()...)
	}
	for _, boundary := range w.boundaryDrivers {
		steps = append(steps, boundary.Steps()...)
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Index < steps[j].Index })
	w.report.Steps = steps
}

// walkthrough holds the mutable state of one run.
type walkthrough struct {
	config  Config
	root    string
	report  *Report
	route   LocalRoute
	driver  *Driver
	hosting *FakeHosting
	// projectID is the persisted project under rehearsal.
	projectID string
	// boundaryDrivers holds the additional drivers each checkpoint C case
	// created, so every production invocation ends up in the report.
	boundaryDrivers []*Driver
	// fixtureBase and bareRemote are the disposable repository and its local
	// bare remote.
	fixtureBase string
	bareRemote  string
}

// recordBoundaryDriver retains a boundary case's driver for the step report.
func (w *walkthrough) recordBoundaryDriver(driver *Driver) {
	if driver != nil {
		w.boundaryDrivers = append(w.boundaryDrivers, driver)
	}
}

// assert records an observed property. A failing assertion is a scenario
// failure: the walkthrough must not continue past a broken invariant and then
// report the rest as a pass.
func (w *walkthrough) assert(name string, pass bool, observed, detail string) {
	w.report.Checks = append(w.report.Checks, Assertion{Name: name, Observed: observed, Pass: pass, Detail: detail})
	if !pass {
		panic(&ScenarioAbort{Step: "assert:" + name, Err: fmt.Errorf("observed %q; %s", observed, detail)})
	}
}

// note records a limitation without failing the run.
func (w *walkthrough) note(limitation string) {
	w.report.Limits = append(w.report.Limits, limitation)
}

func (w *walkthrough) run(ctx context.Context) {
	if w.config.Stage == "" || w.config.Stage == StageA {
		w.stageA(ctx)
	}
	if w.config.Stage == "" || w.config.Stage == StageB {
		w.stageB(ctx)
	}
	if w.config.Stage == "" || w.config.Stage == StageC {
		w.stageC(ctx)
	}
	if w.config.Stage == "" || w.config.Stage == StageD {
		w.stageD(ctx)
	}
	if err := w.report.Matrix.RequireComplete(); err != nil {
		panic(&ScenarioAbort{Step: "matrix", Err: err})
	}
}

// requirePrivateRoot refuses to run against a path that is not clearly
// agent-owned. The scenario creates and removes its own root, so a mistaken
// root would be destructive.
func requirePrivateRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("scenario root required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	base := filepath.Base(absolute)
	if base == "" || base == "." || base == string(filepath.Separator) || base == "home" || base == "tmp" {
		return fmt.Errorf("refusing to use %q as a disposable scenario root", absolute)
	}
	// The root must live under a temporary or explicitly private parent, and it
	// must not be the repository itself.
	if strings.Contains(absolute, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return errors.New("scenario root cannot be inside a Git directory")
	}
	return nil
}

func identifyBinary(path string) (BinaryIdentity, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return BinaryIdentity{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return BinaryIdentity{}, fmt.Errorf("production binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return BinaryIdentity{}, errors.New("production binary is not a regular file")
	}
	raw, err := os.ReadFile(absolute)
	if err != nil {
		return BinaryIdentity{}, err
	}
	return BinaryIdentity{Path: absolute, Bytes: info.Size(), SHA256: Digest(raw)}, nil
}

func observePlatform(declared string) PlatformIdentity {
	host, _ := os.Hostname()
	identity := PlatformIdentity{OS: runtime.GOOS, Arch: runtime.GOARCH, Hostname: host, Declared: declared}
	if raw, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		identity.Kernel = strings.TrimSpace(string(raw))
	}
	return identity
}

func reportDigest(report *Report) string {
	clone := *report
	clone.Digest = ""
	raw, err := json.Marshal(clone)
	if err != nil {
		return ""
	}
	return Digest(raw)
}

// decodeInto is a small helper for the run's ad-hoc decoding needs.
func decodeInto(raw json.RawMessage, target any) error {
	return json.Unmarshal(raw, target)
}
