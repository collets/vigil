package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"vigil/internal/store"
	"vigil/internal/workspace"
)

type RepositoryEnrollment struct {
	ID               string   `json:"id"`
	PlanID           string   `json:"plan_id"`
	Root             string   `json:"root"`
	BaseRef          string   `json:"base_ref"`
	PlanBranch       string   `json:"plan_branch"`
	Remote           string   `json:"remote,omitempty"`
	DirtyChoice      string   `json:"dirty_choice"`
	IncludedPaths    []string `json:"included_paths,omitempty"`
	NestedBoundaries []string `json:"nested_boundaries,omitempty"`
}

type RepositoryRecord struct {
	ID               string             `json:"id"`
	Revision         int                `json:"revision"`
	PlanID           string             `json:"plan_id"`
	Root             string             `json:"root"`
	Identity         workspace.Identity `json:"identity"`
	BaseRef          string             `json:"base_ref"`
	BaseOID          string             `json:"base_oid"`
	PlanBranch       string             `json:"plan_branch"`
	RemoteName       string             `json:"remote_name,omitempty"`
	RemoteIdentity   string             `json:"remote_identity,omitempty"`
	DirtyChoice      string             `json:"dirty_choice"`
	IncludedPaths    []string           `json:"included_paths"`
	NestedBoundaries []string           `json:"nested_boundaries"`
	FingerprintID    string             `json:"fingerprint_id"`
	Baseline         workspace.Baseline `json:"baseline"`
}

func cleanRelativePaths(paths []string) ([]string, error) {
	set := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "." || path == ".." || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || strings.ContainsRune(path, 0) {
			return nil, errors.New("paths must be bounded repository-relative names")
		}
		if set[path] {
			return nil, errors.New("duplicate repository path")
		}
		set[path] = true
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func includesPath(scopes []string, path string) bool {
	for _, scope := range scopes {
		if path == scope || strings.HasPrefix(path, scope+"/") {
			return true
		}
	}
	return false
}

type preparedEnrollment struct {
	input       RepositoryEnrollment
	root        string
	included    []string
	nested      []string
	observation workspace.EnrollmentObservation
}

// prepareRepositoryEnrollment performs every Git/filesystem observation before
// the command transaction. The transaction later rechecks the project revision
// and persists only the exact immutable observation assembled here.
func (e *Engine) prepareRepositoryEnrollment(ctx context.Context, input RepositoryEnrollment) (preparedEnrollment, error) {
	var prepared preparedEnrollment
	if !store.SafeID(input.ID) || !store.SafeID(input.PlanID) {
		return prepared, errors.New("stable repository and plan IDs required")
	}
	if input.DirtyChoice == "save" {
		return prepared, errors.New("dirty-work save is unavailable until Stage 5.3; choose include or postpone without changing the checkout")
	}
	if input.DirtyChoice != "clean" && input.DirtyChoice != "include" && input.DirtyChoice != "postpone" {
		return prepared, errors.New("dirty_choice must be clean, include, or postpone")
	}
	included, err := cleanRelativePaths(input.IncludedPaths)
	if err != nil {
		return prepared, err
	}
	nested, err := cleanRelativePaths(input.NestedBoundaries)
	if err != nil {
		return prepared, err
	}
	if input.DirtyChoice != "include" && len(included) != 0 {
		return prepared, errors.New("included_paths require dirty_choice include")
	}
	var projectRoot string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT root FROM project WHERE id=?", e.ProjectID).Scan(&projectRoot); err != nil {
		return prepared, err
	}
	if count := func() int {
		var n int
		_ = e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM plans WHERE id=?", input.PlanID).Scan(&n)
		return n
	}(); count != 1 {
		return prepared, errors.New("repository enrollment requires an existing plan")
	}
	root, err := filepath.Abs(input.Root)
	if err != nil {
		return prepared, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return prepared, err
	}
	rel, err := filepath.Rel(projectRoot, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return prepared, errors.New("repository must be inside the registered project root")
	}
	discovery, err := workspace.Discover(ctx, projectRoot)
	if err != nil {
		return prepared, err
	}
	discovered := map[string]workspace.Repository{}
	for _, repository := range discovery.Repositories {
		discovered[repository.Identity.Root] = repository
	}
	repository, ok := discovered[root]
	if !ok || repository.GitLayout != "directory" || len(repository.Issues) != 0 {
		return prepared, errors.New("repository is not an eligible ordinary discovered Git root")
	}
	for _, boundary := range nested {
		candidate := filepath.Join(root, filepath.FromSlash(boundary))
		child, found := discovered[candidate]
		if !found || child.GitLayout != "directory" || len(child.Issues) != 0 || candidate == root {
			return prepared, fmt.Errorf("nested boundary %s is not an eligible discovered repository", boundary)
		}
	}
	observation, err := workspace.ObserveEnrollment(ctx, root, input.BaseRef, input.PlanBranch, input.Remote, nested)
	if err != nil {
		return prepared, err
	}
	if input.DirtyChoice == "clean" && observation.Baseline.Dirty {
		return prepared, errors.New("clean enrollment rejected because tracked or untracked work exists")
	}
	if input.DirtyChoice == "include" {
		if !observation.Baseline.Dirty || len(included) == 0 {
			return prepared, errors.New("include requires explicit paths covering existing work")
		}
		for _, path := range observation.Baseline.DirtyPaths {
			if !includesPath(included, path) {
				return prepared, fmt.Errorf("dirty path %s is outside included_paths", path)
			}
		}
	}
	prepared = preparedEnrollment{input: input, root: root, included: included, nested: nested, observation: observation}
	return prepared, nil
}

