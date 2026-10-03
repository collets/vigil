package doccheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The occurrence table is the one place in the review record where a figure is
// derived from an enumeration beside it, and it is the source of rounds 21 and
// 22's first P1: the prose said fourteen occurrences and ten self-inflicted while
// the table held seventeen rows and eleven `**yes**` rows, and a resolution cell
// claimed the total had been "derived from the table" when it had been typed.
//
// Two rounds later the same cell claimed the same thing again. So the figures are
// now read out of the table and compared, rather than trusted.
// The leading character class must accept uppercase. With a lowercase-only class
// Go's regexp finds the leftmost match, which starts one character late and reads
// "Twenty-six" as "wenty-six" — a silent figure error in the checker itself, found
// by running it rather than by reading it.
const stage63OccurrenceProsePattern = `([A-Za-z]+(?:-[a-z]+)*) occurrences in rounds (\d+) to (\d+), ([a-z]+(?:-[a-z]+)*) of them self-inflicted,\s*\n\s*and the ([a-z]+(?:-[a-z]+)*) consecutive self-inflicted ones are the last ([a-z]+(?:-[a-z]+)*) rows\.`

// stage63OccurrenceRows returns the occurrence table's data rows.
func stage63OccurrenceRows(doc string) []string {
	start := strings.Index(doc, "**Every occurrence of this fault in this stage")
	if start < 0 {
		return nil
	}
	rest := doc[start:]
	var rows []string
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(line, "| Round") {
			continue
		}
		if strings.HasPrefix(line, "| ---") {
			continue
		}
		if strings.HasPrefix(line, "| ") {
			rows = append(rows, line)
			// The prose paragraph follows the table; stop there.
			if len(rows) > 1 && !strings.Contains(line, "|") {
				break
			}
			continue
		}
		if len(rows) > 0 && strings.TrimSpace(line) != "" {
			break
		}
	}
	return rows
}

func TestOccurrenceTableFiguresMatchTheirSource(t *testing.T) {
	root := repoRoot
	path := filepath.Join("docs", "research", "stage-6", "6.3-review.md")
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := string(raw)

	rows := stage63OccurrenceRows(doc)
	if len(rows) == 0 {
		t.Fatal("no occurrence table found; the gate cannot check figures that are not there")
	}
	yes := 0
	for _, row := range rows {
		if strings.Contains(row, "**yes**") {
			yes++
		}
	}
	if yes == 0 || yes > len(rows) {
		t.Fatalf("counted %d self-inflicted rows out of %d; the table is not readable", yes, len(rows))
	}

	prose := regexp.MustCompile(stage63OccurrenceProsePattern).FindStringSubmatch(doc)
	if prose == nil {
		t.Fatalf("no occurrence prose matching the expected shape; the figures beside the table cannot be checked.\nWanted a sentence like:\n  Occurrences in rounds 10 to 24, twenty of them self-inflicted, and the twenty\n  consecutive self-inflicted ones are the last twenty rows.")
	}
	statedTotal, ok0 := stage63ParseNumber(prose[1])
	statedSelfInflicted, ok1 := stage63ParseNumber(prose[4])
	statedTailLength, ok2 := stage63ParseNumber(prose[5])
	statedTailRows, ok3 := stage63ParseNumber(prose[6])
	if !ok0 || !ok1 || !ok2 || !ok3 {
		t.Fatalf("cannot read the prose figures: %q", prose)
	}
	spanFirst, err := strconv.Atoi(prose[2])
	if err != nil {
		t.Fatalf("cannot read the first round of the span: %v", err)
	}
	spanLast, err := strconv.Atoi(prose[3])
	if err != nil {
		t.Fatalf("cannot read the last round of the span: %v", err)
	}

	// The last row's round number is the span's end.
	tableLastRound, err := strconv.Atoi(strings.TrimSpace(strings.Split(rows[len(rows)-1], "|")[1]))
	if err != nil {
		t.Fatalf("cannot read the last row's round: %v", err)
	}
	tableFirstRound, err := strconv.Atoi(strings.TrimSpace(strings.Split(rows[0], "|")[1]))
	if err != nil {
		t.Fatalf("cannot read the first row's round: %v", err)
	}

	for _, check := range []struct {
		what      string
		got, want int
	}{
		{"occurrences in total", statedTotal, len(rows)},
		{"self-inflicted occurrences", statedSelfInflicted, yes},
		{"consecutive self-inflicted count", statedTailLength, yes},
		{"rows claimed in the tail", statedTailRows, yes},
		{"first round in the span", spanFirst, tableFirstRound},
		{"last round in the span", spanLast, tableLastRound},
	} {
		if check.got != check.want {
			t.Errorf("the occurrence prose states %d %s; the table has %d. This is the round 21 and round 22 finding: the figures beside the table were typed rather than derived",
				check.got, check.what, check.want)
		}
	}
}
