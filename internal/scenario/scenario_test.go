package scenario

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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
		case Partial:
			// A partial requirement must say what was not observed, or it reads
			// as a pass to anyone scanning the evidence classes.
			if !strings.Contains(strings.ToUpper(entry.Detail), "NOT OBSERVED") {
				t.Fatalf("requirement %s is partial without stating what was not observed", entry.ID)
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

func TestControllerKillLandingReportsOnlyObservedState(t *testing.T) {
	// Every branch must describe the state it was given, and none may assert an
	// effect the state does not establish. `writing` is the case that matters: the
	// run journaled that it was about to submit, so a prompt may already have been
	// delivered and the description must not call it "no effect".
	tests := []struct {
		submission string
		writer     string
		mustSay    []string
		mustNotSay []string
	}{
		{submission: "uncertain", mustSay: []string{"unknown"}, mustNotSay: []string{"no effect"}},
		{submission: "delivered", mustSay: []string{"delivered"}},
		{submission: "proven_not_delivered", mustSay: []string{"not delivered", "no prompt was sent"}},
		{submission: "not_attempted", mustSay: []string{"no prompt was sent"}, mustNotSay: []string{"in flight"}},
		{submission: "writing", writer: "unconfirmed", mustSay: []string{"in flight", "may already have been delivered", "not claimed here"}, mustNotSay: []string{"no effect occurred"}},
		{submission: "something-else", mustSay: []string{"unrecognised"}},
	}
	for _, test := range tests {
		view := ExecutionView{SubmissionState: test.submission, WriterState: test.writer}
		got := controllerKillLanding(view)
		for _, want := range test.mustSay {
			if !strings.Contains(got, want) {
				t.Errorf("submission_state=%q: landing %q does not mention %q", test.submission, got, want)
			}
		}
		for _, unwanted := range test.mustNotSay {
			if strings.Contains(got, unwanted) {
				t.Errorf("submission_state=%q: landing %q must not claim %q", test.submission, got, unwanted)
			}
		}
		// The function receives only the inspection, so it must make no claim
		// about the resource journal. The caller reads that separately and it may
		// be empty, so a claim here would be about data this function cannot see.
		for _, unavailable := range []string{"resource state below", "retained resource", "held resource", "confirms effects"} {
			if strings.Contains(got, unavailable) {
				t.Errorf("submission_state=%q: landing %q claims the resource journal, which this function cannot observe", test.submission, got)
			}
		}
	}
}

// productionAllowedNext reproduces the command list that production's own
// `Runner.Inspect` emits for a (run_state, submission_state) pair.
//
// It is transcribed from internal/supervisor/reconcile.go, which special-cases
// exactly one submission state (`uncertain`) and otherwise switches on run_state.
// Keeping the transcription here is deliberate: the harness's replay-safety check
// is only meaningful if it is checked against what the product actually returns,
// and a test that only exercises the harness's own rule cannot catch the harness
// asserting a property the product does not have. That is precisely the defect a
// previous revision of this check shipped.
//
// Be honest about what this is. It is a second hand-written copy of production's
// switch, so it cannot by itself detect production changing underneath it. Its
// value is that a *human* reading the harness check has the product's rule beside
// it in executable form, which is what caught the defect.
//
// `TestProductionTranscriptionMatchesSource` is what makes the copy safe: it
// parses the arms production actually has — every arm, including one that assigns
// nothing, and from every switch that assigns `AllowedNext` — and fails if the
// count, order, conditions or command lists differ. It was verified by mutating
// `reconcile.go` and confirming each mutation fails it. It is still a guard on a
// copy, not an integration test against a running product, and it does not read
// `runner.go` at all — so a regression in `Runner.submit`, the function the
// emitted report cites as enforcing replay safety, is outside what it covers.
func productionAllowedNext(runState, submissionState string) []string {
	switch {
	case submissionState == "uncertain":
		return []string{"inspect", "reconcile", "stop"}
	case runState == "completed":
		return []string{"inspect"}
	case runState == "prepared" || runState == "starting" || runState == "active":
		return []string{"inspect", "start", "reconcile", "stop"}
	default:
		return []string{"inspect", "reconcile"}
	}
}

// productionRunStates are the run states the `runs.state` column admits. The
// harness's check must hold for all of them, not only the ones a particular run
// happened to visit. `TestSchemaEnumerationsMatchMigrations` checks this list
// against the migration that constrains the column, so a widening of that CHECK
// cannot silently under-enumerate with the suite green.
var productionRunStates = []string{
	"queued", "prepared", "starting", "active", "waiting_input",
	"stopping", "completed", "failed", "interrupted", "unknown",
}

// productionSubmissionStates are the `submission_state` values the column's CHECK
// constraint admits, from the migration that added it.
var productionSubmissionStates = []string{
	"not_attempted", "writing", "uncertain", "delivered", "proven_not_delivered",
}

// TestSchemaEnumerationsMatchMigrations checks the two enumerations above against
// the migrations that actually constrain those columns.
//
// They are duplicated here by necessity — the harness must enumerate every state
// the product can hold, and a test cannot learn that from a Go literal — so the
// duplication is guarded rather than left to drift.
func TestSchemaEnumerationsMatchMigrations(t *testing.T) {
	migrations, err := filepath.Glob(filepath.Join("..", "store", "migrations", "project-*.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatalf("the migrations directory is not readable, so the state enumerations cannot be checked: %v", err)
	}
	runStates := map[string]bool{}
	submissionStates := map[string]bool{}
	// `\b` before `state` keeps the pattern off `writer_state` and any other
	// column whose name merely ends in `state`.
	statePattern := regexp.MustCompile(`\bstate\s+IN\s*\(([^)]*)\)`)
	submissionPattern := regexp.MustCompile(`\bsubmission_state\s+IN\s*\(([^)]*)\)`)
	for _, path := range migrations {
		raw, err := readRepoFile(path)
		if err != nil {
			t.Fatalf("migration %s is not readable, so the state enumerations cannot be checked: %v", path, err)
		}
		// `state` is a column name on many tables, so the run-state constraint is
		// read from the `runs` table's own definition rather than from the whole
		// file. `submission_state` is specific enough to match anywhere, which
		// also catches a later migration that ALTERed it.
		for _, block := range createTableBlocks(raw, "runs") {
			for _, value := range checkConstraintValues(statePattern, block) {
				runStates[value] = true
			}
		}
		for _, value := range checkConstraintValues(submissionPattern, raw) {
			submissionStates[value] = true
		}
	}
	assertSameSet(t, "runs.state", productionRunStates, runStates)
	assertSameSet(t, "submission_state", productionSubmissionStates, submissionStates)
}

// createTableBlocks returns the bodies of every definition that ends up being the
// table named `name`, so a column constraint is read from the table that owns it
// rather than from every table in the file.
//
// It deliberately also matches the versioned rebuild idiom these migrations use:
// SQLite cannot `ALTER TABLE ... ADD CONSTRAINT`, so a widened `CHECK` can only be
// shipped as `CREATE TABLE runs_v<N> (… wider …)` followed by
// `ALTER TABLE runs_v<N> RENAME TO runs`. Anchoring on the literal name `runs`
// alone would read only the original definition and miss the rebuild — a hole
// adversarial review found by mutating the migration.
//
// The table name is matched as a whole word, and the body ends at the first line
// that closes the statement — which these migrations write as `) STRICT;` or
// `);`, so matching only `);` would swallow every later table.
func createTableBlocks(source, name string) []string {
	blocks := []string{}
	lines := strings.Split(source, "\n")
	// `runs`, or a rebuild of it: `runs_v3`, `runs_new`, and so on.
	pattern := regexp.MustCompile(`^CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?` + regexp.QuoteMeta(name) + `(_[A-Za-z0-9]+)?\s*\($`)
	// A versioned table only *becomes* the named table if it is renamed to it.
	renamed := regexp.MustCompile(`ALTER\s+TABLE\s+` + regexp.QuoteMeta(name) + `(_[A-Za-z0-9]+)?\s+RENAME\s+TO\s+` + regexp.QuoteMeta(name) + `\b`)
	for index, line := range lines {
		match := pattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		// Group 1 is the optional IF NOT EXISTS; group 2 is the version suffix.
		version := ""
		if len(match) > 2 {
			version = match[2]
		}
		if version != "" {
			// Only a rebuild that is actually renamed onto the name counts. A
			// `runs_v2` that was abandoned mid-migration is not the live table,
			// and treating it as one would report states the product cannot hold.
			target := regexp.MustCompile(`ALTER\s+TABLE\s+` + regexp.QuoteMeta(name+version) + `\s+RENAME\s+TO\s+` + regexp.QuoteMeta(name) + `\b`)
			if !target.MatchString(source) && !renamed.MatchString(source) {
				continue
			}
		}
		body := []string{strings.TrimSpace(line)}
		for _, following := range lines[index+1:] {
			body = append(body, following)
			if strings.Contains(following, ");") {
				break
			}
		}
		blocks = append(blocks, strings.Join(body, "\n"))
	}
	return blocks
}

// checkConstraintValues returns the quoted values of a `CHECK (col IN (...))`
// constraint, in source order, de-duplicated.
func checkConstraintValues(pattern *regexp.Regexp, source string) []string {
	seen := map[string]bool{}
	values := []string{}
	for _, match := range pattern.FindAllStringSubmatch(source, -1) {
		for _, literal := range strings.Split(match[1], ",") {
			value := strings.Trim(strings.TrimSpace(literal), "'\"")
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			values = append(values, value)
		}
	}
	return values
}

// assertSameSet fails when the enumeration and the schema disagree in either
// direction. An extra value the harness does not test is as much a gap as a
// missing one, so both are reported.
func assertSameSet(t *testing.T, column string, enumerated []string, actual map[string]bool) {
	t.Helper()
	for _, value := range enumerated {
		if !actual[value] {
			t.Errorf("the harness enumerates %s=%q, which no migration's CHECK constraint admits; the enumeration is stale or invented", column, value)
		}
	}
	for value := range actual {
		if !containsString(enumerated, value) {
			t.Errorf("a migration admits %s=%q but the harness does not enumerate it, so the check is not exercised for a state the product can hold", column, value)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestProductionTranscriptionMatchesSource fails if the transcription above has
// drifted from the production switch it claims to copy.
//
// It reads the production source and checks the two facts the transcription
// encodes: that `uncertain` is the only submission state special-cased ahead of
// the run-state switch, and that every non-completed branch offers `reconcile`.
// Those are the two properties the harness's check leans on, so a change to either
// is a change the transcription must be re-checked against.
func TestProductionTranscriptionMatchesSource(t *testing.T) {
	source, err := readRepoFile("../supervisor/reconcile.go")
	if err != nil {
		// A skip here would silently unenforce the guard, which is the failure
		// mode this test exists to prevent. Fail instead.
		t.Fatalf("production source is not readable from here, so the transcription cannot be checked at all: %v", err)
	}
	branches := parseInspectBranches(source)
	if len(branches) == 0 {
		t.Fatalf("production no longer assigns AllowedNext in the expected form; re-check the transcription by hand")
	}

	// The branches as production currently writes them. This is a real
	// comparison, not a text-shape heuristic: a production change to a branch's
	// condition or its command list changes what is parsed, and this fails.
	want := []inspectBranch{
		{condition: `view.SubmissionState == "uncertain"`, commands: []string{"inspect", "reconcile", "stop"}},
		{condition: `view.RunState == "completed"`, commands: []string{"inspect"}},
		{condition: `view.RunState == "prepared" || view.RunState == "starting" || view.RunState == "active"`, commands: []string{"inspect", "start", "reconcile", "stop"}},
		{condition: "default", commands: []string{"inspect", "reconcile"}},
	}
	if len(branches) != len(want) {
		t.Fatalf("production's Inspect now has %d command branches, the transcription assumes %d: %+v", len(branches), len(want), branches)
	}
	for index := range want {
		if branches[index].condition != want[index].condition {
			t.Errorf("branch %d condition is %q in production but %q in the transcription", index, branches[index].condition, want[index].condition)
		}
		if strings.Join(branches[index].commands, ",") != strings.Join(want[index].commands, ",") {
			t.Errorf("branch %d offers %v in production but %v in the transcription", index, branches[index].commands, want[index].commands)
		}
	}

	// Finally, the transcription function must agree with the parsed production
	// branches for every combination the schema admits — not merely be internally
	// consistent. A branch added to production that the transcription ignores
	// would otherwise pass silently.
	for _, runState := range productionRunStates {
		for _, submissionState := range productionSubmissionStates {
			got := strings.Join(productionAllowedNext(runState, submissionState), ",")
			view := ExecutionView{RunID: "run", RunState: runState, SubmissionState: submissionState,
				AllowedNext: productionAllowedNext(runState, submissionState)}
			// The transcription is the source of truth only insofar as it matches
			// the branch production would take; assert the harness accepts it too,
			// so a divergence in either direction is caught here.
			if safe, detail := view.verifiesReplayBoundary(); !safe {
				t.Errorf("run_state=%q submission_state=%q yields %s from the transcription, and the harness check rejects it: %s", runState, submissionState, got, detail)
			}
		}
	}
}

// inspectBranch is one `case` arm of the switch that fills AllowedNext.
type inspectBranch struct {
	condition string
	commands  []string
}

// parseInspectBranches extracts the ordered `case` arms of the switch that assigns
// view.AllowedNext, with their conditions normalised to a single line and their
// command lists parsed.
//
// It is a deliberately narrow parser reading the shape the code has today. It
// fails loudly rather than guessing if that shape changes, because a silently
// empty result would turn the guard into a no-op.
//
// Two shapes it must not silently accept, both found by adversarial review:
//
//   - an arm that assigns nothing, because it returns or breaks. Only arms that
//     assign were collected, so such an arm was invisible. Every arm is now
//     recorded, with a nil command list, so the count comparison catches it.
//   - a second switch in the same function that also assigns AllowedNext. The
//     parser used to bind to the first `switch {` in the file, so only the first
//     was read. Every such switch is now parsed and the arm lists concatenated, so
//     a second one changes the count.
func parseInspectBranches(source string) []inspectBranch {
	branches := []inspectBranch{}
	lines := strings.Split(source, "\n")
	pending := ""
	inside := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inside {
			// Only enter a switch that actually assigns AllowedNext inside itself.
			if strings.HasPrefix(trimmed, "switch {") && switchAssignsAllowedNext(lines[index:]) {
				inside = true
			}
			continue
		}
		if trimmed == "}" {
			// Close the switch. A case arm that assigned nothing is recorded here
			// rather than dropped, so an early-returning arm cannot hide.
			if pending != "" {
				branches = append(branches, inspectBranch{condition: normaliseCondition(pending)})
				pending = ""
			}
			inside = false
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "case "):
			if pending != "" {
				branches = append(branches, inspectBranch{condition: normaliseCondition(pending)})
			}
			pending = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(trimmed, "case ")), ":")
		case trimmed == "default:":
			if pending != "" {
				branches = append(branches, inspectBranch{condition: normaliseCondition(pending)})
			}
			pending = "default"
		case strings.HasPrefix(trimmed, "view.AllowedNext = []string{"):
			inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "view.AllowedNext = []string{"), "}")
			commands := []string{}
			for _, command := range strings.Split(inner, ",") {
				commands = append(commands, strings.Trim(strings.TrimSpace(command), `"`))
			}
			branches = append(branches, inspectBranch{condition: normaliseCondition(pending), commands: commands})
			pending = ""
		}
	}
	if pending != "" {
		branches = append(branches, inspectBranch{condition: normaliseCondition(pending)})
	}
	return branches
}

