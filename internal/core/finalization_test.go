package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

func seedAcceptedPlan(t *testing.T, e *Engine) {
	seedAcceptedPlanWithChange(t, e, false)
}

func seedAcceptedPlanWithChange(t *testing.T, e *Engine, dirty bool, remotePath ...string) {
	t.Helper()
	ctx := context.Background()
	var root string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT root FROM project").Scan(&root); err != nil {
		t.Fatal(err)
	}
	initRepository(t, root)
	if err := os.WriteFile(filepath.Join(root, ".vigil-disposable-fixture"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", root, "add", ".vigil-disposable-fixture"}, {"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture marker"}} {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture Git: %v %s", err, b)
		}
	}
	if len(remotePath) != 0 {
		if b, err := exec.Command("git", "-C", root, "remote", "add", "origin", remotePath[0]).CombinedOutput(); err != nil {
			t.Fatalf("fixture remote: %v %s", err, b)
		}
	}
	if dirty {
		if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "src", "new.txt"), []byte("accepted change\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	apply(t, e, "project.configure", config())
	finalizationProfile := profile()
	finalizationProfile.Roles = append(finalizationProfile.Roles, "finalization")
	apply(t, e, "profile.put", finalizationProfile)
	apply(t, e, "plan.put", plan())
	enrollment := RepositoryEnrollment{ID: "fixture-repo", PlanID: "plan", Root: root, BaseRef: "refs/heads/main", PlanBranch: "vigil/fixture", DirtyChoice: "clean"}
	if len(remotePath) != 0 {
		enrollment.Remote = "origin"
	}
	if dirty {
		enrollment.DirtyChoice = "include"
		enrollment.IncludedPaths = []string{"src/new.txt"}
	}
	apply(t, e, "repository.enroll", enrollment)
	repositoryRecord, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	repositoryManifest, err := json.Marshal([]map[string]any{{"id": repositoryRecord.ID, "revision": repositoryRecord.Revision, "identity": repositoryRecord.Identity, "observed": repositoryRecord.Baseline}})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		t.Fatal(err)
	}
	var configDigest string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT s.digest FROM project_configurations c JOIN config_snapshots s ON s.id=c.config_id ORDER BY c.revision DESC LIMIT 1`).Scan(&configDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE plans SET state='finalizing' WHERE id='plan'"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second", "plan"} {
		artifact, err := repository.PutCore(ctx, "seed-acceptance-"+id, "acceptance-manifest", "durable", bytes.NewBufferString(`{"accepted":true}`))
		if err != nil {
			t.Fatal(err)
		}
		scopeID := store.Digest([]byte("scope:" + id))
		targetKind, taskID, taskRevision := "task", any(id), any(1)
		if id == "plan" {
			targetKind, taskID, taskRevision = "plan", nil, nil
		} else if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE tasks SET state='accepted' WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		_, err = e.DB.SQL.ExecContext(ctx, `INSERT INTO quality_scopes_v2(id,target_kind,plan_id,plan_revision,task_id,task_revision,repository_set_digest,repository_manifest_json,criteria_digest,definition_digest,config_digest,check_set_digest,reviewer_profile_id,reviewer_profile_revision,reviewer_profile_digest,instruction_digest,created_at)
			VALUES(?,?, 'plan',1,?,?,?,?,?,?,?,?, 'local',1,?,?,?)`, scopeID, targetKind, taskID, taskRevision, store.Digest(repositoryManifest), string(repositoryManifest), strings.Repeat("b", 64), strings.Repeat("c", 64), configDigest, strings.Repeat("e", 64), strings.Repeat("f", 64), strings.Repeat("0", 64), store.Now())
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.DB.SQL.ExecContext(ctx, `INSERT INTO quality_acceptances_v2(id,scope_id,target_kind,plan_id,task_id,evidence_manifest_id,evidence_manifest_digest,actor,accepted_at)
			VALUES(?,?,?,'plan',?,?,?,'fixture_core',?)`, "accept-"+id, scopeID, targetKind, taskID, artifact.ID, artifact.Digest, store.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFactualArchivePersistsBeforeNarrative(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	record, err := e.BuildFactualArchive(ctx, "archive-first", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != "narrative_pending" || record.Revision != 1 || record.ManifestDigest == "" {
		t.Fatal(record)
	}
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&state); err != nil || state != "finalization_pending" {
		t.Fatalf("plan state %q: %v", state, err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM tasks WHERE id=?", record.TaskID).Scan(&state); err != nil || state != "ready" {
		t.Fatalf("visible finalization task state %q: %v", state, err)
	}
	replayed, err := e.BuildFactualArchive(ctx, "archive-first", "plan")
	if err != nil || replayed != record {
		t.Fatalf("archive receipt replay: %#v %v", replayed, err)
	}
	got, manifest, err := e.Archive(ctx, "plan", 1)
	if err != nil || got != record || len(manifest.Tasks) != 2 || len(manifest.Artifacts) != 3 {
		t.Fatalf("archive: %#v %#v %v", got, manifest, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE plans SET state='stopped' WHERE id='plan'"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildFactualArchive(ctx, "archive-after-stop", "plan"); err == nil {
		t.Fatal("stopped plan produced another archive")
	}
}

func TestFactualArchiveRejectsCorruptRequiredEvidence(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	var artifactID string
	if err := e.DB.SQL.QueryRow("SELECT evidence_manifest_id FROM quality_acceptances_v2 WHERE id='accept-first'").Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.Exec("UPDATE artifacts SET digest=? WHERE id=?", fmt.Sprintf("%064x", 1), artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildFactualArchive(context.Background(), "archive-corrupt", "plan"); err == nil {
		t.Fatal("corrupt acceptance evidence was archived")
	}
}

func TestFactualArchiveRejectsChangedAcceptedTree(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlan(t, e)
	if err := os.WriteFile(filepath.Join(p.Root, "file.txt"), []byte("changed after acceptance\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildFactualArchive(context.Background(), "archive-changed-tree", "plan"); err == nil {
		t.Fatal("archive accepted a changed repository fingerprint")
	}
}

func TestFixtureFinalizationValidatesManifestReferences(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	archive, err := e.BuildFactualArchive(ctx, "archive-for-narrative", "plan")
	if err != nil {
		t.Fatal(err)
	}
	request := NarrativeResult{CommandID: "narrative-first", PlanID: "plan", ManifestRevision: archive.Revision, ManifestDigest: archive.ManifestDigest, Text: "The accepted fixture work is recorded in the factual archive.", CitedIDs: []string{"accept-plan", "accept-first", "accept-second"}, Actor: "fixture"}
	bad := request
	bad.CommandID = "narrative-foreign"
	bad.CitedIDs = append(append([]string{}, request.CitedIDs...), "not-in-manifest")
	if _, err := e.RecordFixtureNarrative(ctx, bad); err == nil {
		t.Fatal("foreign citation was accepted")
	}
	bad.CommandID = "narrative-missing"
	bad.CitedIDs = []string{"accept-plan", "accept-first"}
	if _, err := e.RecordFixtureNarrative(ctx, bad); err == nil {
		t.Fatal("missing task acceptance citation was accepted")
	}
	completed, err := e.RecordFixtureNarrative(ctx, request)
	if err != nil || completed.State != "verified" || completed.NarrativeID == "" {
		t.Fatalf("finalization: %#v %v", completed, err)
	}
	if replayed, err := e.RecordFixtureNarrative(ctx, request); err != nil || replayed != completed {
		t.Fatalf("narrative replay: %#v %v", replayed, err)
	}
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&state); err != nil || state != "completed" {
		t.Fatalf("completed plan: %s %v", state, err)
	}
	if _, err := e.RecordFixtureNarrative(ctx, NarrativeResult{CommandID: "narrative-twice", PlanID: "plan", ManifestRevision: archive.Revision, ManifestDigest: archive.ManifestDigest, Text: "Duplicate", CitedIDs: request.CitedIDs, Actor: "fixture"}); err == nil {
		t.Fatal("second narrative completed an already completed archive")
	}
}

type fixtureFinalizationProvider struct {
	output []byte
	err    error
	idle   bool
	calls  int
}

func (p *fixtureFinalizationProvider) Identity() PlanningProviderIdentity {
	return PlanningProviderIdentity{Harness: "hermes", Model: "fixture-local", Provider: "custom"}
}
func (p *fixtureFinalizationProvider) IdleObserved() bool { return p.idle }
func (p *fixtureFinalizationProvider) GenerateFinalization(_ context.Context, input FinalizationInput) ([]byte, error) {
	p.calls++
	if input.Manifest.AcceptanceID != "accept-plan" || input.ManifestDigest == "" {
		return nil, fmt.Errorf("fixture provider got wrong factual input")
	}
	return p.output, p.err
}

func TestFinalizationRunnerChargesFailureAndRetriesOnlyNarrative(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	archive, err := e.BuildFactualArchive(ctx, "archive-for-provider", "plan")
	if err != nil {
		t.Fatal(err)
	}
	request := FinalizationRunRequest{CommandID: "finalization-failure", PlanID: "plan", ManifestRevision: archive.Revision,
		ManifestDigest: archive.ManifestDigest, ProfileID: "local", ProfileRevision: 1, ActiveLimit: 2 * time.Second}
	failed := &fixtureFinalizationProvider{err: fmt.Errorf("fixture model failure"), idle: true}
	if _, err := e.RunFinalization(ctx, request, failed); err == nil || failed.calls != 1 {
		t.Fatalf("failed provider did not fail once: %v calls=%d", err, failed.calls)
	}
	if _, err := e.RunFinalization(ctx, request, failed); err == nil || failed.calls != 1 {
		t.Fatalf("failed provider was replayed: %v calls=%d", err, failed.calls)
	}
	var planState string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&planState); err != nil || planState != "finalization_pending" {
		t.Fatalf("provider failure changed accepted plan: %s %v", planState, err)
	}
	request.CommandID = "finalization-retry"
	output, _ := json.Marshal(map[string]any{"text": "The accepted fixture tasks passed their recorded evidence.",
		"cited_ids": []string{"accept-plan", "accept-first", "accept-second"}})
	success := &fixtureFinalizationProvider{output: output, idle: true}
	completed, err := e.RunFinalization(ctx, request, success)
	if err != nil || completed.State != "verified" || success.calls != 1 {
		t.Fatalf("finalization retry: %#v %v calls=%d", completed, err, success.calls)
	}
	if replay, err := e.RunFinalization(ctx, request, success); err != nil || replay != completed || success.calls != 1 {
		t.Fatalf("completed provider was replayed: %#v %v calls=%d", replay, err, success.calls)
	}
	var charged int64
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms FROM budget_ledgers WHERE scope='plan_services' AND plan_id='plan'").Scan(&charged); err != nil || charged < 2 {
		t.Fatalf("failed and successful finalization not charged: %d %v", charged, err)
	}
	var runCount int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM runs WHERE role='finalization' AND plan_id='plan'").Scan(&runCount); err != nil || runCount != 2 {
		t.Fatalf("finalization runs not recorded: %d %v", runCount, err)
	}
}

func TestArchiveExportIsPortableAndRefusesSymlinkParent(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	archive, err := e.BuildFactualArchive(ctx, "archive-for-export", "plan")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExportArchive(ctx, "plan", archive.Revision, filepath.Join(alias, "bundle")); err == nil {
		t.Fatal("export followed a symbolic-link parent")
	}
	destination := filepath.Join(base, "bundle")
	result, err := e.ExportArchive(ctx, "plan", archive.Revision, destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest != "manifest.json" || len(result.Artifacts) != 3 {
		t.Fatalf("export index: %#v", result)
	}
	for _, path := range []string{"manifest.json", "export-index.json"} {
		if _, err := os.ReadFile(filepath.Join(destination, path)); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range result.Artifacts {
		b, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(ref.Path)))
		if err != nil || store.Digest(b) != ref.Digest {
			t.Fatalf("exported artifact %s: %v", ref.ID, err)
		}
	}
	if _, err := e.ExportArchive(ctx, "plan", archive.Revision, destination); err == nil {
		t.Fatal("export overwrote an existing directory")
	}
}

func TestTranscriptRetentionFakeClockAndSharedBlob(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	var configID string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT config_id FROM project_configurations ORDER BY revision DESC LIMIT 1").Scan(&configID); err != nil {
		t.Fatal(err)
	}
	_, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at)
		VALUES('retention-run','plan',1,'first',1,?,'local',1,'implementation','initial','completed','contained_stopped',1000,2000,1000)`, configID)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := repository.PutCore(ctx, "retention-shared", "raw-transcript", "transcript", strings.NewReader("shared transcript"))
	if err != nil {
		t.Fatal(err)
	}
	durable, err := repository.PutCore(ctx, "retention-durable", "extracted-decision", "durable", strings.NewReader("shared transcript"))
	if err != nil {
		t.Fatal(err)
	}
	unique, err := repository.PutCore(ctx, "retention-unique", "raw-transcript", "transcript", strings.NewReader("unique transcript"))
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []artifacts.Artifact{shared, unique} {
		if _, err := e.DB.SQL.ExecContext(ctx, "INSERT INTO run_artifacts(run_id,artifact_id,purpose) VALUES('retention-run',?,'transcript')", artifact.ID); err != nil {
			t.Fatal(err)
		}
	}
	now := int64(50 * 86400000)
	if candidates, err := e.InspectTranscriptExpiry(ctx, now); err != nil || len(candidates) != 0 {
		t.Fatalf("unfinished plan eligible: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE plans SET state='completed',completed_at=? WHERE id='plan'", int64(10*86400000)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,evidence_json,created_at) VALUES('unresolved-retention','commit',?,?,1,'uncertain','plan','{}',1000)", strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if candidates, err := e.InspectTranscriptExpiry(ctx, now); err != nil || len(candidates) != 0 {
		t.Fatalf("unresolved operation eligible: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE operations SET state='reconciled' WHERE id='unresolved-retention'"); err != nil {
		t.Fatal(err)
	}
	checkpointManifest, err := repository.PutCore(ctx, "retention-checkpoint", "checkpoint-manifest", "durable", strings.NewReader("saved recovery context"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "INSERT INTO checkpoint_sets(id,run_id,operation_id,state,manifest_id,created_at) VALUES('retention-checkpoint','retention-run','unresolved-retention','saved',?,1000)", checkpointManifest.ID); err != nil {
		t.Fatal(err)
	}
	if candidates, err := e.InspectTranscriptExpiry(ctx, now); err != nil || len(candidates) != 0 {
		t.Fatalf("saved recovery checkpoint eligible: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE checkpoint_sets SET state='restored' WHERE id='retention-checkpoint'"); err != nil {
		t.Fatal(err)
	}
	if candidates, err := e.InspectTranscriptExpiry(ctx, int64(39*86400000)); err != nil || len(candidates) != 0 {
		t.Fatalf("premature expiry: %#v %v", candidates, err)
	}
	candidates, err := e.InspectTranscriptExpiry(ctx, now)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("eligible raw transcripts: %#v %v", candidates, err)
	}
	result, err := e.ExpireTranscripts(ctx, "expire-transcripts", now)
	if err != nil || len(result.Expired) != 2 || len(result.Deleted) != 1 || result.Deleted[0] != unique.Digest {
		t.Fatalf("expiry result: %#v %v", result, err)
	}
	if _, err := repository.Read(ctx, shared.ID); err == nil {
		t.Fatal("expired transcript remained readable")
	}
	if err := repository.Verify(ctx, durable.ID, durable.Digest, durable.Kind); err != nil {
		t.Fatalf("shared durable blob deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository.Dir, "blobs", unique.Digest)); !os.IsNotExist(err) {
		t.Fatalf("unique expired blob remains: %v", err)
	}
	if replay, err := e.ExpireTranscripts(ctx, "expire-transcripts", now); err != nil || len(replay.Expired) != 2 {
		t.Fatalf("expiry replay: %#v %v", replay, err)
	}
}

func TestApprovedCommitPreservesCheckoutAndReconcilesRef(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	before, err := workspace.Fingerprint(ctx, p.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := CommitRequest{CommandID: "commit-prepare", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Add accepted fixture change", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"}
	prepared, err := e.PrepareCommit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := e.PrepareCommit(ctx, request); err != nil || !reflect.DeepEqual(replay, prepared) {
		t.Fatalf("prepare replay: %#v %v", replay, err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	result, err := e.ExecuteCommit(ctx, prepared.OperationID, grant)
	if err != nil || result.State != "succeeded" || !gitOID(result.CommitOID) {
		t.Fatalf("commit: %#v %v", result, err)
	}
	if result.CommitOID == prepared.Intent.ParentOID {
		t.Fatal("commit did not advance plan branch")
	}
	ref, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "--verify", prepared.Intent.TargetRef)
	if err != nil || ref != result.CommitOID {
		t.Fatalf("wrong plan ref: %q %v", ref, err)
	}
	after, err := workspace.Fingerprint(ctx, p.Root, nil)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("commit disturbed accepted checkout: %#v %#v %v", before, after, err)
	}
	if replay, err := e.ExecuteCommit(ctx, prepared.OperationID, grant); err != nil || replay != result {
		t.Fatalf("commit replay: %#v %v", replay, err)
	}
	if _, err := e.BuildFactualArchive(ctx, "archive-after-commit", "plan"); err != nil {
		t.Fatalf("accepted fingerprint changed by plan ref commit: %v", err)
	}
}

func TestCommitReconcilesCrashAfterRefUpdate(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-crash", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Crash fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.start\x00"+prepared.OperationID)), "operation.start", map[string]string{"operation_id": prepared.OperationID, "grant_id": grant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	record, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := buildCommitTree(ctx, record, prepared.Intent.ParentOID, prepared.Intent.Paths, true)
	if err != nil || tree != prepared.Intent.TreeOID {
		t.Fatalf("crash tree: %s %v", tree, err)
	}
	date := fmt.Sprintf("%d +0000", prepared.Intent.Timestamp)
	env := []string{"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@invalid", "GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	oid, err := deliveryGit(ctx, p.Root, env, nil, "commit-tree", tree, "-p", prepared.Intent.ParentOID, "-m", prepared.Intent.Message)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Repeat("0", len(oid))
	if prepared.Intent.ExpectedRefOID != "" {
		old = prepared.Intent.ExpectedRefOID
	}
	if _, err := deliveryGit(ctx, p.Root, nil, nil, "update-ref", prepared.Intent.TargetRef, oid, old); err != nil {
		t.Fatal(err)
	}
	result, err := e.ExecuteCommit(ctx, prepared.OperationID, "")
	if err != nil || result.State != "succeeded" || result.CommitOID != oid {
		t.Fatalf("reconcile successful ref: %#v %v", result, err)
	}
	var count int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE kind='commit'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate commit delivery: %d %v", count, err)
	}
}

func TestCommitPreviewRejectsChangedAcceptedBytes(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(p.Root, "src", "new.txt"), []byte("unaccepted change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "stale-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Must fail", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err == nil {
		t.Fatal("changed accepted bytes prepared a commit")
	}
	var count int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM operations WHERE kind='commit'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale commit left approval operation: %d %v", count, err)
	}
}

func TestApprovedPushSendsOnlyExactPlanRefToBareRemote(t *testing.T) {
	_, e, p := setup(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if b, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare fixture: %v %s", err, b)
	}
	seedAcceptedPlanWithChange(t, e, true, remote)
	ctx := context.Background()
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-for-push", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Add fixture for push", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	commitGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, commitGrant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-push", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-push", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"}); err != nil || !reflect.DeepEqual(replay, push) {
		t.Fatalf("push prepare replay: %#v %v", replay, err)
	}
	pushGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: push.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	result, err := e.ExecutePush(ctx, push.OperationID, pushGrant)
	if err != nil || result.State != "succeeded" || result.HeadOID != committed.CommitOID {
		t.Fatalf("push: %#v %v", result, err)
	}
	refs, err := exec.Command("git", "--git-dir="+remote, "for-each-ref", "--format=%(refname) %(objectname)").CombinedOutput()
	if err != nil || strings.TrimSpace(string(refs)) != push.Intent.RemoteRef+" "+committed.CommitOID {
		t.Fatalf("unexpected remote refs: %s %v", refs, err)
	}
	if replay, err := e.ExecutePush(ctx, push.OperationID, pushGrant); err != nil || replay != result {
		t.Fatalf("push replay: %#v %v", replay, err)
	}
	if head, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "HEAD"); err != nil || head != commit.Intent.ParentOID {
		t.Fatalf("push touched user HEAD: %s %v", head, err)
	}
}

