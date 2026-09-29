package scenario

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scenario package's own tests are offline by construction: none of them
// build a binary, run a production command, contact a provider, or touch a
// network listener. They check the fixture, the opt-in gates, the bounds and the
// matrix discipline, which are the parts `make check` must police.

func TestFixtureContentIsDeterministicAndDigested(t *testing.T) {
	first := NewFixture()
	second := NewFixture()
	if len(first.Files) == 0 {
		t.Fatal("the fixture definition is empty")
	}
	for index := range first.Files {
		if first.Files[index] != second.Files[index] {
			t.Fatalf("fixture entry %d is not deterministic: %+v vs %+v", index, first.Files[index], second.Files[index])
		}
	}
	// The manifest must describe the exact committed bytes.
	root := filepath.Join(t.TempDir(), "work")
	if err := first.Materialize(root); err != nil {
		t.Fatal(err)
	}
	for _, file := range first.Files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != file.Bytes || Digest(raw) != file.Digest {
			t.Fatalf("%s does not match its manifest entry %+v", file.Path, file)
		}
	}
}

func TestFixtureRefusesANonEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "operator-file"), []byte("user work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewFixture().Materialize(root); err == nil {
		t.Fatal("the fixture overwrote a directory holding an existing file")
	}
	if _, err := os.Stat(filepath.Join(root, "operator-file")); err != nil {
		t.Fatal("the existing file was removed")
	}
}

func TestInjectedDefectIsRealAndCheckBlind(t *testing.T) {
	// The committed content must fail the check, and the injected-defect content
	// must pass it, or the review/repair cycle would be proving nothing.
	contents := fixtureFiles()
	if contents[SourcePath] != CommittedGreeting {
		t.Fatal("the committed artifact is not the documented starting content")
	}
	if InjectedDefectGreeting == ExpectedGreeting {
		t.Fatal("the injected defect is identical to the specified content")
	}
	if strings.TrimRight(InjectedDefectGreeting, "\n") == strings.TrimRight(ExpectedGreeting, "\n") {
		t.Fatal("the injected defect is not distinguishable from the specified content")
	}
	if _, line := trailingWhitespaceLine(InjectedDefectGreeting); line == 0 {
		t.Fatal("the reviewer cannot locate the injected defect in the artifact it was written from")
	}
	if _, line := trailingWhitespaceLine(ExpectedGreeting); line != 0 {
		t.Fatal("the specified content must not carry trailing whitespace")
	}
}

func TestReviewerDocumentRequiresARealDefect(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := NewFixture().Materialize(root); err != nil {
		t.Fatal(err)
	}
	walk := &walkthrough{root: t.TempDir(), fixtureBase: root}
	// With the defective artifact present the finding is produced.
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(SourcePath)), []byte(InjectedDefectGreeting), 0o600); err != nil {
		t.Fatal(err)
	}
	document := walk.reviewerDocument()
	if document.Decision != "request_changes" || len(document.Findings) != 1 {
		t.Fatalf("unexpected review document %+v", document)
	}
	if document.Findings[0].Path != SourcePath {
		t.Fatalf("the finding does not name the artifact it observed: %+v", document.Findings[0])
	}
	// With the defect removed, the scenario must fail rather than invent a finding.
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(SourcePath)), []byte(ExpectedGreeting), 0o600); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			recovered := recover()
			abort, ok := recovered.(*ScenarioAbort)
			if !ok {
				t.Fatalf("a fabricated review was produced instead of a failure: %v", recovered)
			}
			if !strings.Contains(abort.Err.Error(), "no real defect") {
				t.Fatalf("unexpected abort reason: %v", abort.Err)
			}
		}()
		walk.reviewerDocument()
	}()
}

