package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"vigil/internal/store"
)

// FixtureDriver is deliberately not a production fallback. It performs one
// deterministic write only in a marked disposable repository and exists for
// routine crash/recovery validation without model or Docker use.
type FixtureDriver struct {
	mu              sync.Mutex
	observation     Observation
	RepositoryID    string
	Root            string
	RelativePath    string
	Content         []byte
	Calls           map[string]int
	CreateUncertain bool
	SubmitUncertain bool
	ExtraEvents     int
}

func (d *FixtureDriver) Restore(observation Observation) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.observation = observation
}

func (d *FixtureDriver) count(name string) {
	if d.Calls == nil {
		d.Calls = map[string]int{}
	}
	d.Calls[name]++
}
func (d *FixtureDriver) validate(prepared PreparedRun) error {
	clean := filepath.Clean(d.RelativePath)
	if prepared.RuntimeKind != "synthetic" || d.RepositoryID == "" || d.RelativePath == "" || len(d.Content) > 65536 || filepath.IsAbs(d.RelativePath) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("invalid synthetic fixture driver")
	}
	marker := filepath.Join(d.Root, ".vigil-disposable-fixture")
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("synthetic execution requires a marked disposable fixture repository")
	}
	known := false
	for _, repository := range prepared.Repositories {
		if repository.ID == d.RepositoryID && repository.Root == d.Root {
			known = true
		}
	}
	if !known {
		return errors.New("fixture driver targets an unenrolled repository")
	}
	return nil
}

func (d *FixtureDriver) Inspect(_ context.Context, prepared PreparedRun) (Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("inspect")
	if err := d.validate(prepared); err != nil {
		return Observation{}, err
	}
	return d.observation, nil
}
func (d *FixtureDriver) Create(_ context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("create")
	if err := d.validate(prepared); err != nil {
		return err
	}
	d.observation.Exists = true
	d.observation.SubmissionState = "not_attempted"
	d.observation.WriterState = "unconfirmed"
	if d.CreateUncertain {
		return errors.New("fixture injected ambiguous create response")
	}
	return nil
}
func (d *FixtureDriver) Start(_ context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("start")
	if err := d.validate(prepared); err != nil {
		return err
	}
	if !d.observation.Exists {
		return errors.New("fixture resource missing")
	}
	d.observation.Started = true
	return nil
}
func (d *FixtureDriver) Attach(_ context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("attach")
	if err := d.validate(prepared); err != nil {
		return err
	}
	if !d.observation.Started {
		return errors.New("fixture resource not started")
	}
	d.observation.Attached = true
	return nil
}
func (d *FixtureDriver) CreateNative(_ context.Context, prepared PreparedRun) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("native_create")
	if err := d.validate(prepared); err != nil {
		return "", err
	}
	if !d.observation.Attached {
		return "", errors.New("fixture transport not attached")
	}
	d.observation.NativeSessionID = "fixture-session-" + prepared.GenerationID
	return d.observation.NativeSessionID, nil
}
func (d *FixtureDriver) Submit(_ context.Context, prepared PreparedRun, _ string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("submit")
	if err := d.validate(prepared); err != nil {
		return "proven_not_delivered", "", err
	}
	if d.observation.NativeSessionID == "" {
		return "proven_not_delivered", "", errors.New("fixture native session missing")
	}
	d.observation.NativeTurnID = "fixture-turn-" + prepared.GenerationID
	if d.SubmitUncertain {
		d.observation.SubmissionState = "uncertain"
		return "uncertain", d.observation.NativeTurnID, errors.New("fixture injected ambiguous submission response")
	}
	d.observation.SubmissionState = "delivered"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(d.Root, d.RelativePath)), 0700); err != nil {
		return "delivered", d.observation.NativeTurnID, err
	}
	if err := os.WriteFile(filepath.Join(d.Root, d.RelativePath), d.Content, 0600); err != nil {
		return "delivered", d.observation.NativeTurnID, err
	}
	result := Result{SchemaVersion: 1, Status: "completed", Summary: "deterministic fixture edit completed", ChangedPaths: []string{d.RepositoryID + ":" + filepath.ToSlash(d.RelativePath)}}
	d.observation.Result, _ = json.Marshal(result)
	d.observation.Terminal = true
	d.observation.Outcome = "completed"
	d.observation.WriterState = "contained_stopped"
	d.observation.Events = []DriverEvent{{Sequence: 1, Kind: "fixture_write", At: store.Now(), Payload: json.RawMessage(`{"bounded":true}`)}}
	for n := 0; n < d.ExtraEvents; n++ {
		d.observation.Events = append(d.observation.Events, DriverEvent{Sequence: int64(n + 2), Kind: "fixture_output", At: store.Now(), Payload: json.RawMessage(`{"bytes":1}`)})
	}
	return "delivered", d.observation.NativeTurnID, nil
}
func (d *FixtureDriver) Await(_ context.Context, prepared PreparedRun) (Observation, error) {
	return d.Inspect(context.Background(), prepared)
}
func (d *FixtureDriver) RenewLease(_ context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("renew_lease")
	return d.validate(prepared)
}
func (d *FixtureDriver) Stop(_ context.Context, prepared PreparedRun) (Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("stop")
	if err := d.validate(prepared); err != nil {
		return Observation{}, err
	}
	d.observation.WriterState = "contained_stopped"
	return d.observation, nil
}