func (e *Engine) persistRepositoryEnrollment(ctx context.Context, tx *store.Tx, prepared preparedEnrollment) (any, error) {
	input, root, included, nested, observation := prepared.input, prepared.root, prepared.included, prepared.nested, prepared.observation
	var revision int
	var oldRoot string
	err := tx.QueryRowContext(ctx, "SELECT root FROM repositories WHERE id=?", input.ID).Scan(&oldRoot)
	if err == nil && oldRoot != root {
		return nil, errors.New("stable repository ID is already bound to another root")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(revision),0)+1 FROM repository_revisions WHERE repository_id=?", input.ID).Scan(&revision); err != nil {
		return nil, err
	}
	identityJSON, _ := json.Marshal(observation.Identity)
	includedJSON, _ := json.Marshal(included)
	nestedJSON, _ := json.Marshal(nested)
	fingerprintJSON, _ := json.Marshal(observation.Baseline)
	// A fingerprint row is an immutable observation owned by one repository
	// revision. Identical content may legitimately be enrolled under a new
	// branch/policy revision, so its row identity cannot be content-only.
	fingerprintID := store.Digest([]byte(input.ID + "\x00" + fmt.Sprint(revision) + "\x00" + string(fingerprintJSON)))
	_, err = tx.ExecContext(ctx, `INSERT INTO repositories(id,root,common_git_identity,base_ref,base_oid,identity_json,inclusion)
		VALUES(?,?,?,?,?,?,'participating')
		ON CONFLICT(id) DO UPDATE SET common_git_identity=excluded.common_git_identity,base_ref=excluded.base_ref,base_oid=excluded.base_oid,identity_json=excluded.identity_json,inclusion='participating'`,
		input.ID, root, observation.Identity.CommonGit, input.BaseRef, observation.BaseOID, string(identityJSON))
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO repository_revisions(repository_id,revision,plan_id,root_identity_json,remote_name,remote_identity,base_ref,base_oid,plan_branch,dirty_choice,included_paths_json,nested_boundaries_json,fingerprint_id,enrolled_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, input.ID, revision, input.PlanID, string(identityJSON), nullable(input.Remote), nullable(observation.RemoteIdentity), input.BaseRef, observation.BaseOID, input.PlanBranch, input.DirtyChoice, string(includedJSON), string(nestedJSON), fingerprintID, store.Now())
	if err != nil {
		return nil, err
	}
	dirty := 0
	if observation.Baseline.Dirty {
		dirty = 1
	}
	dirtyJSON, _ := json.Marshal(observation.Baseline.DirtyPaths)
	exclusionsJSON, _ := json.Marshal(observation.Baseline.Exclusions)
	_, err = tx.ExecContext(ctx, `INSERT INTO repository_fingerprints(id,repository_id,repository_revision,head_oid,head_ref,index_digest,content_digest,dirty,dirty_paths_json,exclusions_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, fingerprintID, input.ID, revision, observation.Baseline.HeadOID, nullable(observation.Baseline.HeadRef), observation.Baseline.IndexDigest, observation.Baseline.ContentDigest, dirty, string(dirtyJSON), string(exclusionsJSON), store.Now())
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plan_repositories(plan_id,repository_id,branch,base_oid,observed_head) VALUES(?,?,?,?,?)
		ON CONFLICT(plan_id,repository_id) DO UPDATE SET branch=excluded.branch,base_oid=excluded.base_oid,observed_head=excluded.observed_head`, input.PlanID, input.ID, input.PlanBranch, observation.BaseOID, observation.Baseline.HeadOID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"repository_id": input.ID, "repository_revision": revision, "fingerprint_id": fingerprintID, "dirty": observation.Baseline.Dirty, "dirty_choice": input.DirtyChoice}, nil
}