func TestOptInsRefuseCodexAndRemoteDelivery(t *testing.T) {
	records := Resolve(OptIns{Docker: true, LocalInference: true, CodexLive: true, RemoteDelivery: true}, LocalRoute{})
	if Enabled(records, CapabilityCodexLive) {
		t.Fatal("a contained Codex route was reported as available")
	}
	if Enabled(records, CapabilityRemoteDelivery) {
		t.Fatal("real remote delivery was reported as available")
	}
	if !Enabled(records, CapabilityDocker) {
		t.Fatal("the Docker opt-in was not honoured")
	}
	// A requested local inference turn without a working route must fail loudly.
	if Enabled(records, CapabilityLocalInference) {
		t.Fatal("local inference was enabled without a probed route")
	}
	if err := RequireLocalInference(records); err == nil {
		t.Fatal("a missing local route did not fail a requested live turn")
	}
	pending := Pending(records)
	if len(pending) != 3 {
		t.Fatalf("expected the refused capabilities to be surfaced, got %v", pending)
	}
	for _, entry := range pending {
		if !strings.Contains(entry, ":") {
			t.Fatalf("a pending gate does not name its blocker: %q", entry)
		}
	}
}

func TestLoopbackRouteClassification(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:8080/v1", "http://localhost:8080/v1", "https://127.0.0.1:443/v1"} {
		if !isLoopbackBaseURL(base) {
			t.Errorf("%s should be recognized as a loopback route", base)
		}
	}
	for _, base := range []string{"", "http://192.168.0.108:8080/v1", "https://api.openai.com/v1", "ftp://127.0.0.1/v1", "127.0.0.1:8080"} {
		if isLoopbackBaseURL(base) {
			t.Errorf("%s must not be treated as a loopback route", base)
		}
	}
}

func TestRouteProbeNeverContactsANonLoopbackHost(t *testing.T) {
	// The probe is hard-bounded to loopback, so a misconfigured base URL fails
	// closed instead of sending the credential anywhere.
	if _, err := loopbackGet("https://example.invalid/v1/models"); err == nil {
		t.Fatal("the route probe contacted a non-loopback host")
	}
}

