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
	if record.State != "factual_ready" || record.Revision != 1 || record.ManifestDigest == "" {
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
	identity *PlanningProviderIdentity
	crash bool
}

func (p *fixtureFinalizationProvider) Identity() PlanningProviderIdentity {
	if p.identity != nil {
		return *p.identity
	}
	return PlanningProviderIdentity{Harness: "hermes", Model: "fixture-local", Provider: "custom", Version: "0.21.3", EndpointID: "windows-llama", CredentialRef: "env:VIGIL_LLAMA_API_KEY"}
}
func (p *fixtureFinalizationProvider) IdleObserved() bool { return p.idle }
func (p *fixtureFinalizationProvider) GenerateFinalization(_ context.Context, input FinalizationInput) ([]byte, error) {
	p.calls++
	if p.crash {
		panic("simulated finalization process loss")
	}
	if input.Manifest.AcceptanceID != "accept-plan" || input.ManifestDigest == "" {
		return nil, fmt.Errorf("fixture provider got wrong factual input")
	}
	return p.output, p.err
}

func TestFinalizationCrashQuarantinePreservesExecutionFence(t *testing.T) {
	for _, afterNarrative := range []bool{false, true} {
		t.Run(fmt.Sprint("after-narrative-", afterNarrative), func(t *testing.T) {
			_, e, _ := setup(t)
			seedAcceptedPlan(t, e)
			ctx := context.Background()
			archive, err := e.BuildFactualArchive(ctx, "archive-for-crash", "plan")
			if err != nil {
				t.Fatal(err)
			}
			request := FinalizationRunRequest{CommandID: "crashed-finalization", PlanID: "plan", ManifestRevision: archive.Revision,
				ManifestDigest: archive.ManifestDigest, ProfileID: "local", ProfileRevision: 1, ActiveLimit: time.Second}
			func() {
				defer func() { _ = recover() }()
				_, _ = e.RunFinalization(ctx, request, &fixtureFinalizationProvider{crash: true})
			}()
			if afterNarrative {
				_, err := e.RecordFixtureNarrative(ctx, NarrativeResult{CommandID: "narrative-after-crash", PlanID: "plan", ManifestRevision: archive.Revision,
					ManifestDigest: archive.ManifestDigest, Text: "Accepted fixture", CitedIDs: []string{"accept-plan", "accept-first", "accept-second"}, Actor: "fixture"})
				if err != nil {
					t.Fatal(err)
				}
			}
			runID := store.Digest([]byte("finalization-run\x00" + request.CommandID))
			if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE runs SET started_at=? WHERE id=?", store.Now()-62000, runID); err != nil {
				t.Fatal(err)
			}
			if state, err := e.QuarantineFinalizationAttempt(ctx, request.CommandID); err != nil || state != "unknown" {
				t.Fatalf("quarantine failed: %q %v", state, err)
			}
			if state, err := e.QuarantineFinalizationAttempt(ctx, request.CommandID); err != nil || state != "unknown" {
				t.Fatalf("quarantine receipt was not idempotent: %q %v", state, err)
			}
			var writer string
			var unknown int64
			if err := e.DB.SQL.QueryRowContext(ctx, "SELECT writer_state FROM runs WHERE id=?", runID).Scan(&writer); err != nil || writer != "unconfirmed" {
				t.Fatalf("quarantine asserted unsupported writer stop: %q %v", writer, err)
			}
			if err := e.DB.SQL.QueryRowContext(ctx, "SELECT unknown_ms FROM budget_ledgers WHERE scope='plan_services' AND plan_id='plan'").Scan(&unknown); err != nil || unknown != 1000 {
				t.Fatalf("crashed attempt not charged full cap: %d %v", unknown, err)
			}
			if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at)
				SELECT 'another-run',plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,'active','unconfirmed',active_limit_ms,wall_limit_ms,? FROM runs WHERE id=?`, store.Now(), runID); err == nil {
				t.Fatal("unknown writer did not retain global execution fence")
			}
		})
	}
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
	mismatch := (&fixtureFinalizationProvider{}).Identity()
	mismatch.EndpointID = "other-local-endpoint"
	wrongRoute := &fixtureFinalizationProvider{identity: &mismatch, idle: true}
	if _, err := e.RunFinalization(ctx, request, wrongRoute); err == nil || wrongRoute.calls != 0 {
		t.Fatalf("mismatched endpoint reached finalization provider: %v calls=%d", err, wrongRoute.calls)
	}
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

func TestDefaultPlanArchiveViewIsLocalGitIgnoredAndFingerprintNeutral(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	record, err := e.BuildFactualArchive(ctx, "local-plan-view", "plan")
	if err != nil {
		t.Fatal(err)
	}
	view := filepath.Join(p.Root, ".vigil", "plans", "plan", "archive")
	name := fmt.Sprintf("factual-r%d-%s.json", record.Revision, record.ManifestDigest)
	content, err := os.ReadFile(filepath.Join(view, name))
	if err != nil || store.Digest(content) != record.ManifestDigest {
		t.Fatalf("default factual view missing or corrupt: %v", err)
	}
	if b, err := exec.Command("git", "-C", p.Root, "check-ignore", "--quiet", "--", filepath.ToSlash(filepath.Join(".vigil", "plans", "plan", "archive", name))).Output(); err != nil {
		t.Fatalf("archive view is not Git-ignored: %v %s", err, b)
	}
	// The in-repository view must not disturb the accepted exact fingerprint.
	accepted, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	observed, err := workspace.Fingerprint(ctx, accepted.Root, accepted.Baseline.Exclusions)
	if err != nil || !reflect.DeepEqual(observed, accepted.Baseline) {
		t.Fatalf("view write changed the accepted fingerprint: %v", err)
	}
	if _, err := e.BuildFactualArchive(ctx, "local-plan-view", "plan"); err != nil {
		t.Fatalf("exact archive receipt did not verify existing view: %v", err)
	}
	if err := os.WriteFile(filepath.Join(view, name), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildFactualArchive(ctx, "local-plan-view", "plan"); err == nil {
		t.Fatal("corrupt view passed receipt verification")
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
	if candidates, err := e.InspectTranscriptExpiry(ctx, "retention-inspect-1", now); err != nil || len(candidates) != 0 {
		t.Fatalf("unfinished plan eligible: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE plans SET state='completed',completed_at=? WHERE id='plan'", int64(10*86400000)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,evidence_json,created_at) VALUES('unresolved-retention','commit',?,?,1,'uncertain','plan','{}',1000)", strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if candidates, err := e.InspectTranscriptExpiry(ctx, "retention-inspect-2", now); err != nil || len(candidates) != 0 {
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
	if candidates, err := e.InspectTranscriptExpiry(ctx, "retention-inspect-3", now); err != nil || len(candidates) != 0 {
		t.Fatalf("saved recovery checkpoint eligible: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE checkpoint_sets SET state='restored' WHERE id='retention-checkpoint'"); err != nil {
		t.Fatal(err)
	}
	if candidates, err := e.InspectTranscriptExpiry(ctx, "retention-inspect-4", int64(39*86400000)); err != nil || len(candidates) != 0 {
		t.Fatalf("premature expiry: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "INSERT INTO requests(id,kind,state,plan_id,context_json,blocking,created_at,deadline) VALUES('retention-deadline','approval','pending','plan','{}',0,1000,?)", now+1000); err != nil {
		t.Fatal(err)
	}
	if candidates, err := e.InspectTranscriptExpiry(ctx, "retention-inspect-5", now); err != nil || len(candidates) != 0 {
		t.Fatalf("live pending request eligible: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE requests SET deadline=? WHERE id='retention-deadline'", now-1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExpireTranscripts(ctx, "expire-without-inspection", "retention-inspect-missing", now); err == nil {
		t.Fatal("expiry without a dry-run inspection receipt succeeded")
	}
	candidates, err := e.InspectTranscriptExpiry(ctx, "retention-inspect-final", now)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("eligible raw transcripts: %#v %v", candidates, err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE artifacts SET state='corrupt' WHERE id=?", unique.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExpireTranscripts(ctx, "expire-stale-inspection", "retention-inspect-final", now); err == nil {
		t.Fatal("expiry consumed a stale dry-run inspection receipt")
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE artifacts SET state='available' WHERE id=?", unique.ID); err != nil {
		t.Fatal(err)
	}
	result, err := e.ExpireTranscripts(ctx, "expire-transcripts", "retention-inspect-final", now)
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
	if replay, err := e.ExpireTranscripts(ctx, "expire-transcripts", "retention-inspect-final", now); err != nil || len(replay.Expired) != 2 {
		t.Fatalf("expiry replay: %#v %v", replay, err)
	}
	if _, err := e.ExpireTranscripts(ctx, "expire-again", "retention-inspect-final", now); err == nil {
		t.Fatal("a second expiry consumed the already-expired inspection receipt")
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

func TestCommitReconciliationRefusesNewlyCheckedOutPlanBranch(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-checked-out", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Guard checked-out branch", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
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
	if b, err := exec.Command("git", "-C", p.Root, "checkout", "-q", "-b", "vigil/fixture").CombinedOutput(); err != nil {
		t.Fatalf("switch fixture checkout: %v %s", err, b)
	}
	if _, err := e.ExecuteCommit(ctx, prepared.OperationID, ""); err == nil {
		t.Fatal("reconciliation moved a newly checked-out plan ref")
	}
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", prepared.OperationID).Scan(&state); err != nil || state != "executing" {
		t.Fatalf("checked-out reconciliation was marked observed: %s %v", state, err)
	}
	if _, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "--verify", prepared.Intent.TargetRef); err != nil {
		t.Fatal(err)
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

func TestPushTransportIgnoresManagedRepositoryURLRewrite(t *testing.T) {
	_, e, p := setup(t)
	approved := filepath.Join(t.TempDir(), "approved.git")
	redirected := filepath.Join(t.TempDir(), "redirected.git")
	for _, location := range []string{approved, redirected} {
		if b, err := exec.Command("git", "init", "--bare", "-q", location).CombinedOutput(); err != nil {
			t.Fatalf("bare fixture: %v %s", err, b)
		}
	}
	seedAcceptedPlanWithChange(t, e, true, approved)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-rewrite-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Rewrite fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, prepared.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", p.Root, "config", "url."+redirected+".insteadOf", approved).CombinedOutput(); err != nil {
		t.Fatalf("fixture URL rewrite: %v %s", err, b)
	}
	record, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := isolatedRemoteGit(ctx, record, []string{"GIT_ALLOW_PROTOCOL=file"}, true,
		"push", "--porcelain", "--no-verify", "--no-follow-tags", "--recurse-submodules=no", approved,
		committed.CommitOID+":refs/heads/vigil/fixture"); err != nil {
		t.Fatal(err)
	}
	if got, err := exec.Command("git", "--git-dir", approved, "rev-parse", "refs/heads/vigil/fixture").Output(); err != nil || strings.TrimSpace(string(got)) != committed.CommitOID {
		t.Fatalf("approved destination was not updated: %s %v", got, err)
	}
	if _, err := exec.Command("git", "--git-dir", redirected, "rev-parse", "refs/heads/vigil/fixture").Output(); err == nil {
		t.Fatal("repository URL rewrite retargeted isolated push")
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
					return map[string]any{"number": 1, "draft": true, "state": "open", "html_url": server.URL + "/fixture/project/pull/1", "body": input["body"],
						"head": map[string]any{"ref": "vigil/fixture", "sha": head, "repo": map[string]string{"full_name": "fixture/project"}},
						"base": map[string]any{"ref": "main", "repo": map[string]string{"full_name": "fixture/project"}}}
				}
				return map[string]any{"iid": 1, "draft": true, "state": "opened", "web_url": server.URL + "/fixture/project/-/merge_requests/1", "description": input["description"],
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

func TestConcurrentDraftLoserCancelsBeforePost(t *testing.T) {
	e, _ := seededPushedArchive(t)
	ctx := context.Background()
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			t.Error("duplicate draft loser posted externally")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()
	request := DraftRequest{CommandID: "first-draft-race", PlanID: "plan", RepositoryID: "fixture-repo", Provider: "github",
		Project: "fixture/project", APIBase: server.URL, BaseBranch: "main", Title: "Race fixture", Body: "Accepted work", Fixture: true}
	first, err := e.PrepareDraft(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.CommandID = "second-draft-race"
	second, err := e.PrepareDraft(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	firstGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: first.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	secondGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: second.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.draft.start\x00"+first.OperationID)), "operation.start", map[string]string{"operation_id": first.OperationID, "grant_id": firstGrant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,remote_identity,head_oid,base_ref)
		VALUES(?,?,?,?,'draft_request','pending',?,?,?)`, draftDeliveryID(first.OperationID), first.Intent.PlanID, first.Intent.RepositoryID, first.OperationID,
		first.Intent.RemoteIdentity, first.Intent.HeadOID, first.Intent.BaseBranch); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExecuteDraft(ctx, second.OperationID, secondGrant); err == nil {
		t.Fatal("duplicate draft loser was not rejected")
	}
	status, err := e.DeliveryStatus(ctx, second.OperationID)
	if err != nil || status.State != "cancelled" || status.DeliveryID != "" || posts != 0 {
		t.Fatalf("duplicate loser retained effect authority: %#v %v posts=%d", status, err, posts)
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

func TestFinalizationTaskLifecycleIsVisibleAndNeverAccepted(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	archive, err := e.BuildFactualArchive(ctx, "task-lifecycle-archive", "plan")
	if err != nil {
		t.Fatal(err)
	}
	taskState := func() string {
		var state string
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM tasks WHERE id=?", archive.TaskID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if taskState() != "ready" {
		t.Fatalf("finalization task not visibly ready: %s", taskState())
	}
	request := FinalizationRunRequest{CommandID: "task-lifecycle-failure", PlanID: "plan", ManifestRevision: archive.Revision,
		ManifestDigest: archive.ManifestDigest, ProfileID: "local", ProfileRevision: 1, ActiveLimit: 2 * time.Second}
	failed := &fixtureFinalizationProvider{err: fmt.Errorf("fixture model failure"), idle: true}
	if _, err := e.RunFinalization(ctx, request, failed); err == nil {
		t.Fatal("failing fixture provider succeeded")
	}
	if taskState() != "ready" {
		t.Fatalf("failed attempt did not leave the finalization task ready: %s", taskState())
	}
	request.CommandID = "task-lifecycle-success"
	output, _ := json.Marshal(map[string]any{"text": "The accepted fixture tasks passed their recorded evidence.",
		"cited_ids": []string{"accept-plan", "accept-first", "accept-second"}})
	success := &fixtureFinalizationProvider{output: output, idle: true}
	completed, err := e.RunFinalization(ctx, request, success)
	if err != nil || completed.State != "verified" {
		t.Fatalf("finalization success: %#v %v", completed, err)
	}
	if taskState() != "stopped" {
		t.Fatalf("verified narrative left the finalization task %q instead of stopped", taskState())
	}
	var accepted int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM quality_acceptances_v2 WHERE task_id=? AND invalidated_at IS NULL", archive.TaskID).Scan(&accepted); err != nil || accepted != 0 {
		t.Fatalf("finalization task claims acceptance evidence: %d %v", accepted, err)
	}
}

func TestCommitReconciliationClosesStuckOperationByObservation(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-reconcile-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Reconcile fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
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
	tree, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", fmt.Sprintf("%s^{tree}", prepared.Intent.ParentOID))
	if err != nil {
		t.Fatal(err)
	}
	other, err := deliveryGit(ctx, p.Root, nil, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit-tree", tree, "-m", "Third-party move")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash window after effect start: the journal is pending and a
	// third party has moved the plan ref, so the approved CAS can never succeed.
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,head_oid)
		VALUES(?,?,?,?, 'commit','pending',?)`, commitDeliveryID(prepared.OperationID), "plan", "fixture-repo", prepared.OperationID, prepared.Intent.ParentOID); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", p.Root, "update-ref", prepared.Intent.TargetRef, other, prepared.Intent.ExpectedRefOID).CombinedOutput(); err != nil {
		t.Fatalf("third-party ref move: %v %s", err, b)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-moved-commit", prepared.OperationID); err == nil {
		t.Fatal("reconciliation closed a commit whose ref was moved by a third party")
	}
	if status, err := e.DeliveryStatus(ctx, prepared.OperationID); err != nil || status.State != "executing" {
		t.Fatalf("blocked reconciliation changed the operation: %#v %v", status, err)
	}
	// The approved prior state is the absent branch: restore it by deleting
	// the third-party ref.
	if b, err := exec.Command("git", "-C", p.Root, "update-ref", "-d", prepared.Intent.TargetRef, other).CombinedOutput(); err != nil {
		t.Fatalf("restore approved absent ref: %v %s", err, b)
	}
	status, err := e.ReconcileDelivery(ctx, "reconcile-commit", prepared.OperationID)
	if err != nil || status.State != "reconciled" || status.DeliveryState != "failed" {
		t.Fatalf("commit reconciliation: %#v %v", status, err)
	}
	var operationState, deliveryState string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", prepared.OperationID).Scan(&operationState); err != nil || operationState != "reconciled" {
		t.Fatalf("operation not reconciled: %s %v", operationState, err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE operation_id=?", prepared.OperationID).Scan(&deliveryState); err != nil || deliveryState != "failed" {
		t.Fatalf("delivery not closed: %s %v", deliveryState, err)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-commit-again", prepared.OperationID); err == nil {
		t.Fatal("already reconciled operation was reconciled again")
	}
}

func TestPushReconciliationClosesUncertainOperationByObservation(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-reconcile-push", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Reconcile push", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	commitGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	if _, err := e.ExecuteCommit(ctx, commit.OperationID, commitGrant); err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-reconcile-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	pushGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: push.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.push.start\x00"+push.OperationID)), "operation.start", map[string]string{"operation_id": push.OperationID, "grant_id": pushGrant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	// Simulate a lost push response: the journal is uncertain and the approved
	// remote ref never moved.
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,remote_identity,head_oid,base_ref)
		VALUES(?,?,?,?, 'push','uncertain',?,?,?)`, pushDeliveryID(push.OperationID), "plan", "fixture-repo", push.OperationID, push.Intent.RemoteIdentity, push.Intent.HeadOID, push.Intent.RemoteRef); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=?", push.OperationID); err != nil {
		t.Fatal(err)
	}
	// A third party creating the destination ref blocks reconciliation.
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", record.BaseOID+":refs/heads/"+record.PlanBranch).CombinedOutput(); err != nil {
		t.Fatalf("third-party remote ref: %v %s", err, b)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-moved-push", push.OperationID); err == nil {
		t.Fatal("reconciliation closed a push whose remote ref was created by a third party")
	}
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", ":refs/heads/"+record.PlanBranch).CombinedOutput(); err != nil {
		t.Fatalf("restore approved absent remote ref: %v %s", err, b)
	}
	closed, err := e.ReconcileDelivery(ctx, "reconcile-quiet-push", push.OperationID)
	if err != nil || closed.State != "reconciled" || closed.DeliveryState != "failed" {
		t.Fatalf("quiet push reconciliation: %#v %v", closed, err)
	}
}