func (e *Engine) applyRepositoryEnrollment(ctx context.Context, actor Authority, cmd Envelope) (json.RawMessage, error) {
	if actor != Human {
		return nil, errors.New("repository enrollment requires human authority")
	}
	args, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	command := store.Command{ID: cmd.CommandID, Actor: string(actor), Kind: cmd.Kind, Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		return receipt, err
	}
	var input RepositoryEnrollment
	if err := store.Decode(cmd.Payload, &input); err != nil {
		return nil, err
	}
	prepared, err := e.prepareRepositoryEnrollment(ctx, input)
	if err != nil {
		return nil, err
	}
	return e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", e.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != cmd.ExpectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", cmd.ExpectedRevision, revision)
		}
		result, err := e.persistRepositoryEnrollment(ctx, tx, prepared)
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
			return nil, err
		}
		return map[string]any{"project_id": e.ProjectID, "revision": revision + 1, "result": result}, nil
	})
}

func (e *Engine) Repository(ctx context.Context, id string) (RepositoryRecord, error) {
	return e.repositoryRevision(ctx, id, 0)
}

func (e *Engine) repositoryRevision(ctx context.Context, id string, revision int) (RepositoryRecord, error) {
	var result RepositoryRecord
	var identityJSON, includedJSON, nestedJSON, dirtyJSON, exclusionsJSON string
	var dirty int
	query := `SELECT r.id,rr.revision,rr.plan_id,r.root,rr.root_identity_json,rr.base_ref,rr.base_oid,rr.plan_branch,coalesce(rr.remote_name,''),coalesce(rr.remote_identity,''),rr.dirty_choice,rr.included_paths_json,rr.nested_boundaries_json,rr.fingerprint_id,
		f.head_oid,coalesce(f.head_ref,''),f.index_digest,f.content_digest,f.dirty,f.dirty_paths_json,f.exclusions_json
		FROM repositories r JOIN repository_revisions rr ON rr.repository_id=r.id
		JOIN repository_fingerprints f ON f.id=rr.fingerprint_id
		WHERE r.id=?`
	args := []any{id}
	if revision > 0 {
		query += " AND rr.revision=?"
		args = append(args, revision)
	} else {
		query += " ORDER BY rr.revision DESC LIMIT 1"
	}
	err := e.DB.SQL.QueryRowContext(ctx, query, args...).Scan(&result.ID, &result.Revision, &result.PlanID, &result.Root, &identityJSON, &result.BaseRef, &result.BaseOID, &result.PlanBranch, &result.RemoteName, &result.RemoteIdentity, &result.DirtyChoice, &includedJSON, &nestedJSON, &result.FingerprintID, &result.Baseline.HeadOID, &result.Baseline.HeadRef, &result.Baseline.IndexDigest, &result.Baseline.ContentDigest, &dirty, &dirtyJSON, &exclusionsJSON)
	if err != nil {
		return result, err
	}
	result.Baseline.Dirty = dirty == 1
	for _, field := range []struct {
		raw    string
		target any
	}{{identityJSON, &result.Identity}, {includedJSON, &result.IncludedPaths}, {nestedJSON, &result.NestedBoundaries}, {dirtyJSON, &result.Baseline.DirtyPaths}, {exclusionsJSON, &result.Baseline.Exclusions}} {
		if err := json.Unmarshal([]byte(field.raw), field.target); err != nil {
			return result, err
		}
	}
	return result, nil
}