func TestPushRejectsChangedRemoteBeforeGrantConsumption(t *testing.T) {
	_, e, p := setup(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	other := filepath.Join(t.TempDir(), "other.git")
	for _, location := range []string{remote, other} {
		if b, err := exec.Command("git", "init", "--bare", "-q", location).CombinedOutput(); err != nil {
			t.Fatalf("bare fixture: %v %s", err, b)
		}
	}
	seedAcceptedPlanWithChange(t, e, true, remote)
	ctx := context.Background()
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-remote-change", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Commit", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	if _, err := e.ExecuteCommit(ctx, commit.OperationID, grant); err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-target-change", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	pushGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: push.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	if b, err := exec.Command("git", "-C", p.Root, "remote", "set-url", "origin", other).CombinedOutput(); err != nil {
		t.Fatalf("change fixture remote: %v %s", err, b)
	}
	if _, err := e.ExecutePush(ctx, push.OperationID, pushGrant); err == nil {
		t.Fatal("changed remote accepted for approved push")
	}
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", push.OperationID).Scan(&state); err != nil || state != "prepared" {
		t.Fatalf("changed remote consumed grant: %s %v", state, err)
	}
}

func TestPushReconcilesRemoteSuccessAfterLostObservation(t *testing.T) {
	_, e, p := setup(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if b, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare fixture: %v %s", err, b)
	}
	seedAcceptedPlanWithChange(t, e, true, remote)
	ctx := context.Background()
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-lost-push-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Lost push fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-lost-push", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	grant = resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: push.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.push.start\x00"+push.OperationID)), "operation.start", map[string]string{"operation_id": push.OperationID, "grant_id": grant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", committed.CommitOID+":"+push.Intent.RemoteRef).CombinedOutput(); err != nil {
		t.Fatalf("simulate remote success: %v %s", err, b)
	}
	result, err := e.ExecutePush(ctx, push.OperationID, "")
	if err != nil || result.State != "succeeded" || result.HeadOID != committed.CommitOID {
		t.Fatalf("lost push observation: %#v %v", result, err)
	}
	var count int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE kind='push'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate push delivery: %d %v", count, err)
	}
}

