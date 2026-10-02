// Package doccheck's claims harness re-derives every countable claim in the
// Stage 6 documents from the documents on every run, rather than asserting
// counts from memory. Fourteen review rounds established that an author
// restating a count gets it wrong about as often as not.
//
// Two requirements govern the harness itself, both learned from its first
// run: every check asserts its own inputs are non-empty, so a check that
// silently matches nothing cannot report success forever; and a harness
// failure is read first as a bug in the harness and only second as a defect
// in the tree — the first run reported six failures of which only two were
// real, and calibration after that found seven more harness bugs before the
// eighteenth check passed for the right reason. Negative coverage comes in
// two honest tiers, stated per test rather than claimed wholesale: checks
// with a pure core (C1, C1b, C2, C3, C4, C5, C8, C9, C10, C12, C13, C15,
// C16, C17, C18) are mutation-tested through that core; checks bound to
// git, STATUS or the live tree (C6 via the parity package, C7, C11, C14)
// are tested through their resolvers and regexes plus a live-tree positive.
// Deviations from the review record's specification are stated where they
// occur rather than smoothed over.
package doccheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func gitResolve(root, rev string) error {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("revision %s does not resolve", rev)
	}
	return nil
}

func gitAncestor(root, from, to string) bool {
	return exec.Command("git", "-C", root, "merge-base", "--is-ancestor", from, to).Run() == nil
}

// firstControlChar returns the line and rune of the first control character
// outside the legitimate Markdown set (tab and newline). Every other Cc
// character fails, including DEL.
func firstControlChar(content string) (line int, r rune, found bool) {
	for index, candidate := range content {
		if candidate == '\n' || candidate == '\t' {
			continue
		}
		if candidate < 0x20 || candidate == 0x7f {
			return 1 + strings.Count(content[:index], "\n"), candidate, true
		}
	}
	return 0, 0, false
}

// countStatusMarkers returns the canonical markers outside code in content.
func countStatusMarkers(content string) []string {
	return statusMarkerClaims.FindAllString(stripClaimsCode(content), -1)
}

// contradictsAcceptance reports whether live prose denies the recorded
// acceptance of a stage.
func contradictsAcceptance(content, stage string) bool {
	denied := regexp.MustCompile(`(?i)stage ` + regexp.QuoteMeta(stage) + ` is not accepted|not accepted[^.]{0,40}stage ` + regexp.QuoteMeta(stage))
	return denied.MatchString(stripClaimsCode(content))
}

// witnessHolds reports whether a quoted needle occurs more often in the
// corpus than inside its own row — i.e. something outside the row witnesses it.
func witnessHolds(corpus, row, needle string) bool {
	return strings.Count(corpus, needle) > strings.Count(row, needle)
}

// roundIDsContiguous asserts per-round finding IDs are unique and start at 1.
func roundIDsContiguous(tableIDs map[string][]int, expected []string) error {
	if len(tableIDs) != len(expected) {
		return fmt.Errorf("findings cover %d rounds, want %d", len(tableIDs), len(expected))
	}
	for _, round := range expected {
		ids, ok := tableIDs[round]
		if !ok {
			return fmt.Errorf("round %s has no findings table", round)
		}
		sort.Ints(ids)
		seen := map[int]bool{}
		for i, id := range ids {
			if id != i+1 {
				return fmt.Errorf("round %s IDs are not contiguous from 1", round)
			}
			if seen[id] {
				return fmt.Errorf("round %s duplicates ID %d", round, id)
			}
			seen[id] = true
		}
	}
	return nil
}

// distinctSectionNumbers counts distinct C-number headers, folding suffixed
// variants (C1b) into their number.
func distinctSectionNumbers(combined string) int {
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^// C(\d+)[ab]?: `).FindAllStringSubmatch(combined, -1) {
		seen[match[1]] = true
	}
	return len(seen)
}

// findStillClaim extracts a "still <verb> <quoted>" assertion from a finding
// cell, if it makes one.
func findStillClaim(row string) (quoted, doc string, ok bool) {
	stillPattern := regexp.MustCompile(`(?i)still\s+(reads?|said|holds|claims?|contains?|states?)\b[^"]{0,80}?"([^"]{8,})"`)
	match := stillPattern.FindStringSubmatch(row)
	if match == nil {
		return "", "", false
	}
	docPattern := regexp.MustCompile("(`?[a-zA-Z0-9_./-]+\\.md`?)")
	docMatch := docPattern.FindStringSubmatch(row)
	if docMatch == nil {
		return "", "", false
	}
	return match[2], strings.Trim(docMatch[1], "`"), true
}

type claimCheck struct {
	name string
	run  func(root string) error
}

// checkRoundOne runs C1 and its folded 1b severities together: the record
// enumerates eighteen sections as seventeen items with 1b folded into item
// 1, and the registry follows the same folding.
func checkRoundOne(root string) error {
	if err := checkReviewStateAgreement(root); err != nil {
		return err
	}
	return checkRoundSeverities(root)
}

