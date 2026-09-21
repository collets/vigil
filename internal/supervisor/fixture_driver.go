package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
	"vigil/internal/boundary"
	"vigil/internal/store"
	"vigil/internal/workspace"
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
	History         *HistoryObservation
	Verifier        CheckpointVerifier
}

// SyntheticHistoryInspector supplies an explicit observation only for a
// disposable synthetic run. It cannot qualify a production recovery path.
type SyntheticHistoryInspector struct {
	Observation HistoryObservation
	Verifier    CheckpointVerifier
}

func (i SyntheticHistoryInspector) VerifyCheckpoint(ctx context.Context, checkpointID string) error {
	if i.Verifier == nil {
		return errors.New("synthetic checkpoint verifier required")
	}
	return i.Verifier.VerifyCheckpoint(ctx, checkpointID)
}

func (i SyntheticHistoryInspector) InspectHistory(_ context.Context, prepared PreparedRun) (HistoryObservation, error) {
	if prepared.RuntimeKind != "synthetic" || i.Observation.Qualification != "synthetic" {
		return HistoryObservation{}, errors.New("synthetic history evidence cannot qualify a production run")
	}
	return i.Observation, nil
}

func (d *FixtureDriver) InspectHistory(ctx context.Context, prepared PreparedRun) (HistoryObservation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("inspect_history")
	if _, err := d.validate(ctx, prepared); err != nil {
		return HistoryObservation{}, err
	}
	if d.History == nil {
		return HistoryObservation{State: "missing", Qualification: "synthetic"}, nil
	}
	return *d.History, nil
}

func (d *FixtureDriver) VerifyCheckpoint(ctx context.Context, checkpointID string) error {
	if d.Verifier == nil {
		return errors.New("fixture checkpoint verifier required")
	}
	return d.Verifier.VerifyCheckpoint(ctx, checkpointID)
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
func (d *FixtureDriver) validate(ctx context.Context, prepared PreparedRun) (string, error) {
	clean := filepath.Clean(d.RelativePath)
	if prepared.RuntimeKind != "synthetic" || d.RepositoryID == "" || d.RelativePath == "" || d.RelativePath != filepath.ToSlash(clean) || len(d.Content) > 65536 || filepath.IsAbs(d.RelativePath) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid synthetic fixture driver")
	}
	identity, err := workspace.Inspect(ctx, d.Root)
	if err != nil {
		return "", errors.New("synthetic fixture repository identity is unavailable")
	}
	marker := filepath.Join(identity.Root, ".vigil-disposable-fixture")
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("synthetic execution requires a marked disposable fixture repository")
	}
	known := false
	for _, repository := range prepared.Repositories {
		if repository.ID == d.RepositoryID && repository.Root == identity.Root && repository.Identity.Key == identity.Key && repository.Identity.CommonGit == identity.CommonGit {
			relative := filepath.ToSlash(clean)
			if !pathAllowed(prepared.Task.Scope, relative) || boundary.IsProtectedCheckoutPath(relative) {
				return "", errors.New("synthetic fixture path is outside task scope or protected")
			}
			for _, exclusion := range repository.Baseline.Exclusions {
				if relative == exclusion || strings.HasPrefix(relative, exclusion+"/") {
					return "", errors.New("synthetic fixture path is excluded from the enrolled repository")
				}
			}
			known = true
		}
	}
	if !known {
		return "", errors.New("fixture driver targets an unenrolled repository")
	}
	return identity.Root, nil
}

// confinedReplace walks existing parents with openat/O_NOFOLLOW and atomically
// replaces the target with a new single-link inode. It never truncates an
// existing hard link and never follows a target symlink.
func confinedReplace(root, relative string, content []byte) error {
	clean := filepath.Clean(relative)
	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return errors.New("invalid confined fixture path")
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD := rootFD
	for _, component := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(parentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			if parentFD != rootFD {
				unix.Close(parentFD)
			}
			return errors.New("synthetic fixture parent must be an existing confined directory")
		}
		if parentFD != rootFD {
			unix.Close(parentFD)
		}
		parentFD = next
	}
	if parentFD != rootFD {
		defer unix.Close(parentFD)
	}
	base := parts[len(parts)-1]
	var existing unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if existing.Mode&unix.S_IFMT != unix.S_IFREG {
			return errors.New("synthetic fixture target must be a regular file")
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	temporary := ".vigil-fixture-write-" + store.ID()
	fd, err := unix.Openat(parentFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(parentFD, temporary, 0)
		}
	}()
	file := os.NewFile(uintptr(fd), temporary)
	if file == nil {
		unix.Close(fd)
		return errors.New("failed to open confined fixture file")
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(parentFD, temporary, parentFD, base); err != nil {
		return err
	}
	cleanup = false
	return unix.Fsync(parentFD)
}

func (d *FixtureDriver) Inspect(ctx context.Context, prepared PreparedRun) (Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("inspect")
	if _, err := d.validate(ctx, prepared); err != nil {
		return Observation{}, err
	}
	return d.observation, nil
}
func (d *FixtureDriver) Create(ctx context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("create")
	if _, err := d.validate(ctx, prepared); err != nil {
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
func (d *FixtureDriver) Start(ctx context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("start")
	if _, err := d.validate(ctx, prepared); err != nil {
		return err
	}
	if !d.observation.Exists {
		return errors.New("fixture resource missing")
	}
	d.observation.Started = true
	return nil
}
func (d *FixtureDriver) Attach(ctx context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("attach")
	if _, err := d.validate(ctx, prepared); err != nil {
		return err
	}
	if !d.observation.Started {
		return errors.New("fixture resource not started")
	}
	d.observation.Attached = true
	return nil
}
func (d *FixtureDriver) CreateNative(ctx context.Context, prepared PreparedRun) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("native_create")
	if _, err := d.validate(ctx, prepared); err != nil {
		return "", err
	}
	if !d.observation.Attached {
		return "", errors.New("fixture transport not attached")
	}
	d.observation.NativeSessionID = "fixture-session-" + prepared.GenerationID
	return d.observation.NativeSessionID, nil
}
func (d *FixtureDriver) Resume(ctx context.Context, prepared PreparedRun, nativeSessionID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("resume")
	if _, err := d.validate(ctx, prepared); err != nil {
		return err
	}
	if !d.observation.Attached || nativeSessionID == "" {
		return errors.New("fixture resume requires attached transport and exact native session")
	}
	d.observation.NativeSessionID = nativeSessionID
	return nil
}
func (d *FixtureDriver) Submit(ctx context.Context, prepared PreparedRun, _ string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("submit")
	root, err := d.validate(ctx, prepared)
	if err != nil {
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
	if err := confinedReplace(root, d.RelativePath, d.Content); err != nil {
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
func (d *FixtureDriver) RenewLease(ctx context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("renew_lease")
	_, err := d.validate(ctx, prepared)
	return err
}
func (d *FixtureDriver) Interrupt(ctx context.Context, prepared PreparedRun) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("interrupt")
	_, err := d.validate(ctx, prepared)
	return err
}
func (d *FixtureDriver) Stop(ctx context.Context, prepared PreparedRun) (Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count("stop")
	if _, err := d.validate(ctx, prepared); err != nil {
		return Observation{}, err
	}
	d.observation.WriterState = "contained_stopped"
	return d.observation, nil
}
