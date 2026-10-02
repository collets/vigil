package doccheck

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"vigil/internal/parity"
)

// C1: every document asserting 6.1's review state, swept at claim-substance
// strength, agrees on thirteen further reviews and all fourteen rounds of
// findings. Derived: rounds from the verdict table's bold verdicts.
func checkReviewStateAgreement(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	rows, err := verdictRows(record)
	if err != nil {
		return err
	}
	rounds := 0
	for _, row := range rows {
		if strings.Contains(row, "**rejected**") || strings.Contains(row, "**conditional**") {
			rounds++
		}
	}
	if rounds == 0 {
		return fmt.Errorf("no reviewed rounds in the verdict table")
	}
	further := rounds - 1
	files, err := allMarkdown(root)
	if err != nil {
		return err
	}
	furtherDocs, roundsDocs := 0, 0
	for _, file := range files {
		raw, err := osReadFile(file)
		if err != nil {
			return err
		}
		gotFurther, gotRounds, err := checkReviewCounts(stripClaimsCode(raw), rounds, further)
		if err != nil {
			return fmt.Errorf("%s: %v", file, err)
		}
		furtherDocs += gotFurther
		roundsDocs += gotRounds
	}
	if furtherDocs == 0 {
		return fmt.Errorf("no document states the further-review count")
	}
	if roundsDocs == 0 {
		return fmt.Errorf("no document states the rounds-of-findings count")
	}
	return nil
}

// checkReviewCounts verifies one document's further-review and rounds-of-
// findings claims against the derived values, returning how many of each it
// states. Unparsable tokens are skipped; disagreeing ones fail.
func checkReviewCounts(content string, rounds, further int) (furtherDocs, roundsDocs int, err error) {
	furtherPattern := regexp.MustCompile(`(?i)([a-z0-9]+)\s+further\s+reviews?`)
	roundsPattern := regexp.MustCompile(`(?i)([a-z0-9]+)\s+rounds?\s+of\s+findings`)
	for _, match := range furtherPattern.FindAllStringSubmatch(content, -1) {
		n, ok := parseCountToken(match[1])
		if !ok {
			continue
		}
		furtherDocs++
		if n != further {
			return 0, 0, fmt.Errorf("claims %d further reviews, verdict table derives %d", n, further)
		}
	}
	for _, match := range roundsPattern.FindAllStringSubmatch(content, -1) {
		n, ok := parseCountToken(match[1])
		if !ok {
			continue
		}
		roundsDocs++
		if n != rounds {
			return 0, 0, fmt.Errorf("claims %d rounds of findings, verdict table derives %d", n, rounds)
		}
	}
	return furtherDocs, roundsDocs, nil
}

// C1b: the record header's per-round severity account matches the findings
// tables: no P0 after round 1; P1s in rounds 1-3 are 3/4/1, in rounds 9-12
// are 3/2/4/1; rounds 4-8 and 13 find none. The header sentence is the claim;
// the tables are the derivation.
func checkRoundSeverities(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	tables, err := findingsTables(record)
	if err != nil {
		return err
	}
	if len(tables) != 14 {
		return fmt.Errorf("found %d findings tables, want 14", len(tables))
	}
	countSev := func(rows []string, sev string) int {
		return countSeverity(rows, sev)
	}
	p1 := make([]int, 14)
	for i, table := range tables {
		if countSev(table, "P0") > 0 && i != 0 {
			return fmt.Errorf("table %d contains a P0 outside round 1", i+1)
		}
		p1[i] = countSev(table, "P1")
	}
	want := map[int]int{0: 3, 1: 4, 2: 1, 8: 3, 9: 2, 10: 4, 11: 1}
	for round, count := range want {
		if p1[round] != count {
			return fmt.Errorf("round table %d has %d P1s, want %d", round+1, p1[round], count)
		}
	}
	for _, round := range []int{3, 4, 5, 6, 7, 12} {
		if p1[round] != 0 {
			return fmt.Errorf("round table %d has %d P1s, want none", round+1, p1[round])
		}
	}
	return nil
}

// C2: the reviewer-method sentence partitions all fourteen rounds exactly
// once, with no overlap, and 1 + 9 + 4 = 14 matches the enumeration.
func checkMethodPartition(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	flat := strings.Join(strings.Fields(stripClaimsCode(record)), " ")
	index := strings.Index(flat, "1 + 9 + 4 = 14")
	if index < 0 {
		return fmt.Errorf("method partition sentence not found")
	}
	window := flat[max(0, index-600) : index+100]
	return verifyMethodPartition(window)
}

