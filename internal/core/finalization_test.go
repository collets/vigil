package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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

// seedRealAcceptedPlan is seedAcceptedPlanWithChange without the
// disposable-fixture marker and with a `core` acceptance actor, so it
// describes a real, non-fixture plan. It is the fixture used to prove that no
// live model turn can complete a real plan.
func seedRealAcceptedPlan(t *testing.T, e *Engine) {
	t.Helper()
	seedAcceptedPlanWithChangeAndMarker(t, e, false, false, "core")
}

// seedMarkerlessFixtureAcceptedPlan has a `fixture_core` acceptance actor but a
// repository without the disposable-fixture marker, so the repository gate can
// be exercised independently of the acceptance-provenance gate.
func seedMarkerlessFixtureAcceptedPlan(t *testing.T, e *Engine) {
	t.Helper()
	seedAcceptedPlanWithChangeAndMarker(t, e, false, false, "fixture_core")
}

func seedAcceptedPlanWithChange(t *testing.T, e *Engine, dirty bool, remotePath ...string) {
	t.Helper()
	seedAcceptedPlanWithChangeAndMarker(t, e, dirty, true, "fixture_core", remotePath...)
}

// seedAcceptedPlanWithChangeAndMarker builds the seeded plan, optionally
// carrying the disposable-fixture marker and using a chosen acceptance actor.
func seedAcceptedPlanWithChangeAndMarker(t *testing.T, e *Engine, dirty, fixture bool, actor string, remotePath ...string) {
	t.Helper()
	ctx := context.Background()
	var root string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT root FROM project").Scan(&root); err != nil {
		t.Fatal(err)
	}
	initRepository(t, root)
	if fixture {
		if err := os.WriteFile(filepath.Join(root, ".vigil-disposable-fixture"), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"-C", root, "add", ".vigil-disposable-fixture"}, {"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture marker"}} {
			if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Fatalf("fixture Git: %v %s", err, b)
			}
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
			VALUES(?,?,?,'plan',?,?,?,?,?)`, "accept-"+id, scopeID, targetKind, taskID, artifact.ID, artifact.Digest, actor, store.Now())
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

// A platform root reached through a symbolic link (macOS resolves /var and
// /tmp through /private) is the operating system's own layout, not an operator
// redirection, so a real directory beneath it must export successfully. Only
// the directory the operator named is required to be a real directory. This
// fails on Linux if the ancestor walk is restored.
func TestArchiveExportAllowsSystemSymlinkedAncestors(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	archive, err := e.BuildFactualArchive(ctx, "archive-for-ancestor", "plan")
	if err != nil {
		t.Fatal(err)
	}
	// Build the same shape macOS presents: <root>/alias -> <real>.
	real := t.TempDir()
	systemRoot := filepath.Join(t.TempDir(), "var")
	if err := os.Mkdir(systemRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(systemRoot, "private")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(alias, "operator-dir")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(work, "bundle")
	if _, err := e.ExportArchive(ctx, "plan", archive.Revision, destination); err != nil {
		t.Fatalf("export refused a real directory beneath a system symlinked ancestor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "manifest.json")); err != nil {
		t.Fatalf("export did not materialize below the system ancestor: %v", err)
	}
	// The operator-named directory must still be a real directory: an alias
	// there remains a refusal, because that is the redirection under test.
	aliasParent := filepath.Join(real, "alias")
	if err := os.Symlink(work, aliasParent); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExportArchive(ctx, "plan", archive.Revision, filepath.Join(aliasParent, "redirected")); err == nil {
		t.Fatal("export followed a symbolic link in the named parent directory")
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
	// The ignore entry is written before the first view byte, and a user's own
	// .vigil directory content is never touched.
	if err := os.WriteFile(filepath.Join(p.Root, ".vigil", "user-note.txt"), []byte("mine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.BuildFactualArchive(ctx, "local-plan-view", "plan"); err != nil {
		t.Fatalf("user .vigil content blocked the view: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(p.Root, ".vigil", "user-note.txt")); err != nil || string(b) != "mine\n" {
		t.Fatalf("user .vigil content was modified: %q %v", b, err)
	}
	// A blocked view is reported as a warning, not a command failure: the
	// durable archive already committed, so failing would report a false error
	// for advanced state.
	if err := os.WriteFile(filepath.Join(view, name), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	replayed, err := e.BuildFactualArchive(ctx, "local-plan-view", "plan")
	if err != nil || replayed.ViewWarning == "" {
		t.Fatalf("corrupt view was not reported as a warning: %#v %v", replayed, err)
	}
	// The receipt is the durable fact; the warning is presentation only.
	replayed.ViewWarning = ""
	if replayed != record {
		t.Fatalf("replay after a blocked view: %#v", replayed)
	}
	// A user-owned .vigil file where the folder belongs is an operator error
	// the command must not hide behind a failure.
	if err := os.RemoveAll(filepath.Join(p.Root, ".vigil")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, ".vigil"), []byte("user content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	blocked, err := e.BuildFactualArchive(ctx, "blocked-plan-view", "plan")
	if err != nil {
		t.Fatalf("blocked view failed the command: %v", err)
	}
	if blocked.Revision != 2 || blocked.ViewWarning == "" {
		t.Fatalf("blocked view: %#v", blocked)
	}
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&state); err != nil || state != "finalization_pending" {
		t.Fatalf("durable plan state after a blocked view: %s %v", state, err)
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

// The Stage 5.6 plan requires testing policy revocation and a stale approval
// before an effect. A revoked grant and an expired grant must both stop the
// effect before any ref, push or POST, and must leave no trace of one.
func TestRevokedAndExpiredApprovalsStopTheEffectBeforeIt(t *testing.T) {
	for _, revoke := range []bool{true, false} {
		name := "expired"
		if revoke {
			name = "revoked"
		}
		t.Run(name, func(t *testing.T) {
			_, e, p := setup(t)
			seedAcceptedPlanWithChange(t, e, true)
			ctx := context.Background()
			prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-" + name, PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
				Paths: []string{"src/new.txt"}, Message: "Approval fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
			if err != nil {
				t.Fatal(err)
			}
			grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
			if revoke {
				if _, err := e.Apply(ctx, Human, envelope(t, e, "permission.revoke", map[string]string{"grant_id": grant})); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE grants SET expires_at=? WHERE id=?", store.Now()-1, grant); err != nil {
					t.Fatal(err)
				}
			}
			// The effect must be refused: operation.start re-checks the grant.
			if _, err := e.ExecuteCommit(ctx, prepared.OperationID, grant); err == nil {
				t.Fatal("a " + name + " approval still executed the commit")
			}
			// Nothing moved: the plan ref does not exist and no journal was written.
			if _, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", prepared.Intent.TargetRef+"^{commit}"); err == nil {
				t.Fatal("a " + name + " approval moved the plan ref")
			}
			var state string
			if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", prepared.OperationID).Scan(&state); err != nil || state != "prepared" {
				t.Fatalf("operation left %q after a %s approval: %v", state, name, err)
			}
			var rows int
			if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE operation_id=?", prepared.OperationID).Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("a %s approval left a delivery journal: %d %v", name, rows, err)
			}
		})
	}
}

// The plan requires testing a wrong destination base. A base branch that moves
// after draft approval must block the request before any POST.
func TestDraftRejectsChangedDestinationBaseBeforePosting(t *testing.T) {
	e, head := seededPushedArchive(t)
	ctx := context.Background()
	var posts int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()
	prepared, err := e.PrepareDraft(ctx, DraftRequest{CommandID: "prepare-base-drift", PlanID: "plan", RepositoryID: "fixture-repo",
		Provider: "github", Project: "fixture/project", APIBase: server.URL, BaseBranch: "main", Title: "Base drift", Fixture: true})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	// The destination base advances after approval, so the bound base OID no
	// longer describes the remote.
	record, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := deliveryGit(ctx, record.Root, nil, nil, "rev-parse", fmt.Sprintf("%s^{tree}", record.BaseOID))
	if err != nil {
		t.Fatal(err)
	}
	moved, err := deliveryGit(ctx, record.Root, nil, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit-tree", tree, "-p", record.BaseOID, "-m", "base moved")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", record.Root, "push", "-q", "origin", moved+":refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("move the destination base: %v %s", err, b)
	}
	if _, err := e.ExecuteDraft(ctx, prepared.OperationID, grant); err == nil {
		t.Fatal("draft executed against a changed destination base")
	}
	if posts != 0 {
		t.Fatalf("a changed destination base still produced %d POST(s)", posts)
	}
	// The approval is single-use: it was consumed by the refused attempt, so
	// the stale intent cannot be re-driven with a fresh grant either.
	var resolved string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM requests WHERE operation_id=?", prepared.OperationID).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	if resolved != "resolved" {
		t.Fatalf("the refused draft left its approval request %q", resolved)
	}
	if posts != 0 {
		t.Fatalf("a changed destination base produced %d POST(s)", posts)
	}
	_ = head
}

// R67 and the 5.6 plan require that an explicitly authorized draft delivery
// stays possible while the narrative is still pending, and that its URL is
// appended as a new factual archive revision without disturbing the pending
// narrative or the accepted work.
func TestDraftDeliveryIsPermittedWhileNarrativeIsPending(t *testing.T) {
	e, head := seededPushedArchive(t)
	ctx := context.Background()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			// GitHub creates a pull request with 201 Created.
			w.WriteHeader(http.StatusCreated)
			raw, _ := io.ReadAll(r.Body)
			var input map[string]any
			_ = json.Unmarshal(raw, &input)
			marker, _ := input["body"].(string)
			item := fakeHostedItem("github", server.URL, marker, true)
			item["head"] = map[string]any{"ref": "vigil/fixture", "sha": head, "repo": map[string]string{"full_name": "fixture/project"}}
			_ = json.NewEncoder(w).Encode(item)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()
	// The plan is finalization_pending with a factual archive and no narrative.
	archive, err := e.BuildFactualArchive(ctx, "archive-pending-delivery", "plan")
	if err != nil {
		t.Fatal(err)
	}
	var planState string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&planState); err != nil || planState != "finalization_pending" {
		t.Fatalf("plan is not awaiting finalization: %q %v", planState, err)
	}
	if archive.State != "factual_ready" {
		t.Fatalf("archive is not awaiting a narrative: %q", archive.State)
	}
	prepared, err := e.PrepareDraft(ctx, DraftRequest{CommandID: "pending-delivery-draft", PlanID: "plan", RepositoryID: "fixture-repo",
		Provider: "github", Project: "fixture/project", APIBase: server.URL, BaseBranch: "main", Title: "Pending narrative delivery",
		Body: "Accepted work", Fixture: true})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: prepared.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	result, err := e.ExecuteDraft(ctx, prepared.OperationID, grant)
	if err != nil || result.State != "succeeded" || result.URL == "" {
		t.Fatalf("draft delivery while narrative pending: %#v %v", result, err)
	}
	// The delivery is a new archive revision, still awaiting its narrative, and
	// the accepted plan is untouched.
	next, manifest, err := e.Archive(ctx, "plan", archive.Revision+1)
	if err != nil || next.State != "factual_ready" || next.NarrativeID != "" {
		t.Fatalf("delivery revision: %#v %v", next, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, delivery := range manifest.Deliveries {
		if delivery.URL == result.URL && delivery.State == "succeeded" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the delivered URL was not appended to the factual archive: %#v", manifest.Deliveries)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&planState); err != nil || planState != "finalization_pending" {
		t.Fatalf("delivery changed the accepted plan state to %q: %v", planState, err)
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

func TestStoredAcceptancePredatingVigilExclusionStillMatches(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlan(t, e)
	ctx := context.Background()
	record, err := e.Repository(ctx, "fixture-repo")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an acceptance recorded before .vigil became always-excluded:
	// rewrite the stored manifest to carry only the .git exclusion. A scope
	// row is immutable, so the legacy manifest is passed to the verifier
	// directly, which is the exact input a pre-change database would hold.
	var manifest string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT repository_manifest_json FROM quality_scopes_v2 WHERE plan_id='plan' AND task_id IS NULL").Scan(&manifest); err != nil {
		t.Fatal(err)
	}
	var repositories []struct {
		ID       string             `json:"id"`
		Revision int                `json:"revision"`
		Identity workspace.Identity `json:"identity"`
		Observed workspace.Baseline `json:"observed"`
	}
	if err := json.Unmarshal([]byte(manifest), &repositories); err != nil || len(repositories) == 0 {
		t.Fatal(manifest, err)
	}
	for i := range repositories {
		repositories[i].Observed.Exclusions = []string{".git"}
	}
	legacy, _ := json.Marshal(repositories)
	// The forced set alone must not invalidate that stored acceptance.
	baseline, err := acceptedBaselineForRepository(ctx, string(legacy), record)
	if err != nil {
		t.Fatalf("legacy stored exclusions invalidated an unchanged repository: %v", err)
	}
	if !slices.Contains(baseline.Exclusions, ".vigil") {
		t.Fatalf("stored exclusions were not normalized: %#v", baseline.Exclusions)
	}
	// A genuine content change must still be refused.
	if err := os.WriteFile(filepath.Join(record.Root, "tracked.txt"), []byte("tampered\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := acceptedBaselineForRepository(ctx, string(legacy), record); err == nil {
		t.Fatal("changed content passed the normalized acceptance check")
	}
}

func TestReconcileBlocksAnEffectThatRacesTheObservation(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-race-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Race fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	commitGrant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, commitGrant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-race-push", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
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
		VALUES(?,?,?,?, 'push','pending',?,?,?)`, pushDeliveryID(push.OperationID), "plan", "fixture-repo", push.OperationID, push.Intent.RemoteIdentity, push.Intent.HeadOID, push.Intent.RemoteRef); err != nil {
		t.Fatal(err)
	}
	// The push effect lands while the journal stays pending, exactly the
	// window a concurrent reconcile decision has to survive.
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", committed.CommitOID+":refs/heads/"+record.PlanBranch).CombinedOutput(); err != nil {
		t.Fatalf("landed push fixture: %v %s", err, b)
	}
	// Reconciliation of a landed effect is observed, never restated as a
	// net-zero effect, and a completed observation is final.
	status, err := e.ReconcileDelivery(ctx, "reconcile-landed-race", push.OperationID)
	if err != nil || status.State != "observed" || status.DeliveryState != "succeeded" {
		t.Fatalf("landed push was not recorded as observed: %#v %v", status, err)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-after-observed", push.OperationID); err == nil {
		t.Fatal("an observed operation was reconciled again")
	}
	// A claimed operation with a still-pending journal is not executable: no
	// new effect can start while reconciliation owns the decision. This is a
	// claim without a committed closure, so it is exactly what reconcile writes
	// before observing.
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE deliveries SET state='pending' WHERE operation_id=?", push.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=?", push.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='reconciled' WHERE id=?", push.OperationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExecutePush(ctx, push.OperationID, ""); err == nil {
		t.Fatal("a claimed operation was re-executed")
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

