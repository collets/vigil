package doccheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Six consecutive review rounds rejected this stage for one shape: a document
// asserting something about the count gate that the gate did not do. Round 24
// found four such statements still standing after the commit that fixed the
// coverage — including one in the plan's own checklist, saying the gate did not
// read the four documents it had just been made to read.
//
// Editing each of them by hand fixes four statements until the next mechanism
// change, which is how round 23 falsified the same claim one round after round 22
// had it corrected. So the statements are checked instead.
func TestNoDocumentUnderstatesTheGateCoverage(t *testing.T) {
	root := repoRoot

	// Any document that describes the gate may name it and may describe what it
	// covers — but must not claim the coverage is partial. These are the exact
	// phrases that were true once and false later, each with the round that found
	// it stale.
	forbidden := []struct {
		phrase string
		found  string
	}{
		{"for this one file", "claimed the gate verified only the review record (true until round 22)"},
		{"this one record", "claimed the gate verified only the review record"},
		{"the other four documents", "claimed the four other documents were unchecked (true until round 23, and again at round 24)"},
		{"are ungated", "claimed the other documents are unchecked"},
		{"is not gated", "claimed a document is unchecked"},
		{"only thing read", "claimed the state line was the gate's sole input (false: it also reads the header prose and the pending row)"},
	}
	// "ungated" is legitimate in a sentence that scopes it to prose, so the
	// match is allowed when the sentence says so.
	allowedQualifier := "prose"

	err := filepath.Walk(filepath.Join(root, "docs"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		for _, line := range strings.Split(string(body), "\n") {
			// A historical finding row quotes the claim as it stood when the
			// finding was raised; that is evidence, not a live assertion, and
			// rewriting it would destroy the audit trail six rounds preserved.
			if strings.HasPrefix(strings.TrimSpace(line), "| 6.3-R") {
				continue
			}
			for _, rule := range forbidden {
				if !strings.Contains(line, rule.phrase) {
					continue
				}
				if rule.phrase == "are ungated" && strings.Contains(line, allowedQualifier) {
					continue
				}
				t.Errorf("%s: a document understates what the gate covers — %q (this %s)",
					relative, strings.TrimSpace(line), rule.found)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
}

// TestRoundTalliesMatchTheirSections closes P1-3 and P1-4 of round 24: the
// rounds table's Findings column is a figure a machine can count, and it
// disagreed with its own section twice — rows 22 and 23 — each time in the row
// authored by the commit claiming to have fixed the previous one.
//
// The column is derived here instead, so a tally cannot be typed wrongly.
//
// Two bugs in this check were found by running it. It first read a column named
// "tally" while the table's column is "Findings", so the lookup was always empty
// and the check passed without ever running — a silent no-op, the exact shape of
// R23F2. And it compared the expected number by substring, which "4 P1, 6 P2, 1
// P3" satisfies for an expected 4 P2 because the "4" of "4 P1" is present.
// stage63FirstFullyEnumeratedRound is the first round whose section enumerates
// every finding as its own row. Rounds before it record only the P0 and P1 rows
// and state their P2/P3 counts in prose.
const stage63FirstFullyEnumeratedRound = 17

func TestRoundTalliesMatchTheirSections(t *testing.T) {
	root := repoRoot
	raw, err := os.ReadFile(filepath.Join(root, "docs", "research", "stage-6", "6.3-review.md"))
	if err != nil {
		t.Fatalf("read the review record: %v", err)
	}
	doc := string(raw)

	table := stage63RoundsTable(doc)
	rows := stage63RoundRows(table)
	if len(rows) == 0 {
		t.Fatal("no rounds table rows found")
	}
	sections := stage63RoundSections(doc)
	if len(sections) == 0 {
		t.Fatal("no round sections found")
	}

	for _, row := range rows {
		number, err := strconv.Atoi(strings.TrimSpace(row["Round"]))
		if err != nil {
			continue
		}
		// Only from round 17 onward is the tally mechanically derivable. Earlier
		// rounds recorded their P2 and P3 counts in prose ("5 P2 nits", "1 P0,
		// 5 P1, 12 P2") and kept only the P0/P1 as finding rows, so their tallies
		// are not countable from the record and checking them would report the
		// difference between prose and rows as a defect.
		if number < stage63FirstFullyEnumeratedRound {
			continue
		}
		section, ok := sections[strconv.Itoa(number)]
		if !ok {
			continue // an early round with no section; its tally is prose, not rows
		}
		want := map[string]int{}
		for _, severity := range section {
			want[severity]++
		}
		if len(want) == 0 {
			continue
		}
		// The tally is PARSED, not substring-matched. An earlier version of this
		// check asked only whether the tally contained the severity letter and
		// the expected number, which passes for "4 P1, 6 P2, 1 P3" against an
		// expected 4 P2 — because the "4" of "4 P1" satisfies it. That is the
		// same class of loose assertion this repository keeps rejecting, found
		// here by reading the checker's own failure mode.
		got := stage63ParseTally(row["Findings"])
		if got == nil {
			continue // an early round whose tally is prose ("5 P2 nits", "none")
		}
		for _, severity := range []string{"P1", "P2", "P3"} {
			if got[severity] != want[severity] {
				t.Errorf("rounds table row %d states %d %s, but its section holds %d (tally %q)",
					number, got[severity], severity, want[severity], row["tally"])
			}
		}
	}
}

// stage63ParseTally reads a "4 P1, 6 P2, 1 P3" tally into counts. It returns
// nil for a cell that is prose rather than a tally ("5 P2 nits", "none", "—"),
// which is how the early rounds record their findings.
func stage63ParseTally(cell string) map[string]int {
	matches := regexp.MustCompile(`(\d+)\s+(P[123])`).FindAllStringSubmatch(cell, -1)
	if len(matches) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, match := range matches {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			return nil
		}
		counts[match[2]] = value
	}
	return counts
}

// stage63RoundRows parses the rounds table into rows keyed by column name.
func stage63RoundRows(table string) []map[string]string {
	var out []map[string]string
	header := []string{}
	for _, line := range strings.Split(table, "\n") {
		cells := stage63SplitRow(line)
		if len(cells) < 6 {
			continue
		}
		if len(header) == 0 {
			header = cells
			continue
		}
		if cells[0] == "---" || cells[0] == "Round" {
			continue
		}
		row := map[string]string{}
		for i, name := range header {
			if i < len(cells) {
				row[name] = cells[i]
			}
		}
		out = append(out, row)
	}
	return out
}

func stage63SplitRow(line string) []string {
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	var cells []string
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

// stage63RoundSections maps a round number to the severities of its finding rows.
func stage63RoundSections(doc string) map[string][]string {
	out := map[string][]string{}
	pattern := regexp.MustCompile(`(?m)^## Round (\d+)\b`)
	matches := pattern.FindAllStringSubmatchIndex(doc, -1)
	for i, match := range matches {
		number := doc[match[2]:match[3]]
		start := match[1]
		end := len(doc)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := doc[start:end]
		var severities []string
		for _, row := range regexp.MustCompile(`(?m)^\| 6\.3-R\d+F\d+ \| (P\d) \|`).FindAllStringSubmatch(body, -1) {
			severities = append(severities, row[1])
		}
		out[number] = severities
	}
	return out
}
