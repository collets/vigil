package checkpoint

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type ClearRequest struct {
	CommandID            string `json:"command_id"`
	ExpectedRevision     int    `json:"expected_revision"`
	CheckpointID         string `json:"checkpoint_id"`
	BaselineCheckpointID string `json:"baseline_checkpoint_id"`
	// OwnedPaths is retained only to reject legacy callers that attempted to
	// self-assert ownership. Ownership is derived from persisted result evidence.
	OwnedPaths map[string][]string `json:"owned_paths,omitempty"`
}

type RestoreRequest struct {
	CommandID               string `json:"command_id"`
	ExpectedRevision        int    `json:"expected_revision"`
	CheckpointID            string `json:"checkpoint_id"`
	BaselineCheckpointID    string `json:"baseline_checkpoint_id"`
	DestinationCheckpointID string `json:"destination_checkpoint_id"`
}

var ErrRestoreConflict = errors.New("restore preserved divergent destination changes as conflicts")

type RecoveryReceipt struct {
	OperationID             string `json:"operation_id"`
	CheckpointID            string `json:"checkpoint_id"`
	BaselineCheckpointID    string `json:"baseline_checkpoint_id,omitempty"`
	DestinationCheckpointID string `json:"destination_checkpoint_id,omitempty"`
	State                   string `json:"state"`
	Applied                 int    `json:"applied"`
	Conflicts               int    `json:"conflicts"`
	Repeated                bool   `json:"repeated,omitempty"`
}

type recoveryAction struct {
	repository RepositoryManifest
	path       string
	action     string
	expected   *PathEntry
	desired    *PathEntry
}

func repositoryMap(manifest SetManifest) map[string]RepositoryManifest {
	result := make(map[string]RepositoryManifest, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		result[repository.RepositoryID] = repository
	}
	return result
}

func pathMap(manifest RepositoryManifest) map[string]PathEntry {
	result := make(map[string]PathEntry, len(manifest.Paths))
	for _, entry := range manifest.Paths {
		result[entry.Path] = entry
	}
	return result
}

func indexMap(manifest RepositoryManifest) map[string]IndexEntry {
	result := make(map[string]IndexEntry, len(manifest.IndexEntries))
	for _, entry := range manifest.IndexEntries {
		result[entry.Path] = entry
	}
	return result
}

func pathState(entry *PathEntry) string {
	if entry == nil || entry.Kind == "deleted" {
		return store.Digest([]byte("absent"))
	}
	raw, _ := json.Marshal(entry)
	return store.Digest(raw)
}

func indexState(entry IndexEntry, exists bool) string {
	if !exists {
		return store.Digest([]byte("absent-index-entry"))
	}
	raw, _ := json.Marshal(entry)
	return store.Digest(raw)
}

func entryPointer(entries map[string]PathEntry, path string) *PathEntry {
	entry, ok := entries[path]
	if !ok {
		return nil
	}
	return &entry
}

func normalizeOwned(input map[string][]string) (map[string]map[string]bool, error) {
	result := map[string]map[string]bool{}
	for repository, paths := range input {
		if !store.SafeID(repository) || len(paths) == 0 || len(paths) > maxEntries {
			return nil, errors.New("owned path set is invalid")
		}
		result[repository] = map[string]bool{}
		for _, path := range paths {
			clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
			if clean != path || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || clean == ".git" || strings.HasPrefix(clean, ".git/") {
				return nil, errors.New("owned path must be a normalized repository-relative worktree path")
			}
			if result[repository][path] {
				return nil, errors.New("duplicate owned path")
			}
			result[repository][path] = true
		}
	}
	return result, nil
}

func (m *Manager) withRecoveryAuthority(ctx context.Context, manifest SetManifest, fn func() (RecoveryReceipt, error)) (RecoveryReceipt, error) {
	var zero RecoveryReceipt
	if m == nil || m.Owner == nil || m.Owner.Coordinator == nil || m.Reservation.Phase != "reserved" {
		return zero, errors.New("destructive recovery requires a live coordinator reservation")
	}
	var runID string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT run_id FROM checkpoint_sets WHERE id=?", manifest.ID).Scan(&runID); err != nil {
		return zero, err
	}
	if runID != m.Reservation.RunID {
		return zero, errors.New("recovery reservation is not bound to the checkpoint run")
	}
	roots := m.Reservation.Roots
	if len(roots) == 0 && m.Reservation.Root.Root != "" {
		roots = []workspace.Identity{m.Reservation.Root}
	}
	if len(roots) != len(manifest.Repositories) {
		return zero, errors.New("recovery reservation does not cover the entire checkpoint set")
	}
	wanted := map[string]bool{}
	for _, repository := range manifest.Repositories {
		wanted[repository.Identity.Key+"\x00"+repository.Identity.CommonGit] = true
	}
	for _, root := range roots {
		if !wanted[root.Key+"\x00"+root.CommonGit] {
			return zero, errors.New("recovery reservation contains a different workspace root")
		}
	}
	if err := m.writerContained(ctx, runID); err != nil {
		return zero, err
	}
	var receipt RecoveryReceipt
	err := m.Owner.HoldReservation(ctx, m.Engine.ProjectID, runID, m.Reservation.Endpoint, roots, m.Reservation.Claims, m.Reservation.Ticket, func() error {
		if err := m.writerContained(ctx, runID); err != nil {
			return err
		}
		var inner error
		receipt, inner = fn()
		return inner
	})
	return receipt, err
}

func (m *Manager) writerContained(ctx context.Context, runID string) error {
	var state, writer string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state,writer_state FROM runs WHERE id=?", runID).Scan(&state, &writer); err != nil {
		return err
	}
	if writer != "contained_stopped" || (state != "completed" && state != "failed" && state != "interrupted") {
		return errors.New("destructive recovery requires a terminal run with independently proven writer containment")
	}
	return nil
}