func TestPushReconciliationObservesLandedHead(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-landed-push", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Landed push", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	commitGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, commitGrant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-landed-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	pushGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: push.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.push.start\x00"+push.OperationID)), "operation.start", map[string]string{"operation_id": push.OperationID, "grant_id": pushGrant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,remote_identity,head_oid,base_ref)
		VALUES(?,?,?,?, 'push','uncertain',?,?,?)`, pushDeliveryID(push.OperationID), "plan", "fixture-repo", push.OperationID, push.Intent.RemoteIdentity, push.Intent.HeadOID, push.Intent.RemoteRef); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=?", push.OperationID); err != nil {
		t.Fatal(err)
	}
	// The approved push landed but its response was lost.
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", committed.CommitOID+":refs/heads/"+record.PlanBranch).CombinedOutput(); err != nil {
		t.Fatalf("landed push fixture: %v %s", err, b)
	}
	status, err := e.ReconcileDelivery(ctx, "reconcile-landed-push", push.OperationID)
	if err != nil || status.State != "observed" || status.DeliveryState != "succeeded" || status.DeliveryID == "" {
		t.Fatalf("landed push reconciliation: %#v %v", status, err)
	}
}

func TestDraftReconciliationRequiresPositiveObservation(t *testing.T) {
	e, head := seededPushedArchive(t)
	ctx := context.Background()
	mode := "absent"
	var opID string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if mode == "exact" {
			item := fakeHostedItem("github", server.URL, "<!-- vigil-delivery-operation:"+opID+" -->", true)
			item["head"] = map[string]any{"ref": "vigil/fixture", "sha": head, "repo": map[string]string{"full_name": "fixture/project"}}
			_ = json.NewEncoder(w).Encode([]any{item})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()
	prepared, err := e.PrepareDraft(ctx, DraftRequest{CommandID: "draft-reconcile", PlanID: "plan", RepositoryID: "fixture-repo",
		Provider: "github", Project: "fixture/project", APIBase: server.URL, BaseBranch: "main", Title: "Reconcile draft", Body: "Accepted work", Fixture: true})
	if err != nil {
		t.Fatal(err)
	}
	opID = prepared.OperationID
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.draft.start\x00"+prepared.OperationID)), "operation.start", map[string]string{"operation_id": prepared.OperationID, "grant_id": grant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,remote_identity,head_oid,base_ref)
		VALUES(?,?,?,?, 'draft_request','uncertain',?,?,?)`, draftDeliveryID(prepared.OperationID), "plan", "fixture-repo", prepared.OperationID,
		prepared.Intent.RemoteIdentity, prepared.Intent.HeadOID, prepared.Intent.BaseBranch); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=?", prepared.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-absent-draft", prepared.OperationID); err == nil {
		t.Fatal("absent draft listing closed an uncertain draft request")
	}
	mode = "exact"
	status, err := e.ReconcileDelivery(ctx, "reconcile-exact-draft", prepared.OperationID)
	if err != nil || status.State != "observed" || status.DeliveryState != "succeeded" || status.ExternalID != "1" {
		t.Fatalf("exact draft reconciliation: %#v %v", status, err)
	}
}

