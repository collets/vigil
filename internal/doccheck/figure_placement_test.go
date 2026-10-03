package doccheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Nine rounds of this stage failed the same way: a figure restated in prose. Round
// 27 named why the previous attempts at policing it lost — a blacklist of six
// wordings was defeated by a paraphrase, and a three-spelling value check over five
// whole documents could not tell a live claim from a historical one, and was
// defeated in turn.
//
// So this is deliberately NOT a general rule about locations. It is a narrow check
// over the five count-carrying documents for the three shapes of LIVE claim that
// actually caused a P1, and it says plainly that it is narrow. A broad
// location rule was written and deleted: it fired on eleven sentences of
// explanatory prose — "one a review found", "the four consecutive rounds before
// it", a quoted historical example — and a check that fires on correct text gets
// silenced, which is the mistake this repository keeps making.
//
// What actually closes the class is the deletion of the restatements, done in the
// same change. This check exists to catch the next author reintroducing one, and
// it is honest about the fact that a sufficiently different phrasing would evade it.
func TestNoLiveRestatementOfTheStage63Figures(t *testing.T) {
	root := repoRoot

	// A POINTER to the state line is the required replacement, not a restatement,
	// so it is excluded before matching — otherwise the check fails on the very
	// sentences that fix the defect it exists to prevent.
	// The pointer phrase wraps across lines in these documents, so it is matched
	// on its head ("named in the") as well as its tail.
	pointer := regexp.MustCompile(`(?i)named in the|state line's|stated (?:above|below)|\bstate line\b`)

	// Inline code spans are quoted examples, not claims: the record quotes
	// "ten remediations applied" while explaining the gate that checks it.
	inlineCode := regexp.MustCompile("`[^`]*`")

	// Each shape is a live claim: a count of remediations applied, a review or
	// remediation named as the next one, or a review whose return acceptance rests
	// on. Historical statements ("the four rounds before it") do not match.
	live := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b[a-z]+(?:-[a-z]+)* remediations (?:have been |were )?applied`),
		regexp.MustCompile(`(?i)review of [^|]{0,40}?[a-z]+(?:-[a-z]+)* remediation`),
		regexp.MustCompile(`(?i)\b[a-z]+(?:-[a-z]+)* remediation (?:is|remains) outstanding`),
		regexp.MustCompile(`(?i)acceptance rests on the [a-z]+(?:-[a-z]+)* review`),
		regexp.MustCompile(`(?i)follow-up review of [^|]{0,40}?[a-z]+(?:-[a-z]+)* remediation`),
	}

	// Regions where a figure is expected and is not a restatement to be policed:
	// the state line itself, and the historical record's own tables and findings.
	allowed := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		return strings.HasPrefix(trimmed, stage63StateLinePrefix) ||
			strings.HasPrefix(trimmed, "| 6.3-R") ||
			strings.HasPrefix(trimmed, "| ") // rounds and occurrence tables
	}

	for _, relative := range stage63CountCarryingDocuments {
		body, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		inFence := false
		for number, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if inFence || allowed(line) {
				continue
			}
			prose := inlineCode.ReplaceAllString(line, "")
			if pointer.MatchString(prose) {
				continue
			}
			for _, pattern := range live {
				if pattern.MatchString(prose) {
					t.Errorf("%s:%d restates a figure that belongs on the state line:\n  %s\n"+
						"  Delete it and point at the state line instead. Nine rounds failed this way.",
						relative, number+1, strings.TrimSpace(line))
					break
				}
			}
		}
	}
}