func (m *Manager) manifestContentDigest(repository RepositoryManifest) (string, error) {
	paths := append([]PathEntry(nil), repository.Paths...)
	sort.Slice(paths, func(i, j int) bool { return paths[i].Path < paths[j].Path })
	hash := sha256.New()
	for _, entry := range paths {
		if entry.Kind == "deleted" {
			continue
		}
		if entry.Kind != "regular" && entry.Kind != "symlink" {
			return "", errors.New("original run baseline contains an unsupported path")
		}
		content, err := m.readBlob(entry.Digest)
		if err != nil || int64(len(content)) != entry.Size {
			return "", errors.New("run baseline checkpoint blob is unavailable")
		}
		fmt.Fprintf(hash, "%s\x00%o\x00%d\x00", entry.Path, os.FileMode(entry.Mode).Perm(), len(content))
		hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (m *Manager) clearOwnership(ctx context.Context, baseline, captured SetManifest) (map[string]map[string]bool, error) {
	if len(baseline.Repositories) != len(captured.Repositories) {
		return nil, errors.New("clear checkpoint repository sets differ")
	}
	var capturedRun, baselineRun, repositoriesJSON, changedJSON, fingerprintsJSON string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT run_id FROM checkpoint_sets WHERE id=?", captured.ID).Scan(&capturedRun); err != nil {
		return nil, err
	}
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT run_id FROM checkpoint_sets WHERE id=?", baseline.ID).Scan(&baselineRun); err != nil {
		return nil, err
	}
	if capturedRun != baselineRun {
		return nil, errors.New("clear baseline is not bound to the captured run")
	}
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT repository_snapshot_json FROM run_snapshots WHERE run_id=?", capturedRun).Scan(&repositoriesJSON); err != nil {
		return nil, err
	}
	var repositories []core.RepositoryRecord
	if err := json.Unmarshal([]byte(repositoriesJSON), &repositories); err != nil {
		return nil, err
	}
	baseMap := repositoryMap(baseline)
	if len(baseMap) != len(repositories) {
		return nil, errors.New("clear baseline does not cover the original run snapshot")
	}
	for _, repository := range repositories {
		manifest, ok := baseMap[repository.ID]
		expectedRef := "refs/heads/" + repository.PlanBranch
		if !ok || manifest.HeadOID != repository.Baseline.HeadOID || manifest.HeadRef != expectedRef || manifest.IndexDigest != repository.Baseline.IndexDigest {
			return nil, fmt.Errorf("clear baseline is not the immutable pre-attempt state for repository %s", repository.ID)
		}
		contentDigest, err := m.manifestContentDigest(manifest)
		if err != nil || contentDigest != repository.Baseline.ContentDigest {
			return nil, errors.New("clear baseline bytes differ from the immutable pre-attempt state")
		}
	}
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT changed_paths_json,repository_fingerprints_json FROM execution_results WHERE run_id=? AND status='completed'", capturedRun).Scan(&changedJSON, &fingerprintsJSON); err != nil {
		return nil, errors.New("clear requires a validated completed execution result")
	}
	var changed []string
	var fingerprints map[string]workspace.Baseline
	if err := json.Unmarshal([]byte(changedJSON), &changed); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(fingerprintsJSON), &fingerprints); err != nil {
		return nil, err
	}
	ownedInput := map[string][]string{}
	for _, value := range changed {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("validated result contains an invalid changed path")
		}
		ownedInput[parts[0]] = append(ownedInput[parts[0]], parts[1])
	}
	owned, err := normalizeOwned(ownedInput)
	if err != nil {
		return nil, err
	}
	for _, repository := range captured.Repositories {
		current, err := workspace.Fingerprint(ctx, repository.Identity.Root, repository.Exclusions)
		if err != nil {
			return nil, fmt.Errorf("cannot bind clear ownership for repository %s: %w", repository.RepositoryID, err)
		}
		expected, ok := fingerprints[repository.RepositoryID]
		if !ok || !snapshotEqual(current, expected) {
			return nil, fmt.Errorf("repository %s changed after validated execution result", repository.RepositoryID)
		}
	}
	return owned, nil
}

func sameRepositorySet(left, right SetManifest) error {
	l, r := repositoryMap(left), repositoryMap(right)
	if len(l) != len(r) {
		return errors.New("checkpoint repository sets differ")
	}
	for id, a := range l {
		b, ok := r[id]
		if !ok || a.Identity.Key != b.Identity.Key || a.Identity.CommonGit != b.Identity.CommonGit || a.Identity.Root != b.Identity.Root {
			return errors.New("checkpoint repository identities differ")
		}
	}
	return nil
}

func (m *Manager) loadRecoverySet(ctx context.Context, id string) (SetManifest, string, error) {
	var state string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_sets WHERE id=?", id).Scan(&state); err != nil {
		return SetManifest{}, "", err
	}
	switch state {
	case "verified", "clearing", "saved", "restoring", "conflicted", "restored":
	default:
		return SetManifest{}, state, errors.New("checkpoint set is not verified recovery evidence")
	}
	manifest, err := m.verifiedSet(ctx, id, state)
	return manifest, state, err
}

func currentPath(root, relative, source string) (*PathEntry, error) {
	parent, base, closeParent, err := confinedParent(root, "", relative, false)
	if err != nil {
		return nil, err
	}
	defer closeParent()
	content, mode, exists, err := readAt(parent, base)
	if err != nil || !exists {
		return nil, err
	}
	kind := "regular"
	if mode&os.ModeSymlink != 0 {
		kind = "symlink"
	}
	return &PathEntry{Path: relative, Kind: kind, Mode: uint32(mode.Perm()), Digest: store.Digest(content), Size: int64(len(content)), Source: source}, nil
}

func currentHead(ctx context.Context, repository RepositoryManifest) (string, string, error) {
	oidRaw, err := safeGit(ctx, repository.Identity.Root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", "", err
	}
	ref := ""
	if refRaw, refErr := safeGit(ctx, repository.Identity.Root, "symbolic-ref", "--quiet", "HEAD"); refErr == nil {
		ref = strings.TrimSpace(string(refRaw))
	}
	return strings.TrimSpace(string(oidRaw)), ref, nil
}

func currentIndex(repository RepositoryManifest) (string, error) {
	parent, base, closeParent, err := confinedParent(repository.Identity.CommonGitPath, repository.Identity.CommonGit, "index", false)
	if err != nil {
		return "", err
	}
	defer closeParent()
	content, _, exists, err := readAt(parent, base)
	if err != nil || !exists {
		return "", errors.New("Git index is unavailable through its enrolled directory")
	}
	return store.Digest(content), nil
}