type BranchOperation struct {
	OperationID        string `json:"operation_id"`
	RepositoryID       string `json:"repository_id"`
	RepositoryRevision int    `json:"repository_revision"`
	State              string `json:"state"`
	BranchRef          string `json:"branch_ref"`
	BaseOID            string `json:"base_oid"`
	ObservedOID        string `json:"observed_oid,omitempty"`
	ObservedHeadRef    string `json:"observed_head_ref,omitempty"`
}

func (e *Engine) PrepareRepository(ctx context.Context, commandID, repositoryID string, expectedRevision int) (BranchOperation, error) {
	return e.prepareRepository(ctx, commandID, repositoryID, expectedRevision, nil)
}

func (e *Engine) prepareRepository(ctx context.Context, commandID, repositoryID string, expectedRevision int, fault func(string) error) (BranchOperation, error) {
	var result BranchOperation
	if !store.SafeID(commandID) || !store.SafeID(repositoryID) || expectedRevision < 1 {
		return result, errors.New("command, repository and expected project revision required")
	}
	operationID := store.Digest([]byte("repository.prepare\x00" + commandID))
	args, _ := json.Marshal(map[string]any{"repository_id": repositoryID, "expected_revision": expectedRevision})
	command := store.Command{ID: commandID, Actor: string(Human), Kind: "repository.prepare", Args: args}
	receiptFound := false
	if _, found, err := e.DB.Receipt(ctx, command); err != nil {
		return result, err
	} else if found {
		receiptFound = true
		result, err = e.BranchOperation(ctx, operationID)
		if err != nil || result.State == "observed" || result.State == "reconciled" {
			return result, err
		}
	}
	var repository RepositoryRecord
	var err error
	if receiptFound {
		if result.RepositoryID != repositoryID {
			return result, store.ErrConflict
		}
		repository, err = e.repositoryRevision(ctx, result.RepositoryID, result.RepositoryRevision)
	} else {
		repository, err = e.Repository(ctx, repositoryID)
	}
	if err != nil {
		return result, err
	}
	if repository.DirtyChoice != "clean" || repository.Baseline.Dirty {
		return result, errors.New("branch preparation requires a clean enrolled baseline; included/postponed work remains deferred")
	}
	_, err = e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision, epoch int
		if err := tx.QueryRowContext(ctx, "SELECT revision,policy_epoch FROM project WHERE id=?", e.ProjectID).Scan(&revision, &epoch); err != nil {
			return nil, err
		}
		if revision != expectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", expectedRevision, revision)
		}
		evidence, _ := json.Marshal(map[string]any{"repository_id": repositoryID, "repository_revision": repository.Revision, "branch": repository.PlanBranch, "base_oid": repository.BaseOID})
		if _, err := tx.ExecContext(ctx, "INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,evidence_json,created_at) VALUES(?,'repository.branch_prepare',?,?,?,'prepared',?,?,?)", operationID, store.Digest([]byte(repository.Identity.Key)), store.Digest(args), epoch, repository.PlanID, string(evidence), store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO repository_branch_operations(id,repository_id,repository_revision,expected_base_oid,branch_ref,state,created_at) VALUES(?,?,?,?,?,'prepared',?)", operationID, repositoryID, repository.Revision, repository.BaseOID, "refs/heads/"+repository.PlanBranch, store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
			return nil, err
		}
		return map[string]string{"operation_id": operationID, "state": "prepared"}, nil
	})
	if err != nil {
		return result, err
	}
	if fault != nil {
		if err := fault("after_intent"); err != nil {
			result, inspectErr := e.BranchOperation(ctx, operationID)
			if inspectErr != nil {
				return result, inspectErr
			}
			return result, err
		}
	}
	result, err = e.BranchOperation(ctx, operationID)
	if err != nil || result.State == "observed" || result.State == "reconciled" {
		return result, err
	}
	observed, effectErr := workspace.PrepareBranch(ctx, repository.Identity, repository.PlanBranch, repository.BaseOID, repository.Baseline)
	if effectErr != nil {
		_ = e.markBranchUncertain(ctx, operationID)
		result, inspectErr := e.BranchOperation(ctx, operationID)
		if inspectErr != nil {
			return result, inspectErr
		}
		return result, effectErr
	}
	if fault != nil {
		if err := fault("after_effect"); err != nil {
			result, inspectErr := e.BranchOperation(ctx, operationID)
			if inspectErr != nil {
				return result, inspectErr
			}
			return result, err
		}
	}
	err = e.DB.Write(ctx, func(tx *store.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM repository_branch_operations WHERE id=?", operationID).Scan(&state); err != nil {
			return err
		}
		if state != "prepared" && state != "uncertain" {
			return nil
		}
		next := "observed"
		if state == "uncertain" {
			next = "reconciled"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE repository_branch_operations SET state=?,observed_oid=?,observed_head_ref=?,observed_at=? WHERE id=?", next, observed.HeadOID, observed.HeadRef, store.Now(), operationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='observed',evidence_json=? WHERE id=?", string(mustJSON(observed)), operationID); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"operation_id": operationID, "repository_id": repositoryID, "state": next})
		_, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,kind,occurred_at,payload_json) VALUES(1,'repository_branch_prepared',?,?)", store.Now(), string(payload))
		return err
	})
	if err != nil {
		return result, err
	}
	return e.BranchOperation(ctx, operationID)
}

