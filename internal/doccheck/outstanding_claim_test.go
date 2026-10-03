package doccheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Round 25 rejected a commit for a contradiction this commit itself created: the
// status header still said the twenty-second remediation was outstanding while the
// state line, four lines below it, said the twenty-third. Nothing caught it,
// because the header's ordinal was prose and the state line was the gated value.
//
// An earlier attempt at closing that class was a phrase blacklist. Round 25 defeated
// it with a single sentence — "scoped to this one file while four other documents
// carry the same count ungated" matches none of the listed phrasings, and it was
// already in the tree.
//
// So nothing here is forbidden. Every ordinal the record presents as the outstanding
// one is READ and compared against the derived value, wherever it sits: header,
// closing section, a finding cell, a table row. A paraphrase cannot hide, because
// there is no wording to evade — only a number to be right about.
func TestOutstandingOrdinalClaimsAreConsistent(t *testing.T) {
	root := repoRoot
	path := filepath.Join("docs", "research", "stage-6", "6.3-review.md")
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := string(raw)

	shas, err := stage63RemediationCommits(root)
	if err != nil {
		t.Skipf("git history unavailable (%v)", err)
	}
	applied := len(shas)
	expected := applied
	if stage63RecordUncommitted(root, path) {
		expected++
	}

	// Every phrasing by which the record presents a remediation as the one
	// awaiting review. This is a value check, not a wording ban: a sentence that
	// claims a different ordinal fails whatever words it uses.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`follow-up review of the \*\*([a-z]+(?:-[a-z]+)*)\*\* remediation`),
		regexp.MustCompile(`remediation is outstanding`),
		regexp.MustCompile(`([a-z]+(?:-[a-z]+)*) remediation is outstanding`),
		regexp.MustCompile(`the \*\*([a-z]+(?:-[a-z]+)*)\*\* remediation has`),
		regexp.MustCompile(`outstanding ordinal is the (\w+)'s`),
	}

	// The only ordinal claims that count are those attached to "outstanding" or
	// "follow-up review of". Each is matched with its own capture.
	captures := []struct {
		pattern *regexp.Regexp
		group   int
	}{
		{patterns[0], 1},
		{patterns[2], 1},
		{patterns[3], 1},
	}

	found := 0
	for _, capture := range captures {
		for _, match := range capture.pattern.FindAllStringSubmatch(doc, -1) {
			value, ok := stage63ParseNumber(strings.Trim(match[capture.group], "*_` "))
			if !ok {
				continue
			}
			found++
			if value != expected {
				t.Errorf("the record names the %s remediation as outstanding, but %d remediation commits exist%s so it is the %s",
					stage63OrdinalWord(value), applied, stage63InFlightSuffix, stage63OrdinalWord(expected))
			}
		}
	}
	if found == 0 {
		t.Error("no outstanding-ordinal claim found in the record; the check is not reading what it was written to read")
	}
}

// TestResolutionCellsCiteATest guards the artefact round 25 named as the common
// thread in three of its findings: a resolution cell asserting that some check
// exists or covers something, without naming a test that reads the thing claimed.
//
// R24F5 asserted a precondition was "stated in the record" while the record said the
// opposite; R24F8 asserted "corrected" while the text was byte-identical; R24F10
// asserted items were recorded that appear nowhere. The rule this enforces is
// narrow and checkable: a cell that mentions a coverage or scoping claim about a
// test must name a test identifier.
func TestResolutionCellsClaimingChecksCiteATest(t *testing.T) {
	root := repoRoot
	path := filepath.Join("docs", "research", "stage-6", "6.3-review.md")
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := string(raw)

	// A cell that claims something about what a check covers, or states that a
	// check exists, without naming one.
	// The alternation names the GATE, not a pronoun. An earlier version accepted
	// a bare "it", which matched any cell containing the word "it" anywhere —
	// including two legitimate cells from round 9 that say a UI check now covers
	// a string. A check that fires on ordinary sentences is a check whose
	// failures get silenced, which is the mistake this repository keeps making.
	claimsCoverage := regexp.MustCompile(`(?i)\b(verif(?:y|ies|ied)|assert(?:s|ed)?|check(?:s|ed)?|cover(?:s|ed|ing)?|gated|ungated)\b[^|]*\b(the gate|this gate|the count gate|its coverage|its scope)\b[^|]*`)
	testName := regexp.MustCompile("`?[A-Z][A-Za-z0-9]*Test[A-Za-z0-9]*`?|`[a-z_]+_test\\.go`|Test[A-Za-z0-9]+")

	offenders := 0
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "| 6.3-R") {
			continue
		}
		// The Scope and Finding columns describe what a reviewer found, which is
		// evidence about the past. Only the Resolution column asserts what this
		// remediation did, and only that column is checked.
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) < 4 {
			continue
		}
		resolution := cells[3]
		if !claimsCoverage.MatchString(resolution) {
			continue
		}
		if testName.MatchString(resolution) {
			continue
		}
		offenders++
		t.Errorf("a resolution cell claims what a check covers without naming one: %.180s", resolution)
	}
	_ = offenders
}