func (m *Manager) clearActions(ctx context.Context, baseline, captured SetManifest, owned map[string]map[string]bool, operationID string) ([]recoveryAction, error) {
	if err := sameRepositorySet(baseline, captured); err != nil {
		return nil, err
	}
	baseRepositories := repositoryMap(baseline)
	capturedRepositories := repositoryMap(captured)
	for id := range owned {
		if _, ok := capturedRepositories[id]; !ok {
			return nil, errors.New("owned path names a repository outside the checkpoint set")
		}
	}
	var actions []recoveryAction
	for id, post := range capturedRepositories {
		base := baseRepositories[id]
		if err := post.Identity.Validate(); err != nil {
			return nil, err
		}
		headOID, headRef, err := currentHead(ctx, post)
		if err != nil || headOID != post.HeadOID || headRef != post.HeadRef || base.HeadOID != post.HeadOID || base.HeadRef != post.HeadRef {
			return nil, errors.New("clear refuses branch or HEAD changes")
		}
		indexDigest, err := currentIndex(post)
		if err != nil {
			return nil, err
		}
		baseIndex, postIndex := indexMap(base), indexMap(post)
		indexPaths := map[string]bool{}
		for path := range baseIndex {
			indexPaths[path] = true
		}
		for path := range postIndex {
			indexPaths[path] = true
		}
		indexChanged := base.IndexDigest != post.IndexDigest
		if indexChanged {
			for path := range indexPaths {
				a, aok := baseIndex[path]
				b, bok := postIndex[path]
				if indexState(a, aok) != indexState(b, bok) && !owned[id][path] {
					return nil, fmt.Errorf("index change for %s is not provably agent-owned", path)
				}
			}
			progress := "prepared"
			if operationID != "" {
				if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_path_progress WHERE operation_id=? AND repository_id=? AND path='.git/index'", operationID, id).Scan(&progress); err != nil {
					return nil, err
				}
			}
			if progress == "applied" && indexDigest != base.IndexDigest || progress != "applied" && indexDigest != post.IndexDigest && indexDigest != base.IndexDigest {
				return nil, errors.New("clear index compare-and-swap rejected destination drift")
			}
			actions = append(actions, recoveryAction{repository: post, path: ".git/index", action: "index"})
		} else if indexDigest != post.IndexDigest {
			return nil, errors.New("clear index compare-and-swap rejected destination drift")
		}
		basePaths, postPaths := pathMap(base), pathMap(post)
		all := map[string]bool{}
		for path := range basePaths {
			all[path] = true
		}
		for path := range postPaths {
			all[path] = true
		}
		for path := range owned[id] {
			if !all[path] {
				return nil, fmt.Errorf("owned path %s is absent from both recovery states", path)
			}
		}
		for path := range all {
			before, after := entryPointer(basePaths, path), entryPointer(postPaths, path)
			if pathState(before) == pathState(after) || !owned[id][path] {
				continue
			}
			expectedSource := "untracked"
			if after != nil {
				expectedSource = after.Source
			} else if before != nil {
				expectedSource = before.Source
			}
			progress := "prepared"
			if operationID != "" {
				if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_path_progress WHERE operation_id=? AND repository_id=? AND path=?", operationID, id, path).Scan(&progress); err != nil {
					return nil, err
				}
			}
			current, err := currentPath(post.Identity.Root, path, expectedSource)
			currentState := pathState(current)
			valid := progress == "applied" && currentState == pathState(before) || progress != "applied" && (currentState == pathState(after) || currentState == pathState(before))
			if err != nil || !valid {
				return nil, fmt.Errorf("clear compare-and-swap rejected concurrent or mixed edit at %s", path)
			}
			action := "delete"
			if before != nil {
				action = before.Kind
				if action == "regular" {
					action = "write"
				}
				if action == "deleted" {
					action = "delete"
				}
			}
			actions = append(actions, recoveryAction{repository: post, path: path, action: action, expected: after, desired: before})
		}
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].repository.RepositoryID != actions[j].repository.RepositoryID {
			return actions[i].repository.RepositoryID < actions[j].repository.RepositoryID
		}
		if actions[i].action == "index" {
			return false
		}
		if actions[j].action == "index" {
			return true
		}
		return actions[i].path < actions[j].path
	})
	return actions, nil
}

func (m *Manager) Clear(ctx context.Context, request ClearRequest) (RecoveryReceipt, error) {
	if len(request.OwnedPaths) != 0 {
		return RecoveryReceipt{}, errors.New("caller-declared ownership is not accepted")
	}
	manifest, _, err := m.loadRecoverySet(ctx, request.CheckpointID)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	return m.withRecoveryAuthority(ctx, manifest, func() (RecoveryReceipt, error) {
		return m.clearAuthorized(ctx, request)
	})
}