var claimChecks = []claimCheck{
	{name: "C1: review-state agreement across documents (with 1b severities)", run: checkRoundOne},
	{name: "C2: reviewer-method partition covers all rounds once", run: checkMethodPartition},
	{name: "C3: round-1 findings contiguous with a true tally", run: checkRoundOneFindings},
	{name: "C4: verdict table carries no severity totals", run: checkVerdictTableClean},
	{name: "C5: R01-R71 contiguous with no interrupting prose", run: checkRequirementsContiguous},
	{name: "C6: exclusion lists byte-identical", run: checkExclusionByteIdentical},
	{name: "C7: review range chain is continuous", run: checkReviewChain},
	{name: "C8: no control character in Markdown", run: checkNoControlCharactersHarness},
	{name: "C9: bare F-refs live only in the review record", run: checkBareFRefs},
	{name: "C10: STATUS markers agree", run: checkStatusMarkersHarness},
	{name: "C11: accepted slice diff resolves and is scoped", run: checkSliceDiff},
	{name: "C12: every stated partition sums correctly", run: checkPartitionsSum},
	{name: "C13: no live doc contradicts acceptance", run: checkAcceptanceProse},
	{name: "C14: tight claim shapes match derived values", run: checkClaimShapes},
	{name: "C15: quoted replacements exist outside their row", run: checkQuotedReplacements},
	{name: "C16: asserted-stale text is gone from the named doc", run: checkStillReadsGone},
	{name: "C17: finding IDs unique, contiguous, range-true", run: checkFindingIDs},
	{name: "C18: harness self-count is eighteen", run: checkHarnessSelfCount},
}

func claimsRepoPath(root string, parts ...string) string {
	return filepath.Join(append([]string{root}, parts...)...)
}

func readDoc(root, rel string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rel, err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("%s is empty", rel)
	}
	return string(raw), nil
}

func filepathRel(root, path string) (string, error) {
	return filepath.Rel(root, path)
}

func allMarkdown(root string) ([]string, error) {
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".cache", ".tools", "bin", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	if len(found) == 0 {
		return nil, fmt.Errorf("no Markdown files found under %s", root)
	}
	return found, nil
}

var numberWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
	"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11,
	"twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15,
	"sixteen": 16, "seventeen": 17, "eighteen": 18,
}

func parseCountToken(token string) (int, bool) {
	token = strings.ToLower(token)
	if n, err := strconv.Atoi(token); err == nil {
		return n, true
	}
	n, ok := numberWords[token]
	return n, ok
}

// verdictRows returns the data rows of the verdict table in the review
// record, in order. It fails when the table is absent, so the check cannot
// pass vacuously.
func verdictRows(record string) ([]string, error) {
	index := strings.Index(record, "## Verdict table")
	if index < 0 {
		return nil, fmt.Errorf("verdict table heading not found")
	}
	rest := record[index:]
	var rows []string
	inTable := false
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && len(rows) > 0 {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		var clean []string
		for _, cell := range cells {
			clean = append(clean, strings.TrimSpace(cell))
		}
		joined := strings.Join(clean, "")
		stripped := strings.ReplaceAll(strings.ReplaceAll(joined, "-", ""), ":", "")
		if strings.TrimSpace(stripped) == "" {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		rows = append(rows, trimmed)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("verdict table has no data rows")
	}
	return rows, nil
}

// findingsTables returns every findings-table body (data rows) in the review
// record, keyed by occurrence order.
func findingsTables(record string) ([][]string, error) {
	var tables [][]string
	var current []string
	inTable := false
	sawHeader := false
	for _, line := range strings.Split(record, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			if len(current) > 0 {
				tables = append(tables, current)
				current = nil
			}
			inTable, sawHeader = false, false
			continue
		}
		if !strings.Contains(trimmed, "| ID |") {
			if !sawHeader {
				continue
			}
		}
		if strings.Contains(trimmed, "| ID |") {
			sawHeader = true
			continue
		}
		cells := strings.Trim(trimmed, "|")
		if isDelimRow(cells) {
			inTable = true
			continue
		}
		if inTable {
			current = append(current, trimmed)
		}
	}
	if len(current) > 0 {
		tables = append(tables, current)
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no findings tables found")
	}
	return tables, nil
}

func isDelimRow(cells string) bool {
	for _, cell := range strings.Split(cells, "|") {
		stripped := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(cell), "-", ""), ":", "")
		if stripped != "" {
			return false
		}
	}
	return true
}

var rangePatternClaims = regexp.MustCompile("([0-9a-f]{7,40})\\.\\.([0-9a-f]{7,40})")

// bareFPatternCompiled matches a bare F<n> reference: an F-number NOT
// preceded by an identifier character, dot or dash, so 6.1-F16 defect
// references and R2F3 finding IDs never match.
var bareFPatternCompiled = regexp.MustCompile(`(?:^|[^0-9A-Za-z.\-])F([0-9]+)`)

func bareFPattern() *regexp.Regexp { return bareFPatternCompiled }

func osReadFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

var (
	claimsFenceBacktick = regexp.MustCompile("(?ms)^[ \t]*```.*?^[ \t]*```[ \t]*$")
	claimsFenceTilde    = regexp.MustCompile("(?ms)^[ \t]*~~~.*?^[ \t]*~~~[ \t]*$")
	claimsInlineCode    = regexp.MustCompile("`+[^`]*`+")
)

// stripClaimsCode removes fenced blocks and inline code spans so a marker or
// reference quoted as an example is not mistaken for a live claim.
func stripClaimsCode(content string) string {
	content = claimsFenceBacktick.ReplaceAllString(content, "")
	content = claimsFenceTilde.ReplaceAllString(content, "")
	return claimsInlineCode.ReplaceAllString(content, "")
}

func normaliseClaim(value string) string {
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "`", "")
	return strings.Join(strings.Fields(value), " ")
}

// exclusionRows is retired: section extraction and table parsing both run
// through the parity package now, so C6 shares code with the register check
// instead of shadowing it. (Removed 2026-10-02 during the D/E follow-up;
// the parity package's ParseExclusionTable plus CheckExclusionMirror cover
// it, including the negative test.)