// verifyMethodPartition asserts a text window holds three round groups that
// partition rounds 1..14 exactly once with sizes 1, 4 and 9.
func verifyMethodPartition(window string) error {
	groupPattern := regexp.MustCompile(`\(rounds? ([0-9,\s]+and [0-9]+|[0-9]+)\)`)
	groups := groupPattern.FindAllStringSubmatch(window, -1)
	if len(groups) != 3 {
		return fmt.Errorf("partition has %d round groups, want 3", len(groups))
	}
	numPattern := regexp.MustCompile(`[0-9]+`)
	seen := map[int]int{}
	sizes := []int{}
	for _, group := range groups {
		members := numPattern.FindAllString(group[1], -1)
		sizes = append(sizes, len(members))
		for _, member := range members {
			n, _ := strconv.Atoi(member)
			seen[n]++
		}
	}
	sort.Ints(sizes)
	if len(sizes) != 3 || sizes[0] != 1 || sizes[1] != 4 || sizes[2] != 9 {
		return fmt.Errorf("partition sizes are %v, want [1 4 9]", sizes)
	}
	if len(seen) != 14 {
		return fmt.Errorf("partition covers %d distinct rounds, want 14", len(seen))
	}
	for round := 1; round <= 14; round++ {
		if seen[round] != 1 {
			return fmt.Errorf("round %d appears %d times in the partition", round, seen[round])
		}
	}
	return nil
}

// C3: 6.1-R1..R15 are contiguous in one table, and the recomputed severity
// tally equals 4 P0 / 3 P1 / 5 P2 / 3 P3 = 15.
func checkRoundOneFindings(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	tables, err := findingsTables(record)
	if err != nil {
		return err
	}
	ids, tally := tallyFindingsTable(tables[0])
	if len(ids) != 15 {
		return fmt.Errorf("first findings table has %d rows, want 15", len(ids))
	}
	for i, id := range ids {
		if id != i+1 {
			return fmt.Errorf("first findings table is not contiguous from 1")
		}
	}
	if tally["P0"] != 4 || tally["P1"] != 3 || tally["P2"] != 5 || tally["P3"] != 3 {
		return fmt.Errorf("round-1 tally is %d/%d/%d/%d, want 4/3/5/3", tally["P0"], tally["P1"], tally["P2"], tally["P3"])
	}
	return nil
}

// tallyFindingsTable extracts round-1-style finding IDs and the severity
// tally from one findings table's data rows.
func tallyFindingsTable(rows []string) (ids []int, tally map[string]int) {
	idPattern := regexp.MustCompile(`6\.1-R([0-9]+)`)
	tally = map[string]int{}
	for _, row := range rows {
		match := idPattern.FindStringSubmatch(row)
		if len(match) < 2 {
			continue
		}
		n, _ := strconv.Atoi(match[1])
		ids = append(ids, n)
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) > 1 {
			tally[strings.Trim(strings.TrimSpace(cells[1]), "*")]++
		}
	}
	return ids, tally
}

// C4: the verdict table carries no restated severity totals.
func checkVerdictTableClean(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	rows, err := verdictRows(record)
	if err != nil {
		return err
	}
	joined := strings.Join(rows, "\n")
	if match := findTallyShape(joined); match != "" {
		return fmt.Errorf("verdict table restates severity total %q", match)
	}
	return nil
}

// tallyShapes are the cross-round total forms. Per-round outcome prose
// ("no P0", "one P1") is allowed: each round's counts are verified
// mechanically by C1b instead.
var tallyShapes = []string{`×\s*P[0-3]`, `\b\d+\s*P[0-3]\s*/`, `P[0-3]\s*/\s*P[0-3]`, `\b[2-9]\d*\s+P[0-3]\b`, `\b\d+\s+P[0-3]\b.*\b\d+\s+P[0-3]\b`}

// findTallyShape strips finding IDs and explicit no-finding statements,
// then returns the first tally shape found, if any.
func findTallyShape(joined string) string {
	idRangePattern := regexp.MustCompile(`6\.1-[A-Z0-9]+-?[0-9]+(…|\.\.\.|-)\S*`)
	joined = idRangePattern.ReplaceAllString(joined, "")
	idPattern := regexp.MustCompile(`6\.1-[A-Z0-9]+-?[0-9]+`)
	joined = idPattern.ReplaceAllString(joined, "")
	noPattern := regexp.MustCompile(`(?i)no P[0-3](, no P[0-3])*`)
	joined = noPattern.ReplaceAllString(joined, "")
	for _, shape := range tallyShapes {
		if match := regexp.MustCompile(shape).FindString(joined); match != "" {
			return match
		}
	}
	return ""
}