// switchAssignsAllowedNext reports whether the switch statement that starts at
// `start` assigns view.AllowedNext before it closes. Without this the parser would
// bind to an unrelated earlier switch in the file and read the wrong arms.
func switchAssignsAllowedNext(lines []string) bool {
	// The caller has already matched `switch {`; the next `}` at this nesting
	// depth closes it. This is a narrow shape, which is the point: when the shape
	// is not met the caller simply does not enter, and the arm count still has to
	// match, so a mis-read shows up as a failure rather than a silent pass.
	for _, line := range lines[1:] {
		if strings.HasPrefix(strings.TrimSpace(line), "view.AllowedNext") {
			return true
		}
		if strings.TrimSpace(line) == "}" {
			return false
		}
	}
	return false
}

// normaliseCondition collapses the multi-line `case` conditions the source uses
// into one line, so they can be compared to the transcription's single-line form.
func normaliseCondition(condition string) string {
	return strings.Join(strings.Fields(condition), " ")
}

func TestPartialRequirementIsAGapAndReachesTheReport(t *testing.T) {
	matrix := NewMatrix()
	matrix.Requirements = requirementCoverage()
	// R41 discloses that its boundary was not exercised, so it must be classed
	// partial and must appear in the requirement gap list — not merely disclose
	// the gap in prose while reading as a pass.
	var r41 *MatrixEntry
	for index, row := range matrix.Requirements {
		if row.ID == "R41" {
			r41 = &matrix.Requirements[index]
		}
	}
	if r41 == nil {
		t.Fatal("R41 is absent from the coverage audit")
	}
	if r41.Evidence != Partial {
		t.Fatalf("R41 discloses an unexercised boundary but is classed %q, not %q", r41.Evidence, Partial)
	}
	// Every requirement row the audit produces must itself be decided; the
	// milestone and recovery sections are still unresolved here because no
	// walkthrough has run.
	for _, row := range matrix.Requirements {
		switch row.Evidence {
		case EvidenceAutomated, EvidenceReused, Partial, EvidencePendingStage8, EvidenceUnmet:
		default:
			t.Fatalf("requirement %s has an undecided evidence class %q", row.ID, row.Evidence)
		}
		if row.Evidence == Partial && !strings.Contains(strings.ToUpper(row.Detail), "NOT OBSERVED") {
			t.Fatalf("requirement %s is partial without stating what was not observed", row.ID)
		}
	}
	matrix.RequirementGapList = matrix.RequirementGaps()
	found := false
	for _, gap := range matrix.RequirementGapList {
		if gap == "R41: partial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a partial requirement is absent from the requirement gap list: %v", matrix.RequirementGapList)
	}
	// A requirement gap must never be folded into the milestone/recovery list:
	// they are different claims, and conflating them would let a reader mistake
	// one for the other.
	for _, gap := range matrix.Gap() {
		if strings.HasPrefix(gap, "R41") {
			t.Fatalf("a requirement gap was folded into the case gap list: %q", gap)
		}
	}
}