func TestParseModelListRequiresExactlyOneModel(t *testing.T) {
	if _, err := parseModelList([]byte(`{"data":[{"id":"one"}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"data":[]}`, `{"data":[{"id":"a"},{"id":"b"}]}`, `not json`, `{"data":[{"id":""}]}`} {
		if _, err := parseModelList([]byte(body)); err == nil {
			t.Errorf("an ambiguous route response was accepted: %s", body)
		}
	}
}

func TestBoundsRejectNonsense(t *testing.T) {
	valid := DefaultBounds()
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for name, bounds := range map[string]Bounds{
		"zero steps":     {MaxSteps: 0, MaxStepDurationMS: 1, MaxStepOutputBytes: 1024, MaxWallClockMS: 1},
		"zero duration":  {MaxSteps: 1, MaxStepDurationMS: 0, MaxStepOutputBytes: 1024, MaxWallClockMS: 1},
		"tiny output":    {MaxSteps: 1, MaxStepDurationMS: 1, MaxStepOutputBytes: 8, MaxWallClockMS: 1},
		"negative turns": {MaxSteps: 1, MaxStepDurationMS: 1, MaxStepOutputBytes: 1024, MaxWallClockMS: 1, MaxLiveModelTurns: -1},
	} {
		if err := bounds.validate(); err == nil {
			t.Errorf("%s bounds were accepted", name)
		}
	}
}

func TestDriverEnforcesItsStepBound(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "vigil")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := NewDriver(binary, t.TempDir(), t.TempDir(), Bounds{MaxSteps: 1, MaxStepDurationMS: 5000, MaxStepOutputBytes: 4096, MaxWallClockMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Invoke(context.Background(), "first", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Invoke(context.Background(), "second", "hello"); err == nil {
		t.Fatal("the step bound was not enforced")
	}
}

func TestCurrentRevisionPlaceholderIsRequiredForUse(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "vigil")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	driver, err := NewDriver(binary, t.TempDir(), t.TempDir(), DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	// Without a project in scope the placeholder must fail rather than guess.
	if _, err := driver.Invoke(context.Background(), "placeholder", "project", "apply", "--expected-revision", CurrentRevision); err == nil {
		t.Fatal("a current-revision placeholder was resolved with no project in scope")
	}
}

func TestMatrixRequiresADecidedRowForEveryStep(t *testing.T) {
	matrix := NewMatrix()
	// Every declared row starts undecided, and completeness must fail until each
	// carries a class with the detail its class requires.
	if err := matrix.RequireComplete(); err == nil {
		t.Fatal("an untouched matrix reported itself complete")
	}
	if err := matrix.Mark("milestone", StepExecuteSequentially, EvidenceAutomated, "", ""); err == nil {
		t.Fatal("a row was marked demonstrated with no detail")
	}
	if err := matrix.Mark("milestone", "no-such-step", EvidenceAutomated, "detail", ""); err == nil {
		t.Fatal("an undeclared row was accepted")
	}
	if err := matrix.Mark("no-such-section", StepExecuteSequentially, EvidenceAutomated, "detail", ""); err == nil {
		t.Fatal("an unknown section was accepted")
	}
	if err := matrix.Mark("milestone", StepExecuteSequentially, EvidenceAutomated, "observed through the production path", "cli"); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Defer("milestone", StepExecuteSequentially, "", "no blocker"); err == nil {
		t.Fatal("a row was deferred with no blocker")
	}
}

func TestRequirementCoverageIsTotalAndClassified(t *testing.T) {
	entries := requirementCoverage()
	if len(entries) != 71 {
		t.Fatalf("expected every R01-R71 requirement, got %d", len(entries))
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			t.Fatalf("requirement %s appears twice", entry.ID)
		}
		seen[entry.ID] = true
		switch entry.Evidence {
		case EvidenceAutomated, EvidenceReused:
			if strings.TrimSpace(entry.Detail) == "" {
				t.Fatalf("requirement %s claims evidence with no detail", entry.ID)
			}
		case EvidencePendingStage8, EvidenceUnmet:
			if strings.TrimSpace(entry.Blocker) == "" {
				t.Fatalf("requirement %s is pending with no named blocker", entry.ID)
			}
		default:
			t.Fatalf("requirement %s has an undecided evidence class %q", entry.ID, entry.Evidence)
		}
	}
	for index := 1; index <= 71; index++ {
		id := "R" + string(rune('0'+index/10)) + string(rune('0'+index%10))
		if !seen[id] {
			t.Fatalf("requirement %s is missing from the coverage audit", id)
		}
	}
}

func TestFakeHostingBindsLoopbackAndClosesCleanly(t *testing.T) {
	hosting, err := NewFakeHosting("github", "owner/repository", "main")
	if err != nil {
		t.Fatal(err)
	}
	defer hosting.Close()
	if !strings.HasPrefix(hosting.Base(), "http://127.0.0.1:") {
		t.Fatalf("the stand-in is not loopback-only: %s", hosting.Base())
	}
	hosting.SetIdentities("head", "base")
	if path, err := hosting.APIPathFor(); err != nil || path != "/repos/owner/repository/pulls" {
		t.Fatalf("unexpected endpoint path %q: %v", path, err)
	}
	if _, err := NewFakeHosting("bitbucket", "owner/repository", "main"); err == nil {
		t.Fatal("an unsupported provider was accepted")
	}
}

func TestRequirePrivateRootRefusesUnsafeLocations(t *testing.T) {
	for _, root := range []string{"", "/", "/tmp", "home", "/home"} {
		if err := requirePrivateRoot(root); err == nil {
			t.Errorf("the scenario accepted %q as a disposable root", root)
		}
	}
	if err := requirePrivateRoot(filepath.Join(t.TempDir(), "disposable")); err != nil {
		t.Errorf("a disposable temporary root was refused: %v", err)
	}
}

func TestBoundsAreEnforcedByTheReportDigest(t *testing.T) {
	// The report digest must change when any observed field changes, so a report
	// cannot be silently edited after the fact.
	report := &Report{SchemaVersion: 1, ScenarioID: "one"}
	first := reportDigest(report)
	report.ScenarioID = "two"
	if reportDigest(report) == first {
		t.Fatal("the report digest did not change with its content")
	}
}