func (m *Manager) clearAuthorized(ctx context.Context, request ClearRequest) (RecoveryReceipt, error) {
	var receipt RecoveryReceipt
	if !store.SafeID(request.CommandID) || request.ExpectedRevision < 1 || !store.SafeID(request.CheckpointID) || !store.SafeID(request.BaselineCheckpointID) || request.CheckpointID == request.BaselineCheckpointID {
		return receipt, errors.New("valid clear command, revision, captured set and distinct baseline set required")
	}
	captured, _, err := m.loadRecoverySet(ctx, request.CheckpointID)
	if err != nil {
		return receipt, err
	}
	baseline, _, err := m.loadRecoverySet(ctx, request.BaselineCheckpointID)
	if err != nil {
		return receipt, err
	}
	owned, err := m.clearOwnership(ctx, baseline, captured)
	if err != nil {
		return receipt, err
	}
	args, _ := json.Marshal(request)
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "checkpoint.clear", Args: args}
	commandReceipt, repeated, err := m.Engine.DB.Receipt(ctx, command)
	if err != nil {
		return receipt, err
	}
	if repeated {
		if err := json.Unmarshal(commandReceipt, &receipt); err != nil {
			return receipt, err
		}
	}
	actions, err := m.clearActions(ctx, baseline, captured, owned, receipt.OperationID)
	if err != nil {
		return receipt, err
	}
	if !repeated {
		commandReceipt, err = m.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
			var revision int
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", m.Engine.ProjectID).Scan(&revision); err != nil {
				return nil, err
			}
			if revision != request.ExpectedRevision {
				return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
			}
			operationID := store.ID()
			authority := store.Digest(args)
			var runID, planID, taskID string
			if err := tx.QueryRowContext(ctx, `SELECT s.run_id,r.plan_id,r.task_id FROM checkpoint_sets s JOIN runs r ON r.id=s.run_id WHERE s.id=?`, request.CheckpointID).Scan(&runID, &planID, &taskID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,task_id,run_id,evidence_json,created_at) SELECT ?,'checkpoint_clear',?,?,policy_epoch,'prepared',?,?,?, ?,? FROM project WHERE id=?`, operationID, captured.Digest, authority, planID, taskID, runID, string(args), store.Now(), m.Engine.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_operations(id,kind,checkpoint_id,baseline_checkpoint_id,authority_digest,state,created_at) VALUES(?,'clear',?,?,?,'prepared',?)`, operationID, request.CheckpointID, request.BaselineCheckpointID, authority, store.Now()); err != nil {
				return nil, err
			}
			for _, action := range actions {
				if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoint_path_progress(operation_id,repository_id,path,action,state,expected_digest,desired_digest,observation_json,updated_at) VALUES(?,?,?,?, 'prepared',?,?, '{}',?)`, operationID, action.repository.RepositoryID, action.path, action.action, actionExpectedDigest(action, captured), actionDesiredDigest(action, baseline), store.Now()); err != nil {
					return nil, err
				}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='clearing' WHERE id=?", request.CheckpointID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='recovering',revision=revision+1 WHERE id=?", m.Engine.ProjectID); err != nil {
				return nil, err
			}
			return RecoveryReceipt{OperationID: operationID, CheckpointID: request.CheckpointID, BaselineCheckpointID: request.BaselineCheckpointID, State: "prepared"}, nil
		})
		if err != nil {
			return receipt, err
		}
		if err := json.Unmarshal(commandReceipt, &receipt); err != nil {
			return receipt, err
		}
	} else {
		receipt.Repeated = true
	}
	var operationState string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM recovery_operations WHERE id=?", receipt.OperationID).Scan(&operationState); err != nil {
		return receipt, err
	}
	if operationState == "observed" {
		receipt.State = "saved"
		receipt.Applied = len(actions)
		return receipt, nil
	}
	if err := m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='executing' WHERE id=? AND state IN('prepared','conflicted')", receipt.OperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='executing' WHERE id=? AND state='prepared'", receipt.OperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='clearing' WHERE id=?", request.CheckpointID)
		return err
	}); err != nil {
		return receipt, err
	}
	for _, action := range actions {
		var state string
		if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_path_progress WHERE operation_id=? AND repository_id=? AND path=?", receipt.OperationID, action.repository.RepositoryID, action.path).Scan(&state); err != nil {
			return receipt, err
		}
		if state == "applied" {
			receipt.Applied++
			continue
		}
		if err := m.checkpoint("before_apply:" + action.repository.RepositoryID + ":" + action.path); err != nil {
			return receipt, err
		}
		if err := m.applyAction(action, baseline); err != nil {
			m.markRecoveryConflict(receipt.OperationID, request.CheckpointID, action, err)
			receipt.State, receipt.Conflicts = "conflicted", 1
			return receipt, err
		}
		if err := m.checkpoint("after_apply:" + action.repository.RepositoryID + ":" + action.path); err != nil {
			return receipt, err
		}
		observation, _ := json.Marshal(map[string]string{"state": "desired", "digest": actionDesiredDigest(action, baseline)})
		if err := m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE checkpoint_path_progress SET state='applied',observation_json=?,updated_at=? WHERE operation_id=? AND repository_id=? AND path=?`, string(observation), store.Now(), receipt.OperationID, action.repository.RepositoryID, action.path)
			return err
		}); err != nil {
			return receipt, err
		}
		receipt.Applied++
	}
	err = m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='saved' WHERE id=?", request.CheckpointID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='observed',observed_at=? WHERE id=?", store.Now(), receipt.OperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=?", receipt.OperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE project SET state='paused' WHERE id=?", m.Engine.ProjectID)
		return err
	})
	if err != nil {
		return receipt, err
	}
	receipt.State = "saved"
	return receipt, nil
}

func actionExpectedDigest(action recoveryAction, captured SetManifest) string {
	if action.action == "index" {
		return repositoryMap(captured)[action.repository.RepositoryID].IndexDigest
	}
	return pathState(action.expected)
}

func actionDesiredDigest(action recoveryAction, baseline SetManifest) string {
	if action.action == "index" {
		return repositoryMap(baseline)[action.repository.RepositoryID].IndexDigest
	}
	return pathState(action.desired)
}

func (m *Manager) actionAtDesired(action recoveryAction, baseline SetManifest) error {
	if action.action == "index" {
		digest, err := currentIndex(action.repository)
		if err != nil || digest != actionDesiredDigest(action, baseline) {
			return errors.New("previous index apply cannot be reconciled")
		}
		return nil
	}
	source := "untracked"
	if action.desired != nil {
		source = action.desired.Source
	} else if action.expected != nil {
		source = action.expected.Source
	}
	current, err := currentPath(action.repository.Identity.Root, action.path, source)
	if err != nil || pathState(current) != pathState(action.desired) {
		return errors.New("previous path apply cannot be reconciled")
	}
	return nil
}

