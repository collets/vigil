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
	// EVERY count-carrying document, not just this one. Round 26 rejected the
	// single-file version on exactly this ground - "a check scoped to one file
	// cannot close a class that lives in five" - and demonstrated it by putting a
	// stale ordinal in next-steps.md, where the one-file check could not see it.
	var docs []string
	for _, doc := range stage63CountCarryingDocuments {
		docs = append(docs, doc)
	}
	shas, err := stage63RemediationCommits(root)
	if err != nil {
		t.Skipf("git history unavailable (%v)", err)
	}
	applied := len(shas)
	// stage63Expected, not an inline increment, so the in-flight suffix is set and
	// the message can say that a remediation is uncommitted rather than asserting a
	// count the history does not hold.
	expected := stage63Expected(root, docs[0], applied)

	// Every phrasing by which the record presents a remediation as the one
	// awaiting review. This is a value check, not a wording ban: a sentence that
	// claims a different ordinal fails whatever words it uses.
	// Three shapes, and only three. Round 26 defeated an earlier four-shape
	// version with "The outstanding review is of the twenty-second remediation" and
	// with "Remediation 22 is the one still awaiting its independent review", both of
	// which pass. This is a short list of spellings, not a value check over the
	// whole field, and the record says so rather than claiming otherwise. The
	// restatements it guards were deleted rather than trusted to it.
	shapes := []*regexp.Regexp{
		regexp.MustCompile(`review of [^|]{0,40}?([a-z]+(?:-[a-z]+)*) remediation`),
		regexp.MustCompile(`([a-z]+(?:-[a-z]+)*) remediation is outstanding`),
		regexp.MustCompile(`the \*\*([a-z]+(?:-[a-z]+)*)\*\* remediation has`),
	}

	found := 0
	for _, relative := range docs {
		path := filepath.Join(root, relative)
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Skipf("read %s: %v", relative, readErr)
		}
		body := string(raw)
		for _, pattern := range shapes {
			for _, match := range pattern.FindAllStringSubmatch(body, -1) {
				value, ok := stage63ParseNumber(strings.Trim(match[1], "*_`'"))
				if !ok {
					continue
				}
				found++
				if value != expected {
					t.Errorf("%s names the %s remediation as outstanding, but %d remediation commits exist%s so it is the %s",
						relative, stage63OrdinalWord(value), applied, stage63InFlightSuffix, stage63OrdinalWord(expected))
				}
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
		t.Errorf("a resolution cell claims what a check covers without naming one: %.180s", resolution)
	}
}