func TestReconcileNeverProvesNonOccurrenceAndAttestationIsExplicit(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-attest-commit", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Attest fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
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
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,head_oid)
		VALUES(?,?,?,?, 'commit','uncertain',?)`, commitDeliveryID(prepared.OperationID), "plan", "fixture-repo", prepared.OperationID, prepared.Intent.ParentOID); err != nil {
		t.Fatal(err)
	}
	// The ref is untouched, so reconcile must NOT record a net-zero closure:
	// an executor could be mid-effect and the system cannot prove otherwise.
	// It must also release the claim to the exact state it found — 'executing'
	// is resumable, 'uncertain' is observation-only, so downgrading would
	// silently remove the operator's ability to re-execute the commit.
	if _, err := e.ReconcileDelivery(ctx, "reconcile-untouched-commit", prepared.OperationID); err == nil {
		t.Fatal("reconcile closed an unobserved effect without proof")
	}
	var state, deliveryState string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", prepared.OperationID).Scan(&state); err != nil || state != "executing" {
		t.Fatalf("blocked reconcile left the operation %q, not the state it found: %v", state, err)
	}
	// A claim is never left held by a failed or blocked reconciliation.
	var closureKind sql.NullString
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT closure_kind FROM operations WHERE id=?", prepared.OperationID).Scan(&closureKind); err != nil || closureKind.Valid {
		t.Fatalf("blocked reconcile left a closure marker: %v %v", closureKind, err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE operation_id=?", prepared.OperationID).Scan(&deliveryState); err != nil || deliveryState != "uncertain" {
		t.Fatalf("blocked reconcile closed the journal: %q %v", deliveryState, err)
	}
	// A human attestation is the explicit exit, and is labelled as such.
	if _, err := e.CloseUnobservedDelivery(ctx, "attest-without-reason", prepared.OperationID, "   "); err == nil {
		t.Fatal("an empty attestation closed the operation")
	}
	closed, err := e.CloseUnobservedDelivery(ctx, "attest-commit", prepared.OperationID, "verified the plan ref is still at the approved predecessor")
	if err != nil || closed.State != "reconciled" || closed.DeliveryState != "failed" || closed.Attestation == "" {
		t.Fatalf("human attestation: %#v %v", closed, err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", prepared.OperationID).Scan(&state); err != nil || state != "reconciled" {
		t.Fatalf("attested operation state %q: %v", state, err)
	}
	// A completed attestation cannot be repeated, and the status of a
	// never-started plan shows an explicit absent predecessor rather than an
	// empty operand, which is when an operator most needs it.
	if status, err := e.DeliveryStatus(ctx, prepared.OperationID); err != nil || len(status.ApprovedTargets) != 3 ||
		!slices.Contains(status.ApprovedTargets, "approved_predecessor=<absent>") {
		t.Fatalf("approved operands: %#v %v", status.ApprovedTargets, err)
	}
	// delivery-status must carry the attestation provenance, so a later reader
	// can tell a human assertion from a system-proven outcome.
	if status, err := e.DeliveryStatus(ctx, prepared.OperationID); err != nil ||
		status.Attestation != "verified the plan ref is still at the approved predecessor" {
		t.Fatalf("delivery-status lost the attestation provenance: %#v %v", status.Attestation, err)
	}
	// The receipt records that a human attested, not that the system proved it.
	var actor, raw string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT actor,result_json FROM command_receipts WHERE id='attest-commit'").Scan(&actor, &raw); err != nil || actor != "human" {
		t.Fatalf("attestation receipt: %q %v", actor, err)
	}
	if !strings.Contains(raw, "attestation") {
		t.Fatalf("attestation receipt is not labelled: %s", raw)
	}
	// A completed attestation cannot be repeated.
	if _, err := e.CloseUnobservedDelivery(ctx, "attest-again", prepared.OperationID, "second claim"); err == nil {
		t.Fatal("an attested operation was closed again")
	}
}

func TestRealPlanCannotReachACompletedNarrativeThroughTheLivePath(t *testing.T) {
	_, e, _ := setup(t)
	// A real plan: no disposable-fixture marker and a `core` acceptance actor.
	seedRealAcceptedPlan(t, e)
	ctx := context.Background()
	archive, err := e.BuildFactualArchive(ctx, "real-plan-archive", "plan")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture narrative path refuses it even with the fixture actor.
	if _, err := e.RecordFixtureNarrative(ctx, NarrativeResult{CommandID: "real-narrative", PlanID: "plan", ManifestRevision: archive.Revision,
		ManifestDigest: archive.ManifestDigest, Text: "should be refused", CitedIDs: []string{"accept-plan", "accept-first", "accept-second"},
		Actor: "fixture"}); err == nil {
		t.Fatal("a real plan completed through the fixture narrative path")
	}
	// The live run path is refused, and no provider turn is ever dispatched.
	provider := &fixtureFinalizationProvider{output: []byte(`{"text":"should be refused","cited_ids":["accept-plan","accept-first","accept-second"]}`), idle: true}
	if _, err := e.RunFinalization(ctx, FinalizationRunRequest{CommandID: "real-finalization", PlanID: "plan", ManifestRevision: archive.Revision,
		ManifestDigest: archive.ManifestDigest, ProfileID: "local", ProfileRevision: 1, ActiveLimit: time.Second}, provider); err == nil {
		t.Fatal("a live model turn completed a real plan")
	}
	if provider.calls != 0 {
		t.Fatalf("a provider was dispatched for a real plan: %d calls", provider.calls)
	}
	// The plan is still awaiting finalization and the task is still ready.
	var planState, taskState string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&planState); err != nil || planState != "finalization_pending" {
		t.Fatalf("real plan state %q: %v", planState, err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM tasks WHERE id=?", archive.TaskID).Scan(&taskState); err != nil || taskState != "ready" {
		t.Fatalf("finalization task state %q: %v", taskState, err)
	}
	// A fixture-actor acceptance over a repository without the marker is still
	// refused, so the two gates are independent. Acceptances are immutable, so
	// this case gets its own engine.
	_, markerless, _ := setup(t)
	seedMarkerlessFixtureAcceptedPlan(t, markerless)
	markerlessArchive, err := markerless.BuildFactualArchive(ctx, "markerless-archive", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markerless.RecordFixtureNarrative(ctx, NarrativeResult{CommandID: "markerless-narrative", PlanID: "plan", ManifestRevision: markerlessArchive.Revision,
		ManifestDigest: markerlessArchive.ManifestDigest, Text: "should be refused", CitedIDs: []string{"accept-plan", "accept-first", "accept-second"},
		Actor: "fixture"}); err == nil {
		t.Fatal("a repository without the disposable-fixture marker was completed")
	}
	if err := markerless.DB.SQL.QueryRowContext(ctx, "SELECT state FROM plans WHERE id='plan'").Scan(&planState); err != nil || planState != "finalization_pending" {
		t.Fatalf("plan completed despite a missing fixture marker: %q %v", planState, err)
	}
}

func TestReconcileNeverReopensAnAttestedClosure(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-closed-push", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Closed fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-closed-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
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
	// The operator attests the push never landed. The destination is absent,
	// which is the approved predecessor, so a naive reconciler would
	// re-attempt the push on an operation already closed.
	closed, err := e.CloseUnobservedDelivery(ctx, "attest-closed-push", push.OperationID, "verified the destination branch does not exist on the remote")
	if err != nil || closed.State != "reconciled" {
		t.Fatalf("attestation: %#v %v", closed, err)
	}
	// Reconciliation must refuse: reopening a committed closure would perform
	// an irreversible external effect on a decided operation.
	if _, err := e.ReconcileDelivery(ctx, "reconcile-after-attest", push.OperationID); err == nil {
		t.Fatal("reconciliation reopened a human-attested closure")
	}
	if out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+record.PlanBranch).Output(); err == nil {
		t.Fatalf("reconcile pushed onto an attested-closed operation: %s", out)
	}
	// The attestation itself is durable and honestly labelled.
	status, err := e.DeliveryStatus(ctx, push.OperationID)
	if err != nil || status.State != "reconciled" || status.DeliveryState != "failed" ||
		status.Attestation != "verified the destination branch does not exist on the remote" {
		t.Fatalf("closed status: %#v %v", status, err)
	}
	// The plan ref is untouched by any of this.
	if got, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+record.PlanBranch+"^{commit}"); err != nil || got != committed.CommitOID {
		t.Fatalf("local plan ref moved: %s %v", got, err)
	}
}

func TestStaleReconcileReleaseCannotReopenAnAttestedClosure(t *testing.T) {
	_, e, p := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-stale-release", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Stale release", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
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
	if _, err := e.DB.SQL.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,head_oid)
		VALUES(?,?,?,?, 'commit','pending',?)`, commitDeliveryID(prepared.OperationID), "plan", "fixture-repo", prepared.OperationID, prepared.Intent.ParentOID); err != nil {
		t.Fatal(err)
	}
	// A human attests the effect never happened: a committed closure.
	if _, err := e.CloseUnobservedDelivery(ctx, "attest-stale", prepared.OperationID, "verified the ref never moved"); err != nil {
		t.Fatal(err)
	}
	// The first reconcile's stale release now runs. It must not resurrect the
	// attested closure, because that would re-arm the exact effect the
	// attestation denied.
	if err := e.releaseClaim(ctx, prepared.OperationID, "executing"); err != nil {
		t.Fatal(err)
	}
	var state string
	var closureKind sql.NullString
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state,closure_kind FROM operations WHERE id=?", prepared.OperationID).Scan(&state, &closureKind); err != nil {
		t.Fatal(err)
	}
	if state != "reconciled" || !closureKind.Valid {
		t.Fatalf("a stale release reopened an attested closure: state=%q closure_kind=%v", state, closureKind)
	}
	// And the operation is still not executable.
	if _, err := e.ExecuteCommit(ctx, prepared.OperationID, ""); err == nil {
		t.Fatal("an attested closure was re-executed after a stale release")
	}
	// The ref is untouched.
	if _, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", prepared.Intent.TargetRef+"^{commit}"); err == nil {
		t.Fatal("the attested commit ref was moved")
	}
}