// C5: R01..R71 contiguous with no interrupting prose.
func checkRequirementsContiguous(root string) error {
	requirements, err := readDoc(root, "docs/core/requirements.md")
	if err != nil {
		return err
	}
	lines := strings.Split(requirements, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "| R01 ") || strings.HasPrefix(strings.TrimSpace(line), "| R01 |") {
			start = i
		}
		if strings.HasPrefix(strings.TrimSpace(line), "| R71 ") || strings.HasPrefix(strings.TrimSpace(line), "| R71 |") {
			end = i
		}
	}
	if start < 0 || end < 0 || end <= start {
		return fmt.Errorf("R01-R71 block not found")
	}
	if err := verifyRequirementsBlock(lines[start : end+1]); err != nil {
		return err
	}
	if end-start+1 != 71 {
		return fmt.Errorf("block holds %d lines, want 71", end-start+1)
	}
	return nil
}

// verifyRequirementsBlock asserts a line slice is a contiguous R01-based
// requirement table with no interrupting prose.
func verifyRequirementsBlock(block []string) error {
	idPattern := regexp.MustCompile(`^\|\s*R([0-9]+)\s*\|`)
	next := 1
	for _, line := range block {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			return fmt.Errorf("non-table line inside the R01-R71 block: %q", trimmed)
		}
		match := idPattern.FindStringSubmatch(trimmed)
		if match == nil {
			return fmt.Errorf("table line without requirement ID inside the block: %q", trimmed)
		}
		n, _ := strconv.Atoi(match[1])
		if n != next {
			return fmt.Errorf("requirement R%02d out of order, want R%02d", n, next)
		}
		next++
	}
	return nil
}

// C6: the two closed-exclusion-list copies are byte-identical (compared by
// normalised hash, not by eye). Deviation from the record: the permanent
// check reads the live documents rather than a scratch copy. Parsing and
// comparison both run through the parity package — the same code the
// register check uses — so a drift between the two implementations cannot
// hide a drift between the documents.
func checkExclusionByteIdentical(root string) error {
	requirements, err := readDoc(root, "docs/core/requirements.md")
	if err != nil {
		return err
	}
	results, err := readDoc(root, "docs/research/stage-6/results.md")
	if err != nil {
		return err
	}
	canonical := parity.ParseExclusionTable(parity.ExtractSection(requirements, "The closed exclusion list R11 depends on"))
	mirror := parity.ParseExclusionTable(parity.ExtractSection(results, "### 5.9 Deliberate exclusions"))
	if len(canonical) == 0 || len(mirror) == 0 {
		return fmt.Errorf("exclusion table parsed empty")
	}
	return parity.CheckExclusionMirror(canonical, mirror)
}

