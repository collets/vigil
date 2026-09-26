// Package checks executes approved check definitions through a durable,
// bounded effect boundary. It never accepts a task or plan.
package checks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/quality"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type Request struct {
	CommandID string         `json:"command_id"`
	Target    quality.Target `json:"target"`
	CheckID   string         `json:"check_id"`
	Actor     string         `json:"actor"`
}

type OutputObservation struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
}

type Result struct {
	ID                   string              `json:"id"`
	EffectID             string              `json:"effect_id"`
	ScopeID              string              `json:"scope_id"`
	CheckID              string              `json:"check_id"`
	DefinitionDigest     string              `json:"definition_digest"`
	Status               string              `json:"status"`
	ExitCode             *int                `json:"exit_code,omitempty"`
	FailureIdentities    []string            `json:"failure_identities"`
	RequiredOutputs      []OutputObservation `json:"required_outputs"`
	OutputArtifactID     string              `json:"output_artifact_id"`
	OutputArtifactDigest string              `json:"output_artifact_digest"`
	EvaluatedDigest      string              `json:"evaluated_repository_set_digest"`
	ObservedDigest       string              `json:"observed_repository_set_digest"`
	StartedAt            int64               `json:"started_at"`
	EndedAt              int64               `json:"ended_at"`
	DurationMS           int64               `json:"duration_ms"`
	ResultDigest         string              `json:"result_digest"`
}

type Runner struct {
	Engine *core.Engine
	Owner  *coordinator.Owner
	// Hook is test-only fault/race orchestration. Production callers leave it nil.
	Hook func(string) error
}

type cappedBuffer struct {
	mu       sync.Mutex
	limit    int64
	bytes    bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.bytes.Len())
	if remaining > 0 {
		n := int64(len(p))
		if n > remaining {
			n = remaining
		}
		_, _ = b.bytes.Write(p[:n])
	}
	if int64(len(p)) > remaining {
		b.overflow = true
	}
	return len(p), nil
}

func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.bytes.Bytes()...)
}

func definition(scope quality.Scope, id string) (policy.CheckDefinition, string, error) {
	for _, check := range scope.RequiredChecks {
		if check.ID == id {
			raw, err := json.Marshal(check)
			if err != nil {
				return check, "", err
			}
			raw, err = store.Canonical(raw)
			if err != nil {
				return check, "", err
			}
			return check, store.Digest(raw), nil
		}
	}
	return policy.CheckDefinition{}, "", errors.New("check is not required by the current target")
}

func (r *Runner) prepare(ctx context.Context, request Request, scope quality.Scope, definitionDigest string) (string, error) {
	if r.Engine == nil || r.Engine.DB == nil || r.Owner == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.CheckID) || request.Actor != "fixture" {
		return "", errors.New("valid check request, engine and live owner required")
	}
	if !qualityFixture(scope) {
		return "", errors.New("fixture check requires disposable fixture repositories; production check dispatch remains disabled")
	}
	effectID := store.Digest([]byte("quality.check\x00" + request.CommandID))
	args, _ := json.Marshal(request)
	receipt, err := r.Engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: "core", Kind: "quality.check.prepare", Args: args}, func(tx *store.Tx) (any, error) {
		if err := quality.EnsureTargetDispatchable(ctx, tx, scope.Target); err != nil {
			return nil, err
		}
		var projectState, targetState string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", r.Engine.ProjectID).Scan(&projectState); err != nil {
			return nil, err
		}
		if projectState == "paused" || projectState == "recovering" || projectState == "quarantined" {
			return nil, errors.New("project state prevents check dispatch")
		}
		if scope.Target.Kind == "task" {
			if err := tx.QueryRowContext(ctx, "SELECT state FROM tasks WHERE id=? AND plan_id=?", scope.Target.TaskID, scope.Target.PlanID).Scan(&targetState); err != nil {
				return nil, err
			}
			if targetState == "accepted" {
				var currentAcceptances int
				if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM quality_acceptances_v2 WHERE task_id=? AND invalidated_at IS NULL", scope.Target.TaskID).Scan(&currentAcceptances); err != nil {
					return nil, err
				}
				if currentAcceptances == 0 {
					if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='checking',block_reason='quality authority changed; fresh evidence required' WHERE id=? AND state='accepted'", scope.Target.TaskID); err != nil {
						return nil, err
					}
					if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='active' WHERE id=? AND state='verifying'", scope.Target.PlanID); err != nil {
						return nil, err
					}
					targetState = "checking"
				}
			}
			if targetState != "checking" {
				return nil, errors.New("task is not checking")
			}
		} else {
			if err := tx.QueryRowContext(ctx, "SELECT state FROM plans WHERE id=?", scope.Target.PlanID).Scan(&targetState); err != nil {
				return nil, err
			}
			if targetState != "verifying" {
				return nil, errors.New("plan is not verifying")
			}
		}
		var charged, unknown, limit int64
		var budgetErr error
		if scope.Target.Kind == "task" {
			budgetErr = tx.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND plan_id=? AND task_id=?", scope.Target.PlanID, scope.Target.TaskID).Scan(&charged, &unknown, &limit)
		} else {
			budgetErr = tx.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='plan_services' AND plan_id=? AND task_id IS NULL", scope.Target.PlanID).Scan(&charged, &unknown, &limit)
		}
		if budgetErr != nil {
			return nil, budgetErr
		}
		if charged+unknown >= limit {
			return nil, errors.New("cumulative quality budget exhausted")
		}
		intent, _ := json.Marshal(map[string]any{"target": request.Target, "check_id": request.CheckID, "scope_id": scope.ID, "definition_digest": definitionDigest})
		_, err := tx.ExecContext(ctx, `INSERT INTO quality_effects_v2(id,scope_id,kind,definition_id,definition_digest,actor,state,intent_json,prepared_at) VALUES(?,?,'check',?,?,?,'prepared',?,?)`, effectID, scope.ID, request.CheckID, definitionDigest, request.Actor, string(intent), store.Now())
		if err != nil {
			return nil, err
		}
		return map[string]string{"effect_id": effectID}, nil
	})
	if err != nil {
		return "", err
	}
	var response map[string]string
	if err := json.Unmarshal(receipt, &response); err != nil {
		return "", err
	}
	return response["effect_id"], nil
}