func TestResumedClaimKeepsAStartedOperationExecutable(t *testing.T) {
	_, e, _ := setup(t)
	seedAcceptedPlanWithChange(t, e, true)
	ctx := context.Background()
	prepared, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-resumed-claim", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Resumed claim", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
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
	// The effect started and journaled, then a reconcile took the claim and
	// died. A second reconcile resumes that claim.
	if _, err := e.ExecuteCommit(ctx, prepared.OperationID, ""); err == nil {
		t.Skip("commit completed without the intended crash window")
	}
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='reconciled' WHERE id=?", prepared.OperationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A resumed claim that is blocked must restore 'executing', not
	// 'uncertain': an operator inspecting a stuck operation must keep the
	// ability to re-execute it.
	if _, err := e.ReconcileDelivery(ctx, "reconcile-resumed-claim", prepared.OperationID); err == nil {
		t.Fatal("a resumed claim closed an effect it could not observe")
	}
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", prepared.OperationID).Scan(&state); err != nil || state != "executing" {
		t.Fatalf("a resumed claim downgraded the operation to %q: %v", state, err)
	}
}

func TestReconcileNeverPushesAStartedOperationWithNoJournal(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-unattested-window", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Unattested window", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-unattested-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
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
	// The unattested crash window: operation.start committed as executing, the
	// journal INSERT did not. The push branch is the only reconciliation branch
	// that reaches an external effect, so it must refuse here — otherwise a
	// landing would occur that nothing could record, leaving the operation in a
	// state no command can close.
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", push.OperationID).Scan(&state); err != nil || state != "executing" {
		t.Fatalf("crash-window precondition: %q %v", state, err)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-unattested-window", push.OperationID); err == nil {
		t.Fatal("reconcile accepted a journal-less push")
	}
	if out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+record.PlanBranch).Output(); err == nil {
		t.Fatalf("reconcile pushed an operation with no journal: %s", out)
	}
	// The claim is released to the state it found, so the push stays resumable
	// and the documented remedy actually works.
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", push.OperationID).Scan(&state); err != nil || state != "executing" {
		t.Fatalf("refused reconcile downgraded the operation to %q: %v", state, err)
	}
	var closureKind sql.NullString
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT closure_kind FROM operations WHERE id=?", push.OperationID).Scan(&closureKind); err != nil || closureKind.Valid {
		t.Fatalf("refused reconcile left a closure marker: %v %v", closureKind, err)
	}
	result, err := e.ExecutePush(ctx, push.OperationID, "")
	if err != nil || result.State != "succeeded" {
		t.Fatalf("the documented remedy did not work: %#v %v", result, err)
	}
	if out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+record.PlanBranch).Output(); err != nil || strings.TrimSpace(string(out)) != committed.CommitOID {
		t.Fatalf("re-executed push did not deliver the approved head: %s %v", out, err)
	}
}