func seededPushedArchive(t *testing.T) (*Engine, string) {
	t.Helper()
	_, e, p := setup(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if b, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare fixture: %v %s", err, b)
	}
	seedAcceptedPlanWithChange(t, e, true, remote)
	ctx := context.Background()
	record, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", record.BaseOID+":refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("seed remote base: %v %s", err, b)
	}
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-hosting-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Hosted fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-hosting-push", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	grant = resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: push.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	if _, err := e.ExecutePush(ctx, push.OperationID, grant); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildFactualArchive(ctx, "hosting-factual-archive", "plan"); err != nil {
		t.Fatal(err)
	}
	return e, committed.CommitOID
}

func TestDraftAdaptersUseOneVerifiedFixtureRequest(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			e, head := seededPushedArchive(t)
			var mutex sync.Mutex
			var posted []byte
			postCount := 0
			var server *httptest.Server
			response := func() map[string]any {
				var input map[string]any
				_ = json.Unmarshal(posted, &input)
				if provider == "github" {
					return map[string]any{"number": 1, "draft": true, "state": "open", "html_url": server.URL + "/pull/1", "body": input["body"],
						"head": map[string]any{"ref": "vigil/fixture", "sha": head, "repo": map[string]string{"full_name": "fixture/project"}},
						"base": map[string]any{"ref": "main", "repo": map[string]string{"full_name": "fixture/project"}}}
				}
				return map[string]any{"iid": 1, "draft": true, "state": "opened", "web_url": server.URL + "/merge_requests/1", "description": input["description"],
					"source_branch": "vigil/fixture", "target_branch": "main", "sha": head, "source_project_id": 7, "target_project_id": 7}
			}
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					if posted == nil {
						_, _ = w.Write([]byte("[]"))
						return
					}
					_ = json.NewEncoder(w).Encode([]any{response()})
				case http.MethodPost:
					postCount++
					posted, _ = io.ReadAll(r.Body)
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(response())
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			defer server.Close()
			ctx := context.Background()
			request := DraftRequest{CommandID: "prepare-draft-" + provider, PlanID: "plan", RepositoryID: "fixture-repo", Provider: provider,
				Project: "fixture/project", APIBase: server.URL, BaseBranch: "main", Title: "Fixture draft", Body: "Verified accepted work", Fixture: true}
			prepared, err := e.PrepareDraft(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
			result, err := e.ExecuteDraft(ctx, prepared.OperationID, grant)
			if err != nil || result.State != "succeeded" || result.HeadOID != head {
				t.Fatalf("draft result: %#v %v", result, err)
			}
			if postCount != 1 {
				t.Fatalf("expected one hosted request, got %d", postCount)
			}
			if replay, err := e.ExecuteDraft(ctx, prepared.OperationID, grant); err != nil || replay != result || postCount != 1 {
				t.Fatalf("draft replay: %#v %v posts=%d", replay, err, postCount)
			}
			_, archive, err := e.Archive(ctx, "plan", 2)
			foundURL := false
			for _, delivery := range archive.Deliveries {
				if delivery.Kind == "draft_request" && delivery.URL == result.URL {
					foundURL = true
				}
			}
			if err != nil || len(archive.Deliveries) != 3 || !foundURL {
				t.Fatalf("delivery not appended to new factual revision: %#v %v", archive.Deliveries, err)
			}
		})
	}
}