// C7: the next-action floor equals the previous round's reviewed head and
// is a real ancestor of the branch tip. The verdict table reviews cumulative
// ranges (27182de..X), so consecutive rows do not share boundaries; the
// checkable property is on the head: the last ranged row's end resolves in
// git, is an ancestor of the STATUS implementation commit, and the live
// next-action line names no superseded range.
func checkReviewChain(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	rows, err := verdictRows(record)
	if err != nil {
		return err
	}
	type span struct {
		from, to string
	}
	var spans []span
	var unranged []string
	for _, row := range rows {
		match := rangePatternClaims.FindStringSubmatch(row)
		if match == nil {
			unranged = append(unranged, row)
			continue
		}
		spans = append(spans, span{match[1], match[2]})
	}
	// Exactly two unranged rows are documented: the bare-SHA fifth round
	// and the open tail row awaiting its remediation commit.
	if len(unranged) != 2 {
		return fmt.Errorf("found %d unranged verdict rows, want the bare fifth-round row and the open tail", len(unranged))
	}
	bareSHA := regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	if bareSHA.FindString(unranged[0]) == "" || !strings.Contains(unranged[1], "not yet given") {
		return fmt.Errorf("unranged verdict rows are not the documented bare round and open tail")
	}
	if len(spans) == 0 {
		return fmt.Errorf("no reviewed ranges in the verdict table")
	}
	head := spans[len(spans)-1].to
	status, err := loadClaimsStatus(root)
	if err != nil {
		return err
	}
	if err := gitResolve(root, head); err != nil {
		return fmt.Errorf("reviewed head %s: %v", head, err)
	}
	if err := gitResolve(root, status["implementation_commit"]); err != nil {
		return err
	}
	if !gitAncestor(root, head, status["implementation_commit"]) {
		return fmt.Errorf("reviewed head %s is not an ancestor of %s", head, status["implementation_commit"])
	}
	next, err := readDoc(root, "docs/process/next-steps.md")
	if err != nil {
		return err
	}
	found := false
	for _, line := range strings.Split(next, "\n") {
		lowered := strings.ToLower(line)
		if strings.Contains(lowered, "next concrete action") || strings.Contains(lowered, "next action") {
			found = true
			for _, match := range rangePatternClaims.FindAllStringSubmatch(line, -1) {
				if gitResolve(root, match[2]) == nil && gitAncestor(root, match[2], status["implementation_commit"]) && match[2] != status["implementation_commit"] {
					return fmt.Errorf("next-action line names superseded range end %s", match[2])
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("no next-action line in next-steps.md")
	}
	return nil
}

// C8: no control character in any Markdown file. Tab and newline are
// legitimate; every other Cc character fails. This is the same rule as the
// documentation gate's own test, re-run here so the harness count covers it.
func checkNoControlCharactersHarness(root string) error {
	files, err := allMarkdown(root)
	if err != nil {
		return err
	}
	if len(files) < 50 {
		return fmt.Errorf("only %d Markdown files scanned", len(files))
	}
	for _, file := range files {
		raw, err := osReadFile(file)
		if err != nil {
			return err
		}
		if line, r, found := firstControlChar(raw); found {
			return fmt.Errorf("%s:%d: control character U+%04X", file, line, r)
		}
	}
	return nil
}

// C9: a bare F<n> outside the review record is allowed only where its
// namespace is unambiguous on the same line: a 6.1-F head (the stage-6
// plans' list-abbreviation convention, resolved by M3) or an explicit
// other-stage marker (the Stage 5.4 F1-F3 series). 6.1-F<n> defect
// references are not bare and are allowed everywhere. Anything else is the
// identifier collision the record exists to prevent.
func checkBareFRefs(root string) error {
	files, err := allMarkdown(root)
	if err != nil {
		return err
	}
	bare := bareFPattern()
	scanned := 0
	for _, file := range files {
		raw, err := osReadFile(file)
		if err != nil {
			return err
		}
		scanned++
		if strings.HasSuffix(file, "docs/research/stage-6/6.1-review.md") {
			continue
		}
		rel, relErr := filepathRel(root, file)
		if relErr != nil {
			return relErr
		}
		if strings.HasPrefix(rel, "docs/research/stage-5/") || strings.HasPrefix(rel, "docs/plans/stage-5/") || strings.HasPrefix(rel, "docs/history/") {
			// Other stages' own records use their own finding series.
			continue
		}
		if strings.HasPrefix(rel, "docs/plans/") {
			// The stage plans keep their list-abbreviation convention by
			// documented resolution (M3): bare F<n> there resolves against
			// the surrounding 6.1-defect discussion, not against nothing.
			continue
		}
		for _, line := range strings.Split(stripClaimsCode(raw), "\n") {
			if bare.FindString(line) == "" {
				continue
			}
			if namespaceOK(line) {
				continue
			}
			return fmt.Errorf("bare F-ref with no namespace on its line: %s: %q", file, strings.TrimSpace(line))
		}
	}
	if scanned == 0 {
		return fmt.Errorf("no files scanned")
	}
	return nil
}

// countSeverity tallies table rows whose severity cell equals sev, ignoring
// Markdown emphasis around the marker.
func countSeverity(rows []string, sev string) int {
	n := 0
	for _, row := range rows {
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) > 1 && strings.Trim(strings.TrimSpace(cells[1]), "*") == sev {
			n++
		}
	}
	return n
}

// namespaceOK reports whether a bare F-ref line carries its namespace on
// the same line: a 6.1-F head or an other-stage marker.
func namespaceOK(line string) bool {
	if strings.Contains(line, "6.1-F") {
		return true
	}
	return otherStagePattern.MatchString(line)
}

var otherStagePattern = regexp.MustCompile(`5\.[1-9]|astra|stage-5|cba322b|99cd6c0`)