func TestAttestationOnAPushWithNoJournalIsNeverPushedTo(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-journal-less", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Journal-less fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	if _, err := e.ExecuteCommit(ctx, commit.OperationID, grant); err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-journal-less-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
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
	// The normal two-transaction crash window in ExecutePush: operation.start
	// has committed as executing, but the delivery journal INSERT never did.
	// The push branch is the only reconciliation branch that can reach an
	// external effect, so this state must never be pushed to.
	var state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", push.OperationID).Scan(&state); err != nil || state != "executing" {
		t.Fatalf("crash-window precondition: operation state %q: %v", state, err)
	}
	var rows int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE operation_id=?", push.OperationID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("crash-window precondition: %d journal rows: %v", rows, err)
	}
	closed, err := e.CloseUnobservedDelivery(ctx, "attest-journal-less", push.OperationID, "verified the destination branch does not exist; the push never started")
	if err != nil || closed.State != "reconciled" {
		t.Fatalf("attestation of a journal-less operation: %#v %v", closed, err)
	}
	// Neither reconcile nor a repeated reconcile may push.
	for _, commandID := range []string{"reconcile-journal-less", "reconcile-journal-less-again"} {
		if _, err := e.ReconcileDelivery(ctx, commandID, push.OperationID); err == nil {
			t.Fatalf("%s reopened an attested closure", commandID)
		}
		if out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+record.PlanBranch).Output(); err == nil {
			t.Fatalf("%s pushed onto an attested closure: %s", commandID, out)
		}
	}
	// The attestation is visible with no journal row present.
	status, err := e.DeliveryStatus(ctx, push.OperationID)
	if err != nil || status.State != "reconciled" || status.DeliveryState != "" ||
		status.Attestation != "verified the destination branch does not exist; the push never started" {
		t.Fatalf("journal-less attested status: %#v %v", status, err)
	}
	// The closure marker is durable.
	var closureKind sql.NullString
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT closure_kind FROM operations WHERE id=?", push.OperationID).Scan(&closureKind); err != nil || !closureKind.Valid || closureKind.String != "attested" {
		t.Fatalf("closure marker: %v %v", closureKind, err)
	}
}

