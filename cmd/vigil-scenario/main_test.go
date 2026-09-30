package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"vigil/internal/scenario"
)

var countLinePattern = regexp.MustCompile(`^ {2}(\S+)\s+total=(\d+) (.*)$`)

// TestCountLineReconcilesEveryClass pins the property the report's central claim
// depends on: the printed per-class numbers must add up to the printed total.
//
// A previous revision added the `partial` class to the matrix and left it out of
// the rendered line, so the summary printed 16 of 20 recovery rows with the four
// partial rows absent and the suite stayed green. This test fails on that exact
// defect, and on any future class that is counted but not printed.
func TestCountLineReconcilesEveryClass(t *testing.T) {
	// One row in every class the matrix can hold, for a section that holds all of
	// them at once.
	counts := map[string]int{
		scenario.EvidenceAutomated:     5,
		scenario.Partial:               4,
		scenario.EvidenceReused:        2,
		scenario.EvidencePendingStage8: 3,
		scenario.EvidenceUnmet:         1,
	}
	line := countLine("recovery", counts)

	match := countLinePattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatalf("the counts line is not parseable, so nothing can pin it: %q", line)
	}
	total, err := strconv.Atoi(match[2])
	if err != nil {
		t.Fatalf("the printed total is not a number: %q", line)
	}
	summed := 0
	for _, value := range counts {
		summed += value
	}
	if total != summed {
		t.Fatalf("the printed total %d does not equal the rows counted %d: %q", total, summed, line)
	}
	// Every counted class must also be printed by name, or the line is
	// unreconcilable by a reader.
	for entry := range counts {
		label := scenario.EvidenceAutomated
		switch entry {
		case scenario.Partial:
			label = "partial"
		case scenario.EvidenceReused:
			label = "reused"
		case scenario.EvidencePendingStage8:
			label = "pending"
		case scenario.EvidenceUnmet:
			label = "unmet"
		}
		if !strings.Contains(line, label+"="+strconv.Itoa(counts[entry])) {
			t.Errorf("class %q is counted but not printed on the line: %q", entry, line)
		}
	}
	if strings.Contains(line, "UNPRINTED") {
		t.Fatalf("a class is counted but not printed, and the line says so: %q", line)
	}
}

// TestCountLineNamesAnUnprintedClass checks the fail-loudly path: a class the
// matrix holds that the renderer does not know about must produce a visible
// contradiction rather than a summary that quietly does not add up.
func TestCountLineNamesAnUnprintedClass(t *testing.T) {
	line := countLine("recovery", map[string]int{
		scenario.EvidenceAutomated: 1,
		"a_class_added_later":      7,
	})
	if !strings.Contains(line, "UNPRINTED=7") {
		t.Fatalf("an unprinted class was silently dropped from the line: %q", line)
	}
}

// TestSummaryLinesShowsEveryGap checks that nothing the report holds as a gap is
// omitted from the rendered summary, across all four of the report's own gap-like
// lists.
func TestSummaryLinesShowsEveryGap(t *testing.T) {
	report := &scenario.Report{
		ScenarioID: "stage-5.7-test",
		Stage:      "5.7",
		Aborted:    true,
		Binary:     scenario.BinaryIdentity{Path: "/tmp/vigil", SHA256: strings.Repeat("a", 64)},
		Matrix:     &scenario.Matrix{},
		Gaps:       []string{"a-case: partial"},
		Pending:    []string{"a-milestone-step: pending_stage_8"},
		Limits:     []string{"a recorded limitation"},
	}
	report.Matrix.RequirementGapList = []string{"R41: partial"}
	report.Matrix.Recovery = []scenario.MatrixEntry{{ID: "a-case", Evidence: scenario.Partial}}

	rendered := summaryLines(report, "/tmp/report.json", time.Second)
	for _, want := range []string{
		"a-case: partial",
		"a-milestone-step: pending_stage_8",
		"a recorded limitation",
		"R41: partial",
		"ABORTED",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the summary omits %q:\n%s", want, rendered)
		}
	}
	// A requirement gap must be labelled distinctly, so it is not read as a
	// recovery case that failed.
	if !strings.Contains(rendered, "req gap      R41: partial") {
		t.Errorf("a requirement gap is not labelled as one:\n%s", rendered)
	}
	// Every section line must be present, so a section cannot silently vanish.
	for _, section := range []string{"milestone", "recovery", "requirements"} {
		if !strings.Contains(rendered, section) {
			t.Errorf("the summary omits the %s section line:\n%s", section, rendered)
		}
	}
}