func (m *Manager) readBlob(digest string) ([]byte, error) {
	if len(digest) != 64 || !store.SafeID(digest) {
		return nil, errors.New("invalid checkpoint blob")
	}
	path := filepath.Join(m.Store.Dir, "blobs", digest)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := verifyBlob(path, digest, info.Size()); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func confinedParent(root, identityKey, relative string, create bool) (int, string, func(), error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return -1, "", func() {}, errors.New("invalid recovery path")
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", func() {}, err
	}
	closeAll := func() { _ = unix.Close(rootFD) }
	if identityKey != "" {
		var stat unix.Stat_t
		if err := unix.Fstat(rootFD, &stat); err != nil || fmt.Sprintf("%d:%d", stat.Dev, stat.Ino) != identityKey {
			closeAll()
			return -1, "", func() {}, errors.New("recovery root descriptor differs from its enrolled filesystem identity")
		}
	}
	parentFD := rootFD
	for _, component := range strings.Split(filepath.Dir(clean), string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		next, openErr := unix.Openat(parentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(parentFD, component, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				if parentFD != rootFD {
					_ = unix.Close(parentFD)
				}
				closeAll()
				return -1, "", func() {}, mkdirErr
			}
			next, openErr = unix.Openat(parentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if openErr != nil {
			if parentFD != rootFD {
				_ = unix.Close(parentFD)
			}
			closeAll()
			return -1, "", func() {}, errors.New("recovery path parent is missing, replaced or not an ordinary directory")
		}
		if parentFD != rootFD {
			_ = unix.Close(parentFD)
		}
		parentFD = next
	}
	closeParent := func() {
		if parentFD != rootFD {
			_ = unix.Close(parentFD)
		}
		closeAll()
	}
	return parentFD, filepath.Base(clean), closeParent, nil
}

func readAt(parentFD int, base string) ([]byte, os.FileMode, bool, error) {
	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &before, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil, 0, false, nil
	} else if err != nil {
		return nil, 0, false, err
	}
	mode := os.FileMode(before.Mode & 0777)
	switch before.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		buffer := make([]byte, maxFileBytes+1)
		n, err := unix.Readlinkat(parentFD, base, buffer)
		if err != nil || n > maxFileBytes {
			return nil, mode | os.ModeSymlink, true, errors.New("recovery symlink target is unavailable or exceeds limit")
		}
		return buffer[:n], mode | os.ModeSymlink, true, nil
	case unix.S_IFREG:
	default:
		return nil, mode, true, errors.New("unsupported recovery destination kind")
	}
	if before.Size > maxFileBytes {
		return nil, mode, true, errors.New("recovery destination exceeds 64 MiB")
	}
	fd, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, mode, true, err
	}
	file := os.NewFile(uintptr(fd), base)
	if file == nil {
		_ = unix.Close(fd)
		return nil, mode, true, errors.New("failed to open recovery destination")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	var after unix.Stat_t
	statErr := unix.Fstat(fd, &after)
	closeErr := file.Close()
	if err != nil || statErr != nil || closeErr != nil || len(content) > maxFileBytes || before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size {
		return nil, mode, true, errors.New("recovery destination changed while observed")
	}
	return content, mode, true, nil
}

func atomicWriteAt(parentFD int, base string, content []byte, mode os.FileMode, symlink bool) error {
	temporary := ".vigil-recovery-" + store.ID()
	if symlink {
		if err := unix.Symlinkat(string(content), parentFD, temporary); err != nil {
			return err
		}
	} else {
		fd, err := unix.Openat(parentFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), temporary)
		if file == nil {
			_ = unix.Close(fd)
			return errors.New("failed to create recovery temporary")
		}
		if _, err = file.Write(content); err == nil {
			err = unix.Fchmod(fd, uint32(mode.Perm()))
		}
		if err == nil {
			err = file.Sync()
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = unix.Unlinkat(parentFD, temporary, 0)
			return err
		}
	}
	if err := unix.Renameat(parentFD, temporary, parentFD, base); err != nil {
		_ = unix.Unlinkat(parentFD, temporary, 0)
		return err
	}
	return unix.Fsync(parentFD)
}

func atomicIndexWriteAt(parentFD int, base string, content []byte, expectedDigest string) error {
	lock := base + ".lock"
	fd, err := unix.Openat(parentFD, lock, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("Git index lock is unavailable: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(parentFD, lock, 0)
		}
	}()
	observed, _, exists, err := readAt(parentFD, base)
	if err != nil || !exists || store.Digest(observed) != expectedDigest {
		_ = unix.Close(fd)
		return errors.New("index changed before locked descriptor-relative apply")
	}
	file := os.NewFile(uintptr(fd), lock)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("failed to open Git index lock")
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := unix.Renameat(parentFD, lock, parentFD, base); err != nil {
		return err
	}
	cleanup = false
	return unix.Fsync(parentFD)
}

func gitBlobObject(content []byte, oid string) ([]byte, error) {
	header := []byte(fmt.Sprintf("blob %d\x00", len(content)))
	plain := append(header, content...)
	var actual string
	switch len(oid) {
	case 40:
		digest := sha1.Sum(plain)
		actual = hex.EncodeToString(digest[:])
	case 64:
		digest := sha256.Sum256(plain)
		actual = hex.EncodeToString(digest[:])
	default:
		return nil, errors.New("unsupported Git object identity")
	}
	if actual != oid {
		return nil, errors.New("staged recovery bytes do not match their Git object identity")
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(plain); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return compressed.Bytes(), nil
}

func verifyLooseObject(compressed []byte, oid string) error {
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return err
	}
	plain, err := io.ReadAll(io.LimitReader(reader, maxFileBytes+128))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(plain) > maxFileBytes+127 {
		return errors.New("restored Git object is unreadable or exceeds limit")
	}
	separator := bytes.IndexByte(plain, 0)
	if separator < 6 || !bytes.HasPrefix(plain, []byte("blob ")) {
		return errors.New("restored Git object is not a blob")
	}
	_, err = gitBlobObject(plain[separator+1:], oid)
	return err
}

func (m *Manager) restoreIndexObjects(_ context.Context, repository RepositoryManifest) error {
	seen := map[string]bool{}
	for _, entry := range repository.IndexEntries {
		if seen[entry.OID] {
			continue
		}
		seen[entry.OID] = true
		content, err := m.readBlob(entry.BlobDigest)
		if err != nil {
			return errors.New("staged recovery blob is unavailable")
		}
		compressed, err := gitBlobObject(content, entry.OID)
		if err != nil {
			return fmt.Errorf("restored staged object identity differs for %s: %w", entry.Path, err)
		}
		relative := filepath.ToSlash(filepath.Join("objects", entry.OID[:2], entry.OID[2:]))
		parent, name, closeParent, err := confinedParent(repository.Identity.CommonGitPath, repository.Identity.CommonGit, relative, true)
		if err != nil {
			return err
		}
		existing, _, exists, readErr := readAt(parent, name)
		if readErr != nil {
			closeParent()
			return readErr
		}
		if exists {
			err = verifyLooseObject(existing, entry.OID)
		} else {
			err = atomicWriteAt(parent, name, compressed, 0444, false)
			if err == nil {
				existing, _, exists, err = readAt(parent, name)
				if err == nil && exists {
					err = verifyLooseObject(existing, entry.OID)
				} else if err == nil {
					err = errors.New("restored Git object disappeared after write")
				}
			}
		}
		closeParent()
		if err != nil {
			return fmt.Errorf("restored staged object %s is unavailable: %w", entry.OID, err)
		}
	}
	return nil
}