func TestPushReconciliationObservesAHeadLandedDuringReconciliation(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-landing-push", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Landing fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-landing-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
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
	// The push lands between the reconciler's first observation and its
	// pre-push re-observation. The reconciler must recognise the approved head
	// as proof of the effect rather than telling the operator to attest.
	intent, _, err := e.loadPushIntent(ctx, push.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	// A shim around git lands the approved head on the reconciler's SECOND
	// ls-remote, deterministically opening the real window between the first
	// observation and the pre-push re-check. The committed head is what the
	// approved push would have delivered, so this models the original push
	// landing mid-reconciliation rather than a third party.
	shim := t.TempDir()
	log := filepath.Join(shim, "calls.log")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The shim lands the approved head when the reconciler re-observes the
	// destination immediately before the re-attempt (its second ls-remote),
	// which is exactly the window the fix covers.
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *ls-remote*) echo ls-remote >> " + log + "\n" +
		"    if [ \"$(grep -cx ls-remote " + log + ")\" = 2 ]; then\n" +
		"      " + realGit + " -C " + p.Root + " push -q origin " + committed.CommitOID + ":" + intent.RemoteRef + " >/dev/null 2>&1\n" +
		"    fi ;;\n" +
		"esac\n" +
		"exec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	status, err := e.ReconcileDelivery(ctx, "reconcile-concurrent-landing", push.OperationID)
	if err != nil || status.State != "observed" || status.DeliveryState != "succeeded" {
		t.Fatalf("concurrently landed push was not observed: %#v %v", status, err)
	}
	var state, deliveryState string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", push.OperationID).Scan(&state); err != nil || state != "observed" {
		t.Fatalf("durable operation state %q: %v", state, err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE operation_id=?", push.OperationID).Scan(&deliveryState); err != nil || deliveryState != "succeeded" {
		t.Fatalf("durable delivery state %q: %v", deliveryState, err)
	}
}