func qualityFixture(scope quality.Scope) bool {
	for _, repository := range scope.Repositories {
		info, err := os.Lstat(filepath.Join(repository.Identity.Root, ".vigil-disposable-fixture"))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func copyProject(source, destination string) error {
	count := 0
	type copiedDirectory struct {
		path string
		mode os.FileMode
	}
	var directories []copiedDirectory
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(destination, 0700)
		}
		if entry.Name() == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		count++
		if count > 100000 {
			return errors.New("isolated check copy exceeds 100000 entries")
		}
		target := filepath.Join(destination, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := os.Mkdir(target, 0700); err != nil {
				return err
			}
			directories = append(directories, copiedDirectory{path: target, mode: info.Mode().Perm()})
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) || link == ".." || strings.HasPrefix(filepath.Clean(link), ".."+string(filepath.Separator)) {
				return errors.New("isolated check rejects escaping symlink")
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return errors.New("isolated check rejects special file")
		}
		if info.Size() > 64<<20 {
			return errors.New("isolated check file exceeds 64 MiB")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, (64<<20)+1))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		// Creation honors the process umask. Restore the source mode explicitly so
		// an isolated copy has the same source evidence under restrictive umasks.
		return os.Chmod(target, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(directories[i].path, directories[i].mode); err != nil {
			return err
		}
	}
	return nil
}

type treeEntry struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

