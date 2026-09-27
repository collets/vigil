package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vigil/internal/artifacts"
	"vigil/internal/store"
)

func seedAcceptedPlan(t *testing.T, e *Engine) {
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
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	apply(t, e, "repository.enroll", RepositoryEnrollment{ID: "fixture-repo", PlanID: "plan", Root: root, BaseRef: "refs/heads/main", PlanBranch: "vigil/fixture", DirtyChoice: "clean"})
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
			VALUES(?,?, 'plan',1,?,?,?,?,?,?,?,?, 'local',1,?,?,?)`, scopeID, targetKind, taskID, taskRevision, store.Digest(repositoryManifest), string(repositoryManifest), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64), strings.Repeat("0", 64), store.Now())
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