func TestDraftLostPostResponseReconcilesWithoutDuplicate(t *testing.T) {
	e, head := seededPushedArchive(t)
	var mutex sync.Mutex
	var posted []byte
	posts := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		if r.Method == http.MethodPost {
			posts++
			posted, _ = io.ReadAll(r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if posted == nil {
			_, _ = w.Write([]byte("[]"))
			return
		}
		var input map[string]any
		_ = json.Unmarshal(posted, &input)
		body, _ := input["body"].(string)
		item := fakeHostedItem("github", server.URL, body, true)
		item["head"] = map[string]any{"ref": "vigil/fixture", "sha": head, "repo": map[string]string{"full_name": "fixture/project"}}
		_ = json.NewEncoder(w).Encode([]any{item})
	}))
	defer server.Close()
	ctx := context.Background()
	prepared, err := e.PrepareDraft(ctx, DraftRequest{CommandID: "prepare-lost-draft", PlanID: "plan", RepositoryID: "fixture-repo",
		Provider: "github", Project: "fixture/project", APIBase: server.URL, BaseBranch: "main", Title: "Lost response fixture", Fixture: true})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	result, err := e.ExecuteDraft(ctx, prepared.OperationID, grant)
	if err != nil || result.State != "succeeded" || result.HeadOID != head {
		t.Fatalf("lost response was not reconciled: %#v %v", result, err)
	}
	if again, err := e.ExecuteDraft(ctx, prepared.OperationID, ""); err != nil || again != result {
		t.Fatalf("lost-response replay: %#v %v", again, err)
	}
	mutex.Lock()
	count := posts
	mutex.Unlock()
	if count != 1 {
		t.Fatalf("duplicate POST after lost response: %d", count)
	}
}