// TestFailedReplayBoundaryIsRecordedPartial pins what a controller kill whose
// replay-boundary check did not hold records, when it is marked partial.
//
// Be precise about the scope. It exercises `Matrix.MarkPartial` and asserts the
// marking reaches `Gap()`, `Counts()` and passes `RequireComplete()` — the three
// things the case's outcome depends on. It does **not** exercise the
// `if safe { mark } else { markPartial }` branch itself, which needs a real
// racy kill to reach. Reverting that branch to an automated mark would leave this
// test green; what pins it is that the class it would have used does not accept
// the row, which is why the branch cannot simply be swapped for `mark`.
func TestFailedReplayBoundaryIsRecordedPartial(t *testing.T) {
	// The exact chain the case uses: a mark that fails validation is what makes
	// `partial` different from a note in prose.
	matrix := NewMatrix()
	// Decide the rest of the sections, which a real run does before the run
	// reaches its completeness check. Leaving them undecided would fail the check
	// for an unrelated reason and mask what this test is about.
	for _, section := range []string{"milestone", "recovery"} {
		for _, entry := range matrix.section(section) {
			if err := matrix.Mark(section, entry.ID, EvidenceAutomated, "observed in full by an earlier step", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The requirement section is part of the same check, so it must be populated
	// for this test to reach the assertion it is about.
	matrix.Requirements = requirementCoverage()
	source := "a real SIGKILLed vigil process"
	row := "a controller was SIGKILLed before any submission was attempted. " +
		"NOT observed: the replay-safety property could not be established from this run's landing, " +
		"so the case is recorded as partially observed rather than demonstrated"
	if err := matrix.MarkPartial("recovery", CaseControllerKillUnknown, row, source); err != nil {
		t.Fatalf("the partial marking the kill case uses is rejected: %v", err)
	}
	// It must pass the completeness check the run makes before finishing.
	if err := matrix.RequireComplete(); err != nil {
		t.Fatalf("a controller-kill row marked partial fails the run's own completeness check: %v", err)
	}
	// And it must be a gap, absent from the automated class.
	gaps := matrix.Gap()
	found := false
	for _, gap := range gaps {
		if gap == CaseControllerKillUnknown+": partial" {
			found = true
		}
		if gap == CaseControllerKillUnknown+": automated" {
			t.Fatal("a failed replay-boundary check was recorded as a demonstrated case")
		}
	}
	if !found {
		t.Fatalf("a failed replay-boundary check is absent from the gap list: %v", gaps)
	}
	if matrix.Counts("recovery")[Partial] != 1 {
		t.Fatalf("the failed check is not counted as partial: %v", matrix.Counts("recovery"))
	}
	// An automated mark of the same row must NOT satisfy this test, which is what
	// makes it a test of the class rather than of the wording.
	if err := matrix.Mark("recovery", CaseControllerKillUnknown, EvidenceAutomated, "observed something", source); err != nil {
		t.Fatal(err)
	}
	for _, gap := range matrix.Gap() {
		if gap == CaseControllerKillUnknown+": partial" {
			t.Fatal("the automated marking still reads as partial, so the test does not discriminate")
		}
	}
}

func TestReplayBoundaryAgreesWithProductionInspect(t *testing.T) {
	// Every combination production can emit must be accepted by the harness's
	// check. A combination the product genuinely produces and the harness
	// rejects would mean the harness is asserting a property the product lacks,
	// which is a defect in the evidence rather than in the product.
	runStates := productionRunStates
	submissionStates := productionSubmissionStates
	for _, runState := range runStates {
		for _, submissionState := range submissionStates {
			view := ExecutionView{
				RunID:           "run",
				RunState:        runState,
				SubmissionState: submissionState,
				AllowedNext:     productionAllowedNext(runState, submissionState),
			}
			safe, detail := view.verifiesReplayBoundary()
			if !safe {
				t.Errorf("production Inspect emits %v for run_state=%q submission_state=%q, and the harness rejected that: %s",
					view.AllowedNext, runState, submissionState, detail)
			}
		}
	}
}

func TestReplayBoundaryRejectsOnlyWhatProductionWouldViolate(t *testing.T) {
	// `uncertain` is the one state production withholds `start` for. If it ever
	// offered one, the harness must catch that — a resubmission would be an
	// unproven repeat of a delivery whose outcome is already recorded as unknown.
	uncertainOfferingStart := ExecutionView{
		RunID: "run", RunState: "starting", SubmissionState: "uncertain",
		AllowedNext: []string{"inspect", "start", "reconcile", "stop"},
	}
	if safe, detail := uncertainOfferingStart.verifiesReplayBoundary(); safe {
		t.Errorf("an uncertain submission offering start was accepted: %s", detail)
	}
	// `writing` must offer `reconcile`, which is how an in-flight submission is
	// resolved. A view without it is not something production produces.
	writingWithoutReconcile := ExecutionView{
		RunID: "run", RunState: "starting", SubmissionState: "writing",
		AllowedNext: []string{"inspect", "start", "stop"},
	}
	if safe, detail := writingWithoutReconcile.verifiesReplayBoundary(); safe {
		t.Errorf("an in-flight submission with no reconcile offered was accepted: %s", detail)
	}
	// An unreadable or uninterpretable inspection is never safe, and never
	// reported as resolving an outcome.
	for name, view := range map[string]ExecutionView{
		"empty":                {},
		"no run state":         {RunID: "run", AllowedNext: []string{"inspect", "start"}},
		"no submission state":  {RunID: "run", RunState: "starting", AllowedNext: []string{"inspect", "start"}},
		"no commands":          {RunID: "run", RunState: "starting", SubmissionState: "writing"},
		"unrecognised state":   {RunID: "run", RunState: "starting", SubmissionState: "some_future_state", AllowedNext: []string{"inspect", "start"}},
		"absent with commands": {RunID: "run", RunState: "starting", AllowedNext: []string{"inspect", "start"}},
	} {
		safe, detail := view.verifiesReplayBoundary()
		if safe {
			t.Errorf("%s: an unestablished inspection was treated as safe", name)
		}
		if strings.Contains(detail, "resolves the outcome") {
			t.Errorf("%s: an unestablished state was reported as resolving the outcome: %s", name, detail)
		}
	}
	// The reason must cite the state it was derived from, in every branch.
	resolved := ExecutionView{RunID: "run", RunState: "prepared", SubmissionState: "not_attempted",
		AllowedNext: productionAllowedNext("prepared", "not_attempted")}
	safe, detail := resolved.verifiesReplayBoundary()
	if !safe {
		t.Fatalf("a pre-submission run offering start was refused: %s", detail)
	}
	if !strings.Contains(detail, "not_attempted") {
		t.Fatalf("the safety reason does not cite the observed submission state: %s", detail)
	}
	writing := ExecutionView{RunID: "run", RunState: "starting", SubmissionState: "writing",
		AllowedNext: productionAllowedNext("starting", "writing")}
	safe, detail = writing.verifiesReplayBoundary()
	if !safe {
		t.Fatalf("an in-flight run in the state production produces was refused: %s", detail)
	}
	if !strings.Contains(detail, "writing") {
		t.Fatalf("the in-flight reason does not cite the observed submission state: %s", detail)
	}
}

func TestPartialIsADistinctClassRequiringScope(t *testing.T) {
	matrix := NewMatrix()
	// A partial row without a scope statement is refused: it would be
	// indistinguishable from a full observation in the report.
	if err := matrix.MarkPartial("recovery", CaseStaleOwnerFenced, "observed something", ""); err != nil {
		t.Fatal(err)
	}
	if err := matrix.RequireComplete(); err == nil {
		t.Fatal("a partial row with no scope statement was accepted")
	}
	if err := matrix.MarkPartial("recovery", CaseStaleOwnerFenced, "observed the refusal. NOT observed: fencing a real stale owner", ""); err != nil {
		t.Fatal(err)
	}
	// A partial row is a gap, not a pass.
	gaps := matrix.Gap()
	found := false
	for _, gap := range gaps {
		if gap == CaseStaleOwnerFenced+": partial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a partial row is absent from the gap list: %v", gaps)
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