// treeDigest identifies every source byte in an isolated check. Approved
// output paths are the only locations a check may create or replace.
func treeDigest(root string, outputs []string) (string, error) {
	excluded := make(map[string]bool, len(outputs))
	for _, output := range outputs {
		excluded[filepath.ToSlash(filepath.Clean(filepath.FromSlash(output)))] = true
	}
	entries := make([]treeEntry, 0)
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if excluded[rel] {
			return nil
		}
		count++
		if count > 100000 {
			return errors.New("check source digest exceeds 100000 entries")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := treeEntry{Path: rel, Mode: uint32(info.Mode().Perm())}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			item.Kind, item.Digest = "symlink", store.Digest([]byte(target))
		case info.Mode().IsRegular():
			if info.Size() > 64<<20 {
				return errors.New("check source digest file exceeds 64 MiB")
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item.Kind, item.Digest = "file", store.Digest(content)
		default:
			return errors.New("check source digest rejects special file")
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return store.Digest(raw), nil
}

// runContained keeps stdout bounded even when a descendant inherits it, and
// does not report terminal completion until both the Unix process group and
// every observed descendant (including a process that creates a new session)
// are absent. All Stage 5 production targets are Unix; production dispatch is
// still disabled independently of this containment implementation.
func runContained(ctx context.Context, command *exec.Cmd, output io.Writer) (error, bool) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err, false
	}
	command.Stdout, command.Stderr = writer, writer
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	marker := "VIGIL_CHECK_CONTAINMENT_ID=" + store.ID()
	command.Env = append(command.Env, marker)
	containment, err := prepareProcessContainment(command)
	if err != nil {
		reader.Close()
		writer.Close()
		return err, false
	}
	defer containment.close()
	if err = command.Start(); err != nil {
		reader.Close()
		writer.Close()
		return err, true
	}
	if err = containment.started(ctx); err != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		reader.Close()
		writer.Close()
		return fmt.Errorf("retire supervisor configuration: %w", err), false
	}
	pid := command.Process.Pid
	if err = activateProcessContainment(pid); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = command.Wait()
		reader.Close()
		writer.Close()
		return err, false
	}
	tracker := newProcessTracker(pid, marker)
	defer tracker.close()
	tracker.start()
	_ = writer.Close()
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, reader)
		close(drained)
	}()
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	var runErr error
	select {
	case runErr = <-waited:
	case <-ctx.Done():
		// The supervisor owns authoritative descendant cleanup. Ask it to stop
		// first and do not kill the proof-producing process on the same deadline
		// as its child termination phase.
		tracker.signalRoot(syscall.SIGTERM)
		select {
		case runErr = <-waited:
		case <-time.After(processContainmentShutdownGrace()):
			tracker.signal(syscall.SIGKILL)
			runErr = <-waited
		}
	}
	completionErr := containment.completed(command)
	tracker.stop()
	lifecycleStopped, reliable := true, true
	if completionErr != nil || !containment.authoritative() {
		groupHadDescendants := syscall.Kill(-pid, 0) == nil
		// Without authoritative supervisor proof, signal both the process group
		// and every observed detached process. This is best-effort cleanup only;
		// completionErr still prevents it from creating containment authority.
		tracker.signal(syscall.SIGTERM)
		deadline := time.Now().Add(500 * time.Millisecond)
		for tracker.alive() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		tracker.signal(syscall.SIGKILL)
		deadline = time.Now().Add(2 * time.Second)
		for tracker.alive() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		lifecycleStopped = !tracker.alive()
		reliable = tracker.reliable() && (!unresolvedProcessFork(pid) || groupHadDescendants || tracker.hasDescendants())
	}
	contained := lifecycleStopped && reliable && completionErr == nil
	_ = reader.Close()
	drainedOutput := true
	select {
	case <-drained:
	case <-time.After(time.Second):
		contained = false
		drainedOutput = false
	}
	if !contained {
		var containmentErr error
		if !lifecycleStopped || !reliable {
			containmentErr = tracker.failure()
		}
		containmentErr = errors.Join(containmentErr, completionErr)
		if !drainedOutput {
			containmentErr = errors.Join(containmentErr, errors.New("check output did not drain"))
		}
		if runErr != nil {
			containmentErr = errors.Join(runErr, containmentErr)
		}
		runErr = fmt.Errorf("check containment failed (lifecycle_stopped=%t reliable=%t output_drained=%t): %w", lifecycleStopped, reliable, drainedOutput, containmentErr)
	}
	return runErr, contained
}

func requiredOutputs(root string, paths []string) ([]OutputObservation, bool) {
	result := make([]OutputObservation, 0, len(paths))
	for _, path := range paths {
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 64<<20 {
			return result, false
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return result, false
		}
		result = append(result, OutputObservation{Path: path, Digest: store.Digest(content), Bytes: int64(len(content))})
	}
	return result, true
}

func checkEnvironment(check policy.CheckDefinition, home, temporary string) ([]string, string) {
	environment := []string{"HOME=" + home, "TMPDIR=" + temporary, "LANG=C", "LC_ALL=C"}
	searchPath := ""
	for _, variable := range check.Environment {
		environment = append(environment, variable.Name+"="+variable.Value)
		if variable.Name == "PATH" {
			searchPath = variable.Value
		}
	}
	return environment, searchPath
}