func mustJSON(value any) []byte { b, _ := json.Marshal(value); return b }

func (e *Engine) markBranchUncertain(ctx context.Context, operationID string) error {
	return e.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE repository_branch_operations SET state='uncertain' WHERE id=? AND state='prepared'", operationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=? AND state='prepared'", operationID)
		return err
	})
}

func (e *Engine) BranchOperation(ctx context.Context, operationID string) (BranchOperation, error) {
	var result BranchOperation
	err := e.DB.SQL.QueryRowContext(ctx, `SELECT id,repository_id,repository_revision,state,branch_ref,expected_base_oid,coalesce(observed_oid,''),coalesce(observed_head_ref,'') FROM repository_branch_operations WHERE id=?`, operationID).Scan(&result.OperationID, &result.RepositoryID, &result.RepositoryRevision, &result.State, &result.BranchRef, &result.BaseOID, &result.ObservedOID, &result.ObservedHeadRef)
	return result, err
}

// refreshIndexForMovedHead brings the operator's index in line with a commit the
// application made on the ref HEAD already points at.
//
// It is a no-op unless HEAD is the target ref this application prepared. Each
// committed path's index entry is set to the mode and blob recorded in the commit
// that was just written — read back out of that commit, not out of the working
// tree and not through Git's clean filters — so the index cannot acquire content
// the approved commit does not already contain. It writes no working-tree file and
// touches no other index entry.
func (e *Engine) refreshIndexForMovedHead(ctx context.Context, record RepositoryRecord, intent CommitIntent, commitOID, checkedOut string) error {
	if checkedOut != intent.TargetRef {
		return nil
	}
	// Re-read HEAD rather than trusting the value observed before the effect: the
	// ref has moved since, and the index must be refreshed against the commit that
	// is actually checked out.
	head, err := deliveryGit(ctx, record.Root, nil, nil, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || head != intent.TargetRef {
		return errors.New("HEAD no longer names the committed plan ref, so the index was left untouched")
	}
	for _, name := range intent.Paths {
		entry, err := deliveryGit(ctx, record.Root, nil, nil, "ls-tree", "-z", commitOID, "--", name)
		if err != nil {
			return err
		}
		record_ := strings.TrimSuffix(entry, "\x00")
		head, path, ok := strings.Cut(record_, "\t")
		if !ok {
			// The path is absent from the commit: the commit removed it, so the
			// index entry goes with it.
			if _, err := deliveryGit(ctx, record.Root, nil, nil, "update-index", "--force-remove", "--", name); err != nil {
				return err
			}
			continue
		}
		// `ls-tree` prints "<mode> <type> <oid>\t<path>"; the mode and object are
		// fields 0 and 2, not 0 and 1.
		parts := strings.Fields(head)
		if len(parts) != 3 || path != name || !gitOID(parts[2]) {
			return fmt.Errorf("commit tree entry for %s is not an exact path mode pair", name)
		}
		if _, err := deliveryGit(ctx, record.Root, nil, nil, "update-index", "--add", "--cacheinfo", parts[0]+","+parts[2]+","+name); err != nil {
			return err
		}
	}
	return nil
}

// applicationOwnsPlanCheckout reports whether the application itself put HEAD on
// the given plan ref, as opposed to a person having checked it out.
//
// The distinction is the whole point, and it is durably recorded rather than
// inferred. `PrepareBranch` performs `git symbolic-ref HEAD <plan ref>`, and the
// operation that journaled that intent records the head ref it observed
// afterwards. A delivery that finds HEAD on the plan ref can therefore tell two
// situations apart that look identical to `git symbolic-ref`:
//
//   - The application parked HEAD there as a documented, journaled step of the
//     prepare/execute/accept workflow, and the accepted fingerprint binds that
//     checkout. Refusing to move the ref would make delivery unreachable, because
//     preparing the plan branch is itself required for execution.
//   - A person checked the ref out, possibly after a commit was prepared.
//     Advancing the ref under them would move their branch, so the commit path
//     refuses.
//
// Without this predicate the two refusals are mutually exclusive: the application
// leaves the checkout in the one state its own commit path will not act on, and
// the only way out — returning the checkout to the base branch — invalidates the
// accepted fingerprint, because acceptance binds the whole baseline including the
// head ref. That deadlock was Stage 5.7 finding 5.7-F1.
//
// The state requirement mirrors `internal/supervisor/preparation.go`: a branch
// operation is authoritative only once it has been observed or reconciled, never
// while merely prepared. A `prepared` row means the symbolic-ref mutation may not
// have happened, so it cannot establish that the application owns the checkout.
// Requiring `branch_ref` to equal the target and `observed_head_ref` to equal the
// target also means a repository whose HEAD the user later moved elsewhere and
// back is no longer treated as application-owned by accident: the recorded
// observation no longer describes the current checkout.
// applicationOwnsPlanCheckout reports whether the application itself put HEAD on
// the given plan ref, as opposed to a person having checked it out.
//
// The distinction is the whole point, and it is durably recorded rather than
// inferred. `PrepareBranch` performs `git symbolic-ref HEAD <plan ref>`, and the
// operation that journaled that intent records the head ref it observed
// afterwards. A delivery that finds HEAD on the plan ref can therefore tell two
// situations apart that look identical to `git symbolic-ref`:
//
//   - The application parked HEAD there as a documented, journaled step of the
//     prepare/execute/accept workflow, and the accepted fingerprint binds that
//     checkout. Refusing to move the ref would make delivery unreachable, because
//     preparing the plan branch is itself required for execution.
//   - A person checked the ref out, possibly after a commit was prepared.
//     Advancing the ref under them would move their branch, so the commit path
//     refuses.
//
// Without this predicate the two refusals are mutually exclusive: the application
// leaves the checkout in the one state its own commit path will not act on, and
// the only way out — returning the checkout to the base branch — invalidates the
// accepted fingerprint, because acceptance binds the whole baseline including the
// head ref. That deadlock was Stage 5.7 finding 5.7-F1.
//
// The state requirement mirrors `internal/supervisor/preparation.go`: a branch
// operation is authoritative only once it has been observed or reconciled, never
// while merely prepared. A `prepared` row means the symbolic-ref mutation may not
// have happened, so it cannot establish that the application owns the checkout.
// Requiring `branch_ref` to equal the target and `observed_head_ref` to equal the
// target also means a repository whose HEAD the user later moved elsewhere and
// back is no longer treated as application-owned by accident: the recorded
// observation no longer describes the current checkout.
// acceptedRepositoryStillHolds reports whether the enrolled repository still shows
// exactly what the plan's acceptance approved.
//
// It accepts two states, and only two:
//
//   - the accepted baseline exactly, uncommitted; or
//   - the accepted content, already recorded by this plan's own verified delivery
//     commit: the same content digest, the same head ref, a HEAD equal to
//     `deliveryHead`, that commit's parent equal to the accepted head, and a clean
//     checkout.
//
// `deliveryHead` is supplied by the caller rather than looked up here, because the
// publication path runs inside a write transaction and must not open a second
// database handle. An empty value means no delivery commit is recorded, so only
// the exact accepted state is accepted.
//
// The second state is not a relaxation of the acceptance. It is the same acceptance
// after the application has done the one thing the acceptance approved. Without it
// the two states were mutually exclusive in the same way finding 5.7-F1 was:
// `BuildFactualArchive` re-verified the accepted baseline, so it had to run before
// the commit, while draft delivery requires an archive revision, so it had to run
// after it. Delivery was unreachable in every ordering.
//
// Everything the acceptance protects is still enforced. A different content digest, a
// different head ref, a dirty checkout, a head that is not this plan's own recorded
// delivery, or a delivery commit that is not directly on top of the accepted head
// all fail here, exactly as before.
func (e *Engine) acceptedRepositoryStillHolds(ctx context.Context, record RepositoryRecord, accepted workspace.Baseline) error {
	var deliveryHead string
	_ = e.DB.SQL.QueryRowContext(ctx, `SELECT head_oid FROM deliveries WHERE plan_id=? AND repository_id=? AND kind='commit' AND state='succeeded' ORDER BY rowid DESC LIMIT 1`,
		record.PlanID, record.ID).Scan(&deliveryHead)
	return acceptedRepositoryStateIsHonoured(ctx, record.Root, accepted, deliveryHead)
}

// acceptedRepositoryStateIsHonoured reports whether the repository at `root` still
// shows the accepted baseline, either uncommitted or as this plan's own recorded
// delivery of that same content.
func acceptedRepositoryStateIsHonoured(ctx context.Context, root string, accepted workspace.Baseline, deliveryHead string) error {
	normalized, err := workspace.NormalizeExclusions(root, accepted.Exclusions)
	if err != nil {
		return errors.New("accepted repository fingerprint exclusions are invalid")
	}
	expected := accepted
	expected.Exclusions = normalized
	observed, err := workspace.Fingerprint(ctx, root, accepted.Exclusions)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(observed, expected) {
		return nil
	}
	if !gitOID(deliveryHead) {
		return errors.New("accepted repository no longer matches the accepted task fingerprint")
	}
	// The content digest walks the working tree, so recording that content in a
	// commit does not change it. If it differs, nothing below can apply.
	if observed.ContentDigest != expected.ContentDigest || observed.HeadRef != expected.HeadRef || observed.Dirty {
		return errors.New("accepted repository no longer matches the accepted task fingerprint")
	}
	if observed.HeadOID != deliveryHead {
		return errors.New("accepted repository head is not this plan's own recorded delivery commit")
	}
	// The delivery commit must sit directly on the accepted head, so this state can
	// only be reached by recording the accepted content once.
	parent, err := deliveryGit(ctx, root, nil, nil, "rev-parse", "--verify", "--end-of-options", deliveryHead+"^")
	if err != nil || strings.TrimSpace(parent) != expected.HeadOID {
		return errors.New("accepted repository head is not this plan's own recorded delivery commit")
	}
	return nil
}

// applicationOwnsPlanCheckout reports whether the application itself put HEAD on
// the given plan ref, as opposed to a person having checked it out.
func (e *Engine) applicationOwnsPlanCheckout(ctx context.Context, record RepositoryRecord, target string) (bool, error) {
	var state, branchRef, observedHeadRef string
	err := e.DB.SQL.QueryRowContext(ctx, `SELECT state,branch_ref,coalesce(observed_head_ref,'') FROM repository_branch_operations WHERE repository_id=? AND repository_revision=? ORDER BY rowid DESC LIMIT 1`,
		record.ID, record.Revision).Scan(&state, &branchRef, &observedHeadRef)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state != "observed" && state != "reconciled" {
		return false, nil
	}
	return branchRef == target && observedHeadRef == target, nil
}