func (m *Manager) applyAction(action recoveryAction, baseline SetManifest) error {
	if err := action.repository.Identity.Validate(); err != nil {
		return err
	}
	if action.action == "index" {
		current, err := currentIndex(action.repository)
		desiredDigest := actionDesiredDigest(action, baseline)
		if err == nil && current == desiredDigest {
			return nil
		}
		// action.repository is the captured repository, so its index is the CAS expectation.
		if err != nil || current != action.repository.IndexDigest {
			return errors.New("index changed after clear authorization")
		}
		base := repositoryMap(baseline)[action.repository.RepositoryID]
		content, err := m.readBlob(base.IndexBlob)
		if err != nil || store.Digest(content) != base.IndexDigest {
			return errors.New("baseline index recovery blob is unavailable")
		}
		if err := m.restoreIndexObjects(context.Background(), base); err != nil {
			return err
		}
		parent, name, closeParent, err := confinedParent(action.repository.Identity.CommonGitPath, action.repository.Identity.CommonGit, "index", false)
		if err != nil {
			return err
		}
		defer closeParent()
		return atomicIndexWriteAt(parent, name, content, action.repository.IndexDigest)
	}
	source := "untracked"
	if action.expected != nil {
		source = action.expected.Source
	} else if action.desired != nil {
		source = action.desired.Source
	}
	current, err := currentPath(action.repository.Identity.Root, action.path, source)
	if err != nil {
		return err
	}
	if pathState(current) == pathState(action.desired) {
		return nil
	}
	if pathState(current) != pathState(action.expected) {
		return errors.New("destination changed after recovery authorization")
	}
	parent, name, closeParent, err := confinedParent(action.repository.Identity.Root, action.repository.Identity.Key, action.path, action.desired != nil)
	if err != nil {
		return err
	}
	defer closeParent()
	observed, mode, exists, err := readAt(parent, name)
	var descriptorCurrent *PathEntry
	if err == nil && exists {
		kind := "regular"
		if mode&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		descriptorCurrent = &PathEntry{Path: action.path, Kind: kind, Mode: uint32(mode.Perm()), Digest: store.Digest(observed), Size: int64(len(observed)), Source: source}
	}
	if err != nil || pathState(descriptorCurrent) != pathState(action.expected) {
		return errors.New("destination changed before descriptor-relative apply")
	}
	if action.desired == nil || action.desired.Kind == "deleted" {
		if err := unix.Unlinkat(parent, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		return unix.Fsync(parent)
	}
	content, err := m.readBlob(action.desired.Digest)
	if err != nil || int64(len(content)) != action.desired.Size {
		return errors.New("desired recovery blob is unavailable")
	}
	return atomicWriteAt(parent, name, content, os.FileMode(action.desired.Mode), action.desired.Kind == "symlink")
}

func (m *Manager) markRecoveryConflict(operationID, checkpointID string, action recoveryAction, cause error) {
	raw, _ := json.Marshal(map[string]string{"error": cause.Error()})
	_ = m.Engine.DB.Write(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `UPDATE checkpoint_path_progress SET state='conflicted',observation_json=?,updated_at=? WHERE operation_id=? AND repository_id=? AND path=?`, string(raw), store.Now(), operationID, action.repository.RepositoryID, action.path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), "UPDATE recovery_operations SET state='conflicted',observed_at=? WHERE id=?", store.Now(), operationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), "UPDATE operations SET state='uncertain' WHERE id=?", operationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), "UPDATE checkpoint_sets SET state='conflicted' WHERE id=?", checkpointID); err != nil {
			return err
		}
		_, err := tx.ExecContext(context.Background(), "UPDATE project SET state='recovering' WHERE id=?", m.Engine.ProjectID)
		return err
	})
}

func workingStateDigest(repository RepositoryManifest) string {
	repository.BundleBlob = ""
	repository.BundleDigest = ""
	repository.Digest = ""
	raw, _ := json.Marshal(repository)
	return store.Digest(raw)
}

func (m *Manager) verifyCurrentDestination(ctx context.Context, destination SetManifest) error {
	for _, expected := range destination.Repositories {
		observed, _, err := m.Store.captureRepository(ctx, RepositorySpec{ID: expected.RepositoryID, Root: expected.Identity.Root, Identity: expected.Identity, Exclusions: expected.Exclusions, UntrackedScope: expected.UntrackedScope})
		if err != nil {
			return err
		}
		if workingStateDigest(observed) != workingStateDigest(expected) {
			return fmt.Errorf("destination repository %s changed after its recovery snapshot", expected.RepositoryID)
		}
	}
	return nil
}

func threeWayAction(base, target, destination *PathEntry) string {
	b, t, d := pathState(base), pathState(target), pathState(destination)
	switch {
	case t == d:
		return "preserve"
	case d == b:
		if target == nil || target.Kind == "deleted" {
			return "delete"
		}
		if target.Kind == "symlink" {
			return "symlink"
		}
		return "write"
	case t == b:
		return "preserve"
	default:
		return "conflict"
	}
}