func resolveExecutable(name, cwd, searchPath string) (string, error) {
	if filepath.IsAbs(name) {
		info, err := os.Stat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", errors.New("approved absolute check executable is missing or not executable")
		}
		return name, nil
	}
	if strings.ContainsAny(name, `/\`) {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", errors.New("check executable escapes isolated workspace")
		}
		candidate := filepath.Join(cwd, clean)
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", errors.New("approved workspace check executable is missing or not executable")
		}
		return candidate, nil
	}
	if searchPath == "" {
		return "", errors.New("bare check executable requires an explicitly approved PATH")
	}
	for _, directory := range filepath.SplitList(searchPath) {
		if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
			return "", errors.New("approved PATH entries must be absolute and canonical")
		}
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New("check executable is absent from the explicitly approved PATH")
}

func failureIdentities(output []byte, status string, exitCode *int) []string {
	if status == "pass" {
		return []string{}
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			set[store.Digest([]byte(line))] = true
		}
	}
	code := "none"
	if exitCode != nil {
		code = fmt.Sprint(*exitCode)
	}
	set[store.Digest([]byte("status="+status+";exit="+code))] = true
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func (r *Runner) Run(ctx context.Context, request Request) (Result, error) {
	var result Result
	scope, err := quality.Observe(ctx, r.Engine, request.Target)
	if err != nil {
		return result, err
	}
	if err = quality.Persist(ctx, r.Engine, scope); err != nil {
		return result, err
	}
	check, definitionDigest, err := definition(scope, request.CheckID)
	if err != nil {
		return result, err
	}
	effectID, err := r.prepare(ctx, request, scope, definitionDigest)
	if err != nil {
		var state string
		if queryErr := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM quality_effects_v2 WHERE id=?", store.Digest([]byte("quality.check\x00"+request.CommandID))).Scan(&state); queryErr == nil && state != "prepared" {
			return result, fmt.Errorf("check effect already %s; automatic replay is forbidden", state)
		}
		return result, err
	}
	var existing string
	err = r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT id FROM check_results_v2 WHERE effect_id=?", effectID).Scan(&existing)
	if err == nil {
		return r.Load(ctx, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	roots := make([]workspace.Identity, 0, len(scope.Repositories))
	for _, repository := range scope.Repositories {
		roots = append(roots, repository.Identity)
	}
	claims, err := r.Owner.Claims(ctx, r.Engine.ProjectID, roots)
	ownedForCheck := false
	if err != nil {
		claims, err = r.Owner.Claim(ctx, r.Engine.ProjectID, effectID, roots)
		ownedForCheck = err == nil
	}
	if err != nil {
		return result, err
	}
	releaseProof := "never_started"
	releaseAllowed := true
	defer func() {
		if ownedForCheck && releaseAllowed {
			for _, claim := range claims {
				_ = r.Owner.Release(context.Background(), claim, releaseProof)
			}
		}
	}()
	var heldResult Result
	var effectErr error
	err = r.Owner.HoldClaims(ctx, r.Engine.ProjectID, roots, claims, func() error {
		current, err := quality.Observe(ctx, r.Engine, request.Target)
		if err != nil {
			return err
		}
		if len(quality.StaleReasons(scope, current)) != 0 {
			return errors.New("quality scope changed before check start")
		}
		if err := r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
			var state, projectState string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM quality_effects_v2 WHERE id=?", effectID).Scan(&state); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", r.Engine.ProjectID).Scan(&projectState); err != nil {
				return err
			}
			if state != "prepared" || projectState != "ready" {
				return errors.New("check dispatch became unavailable")
			}
			started := store.Now()
			if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='executing',started_at=? WHERE id=?", started, effectID); err != nil {
				return err
			}
			return quality.StartBudgetSegment(ctx, tx, scope, effectID, started)
		}); err != nil {
			return err
		}
		releaseAllowed = false
		heldResult, effectErr = r.runHeld(ctx, effectID, scope, check, definitionDigest)
		return nil
	})
	if err != nil {
		return result, err
	}
	if effectErr != nil {
		_ = quality.MarkEffectUncertain(context.Background(), r.Engine, effectID, effectErr.Error())
		return heldResult, effectErr
	}
	releaseProof = "contained_stopped"
	releaseAllowed = true
	return heldResult, nil
}

// runHeld executes while Owner.HoldClaims retains the live owner capability.
// No sibling operation can release or replace the fences between durable
// effect start, source copy, subprocess containment and terminal observation.
func (r *Runner) runHeld(ctx context.Context, effectID string, scope quality.Scope, check policy.CheckDefinition, definitionDigest string) (Result, error) {
	var result Result
	if r.Hook != nil {
		if err := r.Hook("after_effect_start"); err != nil {
			return result, err
		}
	}
	isolated, err := os.MkdirTemp("", "vigil-check-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(isolated)
	projectRoot := scope.Repositories[0].Identity.Root
	for _, repository := range scope.Repositories {
		if len(repository.Identity.Root) < len(projectRoot) {
			projectRoot = repository.Identity.Root
		}
	}
	started, err := quality.EffectStartedAt(ctx, r.Engine, effectID)
	if err != nil {
		return result, err
	}
	sourceBefore, err := treeDigest(projectRoot, check.RequiredOutputs)
	if err != nil {
		return r.finishError(ctx, effectID, scope, check, definitionDigest, nil, nil, "error", err.Error(), started, store.Now())
	}
	if err = copyProject(projectRoot, filepath.Join(isolated, "work")); err != nil {
		return r.finishError(ctx, effectID, scope, check, definitionDigest, nil, nil, "error", err.Error(), started, store.Now())
	}
	work := filepath.Join(isolated, "work")
	copyDigest, err := treeDigest(work, check.RequiredOutputs)
	if err != nil || copyDigest != sourceBefore {
		return r.finishError(ctx, effectID, scope, check, definitionDigest, nil, nil, "source_mutated", "isolated source copy does not match enrolled source", started, store.Now())
	}
	sourceAfterCopy, err := treeDigest(projectRoot, check.RequiredOutputs)
	if err != nil || sourceAfterCopy != sourceBefore {
		return r.finishError(ctx, effectID, scope, check, definitionDigest, nil, nil, "source_mutated", "source changed while preparing isolated check", started, store.Now())
	}
	limit := check.OutputLimit()
	output := &cappedBuffer{limit: limit}
	remainingMS, err := quality.RemainingBudgetMS(ctx, r.Engine, scope)
	if err != nil {
		return result, err
	}
	timeoutMS := check.TimeoutMS
	if remainingMS < timeoutMS {
		timeoutMS = remainingMS
	}
	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	commandDir := filepath.Join(work, filepath.FromSlash(check.Cwd))
	home := filepath.Join(isolated, "home")
	tmp := filepath.Join(isolated, "tmp")
	_ = os.Mkdir(home, 0700)
	_ = os.Mkdir(tmp, 0700)
	environment, searchPath := checkEnvironment(check, home, tmp)
	resolved, resolveErr := resolveExecutable(check.Argv[0], commandDir, searchPath)
	if resolveErr != nil {
		ended := store.Now()
		return r.finishError(ctx, effectID, scope, check, definitionDigest, nil, nil, "error", resolveErr.Error(), started, ended)
	}
	command := exec.Command(resolved, check.Argv[1:]...)
	command.Dir = commandDir
	command.Env = environment
	runErr, contained := runContained(checkCtx, command, output)
	status := "pass"
	var exitCode *int
	if command.ProcessState != nil {
		code := command.ProcessState.ExitCode()
		exitCode = &code
	}
	if checkCtx.Err() == context.DeadlineExceeded {
		status = "timeout"
	} else if ctx.Err() != nil {
		status = "interrupted"
	} else if runErr != nil {
		status = "fail"
	}
	if output.overflow {
		status = "output_overflow"
	}
	if !contained {
		return result, fmt.Errorf("check descendant containment could not be proven: %w", runErr)
	}
	evaluatedAfter, digestErr := treeDigest(work, check.RequiredOutputs)
	if digestErr != nil || evaluatedAfter != copyDigest {
		status = "source_mutated"
	}
	outputs, present := requiredOutputs(work, check.RequiredOutputs)
	if !present && status != "timeout" && status != "interrupted" && status != "output_overflow" {
		status = "missing_output"
	}
	if r.Hook != nil {
		if err := r.Hook("before_source_recheck"); err != nil {
			return result, err
		}
	}
	observedDigest := scope.RepositorySetDigest
	if ctx.Err() == nil {
		current, observeErr := quality.Observe(ctx, r.Engine, scope.Target)
		if observeErr != nil {
			status = "error"
		} else {
			observedDigest = current.RepositorySetDigest
			if len(quality.StaleReasons(scope, current)) != 0 {
				status = "source_mutated"
			}
		}
	}
	finishCtx := ctx
	var finishCancel context.CancelFunc
	if ctx.Err() != nil {
		finishCtx, finishCancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer finishCancel()
	}
	return r.finish(finishCtx, effectID, scope, check, definitionDigest, output.Bytes(), outputs, status, exitCode, started, observedDigest)
}

func (r *Runner) finishError(ctx context.Context, effectID string, scope quality.Scope, check policy.CheckDefinition, definitionDigest string, output []byte, outputs []OutputObservation, status, message string, started, _ int64) (Result, error) {
	output = append(output, []byte(message)...)
	return r.finish(ctx, effectID, scope, check, definitionDigest, output, outputs, status, nil, started, scope.RepositorySetDigest)
}

func (r *Runner) finish(ctx context.Context, effectID string, scope quality.Scope, check policy.CheckDefinition, definitionDigest string, output []byte, outputs []OutputObservation, status string, exitCode *int, started int64, observedDigest string) (Result, error) {
	var result Result
	repository, err := artifacts.New(r.Engine.DB)
	if err != nil {
		return result, err
	}
	artifact, err := repository.PutCore(ctx, store.Digest([]byte(effectID+"\x00output")), "check-output", "durable", bytes.NewReader(output))
	if err != nil {
		return result, err
	}
	result.ID = store.ID()
	err = r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM quality_effects_v2 WHERE id=?", effectID).Scan(&state); err != nil {
			return err
		}
		if state != "executing" {
			return errors.New("check effect is not executing")
		}
		ended := store.Now()
		duration, exhausted, err := quality.FinishBudgetSegment(ctx, tx, effectID, ended)
		if err != nil {
			return err
		}
		terminalStatus := status
		if exhausted && terminalStatus == "pass" {
			terminalStatus = "error"
		}
		result = Result{ID: result.ID, EffectID: effectID, ScopeID: scope.ID, CheckID: check.ID, DefinitionDigest: definitionDigest, Status: terminalStatus, ExitCode: exitCode, FailureIdentities: failureIdentities(output, terminalStatus, exitCode), RequiredOutputs: outputs, OutputArtifactID: artifact.ID, OutputArtifactDigest: artifact.Digest, EvaluatedDigest: scope.RepositorySetDigest, ObservedDigest: observedDigest, StartedAt: started, EndedAt: ended, DurationMS: duration}
		digestValue := result
		digestValue.ID, digestValue.ResultDigest = "", ""
		raw, _ := json.Marshal(digestValue)
		canonical, _ := store.Canonical(raw)
		result.ResultDigest = store.Digest(canonical)
		failures, _ := json.Marshal(result.FailureIdentities)
		required, _ := json.Marshal(outputs)
		observation, _ := json.Marshal(result)
		if _, err := tx.ExecContext(ctx, `INSERT INTO check_results_v2(id,effect_id,scope_id,check_id,definition_digest,status,exit_code,failure_identities_json,required_outputs_json,output_artifact_id,output_artifact_digest,evaluated_repository_set_digest,observed_repository_set_digest,started_at,ended_at,duration_ms,result_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, result.ID, effectID, scope.ID, check.ID, definitionDigest, result.Status, nullableInt(exitCode), string(failures), string(required), artifact.ID, artifact.Digest, scope.RepositorySetDigest, observedDigest, started, ended, result.DurationMS, result.ResultDigest); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='observed',observation_json=?,observed_at=? WHERE id=?", string(observation), ended, effectID); err != nil {
			return err
		}
		if scope.Target.Kind == "task" && result.Status != "pass" {
			_, err = tx.ExecContext(ctx, "UPDATE tasks SET state='needs_repair',block_reason=? WHERE id=? AND state='checking'", "check "+check.ID+" returned "+result.Status, scope.Target.TaskID)
		}
		return err
	})
	return result, err
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func (r *Runner) Load(ctx context.Context, id string) (Result, error) {
	var result Result
	var failures, outputs string
	err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT id,effect_id,scope_id,check_id,definition_digest,status,exit_code,failure_identities_json,required_outputs_json,output_artifact_id,output_artifact_digest,evaluated_repository_set_digest,observed_repository_set_digest,started_at,ended_at,duration_ms,result_digest FROM check_results_v2 WHERE id=?`, id).Scan(&result.ID, &result.EffectID, &result.ScopeID, &result.CheckID, &result.DefinitionDigest, &result.Status, &result.ExitCode, &failures, &outputs, &result.OutputArtifactID, &result.OutputArtifactDigest, &result.EvaluatedDigest, &result.ObservedDigest, &result.StartedAt, &result.EndedAt, &result.DurationMS, &result.ResultDigest)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal([]byte(failures), &result.FailureIdentities); err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(outputs), &result.RequiredOutputs)
	return result, err
}

func signalName(state *os.ProcessState) string {
	if state == nil {
		return ""
	}
	if wait, ok := state.Sys().(syscall.WaitStatus); ok && wait.Signaled() {
		return wait.Signal().String()
	}
	return ""
}
