package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
	"vigil/internal/supervisor"
)

type recoveryFixture struct {
	manager  *core.Manager
	engine   *core.Engine
	prepared supervisor.PreparedRun
	root     string
	recovery *Manager
}

func applyCore(t *testing.T, engine *core.Engine, kind string, payload any) {
	t.Helper()
	var revision int
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	if _, err := engine.Apply(context.Background(), core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: kind, Payload: raw}); err != nil {
		t.Fatal(kind, err)
	}
}

func recoverySetup(t *testing.T) recoveryFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	write(t, filepath.Join(root, ".vigil-disposable-fixture"), []byte("fixture\n"), 0600)
	write(t, filepath.Join(root, "same.txt"), []byte("base\n"), 0600)
	write(t, filepath.Join(root, "delete.txt"), []byte("delete\n"), 0600)
	write(t, filepath.Join(root, "script.sh"), []byte("#!/bin/sh\n"), 0600)
	write(t, filepath.Join(root, "dir", "item.txt"), []byte("base item\n"), 0600)
	git(t, "init", "-q", "-b", "main", root)
	git(t, "-C", root, "add", ".")
	git(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "base")
	manager, err := core.OpenManager(context.Background(), filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := manager.Init(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := manager.Open(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.DB.Close(); manager.Close() })
	config := policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"test"}, CheckDefinitions: []policy.CheckDefinition{{ID: "test", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}}, TaskLimitMS: 60000, AttemptLimitMS: 30000, RepairLimit: 1, SupervisorProfile: "local", ApprovalMode: "supervised"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	task := policy.Task{ID: "task", Objective: "fixture recovery", Criteria: []policy.Criterion{{ID: "c1", Text: "preserved"}}, Scope: []string{"**"}, Implementation: "local", Reviewer: "local", Checks: []string{"test"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 1}
	applyCore(t, engine, "project.configure", config)
	applyCore(t, engine, "profile.put", profile)
	applyCore(t, engine, "plan.put", core.Plan{ID: "plan", Title: "Fixture", Specification: "exercise recovery", Approved: true, Tasks: []policy.Task{task}})
	applyCore(t, engine, "repository.enroll", core.RepositoryEnrollment{ID: "repo", PlanID: "plan", Root: root, BaseRef: "main", PlanBranch: "vigil/fixture", DirtyChoice: "clean"})
	var revision int
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareRepository(context.Background(), "prepare-repo", "repo", revision); err != nil {
		t.Fatal(err)
	}
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	prepared, err := supervisor.Prepare(context.Background(), engine, supervisor.PrepareRequest{CommandID: "prepare-run", ExpectedProjectRevision: revision, TaskID: "task", RuntimeKind: "synthetic", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := NewManager(engine)
	if err != nil {
		t.Fatal(err)
	}
	return recoveryFixture{manager: manager, engine: engine, prepared: prepared, root: root, recovery: recovery}
}

func revision(t *testing.T, engine *core.Engine) int {
	t.Helper()
	var result int
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func saveRun(t *testing.T, fixture recoveryFixture, command string) SaveReceipt {
	t.Helper()
	repository := fixture.prepared.Repositories[0]
	receipt, err := fixture.recovery.Save(context.Background(), SaveRequest{CommandID: command, ExpectedRevision: revision(t, fixture.engine), RunID: fixture.prepared.RunID, Repositories: []RepositorySpec{{ID: repository.ID, Root: repository.Root, Identity: repository.Identity, Exclusions: repository.Baseline.Exclusions, UntrackedScope: fixture.prepared.Task.Scope}}})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func mutateRecoveryFixture(t *testing.T, fixture recoveryFixture) {
	t.Helper()
	write(t, filepath.Join(fixture.root, "same.txt"), []byte("staged\n"), 0600)
	git(t, "-C", fixture.root, "add", "same.txt")
	write(t, filepath.Join(fixture.root, "same.txt"), []byte("unstaged\n"), 0600)
	if err := os.Remove(filepath.Join(fixture.root, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(fixture.root, "script.sh"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(fixture.root, "binary.dat"), []byte{0, 1, 0xff, 2}, 0600)
	if err := os.Symlink("same.txt", filepath.Join(fixture.root, "link")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(fixture.root, "dir", "item.txt"), []byte("agent item\n"), 0600)
}

func ownedRecoveryPaths() map[string][]string {
	return map[string][]string{"repo": {"same.txt", "delete.txt", "script.sh", "binary.dat", "link", "dir/item.txt"}}
}

func TestSaveAndClearRoundTripWithPerPathRecovery(t *testing.T) {
	fixture := recoverySetup(t)
	baseline := saveRun(t, fixture, "save-baseline")
	mutateRecoveryFixture(t, fixture)
	captured := saveRun(t, fixture, "save-captured")
	if baseline.State != "verified" || captured.State != "verified" {
		t.Fatal(baseline, captured)
	}
	alias := filepath.Join(filepath.Dir(fixture.root), "work-alias")
	if err := os.Symlink(fixture.root, alias); err != nil {
		t.Fatal(err)
	}
	again, err := fixture.recovery.Save(context.Background(), SaveRequest{CommandID: "save-captured", ExpectedRevision: revision(t, fixture.engine) - 1, RunID: fixture.prepared.RunID, Repositories: []RepositorySpec{{ID: fixture.prepared.Repositories[0].ID, Root: alias, Identity: fixture.prepared.Repositories[0].Identity, Exclusions: fixture.prepared.Repositories[0].Baseline.Exclusions, UntrackedScope: fixture.prepared.Task.Scope}}})
	if err != nil || !again.Repeated || again.CheckpointID != captured.CheckpointID {
		t.Fatal(again, err)
	}
	request := ClearRequest{CommandID: "clear", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: ownedRecoveryPaths()}
	injected := errors.New("controller interrupted after apply")
	fired := false
	fixture.recovery.Fault = func(point string) error {
		if !fired {
			fired = true
			return injected
		}
		return nil
	}
	if _, err := fixture.recovery.Clear(context.Background(), request); !errors.Is(err, injected) {
		t.Fatal("apply interruption not reached", err)
	}
	fixture.recovery.Fault = nil
	receipt, err := fixture.recovery.Clear(context.Background(), request)
	if err != nil || receipt.State != "saved" || !receipt.Repeated {
		t.Fatal(receipt, err)
	}
	if content, err := os.ReadFile(filepath.Join(fixture.root, "same.txt")); err != nil || string(content) != "base\n" {
		t.Fatal("worktree bytes not restored", string(content), err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "delete.txt")); err != nil {
		t.Fatal("tracked deletion not restored", err)
	}
	if info, err := os.Stat(filepath.Join(fixture.root, "script.sh")); err != nil || info.Mode().Perm()&0100 != 0 {
		t.Fatal("mode not restored", info, err)
	}
	for _, path := range []string{"binary.dat", "link"} {
		if _, err := os.Lstat(filepath.Join(fixture.root, path)); !os.IsNotExist(err) {
			t.Fatal("created path not cleared", path, err)
		}
	}
	staged := git(t, "-C", fixture.root, "diff", "--cached", "--", "same.txt")
	unstaged := git(t, "-C", fixture.root, "diff", "--", "same.txt")
	if len(staged) != 0 || len(unstaged) != 0 {
		t.Fatal("index/worktree distinctions did not return to baseline", string(staged), string(unstaged))
	}
	var progress, applied int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*),sum(state='applied') FROM checkpoint_path_progress WHERE operation_id=?", receipt.OperationID).Scan(&progress, &applied); err != nil || progress == 0 || progress != applied {
		t.Fatal("per-path journal incomplete", progress, applied, err)
	}
}

func TestSaveRejectsPreservationScopeSubstitution(t *testing.T) {
	fixture := recoverySetup(t)
	repository := fixture.prepared.Repositories[0]
	_, err := fixture.recovery.Save(context.Background(), SaveRequest{CommandID: "bad-scope", ExpectedRevision: revision(t, fixture.engine), RunID: fixture.prepared.RunID, Repositories: []RepositorySpec{{ID: repository.ID, Root: repository.Root, Identity: repository.Identity, Exclusions: repository.Baseline.Exclusions, UntrackedScope: []string{"secret/**"}}}})
	if err == nil || !strings.Contains(err.Error(), "preservation scope") {
		t.Fatal("substituted preservation scope reached capture", err)
	}
	var checkpoints int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM checkpoint_sets").Scan(&checkpoints); err != nil || checkpoints != 0 {
		t.Fatal("invalid scope persisted checkpoint intent", checkpoints, err)
	}
}

func TestClearRejectsConcurrentEditBeforeAnyApply(t *testing.T) {
	fixture := recoverySetup(t)
	baseline := saveRun(t, fixture, "save-baseline")
	mutateRecoveryFixture(t, fixture)
	captured := saveRun(t, fixture, "save-captured")
	write(t, filepath.Join(fixture.root, "same.txt"), []byte("manual-after-save\n"), 0600)
	request := ClearRequest{CommandID: "clear-conflict", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: ownedRecoveryPaths()}
	if _, err := fixture.recovery.Clear(context.Background(), request); err == nil {
		t.Fatal("concurrent edit was overwritten")
	}
	if content, err := os.ReadFile(filepath.Join(fixture.root, "same.txt")); err != nil || string(content) != "manual-after-save\n" {
		t.Fatal("manual edit changed", string(content), err)
	}
	var operations int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM recovery_operations WHERE kind='clear'").Scan(&operations); err != nil || operations != 0 {
		t.Fatal("failed preflight began a clear", operations, err)
	}
}

func clearCaptured(t *testing.T, fixture recoveryFixture, baseline, captured SaveReceipt, command string) RecoveryReceipt {
	t.Helper()
	receipt, err := fixture.recovery.Clear(context.Background(), ClearRequest{CommandID: command, ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: ownedRecoveryPaths()})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestApprovedRestoreRoundTripsIndexWorktreeModesAndSymlink(t *testing.T) {
	fixture := recoverySetup(t)
	baseline := saveRun(t, fixture, "save-baseline")
	mutateRecoveryFixture(t, fixture)
	captured := saveRun(t, fixture, "save-captured")
	clearCaptured(t, fixture, baseline, captured, "clear")
	destination := saveRun(t, fixture, "save-destination")
	request := RestoreRequest{CommandID: "restore", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, DestinationCheckpointID: destination.CheckpointID}
	injected := errors.New("restore interrupted after apply")
	fired := false
	fixture.recovery.Fault = func(point string) error {
		if !fired {
			fired = true
			return injected
		}
		return nil
	}
	if _, err := fixture.recovery.Restore(context.Background(), request); !errors.Is(err, injected) {
		t.Fatal("restore apply interruption not reached", err)
	}
	fixture.recovery.Fault = nil
	receipt, err := fixture.recovery.Restore(context.Background(), request)
	if err != nil || receipt.State != "restored" {
		t.Fatal(receipt, err)
	}
	if err := fixture.recovery.verifyCurrentDestination(context.Background(), mustReadSet(t, fixture.recovery, captured.CheckpointID)); err != nil {
		t.Fatal("restored checkout does not match saved recovery set", err)
	}
	staged := git(t, "-C", fixture.root, "show", ":same.txt")
	worktree, err := os.ReadFile(filepath.Join(fixture.root, "same.txt"))
	if err != nil || string(staged) != "staged\n" || string(worktree) != "unstaged\n" {
		t.Fatal("staged and unstaged bytes were not restored", string(staged), string(worktree), err)
	}
	if target, err := os.Readlink(filepath.Join(fixture.root, "link")); err != nil || target != "same.txt" {
		t.Fatal("symlink target was not restored", target, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatal("tracked deletion was not restored", err)
	}
	if info, err := os.Stat(filepath.Join(fixture.root, "script.sh")); err != nil || info.Mode().Perm()&0100 == 0 {
		t.Fatal("executable mode was not restored", info, err)
	}
	// The exact same approved command is a receipt lookup, not another apply.
	again, err := fixture.recovery.Restore(context.Background(), request)
	if err != nil || !again.Repeated || again.OperationID != receipt.OperationID {
		t.Fatal(again, err)
	}
}

func TestClearRejectsReplacedParentAtApplyBoundary(t *testing.T) {
	fixture := recoverySetup(t)
	baseline := saveRun(t, fixture, "save-baseline")
	mutateRecoveryFixture(t, fixture)
	captured := saveRun(t, fixture, "save-captured")
	original := filepath.Join(fixture.root, "dir-original")
	if err := os.Rename(filepath.Join(fixture.root, "dir"), original); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	write(t, filepath.Join(outside, "item.txt"), []byte("agent item\n"), 0600)
	if err := os.Symlink(outside, filepath.Join(fixture.root, "dir")); err != nil {
		t.Fatal(err)
	}
	request := ClearRequest{CommandID: "clear-replaced-parent", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: ownedRecoveryPaths()}
	receipt, err := fixture.recovery.Clear(context.Background(), request)
	if err == nil || receipt.State != "conflicted" {
		t.Fatal("replaced parent was not rejected at the intended apply", receipt, err)
	}
	content, readErr := os.ReadFile(filepath.Join(outside, "item.txt"))
	if readErr != nil || string(content) != "agent item\n" {
		t.Fatal("outside path changed through replacement", string(content), readErr)
	}
	var operations int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM recovery_operations WHERE kind='clear'").Scan(&operations); err != nil || operations != 1 {
		t.Fatal("negative test did not reach clear operation", operations, err)
	}
}

func TestIncompleteSaveCannotAuthorizeClear(t *testing.T) {
	fixture := recoverySetup(t)
	baseline := saveRun(t, fixture, "save-baseline")
	mutateRecoveryFixture(t, fixture)
	fixture.recovery.Store.Fault = func(point string) error { return errors.New("injected checkpoint write failure") }
	repository := fixture.prepared.Repositories[0]
	failed, err := fixture.recovery.Save(context.Background(), SaveRequest{CommandID: "save-failed", ExpectedRevision: revision(t, fixture.engine), RunID: fixture.prepared.RunID, Repositories: []RepositorySpec{{ID: repository.ID, Root: repository.Root, Identity: repository.Identity, Exclusions: repository.Baseline.Exclusions, UntrackedScope: fixture.prepared.Task.Scope}}})
	if err == nil {
		t.Fatal("injected snapshot failure succeeded")
	}
	fixture.recovery.Store.Fault = nil
	before, _ := os.ReadFile(filepath.Join(fixture.root, "same.txt"))
	_, clearErr := fixture.recovery.Clear(context.Background(), ClearRequest{CommandID: "clear-failed", ExpectedRevision: revision(t, fixture.engine), CheckpointID: failed.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: ownedRecoveryPaths()})
	if clearErr == nil {
		t.Fatal("incomplete checkpoint authorized clear")
	}
	after, _ := os.ReadFile(filepath.Join(fixture.root, "same.txt"))
	if string(before) != string(after) || string(after) != "unstaged\n" {
		t.Fatal("failed snapshot cleared work", string(after))
	}
	var clearOperations int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM recovery_operations WHERE kind='clear'").Scan(&clearOperations); err != nil || clearOperations != 0 {
		t.Fatal("failed snapshot created clear effects", clearOperations, err)
	}
}

func mustReadSet(t *testing.T, manager *Manager, id string) SetManifest {
	t.Helper()
	manifest, _, err := manager.loadRecoverySet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestChangedDestinationUsesConflictPreservingRestore(t *testing.T) {
	fixture := recoverySetup(t)
	baseline := saveRun(t, fixture, "save-baseline")
	mutateRecoveryFixture(t, fixture)
	captured := saveRun(t, fixture, "save-captured")
	clearCaptured(t, fixture, baseline, captured, "clear")
	write(t, filepath.Join(fixture.root, "same.txt"), []byte("manual-destination\n"), 0600)
	destination := saveRun(t, fixture, "save-destination")
	request := RestoreRequest{CommandID: "restore-conflict", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, DestinationCheckpointID: destination.CheckpointID}
	receipt, err := fixture.recovery.Restore(context.Background(), request)
	if !errors.Is(err, ErrRestoreConflict) || receipt.State != "conflicted" || receipt.Conflicts == 0 {
		t.Fatal(receipt, err)
	}
	content, readErr := os.ReadFile(filepath.Join(fixture.root, "same.txt"))
	if readErr != nil || string(content) != "manual-destination\n" {
		t.Fatal("divergent manual destination was overwritten", string(content), readErr)
	}
	for _, id := range []string{captured.CheckpointID, destination.CheckpointID} {
		manifest, _, loadErr := fixture.recovery.loadRecoverySet(context.Background(), id)
		if loadErr != nil || fixture.recovery.Store.Verify(context.Background(), manifest) != nil {
			t.Fatal("recovery copy lost after conflict", id, loadErr)
		}
	}
	var conflictRows int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM checkpoint_path_progress WHERE operation_id=? AND state='conflicted'", receipt.OperationID).Scan(&conflictRows); err != nil || conflictRows == 0 {
		t.Fatal("conflict was not journaled", conflictRows, err)
	}
}

func multiRecoverySetup(t *testing.T) recoveryFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "parent")
	write(t, filepath.Join(root, "same.txt"), []byte("parent base\n"), 0600)
	git(t, "init", "-q", "-b", "main", root)
	git(t, "-C", root, "add", ".")
	git(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "parent base")
	child := filepath.Join(root, "child")
	write(t, filepath.Join(child, "same.txt"), []byte("child base\n"), 0600)
	git(t, "init", "-q", "-b", "main", child)
	git(t, "-C", child, "add", ".")
	git(t, "-C", child, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "child base")
	manager, err := core.OpenManager(context.Background(), filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := manager.Init(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := manager.Open(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.DB.Close(); manager.Close() })
	config := policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"test"}, CheckDefinitions: []policy.CheckDefinition{{ID: "test", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}}, TaskLimitMS: 60000, AttemptLimitMS: 30000, RepairLimit: 1, SupervisorProfile: "local", ApprovalMode: "supervised"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	task := policy.Task{ID: "task", Objective: "nested recovery", Criteria: []policy.Criterion{{ID: "c1", Text: "both preserved"}}, Scope: []string{"**"}, Implementation: "local", Reviewer: "local", Checks: []string{"test"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 1}
	applyCore(t, engine, "project.configure", config)
	applyCore(t, engine, "profile.put", profile)
	applyCore(t, engine, "plan.put", core.Plan{ID: "plan", Title: "Nested fixture", Specification: "exercise multi-repository recovery", Approved: true, Tasks: []policy.Task{task}})
	applyCore(t, engine, "repository.enroll", core.RepositoryEnrollment{ID: "parent", PlanID: "plan", Root: root, BaseRef: "main", PlanBranch: "vigil/parent", DirtyChoice: "clean", NestedBoundaries: []string{"child"}})
	applyCore(t, engine, "repository.enroll", core.RepositoryEnrollment{ID: "child", PlanID: "plan", Root: child, BaseRef: "main", PlanBranch: "vigil/child", DirtyChoice: "clean"})
	for _, id := range []string{"parent", "child"} {
		if _, err := engine.PrepareRepository(context.Background(), "prepare-"+id, id, revision(t, engine)); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := supervisor.Prepare(context.Background(), engine, supervisor.PrepareRequest{CommandID: "prepare-run", ExpectedProjectRevision: revision(t, engine), TaskID: "task", RuntimeKind: "synthetic", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := NewManager(engine)
	if err != nil {
		t.Fatal(err)
	}
	return recoveryFixture{manager: manager, engine: engine, prepared: prepared, root: root, recovery: recovery}
}

func saveAll(t *testing.T, fixture recoveryFixture, command string) SaveReceipt {
	t.Helper()
	var specifications []RepositorySpec
	for _, repository := range fixture.prepared.Repositories {
		specifications = append(specifications, RepositorySpec{ID: repository.ID, Root: repository.Root, Identity: repository.Identity, Exclusions: repository.Baseline.Exclusions, UntrackedScope: fixture.prepared.Task.Scope})
	}
	receipt, err := fixture.recovery.Save(context.Background(), SaveRequest{CommandID: command, ExpectedRevision: revision(t, fixture.engine), RunID: fixture.prepared.RunID, Repositories: specifications})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func mutateAll(t *testing.T, fixture recoveryFixture) map[string][]string {
	t.Helper()
	owned := map[string][]string{}
	for _, repository := range fixture.prepared.Repositories {
		content := []byte(repository.ID + " agent\n")
		write(t, filepath.Join(repository.Root, "same.txt"), content, 0600)
		write(t, filepath.Join(repository.Root, repository.ID+".bin"), append([]byte{0, 0xff}, content...), 0600)
		owned[repository.ID] = []string{"same.txt", repository.ID + ".bin"}
	}
	return owned
}

func TestCorruptRepositoryInSetPreventsEveryClear(t *testing.T) {
	fixture := multiRecoverySetup(t)
	baseline := saveAll(t, fixture, "save-baseline")
	owned := mutateAll(t, fixture)
	captured := saveAll(t, fixture, "save-captured")
	manifest := mustReadSet(t, fixture.recovery, captured.CheckpointID)
	child := findRepository(t, manifest, "child")
	entry := findPath(t, child, "same.txt")
	if err := os.WriteFile(filepath.Join(fixture.recovery.Store.Dir, "blobs", entry.Digest), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	parentBefore, _ := os.ReadFile(filepath.Join(fixture.root, "same.txt"))
	_, err := fixture.recovery.Clear(context.Background(), ClearRequest{CommandID: "clear-corrupt", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: owned})
	if err == nil {
		t.Fatal("corrupt repository checkpoint authorized a multi-repository clear")
	}
	parentAfter, _ := os.ReadFile(filepath.Join(fixture.root, "same.txt"))
	if string(parentBefore) != string(parentAfter) {
		t.Fatal("repository A cleared before repository B verification")
	}
	var clearOperations int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM recovery_operations WHERE kind='clear'").Scan(&clearOperations); err != nil || clearOperations != 0 {
		t.Fatal("corrupt set reached clear intent", clearOperations, err)
	}
}

func TestMultiRepositoryRestoreInterruptionRetainsBothRecoveryCopies(t *testing.T) {
	fixture := multiRecoverySetup(t)
	baseline := saveAll(t, fixture, "save-baseline")
	owned := mutateAll(t, fixture)
	captured := saveAll(t, fixture, "save-captured")
	if _, err := fixture.recovery.Clear(context.Background(), ClearRequest{CommandID: "clear", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, OwnedPaths: owned}); err != nil {
		t.Fatal(err)
	}
	destination := saveAll(t, fixture, "save-destination")
	request := RestoreRequest{CommandID: "restore", ExpectedRevision: revision(t, fixture.engine), CheckpointID: captured.CheckpointID, BaselineCheckpointID: baseline.CheckpointID, DestinationCheckpointID: destination.CheckpointID}
	injected := errors.New("stop after repository A apply")
	fixture.recovery.Fault = func(point string) error {
		if len(point) >= len("after_restore_apply:child:") && point[:len("after_restore_apply:child:")] == "after_restore_apply:child:" {
			return injected
		}
		return nil
	}
	if _, err := fixture.recovery.Restore(context.Background(), request); !errors.Is(err, injected) {
		t.Fatal("restore did not reach repository apply", err)
	}
	fixture.recovery.Fault = nil
	for _, id := range []string{captured.CheckpointID, destination.CheckpointID} {
		manifest, _, err := fixture.recovery.loadRecoverySet(context.Background(), id)
		if err != nil || fixture.recovery.Store.Verify(context.Background(), manifest) != nil {
			t.Fatal("partial restore lost a recovery copy", id, err)
		}
	}
	receipt, err := fixture.recovery.Restore(context.Background(), request)
	if err != nil || receipt.State != "restored" || !receipt.Repeated {
		t.Fatal(receipt, err)
	}
	for _, repository := range fixture.prepared.Repositories {
		content, err := os.ReadFile(filepath.Join(repository.Root, "same.txt"))
		if err != nil || string(content) != repository.ID+" agent\n" {
			t.Fatal("repository not restored", repository.ID, string(content), err)
		}
	}
}