func (m *Manager) restoreActions(ctx context.Context, baseline, target, destination SetManifest, operationID string) ([]recoveryAction, int, error) {
	if err := sameRepositorySet(baseline, target); err != nil {
		return nil, 0, err
	}
	if err := sameRepositorySet(baseline, destination); err != nil {
		return nil, 0, err
	}
	baseRepositories, targetRepositories, destinationRepositories := repositoryMap(baseline), repositoryMap(target), repositoryMap(destination)
	var actions []recoveryAction
	conflicts := 0
	for id, destinationRepository := range destinationRepositories {
		baseRepository, targetRepository := baseRepositories[id], targetRepositories[id]
		if err := destinationRepository.Identity.Validate(); err != nil {
			return nil, 0, err
		}
		currentOID, currentRef, err := currentHead(ctx, destinationRepository)
		if err != nil {
			return nil, 0, err
		}
		if currentOID != destinationRepository.HeadOID || currentRef != destinationRepository.HeadRef {
			return nil, 0, errors.New("restore HEAD authority is stale")
		}
		if baseRepository.HeadRef != targetRepository.HeadRef || baseRepository.HeadRef != destinationRepository.HeadRef {
			conflicts++
			actions = append(actions, recoveryAction{repository: destinationRepository, path: ".git/HEAD", action: "conflict"})
		} else if targetRepository.HeadOID != destinationRepository.HeadOID {
			// Stage 5.3 does not guess how to combine divergent commit histories.
			// Both bundles remain available and unrelated refs remain untouched.
			conflicts++
			actions = append(actions, recoveryAction{repository: destinationRepository, path: ".git/HEAD", action: "conflict"})
		}
		currentIndexDigest, err := currentIndex(destinationRepository)
		if err != nil {
			return nil, 0, err
		}
		indexAction := "preserve"
		switch {
		case targetRepository.IndexDigest == destinationRepository.IndexDigest:
		case destinationRepository.IndexDigest == baseRepository.IndexDigest:
			indexAction = "index"
		case targetRepository.IndexDigest == baseRepository.IndexDigest:
		default:
			indexAction = "conflict"
			conflicts++
		}
		if indexAction != "preserve" {
			progress := "prepared"
			if operationID != "" {
				if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_path_progress WHERE operation_id=? AND repository_id=? AND path='.git/index'", operationID, id).Scan(&progress); err != nil {
					return nil, 0, err
				}
			}
			if indexAction == "index" {
				valid := progress == "applied" && currentIndexDigest == targetRepository.IndexDigest || progress != "applied" && (currentIndexDigest == destinationRepository.IndexDigest || currentIndexDigest == targetRepository.IndexDigest)
				if !valid {
					return nil, 0, errors.New("restore index compare-and-swap rejected intervening changes")
				}
			} else if currentIndexDigest != destinationRepository.IndexDigest {
				return nil, 0, errors.New("restore conflict destination changed after snapshot")
			}
			actions = append(actions, recoveryAction{repository: destinationRepository, path: ".git/index", action: indexAction})
		} else if currentIndexDigest != destinationRepository.IndexDigest {
			return nil, 0, errors.New("restore destination index changed after snapshot")
		}
		basePaths, targetPaths, destinationPaths := pathMap(baseRepository), pathMap(targetRepository), pathMap(destinationRepository)
		all := map[string]bool{}
		for path := range basePaths {
			all[path] = true
		}
		for path := range targetPaths {
			all[path] = true
		}
		for path := range destinationPaths {
			all[path] = true
		}
		for path := range all {
			baseEntry, targetEntry, destinationEntry := entryPointer(basePaths, path), entryPointer(targetPaths, path), entryPointer(destinationPaths, path)
			action := threeWayAction(baseEntry, targetEntry, destinationEntry)
			progress := "prepared"
			if operationID != "" {
				if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_path_progress WHERE operation_id=? AND repository_id=? AND path=?", operationID, id, path).Scan(&progress); err != nil {
					return nil, 0, err
				}
			}
			source := "untracked"
			if destinationEntry != nil {
				source = destinationEntry.Source
			} else if targetEntry != nil {
				source = targetEntry.Source
			} else if baseEntry != nil {
				source = baseEntry.Source
			}
			current, err := currentPath(destinationRepository.Identity.Root, path, source)
			if err != nil {
				return nil, 0, err
			}
			currentState := pathState(current)
			if action == "write" || action == "symlink" || action == "delete" {
				valid := progress == "applied" && currentState == pathState(targetEntry) || progress != "applied" && (currentState == pathState(destinationEntry) || currentState == pathState(targetEntry))
				if !valid {
					return nil, 0, fmt.Errorf("restore compare-and-swap rejected intervening change at %s", path)
				}
			} else if currentState != pathState(destinationEntry) {
				return nil, 0, fmt.Errorf("restore destination changed at preserved path %s", path)
			}
			if action == "conflict" {
				conflicts++
			}
			actions = append(actions, recoveryAction{repository: destinationRepository, path: path, action: action, expected: destinationEntry, desired: targetEntry})
		}
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].repository.RepositoryID != actions[j].repository.RepositoryID {
			return actions[i].repository.RepositoryID < actions[j].repository.RepositoryID
		}
		if strings.HasPrefix(actions[i].path, ".git/") != strings.HasPrefix(actions[j].path, ".git/") {
			return !strings.HasPrefix(actions[i].path, ".git/")
		}
		return actions[i].path < actions[j].path
	})
	return actions, conflicts, nil
}

func (m *Manager) Restore(ctx context.Context, request RestoreRequest) (RecoveryReceipt, error) {
	manifest, _, err := m.loadRecoverySet(ctx, request.CheckpointID)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	return m.withRecoveryAuthority(ctx, manifest, func() (RecoveryReceipt, error) {
		return m.restoreAuthorized(ctx, request)
	})
}