func TestPreparedDeliveryCancelIsSafeAndObserved(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-cancel-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Cancel fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := e.CancelPreparedDelivery(ctx, "cancel-prepared", prepared.OperationID)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatalf("prepared cancel: %#v %v", cancelled, err)
	}
	if _, err := e.CancelPreparedDelivery(ctx, "cancel-prepared-again", prepared.OperationID); err == nil {
		t.Fatal("cancelled operation was cancelled again")
	}
	second, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-cancel-started", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Cancel started fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: second.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	start, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.start\x00"+second.OperationID)), "operation.start", map[string]string{"operation_id": second.OperationID, "grant_id": grant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CancelPreparedDelivery(ctx, "cancel-started", second.OperationID); err == nil {
		t.Fatal("started operation was cancelled without reconciliation")
	}
	if status, err := e.DeliveryStatus(ctx, second.OperationID); err != nil || status.State != "executing" {
		t.Fatalf("started cancel changed the operation: %#v %v", status, err)
	}
}

func TestExcludedRepositoryPathNeverEntersApprovedCommits(t *testing.T) {
	exclusions := []string{".git", ".vigil", "nested"}
	for _, path := range []string{".vigil", ".vigil/plans/view.json", ".git/index", "nested/src/file.go"} {
		if !excludedRepositoryPath(path, exclusions) {
			t.Fatalf("excluded path %q was not recognized", path)
		}
	}
	for _, path := range []string{"src/new.txt", "vigil-notes.txt", "nested-thing/file.go"} {
		if excludedRepositoryPath(path, exclusions) {
			t.Fatalf("accepted path %q was excluded", path)
		}
	}
}