func TestPushReconciliationRetriesIdempotentlyWhenDestinationIsAtPredecessor(t *testing.T) {
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
	commit, err := e.PrepareCommit(ctx, CommitRequest{CommandID: "prepare-retry-push", PlanID: "plan", RepositoryID: "fixture-repo", TaskID: "first",
		Paths: []string{"src/new.txt"}, Message: "Retry fixture", AuthorName: "Fixture", AuthorEmail: "fixture@invalid"})
	if err != nil {
		t.Fatal(err)
	}
	grant := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: commit.RequestID, Scope: "once", Decision: "allow"}), "grant_id")
	committed, err := e.ExecuteCommit(ctx, commit.OperationID, grant)
	if err != nil {
		t.Fatal(err)
	}
	push, err := e.PreparePush(ctx, PushRequest{CommandID: "prepare-retry-remote", PlanID: "plan", RepositoryID: "fixture-repo", RemoteName: "origin"})
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
	// The VPN-drop case: the remote still holds the approved predecessor, so
	// the idempotent re-attempt delivers the approved object and reconcile
	// closes on positive proof, with no human attestation.
	status, err := e.ReconcileDelivery(ctx, "reconcile-retry-push", push.OperationID)
	if err != nil || status.State != "observed" || status.DeliveryState != "succeeded" {
		t.Fatalf("idempotent push retry: %#v %v", status, err)
	}
	if b, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+record.PlanBranch).Output(); err != nil || strings.TrimSpace(string(b)) != committed.CommitOID {
		t.Fatalf("re-attempted push did not deliver the approved head: %s %v", b, err)
	}
	// Retrying again is a no-op: the destination already holds the head.
	if _, err := e.ReconcileDelivery(ctx, "reconcile-retry-again", push.OperationID); err == nil {
		t.Fatal("an observed push was reconciled again")
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
	// A third party moves the plan ref before the effect, so the approved
	// compare-and-swap can never succeed.
	if b, err := exec.Command("git", "-C", p.Root, "update-ref", prepared.Intent.TargetRef, other, prepared.Intent.ExpectedRefOID).CombinedOutput(); err != nil {
		t.Fatalf("third-party ref move: %v %s", err, b)
	}
	// Execute the started operation: it journals the commit object and then
	// cannot move the ref, leaving the operation genuinely stuck.
	if _, err := e.ExecuteCommit(ctx, prepared.OperationID, ""); err == nil {
		t.Fatal("commit succeeded against a third-party-moved ref")
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-moved-commit", prepared.OperationID); err == nil {
		t.Fatal("reconciliation closed a commit whose ref was moved by a third party")
	}
	// A blocked reconciliation must leave the operation exactly as executable
	// as it found it, so the operator can resolve the ref and retry.
	if status, err := e.DeliveryStatus(ctx, prepared.OperationID); err != nil || status.State != "executing" {
		t.Fatalf("blocked reconciliation left the operation %q: %#v %v", status.State, status, err)
	}
	if _, err := deliveryGit(ctx, p.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", prepared.Intent.TargetRef+"^{commit}"); err != nil {
		t.Fatalf("blocked reconciliation disturbed the ref: %v", err)
	}
	// Restore the approved absent ref, then re-attempt the commit through the
	// normal executor path: reconciliation itself never moves the plan ref.
	if b, err := exec.Command("git", "-C", p.Root, "update-ref", "-d", prepared.Intent.TargetRef, other).CombinedOutput(); err != nil {
		t.Fatalf("restore approved absent ref: %v %s", err, b)
	}
	if _, err := e.ExecuteCommit(ctx, prepared.OperationID, ""); err != nil {
		t.Fatalf("re-executing the commit after the ref was restored: %v", err)
	}
	status, err := e.DeliveryStatus(ctx, prepared.OperationID)
	if err != nil || status.State != "observed" || status.DeliveryState != "succeeded" {
		t.Fatalf("commit did not complete after the ref was restored: %#v %v", status, err)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-commit-again", prepared.OperationID); err == nil {
		t.Fatal("already observed operation was reconciled again")
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
	// A third party creating the destination ref blocks reconciliation: the
	// approved push must not be forced over someone else's work.
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", record.BaseOID+":refs/heads/"+record.PlanBranch).CombinedOutput(); err != nil {
		t.Fatalf("third-party remote ref: %v %s", err, b)
	}
	if _, err := e.ReconcileDelivery(ctx, "reconcile-moved-push", push.OperationID); err == nil {
		t.Fatal("reconciliation closed a push whose remote ref was created by a third party")
	}
	if out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/"+record.PlanBranch).Output(); err != nil || strings.TrimSpace(string(out)) != record.BaseOID {
		t.Fatalf("blocked reconciliation overwrote a third-party ref: %s %v", out, err)
	}
	// The operator resolves the divergence; only then does the approved push
	// advance, and it does so by an observed effect, not an assertion.
	if b, err := exec.Command("git", "-C", p.Root, "push", "-q", "origin", ":refs/heads/"+record.PlanBranch).CombinedOutput(); err != nil {
		t.Fatalf("operator resolution of the third-party ref: %v %s", err, b)
	}
	closed, err := e.ReconcileDelivery(ctx, "reconcile-quiet-push", push.OperationID)
	if err != nil || closed.State != "observed" || closed.DeliveryState != "succeeded" {
		t.Fatalf("reconcile after operator resolution: %#v %v", closed, err)
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