func (m *Manager) restoreAuthorized(ctx context.Context, request RestoreRequest) (RecoveryReceipt, error) {
	var receipt RecoveryReceipt
	if !store.SafeID(request.CommandID) || request.ExpectedRevision < 1 || !store.SafeID(request.CheckpointID) || !store.SafeID(request.BaselineCheckpointID) || !store.SafeID(request.DestinationCheckpointID) || request.CheckpointID == request.DestinationCheckpointID {
		return receipt, errors.New("valid approved restore command and three distinct recovery roles required")
	}
	target, _, err := m.loadRecoverySet(ctx, request.CheckpointID)
	if err != nil {
		return receipt, err
	}
	baseline, _, err := m.loadRecoverySet(ctx, request.BaselineCheckpointID)
	if err != nil {
		return receipt, err
	}
	destination, _, err := m.loadRecoverySet(ctx, request.DestinationCheckpointID)
	if err != nil {
		return receipt, err
	}
	args, _ := json.Marshal(request)
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "checkpoint.restore", Args: args}
	commandReceipt, repeated, err := m.Engine.DB.Receipt(ctx, command)
	if err != nil {
		return receipt, err
	}
	if repeated {
		if err := json.Unmarshal(commandReceipt, &receipt); err != nil {
			return receipt, err
		}
	} else if err := m.verifyCurrentDestination(ctx, destination); err != nil {
		return receipt, err
	}
	actions, conflicts, err := m.restoreActions(ctx, baseline, target, destination, receipt.OperationID)
	if err != nil {
		return receipt, err
	}
	if !repeated {
		commandReceipt, err = m.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
			var revision int
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", m.Engine.ProjectID).Scan(&revision); err != nil {
				return nil, err
			}
			if revision != request.ExpectedRevision {
				return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
			}
			operationID, authority := store.ID(), store.Digest([]byte(target.Digest+"\x00"+baseline.Digest+"\x00"+destination.Digest))
			var runID, planID, taskID string
			if err := tx.QueryRowContext(ctx, `SELECT s.run_id,r.plan_id,r.task_id FROM checkpoint_sets s JOIN runs r ON r.id=s.run_id WHERE s.id=?`, request.CheckpointID).Scan(&runID, &planID, &taskID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,task_id,run_id,evidence_json,created_at) SELECT ?,'checkpoint_restore',?,?,policy_epoch,'prepared',?,?,?, ?,? FROM project WHERE id=?`, operationID, target.Digest, authority, planID, taskID, runID, string(args), store.Now(), m.Engine.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_operations(id,kind,checkpoint_id,baseline_checkpoint_id,destination_checkpoint_id,authority_digest,state,created_at) VALUES(?,'restore',?,?,?,?, 'prepared',?)`, operationID, request.CheckpointID, request.BaselineCheckpointID, request.DestinationCheckpointID, authority, store.Now()); err != nil {
				return nil, err
			}
			for _, action := range actions {
				state := "prepared"
				if action.action == "preserve" {
					state = "preserved"
				}
				if action.action == "conflict" {
					state = "conflicted"
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoint_path_progress(operation_id,repository_id,path,action,state,expected_digest,desired_digest,observation_json,updated_at) VALUES(?,?,?,?,?,?,?,'{}',?)`, operationID, action.repository.RepositoryID, action.path, action.action, state, restoreExpectedDigest(action, destination), restoreDesiredDigest(action, target), store.Now()); err != nil {
					return nil, err
				}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='restoring' WHERE id=?", request.CheckpointID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='recovering',revision=revision+1 WHERE id=?", m.Engine.ProjectID); err != nil {
				return nil, err
			}
			return RecoveryReceipt{OperationID: operationID, CheckpointID: request.CheckpointID, BaselineCheckpointID: request.BaselineCheckpointID, DestinationCheckpointID: request.DestinationCheckpointID, State: "prepared", Conflicts: conflicts}, nil
		})
		if err != nil {
			return receipt, err
		}
		if err := json.Unmarshal(commandReceipt, &receipt); err != nil {
			return receipt, err
		}
	} else {
		receipt.Repeated = true
	}
	var operationState string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM recovery_operations WHERE id=?", receipt.OperationID).Scan(&operationState); err != nil {
		return receipt, err
	}
	if operationState == "observed" {
		receipt.State = "restored"
		return receipt, nil
	}
	if err := m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='executing' WHERE id=? AND state IN('prepared','conflicted')", receipt.OperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='executing' WHERE id=? AND state='prepared'", receipt.OperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='restoring' WHERE id=?", request.CheckpointID)
		return err
	}); err != nil {
		return receipt, err
	}
	for _, action := range actions {
		if action.action == "preserve" || action.action == "conflict" {
			continue
		}
		var state string
		if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_path_progress WHERE operation_id=? AND repository_id=? AND path=?", receipt.OperationID, action.repository.RepositoryID, action.path).Scan(&state); err != nil {
			return receipt, err
		}
		if state == "applied" {
			receipt.Applied++
			continue
		}
		if err := m.applyAction(action, target); err != nil {
			m.markRecoveryConflict(receipt.OperationID, request.CheckpointID, action, err)
			return receipt, err
		}
		if err := m.checkpoint("after_restore_apply:" + action.repository.RepositoryID + ":" + action.path); err != nil {
			return receipt, err
		}
		if err := m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE checkpoint_path_progress SET state='applied',observation_json='{"state":"desired"}',updated_at=? WHERE operation_id=? AND repository_id=? AND path=?`, store.Now(), receipt.OperationID, action.repository.RepositoryID, action.path)
			return err
		}); err != nil {
			return receipt, err
		}
		receipt.Applied++
	}
	if conflicts != 0 {
		_ = m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='conflicted',observed_at=? WHERE id=?", store.Now(), receipt.OperationID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=?", receipt.OperationID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='conflicted' WHERE id=?", request.CheckpointID)
			return err
		})
		receipt.State, receipt.Conflicts = "conflicted", conflicts
		return receipt, ErrRestoreConflict
	}
	err = m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='observed',observed_at=? WHERE id=?", store.Now(), receipt.OperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=?", receipt.OperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='restored' WHERE id=?", request.CheckpointID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE project SET state='paused' WHERE id=?", m.Engine.ProjectID)
		return err
	})
	if err != nil {
		return receipt, err
	}
	receipt.State = "restored"
	return receipt, nil
}

func restoreExpectedDigest(action recoveryAction, destination SetManifest) string {
	if action.path == ".git/index" {
		return repositoryMap(destination)[action.repository.RepositoryID].IndexDigest
	}
	if action.path == ".git/HEAD" {
		repository := repositoryMap(destination)[action.repository.RepositoryID]
		return store.Digest([]byte(repository.HeadRef + "\x00" + repository.HeadOID))
	}
	return pathState(action.expected)
}

func restoreDesiredDigest(action recoveryAction, target SetManifest) string {
	if action.path == ".git/index" {
		return repositoryMap(target)[action.repository.RepositoryID].IndexDigest
	}
	if action.path == ".git/HEAD" {
		repository := repositoryMap(target)[action.repository.RepositoryID]
		return store.Digest([]byte(repository.HeadRef + "\x00" + repository.HeadOID))
	}
	return pathState(action.desired)
}
