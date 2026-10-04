package doccheck

import (
	_ "embed"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"text/template"
)

// This file is the whole of a stage's review-state checker: the machine gate
// that keeps a stage's applied/review/outstanding figures equal to the commit
// history, the state-line parser and its mutation battery, the occurrence
// table's figures, the rounds table's tallies, and the checks for prose that
// understates or restates what the gate does.
//
// It is ONE file because the previous arrangement was six, and every one of
// them was hand-listed by five documents. Nineteen review rounds of Stage 6.3
// found zero product defects and nineteen bookkeeping defects, every one of
// them a figure in one document disagreeing with a figure in another, and the
// cause was structural: adding or deleting a check required hand-editing five
// documents that described the harness. The generated manifest now holds that
// description (see `make manifest`), so this file holds only the mechanism.
//
// Every stage-specific literal — the record's path, the state-line marker's
// text, the subject prefix that identifies a remediation commit, the
// bookkeeping commits that are not remediations, the documents that carry the
// figures, the phrasing those documents use, and the prose shapes that count
// as a live restatement — is a field of `stageReviewStates` below. Nothing
// outside that table may name a stage. A second stage is a second row, not a
// second file.

// --- configuration ----------------------------------------------------------

// stageReviewState is everything about one stage's review machinery that a
// checker needs to know. It is data, not behaviour, so it is also what the
// generated manifest states: a reader of the manifest can check it against
// this table.
type stageReviewState struct {
	// Stage is the sub-stage number, used for the review record's finding-row
	// prefix and nothing else.
	Stage string
	// Record is the append-only review log. It is deleted, with this file, when
	// the stage closes.
	Record string
	// Manifest is the generated document that describes this mechanism.
	Manifest string
	// StateLinePrefix marks the one line per document that carries the figures.
	StateLinePrefix string
	// SubjectPrefix is the commit-subject prefix that identifies a remediation.
	SubjectPrefix string
	// BookkeepingSubjects are commits that declare themselves this stage's work
	// but answer no review round, so they are not remediations. The
	// classification is inclusive on SubjectPrefix and exclusive only on these:
	// a whitelist of remediation prefixes fails toward under-counting the very
	// commit that introduced it, while a closed exclusion list fails toward
	// over-counting an unrelated commit that merely touched the branch.
	BookkeepingSubjects []string
	// CountCarryingDocuments are the documents that state the figures. Each one
	// is checked; a document that stops carrying the state line FAILS rather
	// than being skipped, because coverage silently dropping is the defect this
	// was extended twice to close.
	CountCarryingDocuments []string
	// AppliedPhrase, ReviewsPhrase and OutstandingPhrase are the tails a state
	// line's three figures are read against.
	AppliedPhrase     string
	ReviewsPhrase     string
	OutstandingPhrase string
	// LiveClaimShapes are the prose shapes that restate a figure and therefore
	// belong on the state line. It is a list of wordings, and a wording not in
	// it evades the check — stated in the manifest rather than glossed.
	LiveClaimShapes []string
	// RejectedCandidate is the commit the follow-up rounds review from.
	RejectedCandidate string
	// NonRemediationRounds is the number of early rounds that reviewed
	// checkpoints and the candidate rather than a remediation.
	NonRemediationRounds int
	// RoundsHeading opens the rounds table.
	RoundsHeading string
	// OccurrenceAnchor opens the occurrence table.
	OccurrenceAnchor string
	// FirstFullyEnumeratedRound is the first round whose section enumerates every
	// finding as its own row. Earlier rounds record only the P0 and P1 rows and
	// state their P2/P3 counts in prose, so their tallies are not derivable.
	FirstFullyEnumeratedRound int
}

// stageReviewStates is the whole configuration surface. One row per stage whose
// review record is live.
var stageReviewStates = []stageReviewState{
	{
		Stage:           "6.3",
		Record:          filepath.Join("docs", "research", "stage-6", "6.3-review.md"),
		Manifest:        filepath.Join("docs", "research", "stage-6", "6.3-manifest.md"),
		StateLinePrefix: "Stage 6.3 review state:",
		SubjectPrefix:   "stage 6.3: ",
		BookkeepingSubjects: []string{
			"stage 6.3: record native macOS evidence and the checkpoint review record",
			"stage 6.3: point the next action at the candidate review",
			"stage 6.3: record that the candidate review dispatch was rate limited",
		},
		CountCarryingDocuments: []string{
			filepath.Join("docs", "research", "stage-6", "6.3-review.md"),
			filepath.Join("docs", "research", "stage-6", "results.md"),
			filepath.Join("docs", "process", "next-steps.md"),
			filepath.Join("docs", "plans", "stage-6", "6.3-main-dashboard.md"),
			filepath.Join("docs", "plans", "stage-6", "stage-6.md"),
		},
		AppliedPhrase:     "remediations applied",
		ReviewsPhrase:     "reviews run",
		OutstandingPhrase: "remediation is outstanding",
		// The live-claim shape list is deliberately narrow, and it is a list of
		// WORDINGS rather than a rule. Nine rounds of this stage failed on a
		// figure restated in prose, and both attempts at a general rule lost: a
		// rule about location fired on eleven sentences of correct explanatory
		// prose, and a value check over five whole documents could not tell a
		// live claim from a historical one. A check that fires on correct text
		// gets silenced, which is the mistake this repository keeps making. The
		// generated manifest states the limit.
		LiveClaimShapes: []string{
			`(?i)\b[a-z]+(?:-[a-z]+)* remediations (?:have been |were )?applied`,
			`(?i)review of [^|]{0,40}?[a-z]+(?:-[a-z]+)* remediation`,
			`(?i)\b[a-z]+(?:-[a-z]+)* remediation (?:is|remains) outstanding`,
			`(?i)acceptance rests on the [a-z]+(?:-[a-z]+)* review`,
			`(?i)follow-up review of [^|]{0,40}?[a-z]+(?:-[a-z]+)* remediation`,
		},
		RejectedCandidate:         "f4196fb",
		NonRemediationRounds:      9,
		RoundsHeading:             "## Rounds",
		OccurrenceAnchor:          "**Every occurrence of this fault in this stage",
		FirstFullyEnumeratedRound: 16,
	},
}

// findingRowPrefix is the leading cell of a finding row in the review record,
// used to exempt a quoted historical finding from the prose checks. It is
// derived, never restated.
func (c stageReviewState) findingRowPrefix() string { return "| " + c.Stage + "-R" }

// findingRowPattern matches a finding row's severity cell. P0 is matched
// alongside P1..P3 because a pattern that could not read it made the P0
// comparison dead code, and a round was rejected on a P0.
func (c stageReviewState) findingRowPattern() string {
	return `(?m)^\| ` + regexp.QuoteMeta(c.Stage) + `-R\d+F\d+ \| \**(P\d)\** \|`
}

// --- the English numeral reader ---------------------------------------------

// English numerals, in both cardinal and ordinal form. The records spell their
// counts in words, so a checker must be able to read words — but a fixed table
// puts a ceiling on the count, which is exactly the defect one stage's round 20
// found: the comparison had moved to integers while the *reading* was still a
// 0..20 table, so the twenty-first commit failed with a message claiming the
// document said "1". A compositional parser has no ceiling.
var cardinals = map[string]int{
	"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
	"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
	"thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19,
	"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60,
	"seventy": 70, "eighty": 80, "ninety": 90,
}

var ordinals = map[string]int{
	"zeroth": 0, "first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5,
	"sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9, "tenth": 10,
	"eleventh": 11, "twelfth": 12, "thirteenth": 13, "fourteenth": 14,
	"fifteenth": 15, "sixteenth": 16, "seventeenth": 17, "eighteenth": 18,
	"nineteenth": 19, "twentieth": 20, "thirtieth": 30, "fortieth": 40,
	"fiftieth": 50, "sixtieth": 60, "seventieth": 70, "eightieth": 80,
	"ninetieth": 90,
}

var irregularOrdinals = map[int]string{
	1: "first", 2: "second", 3: "third", 5: "fifth", 8: "eighth", 9: "ninth", 12: "twelfth",
}

var unitCardinals = map[int]string{
	1: "one", 2: "two", 3: "three", 4: "four", 5: "five", 6: "six", 7: "seven",
	8: "eight", 9: "nine", 10: "ten", 11: "eleven", 12: "twelve",
	20: "twenty", 30: "thirty", 40: "forty", 50: "fifty", 60: "sixty",
	70: "seventy", 80: "eighty", 90: "ninety",
}

// parseNumber reads an English numeral, cardinal or ordinal, compound or
// simple: "eighteen", "twentieth", "twenty-one", "twenty-first",
// "one hundred and four". ok is false for anything it cannot read, which fails
// closed with a message naming the offending text.
func parseNumber(text string) (int, bool) {
	lowered := strings.ToLower(text)
	fields := strings.FieldsFunc(lowered, func(r rune) bool {
		return r == '-' || r == ' ' || r == '\u2011'
	})
	if len(fields) == 0 {
		return 0, false
	}
	// "twenty-first" is a number; "twenty first" is a typo, and reading it as
	// twenty-one would make a misspelt ordinal indistinguishable from a correct
	// one. The single exception is the "one hundred and fourth" shape, where the
	// ordinal follows "hundred and" and spaces are correct.
	if len(fields) > 1 && !strings.ContainsAny(lowered, "-\u2011") && !hasWord(fields, "hundred") {
		for _, field := range fields {
			if _, isOrdinal := ordinals[field]; isOrdinal {
				return 0, false
			}
		}
	}
	// "and" joins two words; it cannot open or close the numeral.
	for i, field := range fields {
		if field == "and" && (i == 0 || i == len(fields)-1) {
			return 0, false
		}
	}
	// A leading or trailing separator means the numeral was not written cleanly
	// ("twenty-first-"), which would otherwise read as a correct number.
	if trimmed := strings.Trim(lowered, "-\u2011"); trimmed != lowered {
		return 0, false
	}
	total, current := 0, 0
	seen := false
	for _, field := range fields {
		switch field {
		case "hundred":
			if !seen || current == 0 {
				return 0, false
			}
			current *= 100
		case "and":
			if !seen {
				return 0, false
			}
			// "and" is additive and changes nothing.
		default:
			value, ok := cardinals[field]
			if !ok {
				// An ordinal is accepted as the final word only; the caller
				// anchors on it, so "twenty first" cannot be misread.
				value, ok = ordinals[field]
				if !ok {
					return 0, false
				}
			}
			current += value
			seen = true
		}
	}
	total += current
	if !seen {
		return 0, false
	}
	return total, true
}

// ordinalWord renders n as an ordinal. Tens are regular ("twenty" + unit), so a
// compound is the cardinal tens word, a hyphen, and the unit's ordinal form;
// the unit's own irregulars — first, second, third, fifth, eighth, ninth,
// twelfth — are the only words that are not the cardinal with "th" appended.
func ordinalWord(n int) string {
	if n < 0 || n > 99 {
		return strconv.Itoa(n)
	}
	// Below twenty every number is a single word, so it is looked up directly
	// rather than split into a tens part and a unit part — which is what made
	// nineteen render as "ten-ninth".
	if word, ok := ordinalSingle(n); ok {
		return word
	}
	unit := n % 10
	tens := n - unit
	// A round ten has no unit part: twentieth, not twenty-zeroth.
	if unit == 0 {
		if word, ok := ordinalSingle(n); ok {
			return word
		}
		return strconv.Itoa(n)
	}
	if tens == 0 {
		return cardinalUnit(unit) + "th"
	}
	unitWord := irregularOrdinals[unit]
	if unitWord == "" {
		unitWord = cardinalUnit(unit) + "th"
	}
	return cardinalUnit(tens) + "-" + unitWord
}

// readNumber finds the first number in a fragment, spelled or in digits. A
// record spells its counts in words, but past ninety-nine a human would write
// digits, and both must be readable for the gate to keep working.
func readNumber(text string) (int, bool) {
	if value, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
		return value, true
	}
	for _, field := range strings.Fields(text) {
		trimmed := strings.Trim(field, "*`,.;:()")
		if trimmed == "" {
			continue
		}
		if value, err := strconv.Atoi(trimmed); err == nil {
			return value, true
		}
	}
	for _, field := range strings.Fields(text) {
		if value, ok := parseNumber(strings.Trim(field, "*`,.;:()")); ok {
			return value, true
		}
	}
	return 0, false
}

func hasWord(fields []string, word string) bool {
	for _, field := range fields {
		if field == word {
			return true
		}
	}
	return false
}

func cardinalUnit(n int) string {
	if word, ok := unitCardinals[n]; ok {
		return word
	}
	return strconv.Itoa(n)
}

func ordinalSingle(n int) (string, bool) {
	for word, value := range ordinals {
		if value == n {
			return word, true
		}
	}
	return "", false
}

// TestNumeralParsingHasNoCeiling proves the round-20 regression cannot return.
// A gate whose comparison moved to integers while its *reading* stayed a fixed
// table failed on the twenty-first commit with a message reporting that the
// document "says 1" — and no spelling could satisfy it. Comparing integers
// while still READING a fixed table moves the ceiling rather than removing it,
// which is what that round found by mutation.
func TestNumeralParsingHasNoCeiling(t *testing.T) {
	for _, tc := range []struct {
		word string
		want int
	}{
		{"eighteen", 18}, {"nineteen", 19}, {"twenty", 20}, {"twenty-one", 21},
		{"thirty-five", 35}, {"ninety-nine", 99},
		{"one hundred", 100}, {"one hundred and four", 104},
		{"two hundred and fifty-six", 256},
		{"eighteenth", 18}, {"nineteenth", 19}, {"twentieth", 20},
		{"twenty-first", 21}, {"twenty-second", 22}, {"forty-first", 41},
		{"fifty-eighth", 58}, {"ninety-ninth", 99}, {"thirty-fifth", 35},
		{"twenty-fourth", 24}, {"twenty-sixth", 26}, {"twenty-seventh", 27},
	} {
		got, ok := parseNumber(tc.word)
		if !ok {
			t.Errorf("%q did not parse", tc.word)
			continue
		}
		if got != tc.want {
			t.Errorf("%q parsed to %d, want %d", tc.word, got, tc.want)
		}
	}
	// Every ordinal this helper produces for the range a gate can reach must
	// parse back, or a correct record would fail on correct English.
	for n := 0; n <= 99; n++ {
		word := ordinalWord(n)
		got, ok := parseNumber(word)
		if !ok {
			t.Errorf("ordinal for %d (%q) does not parse back", n, word)
			continue
		}
		if got != n {
			t.Errorf("ordinal for %d (%q) parses to %d", n, word, got)
		}
	}
	// Past ninety-nine the helper emits digits, and those must read too.
	for _, n := range []int{100, 101, 256, 999, 1234} {
		if got, ok := readNumber(ordinalWord(n)); !ok || got != n {
			t.Errorf("%d does not round-trip: got %d, ok=%v", n, got, ok)
		}
		if got, ok := readNumber(strconv.Itoa(n)); !ok || got != n {
			t.Errorf("digits for %d do not read back", n)
		}
	}
	// Nonsense must fail closed rather than parse to something plausible.
	for _, bad := range []string{"", "   ", "banana", "twenty first", "hundred", "and", "one hundred and", "the third", "twenty-first-"} {
		if got, ok := parseNumber(bad); ok {
			t.Errorf("%q parsed to %d but should not", bad, got)
		}
	}
}

// --- state-line reading -----------------------------------------------------

// reviewState is the set of figures every count-carrying document states.
type reviewState struct {
	applied     int
	reviews     int
	outstanding int
}

// stateLinePatterns builds the three figure patterns from a stage's phrasing.
// Markdown emphasis is tolerated because these documents bold their counts.
func (c stageReviewState) stateLinePatterns() (applied, reviews, outstanding *regexp.Regexp) {
	emphasis := `[*_` + "`" + `]*`
	number := `([a-z]+(?:[- ][a-z]+)*)`
	return regexp.MustCompile(number + emphasis + `\s+` + regexp.QuoteMeta(c.AppliedPhrase)),
		regexp.MustCompile(number + emphasis + `\s+` + regexp.QuoteMeta(c.ReviewsPhrase)),
		regexp.MustCompile(`[Tt]he\s+` + emphasis + number + emphasis + `\s+` + regexp.QuoteMeta(c.OutstandingPhrase))
}

// parseStateLine reads the figures from one state line. It fails closed: all
// three must be present and readable, so a document that states only some of
// them is reported rather than partially accepted.
func parseStateLine(line string, patterns [3]*regexp.Regexp) (reviewState, bool) {
	var state reviewState
	ok := true
	read := func(pattern *regexp.Regexp) int {
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			ok = false
			return 0
		}
		value, parsed := parseNumber(strings.Trim(match[1], "*_` "))
		if !parsed {
			ok = false
			return 0
		}
		return value
	}
	state.applied = read(patterns[0])
	state.reviews = read(patterns[1])
	state.outstanding = read(patterns[2])
	return state, ok
}

// stateLines returns every state line in a document.
//
// A state line must BEGIN with the marker, optionally after a list bullet. The
// first version matched the marker anywhere on the line, and immediately
// counted two state lines in a review record — because a resolution cell
// *mentions* the marker while explaining that documents must carry one. A
// mention of the marker is prose; only a line that opens with it is a claim.
// Requiring the position rather than the presence is what makes the check
// usable inside a document that explains itself.
func stateLines(doc, prefix string) []string {
	var found []string
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimSpace(trimmed)
		if strings.HasPrefix(trimmed, prefix) {
			found = append(found, line)
		}
	}
	return found
}

// --- table and section reading ----------------------------------------------

// roundsTable extracts the rounds table, so a numbered table lower in the
// document — which also has rows — is not counted as rounds.
func roundsTable(doc, heading string) string {
	start := strings.Index(doc, heading)
	if start < 0 {
		return doc
	}
	rest := doc[start:]
	if end := strings.Index(rest[1:], "\n## "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

// occurrenceRows returns the occurrence table's data rows.
func occurrenceRows(doc, anchor string) []string {
	start := strings.Index(doc, anchor)
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

// lastVerdictRound is the highest round whose row carries a verdict, or 0.
func lastVerdictRound(doc, heading string) int {
	highest := 0
	for _, line := range strings.Split(roundsTable(doc, heading), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "| ") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 4 {
			continue
		}
		// The verdict is found BY VALUE, not by column index: the Scope column
		// holds either a remediation or the words "narrow follow-up", so the
		// verdict sits at index 3 in some rows and 4 in others.
		verdict := ""
		for _, cell := range cells {
			trimmed := strings.ToLower(strings.Trim(strings.TrimSpace(cell), "*_` "))
			if trimmed == "accepted" || trimmed == "conditional" || trimmed == "rejected" {
				verdict = trimmed
				break
			}
		}
		if verdict == "" {
			continue // the pending row
		}
		number, err := strconv.Atoi(strings.TrimSpace(cells[0]))
		if err == nil && number > highest {
			highest = number
		}
	}
	return highest
}

// splitRow splits a Markdown table row into trimmed cells.
func splitRow(line string) []string {
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	var cells []string
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

// roundRows parses the rounds table into rows keyed by column name. Two bugs in
// a sibling check were found by running it: it read a column named "tally"
// while the table's column is "Findings", so the lookup was always empty and
// the check passed without ever running — a silent no-op. The key is therefore
// whatever the header says, not what a caller hopes it says.
func roundRows(table string) []map[string]string {
	var out []map[string]string
	header := []string{}
	for _, line := range strings.Split(table, "\n") {
		cells := splitRow(line)
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

// roundSections maps a round number to the severities of its finding rows.
func roundSections(doc string, rowPattern string) map[string][]string {
	out := map[string][]string{}
	pattern := regexp.MustCompile(`(?m)^## Round (\d+)\b`)
	matches := pattern.FindAllStringSubmatchIndex(doc, -1)
	severities := regexp.MustCompile(rowPattern)
	for i, match := range matches {
		number := doc[match[2]:match[3]]
		start := match[1]
		end := len(doc)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := doc[start:end]
		var found []string
		for _, row := range severities.FindAllStringSubmatch(body, -1) {
			found = append(found, row[1])
		}
		out[number] = found
	}
	return out
}

// parseTally reads a "4 P1, 6 P2, 1 P3" tally into counts. It returns nil for a
// cell that is prose rather than a tally ("5 P2 nits", "none", "—"), which is
// how the early rounds record their findings. The tally is PARSED, not
// substring-matched: an earlier version asked only whether the cell contained
// the severity letter and the expected number, which passes for "4 P1, 6 P2,
// 1 P3" against an expected 4 P2 — because the "4" of "4 P1" satisfies it.
func parseTally(cell string) map[string]int {
	matches := regexp.MustCompile(`(\d+)\s+(P[0-3])`).FindAllStringSubmatch(cell, -1)
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

// occurrenceProsePattern reads the figures printed beside the occurrence table.
// The leading character class must accept uppercase. With a lowercase-only
// class Go's regexp finds the leftmost match, which starts one character late
// and reads "Twenty-six" as "wenty-six" — a silent figure error in the checker
// itself, found by running it rather than by reading it.
const occurrenceProsePattern = `([A-Za-z]+(?:-[a-z]+)*) occurrences in remediations (\d+) to (\d+), ([a-z]+(?:-[a-z]+)*) of them self-inflicted,\s*\n\s*and the ([a-z]+(?:-[a-z]+)*) consecutive self-inflicted ones are the last ([a-z]+(?:-[a-z]+)*) rows\.`

// --- the commit-history gate ------------------------------------------------

// inFlightSuffix is set when the expected count is one ahead of the committed
// history because the record is being edited. Without it a failure message
// reads "18 remediation commits exist" when the history holds 17 and the
// eighteenth is in flight — which is the same class of misstatement this gate
// exists to catch, in the gate's own output.
var inFlightSuffix string

// recordUncommitted reports whether the review record itself has uncommitted
// changes, which is what makes a count one ahead of the history legitimate
// rather than merely wrong. It is keyed on THIS record, not on the whole tree:
// keyed on the tree, a correct record failed whenever any tracked file was
// dirty, which would fire on every unrelated edit.
func recordUncommitted(root, review string) bool {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--", review).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// remediationCommits walks the history from the rejected candidate and returns
// the commits that are remediations.
func remediationCommits(root, candidate, subjectPrefix string, bookkeeping []string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "log", "--format=%H %s", candidate+"..HEAD").Output()
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && isRemediation(parts[1], subjectPrefix, bookkeeping) {
			shas = append(shas, parts[0])
		}
	}
	return shas, nil
}

// isRemediation decides whether a commit after the rejected candidate is a
// remediation. Two earlier shapes failed, in opposite directions: enumerating
// the remediation prefixes under-counted the moment a remediation used an
// unseen subject — which happened at once, on the commit that introduced the
// gate — while excluding named subjects and counting everything else counted
// every commit that merely touched the branch, and invalidated the count in
// five documents when a workflow change landed. The rule is therefore
// INCLUSIVE on the stage prefix and EXCLUSIVE only on the named bookkeeping
// commits. The residual weakness — that a new bookkeeping commit must be added
// by hand — is stated in the generated manifest rather than here.
func isRemediation(subject, subjectPrefix string, bookkeeping []string) bool {
	if !strings.HasPrefix(subject, subjectPrefix) {
		return false
	}
	for _, excluded := range bookkeeping {
		if subject == excluded {
			return false
		}
	}
	return true
}

// expectedApplied is the count the record must state, and why.
//
// The gate runs in two states, and must be right in both. Committed, the
// history holds every remediation. Authoring, the next remediation exists in
// the working tree but not in history. So the document's count must equal the
// history count, **or** that count plus one when the record itself carries an
// uncommitted change. The tolerance is one and only one, and it exists only
// while there is something uncommitted — which is what stops it from becoming
// the off-by-one the gate exists to prevent.
func expectedApplied(root, review string, applied int) int {
	if recordUncommitted(root, review) {
		inFlightSuffix = fmt.Sprintf(", and a %s remediation is uncommitted, so %d are in total",
			ordinalWord(applied+1), applied+1)
		return applied + 1
	}
	return applied
}

// TestStageReviewStateCountsMatchHistory is the count gate: the applied count
// includes the commit being written, so it equals the number of remediation
// commits after the rejected candidate; the reviews figure is the rounds table's
// reviewed rows; the outstanding ordinal is the applied ordinal, because the
// outstanding review reviews the newest commit. Each count-carrying document
// must carry exactly one state line stating all three, so coverage is required
// rather than attempted, and the rounds table must carry one row per review plus
// one whose pending row names no commit and does name the outstanding ordinal.
func TestStageReviewStateCountsMatchHistory(t *testing.T) {
	for _, cfg := range stageReviewStates {
		t.Run(cfg.Stage, func(t *testing.T) {
			checkStageReviewStateCounts(t, cfg)
		})
	}
}

func checkStageReviewStateCounts(t *testing.T, cfg stageReviewState) {
	t.Helper()
	root := repoRoot
	inFlightSuffix = ""
	shas, err := remediationCommits(root, cfg.RejectedCandidate, cfg.SubjectPrefix, cfg.BookkeepingSubjects)
	if err != nil {
		t.Skipf("git history unavailable (%v); this gate reads the repository", err)
	}
	if len(shas) == 0 {
		t.Fatal("no remediation commits found; the history walk is broken")
	}
	applied := len(shas)
	expected := expectedApplied(root, cfg.Record, applied)

	raw, err := os.ReadFile(filepath.Join(root, cfg.Record))
	if err != nil {
		// Closing the stage moves this record to docs/history/. The gate has
		// nothing left to check at that point, and failing closed would leave
		// `make check` red for the life of the repository over a closed
		// stage's artifact. Skipping with a named reason is the honest
		// outcome: the check was live while the slice was open, and it goes
		// with the slice. The checker must be DELETED in the same change that
		// moves the record, or this skip becomes permanent silence.
		t.Skipf("%s is gone (%v): the stage is closed and its review record archived, so there is no count left to verify", cfg.Record, err)
	}
	table := roundsTable(string(raw), cfg.RoundsHeading)

	rows := regexp.MustCompile(`(?m)^\|\s*(\d+)\s*\|`).FindAllStringSubmatch(table, -1)
	reviewed := regexp.MustCompile(`(?m)^\|\s*\d+\s*\|.*\|.*\|\s*\**\s*(accepted|conditional|rejected)\s*\**\s*\|`).FindAllString(table, -1)
	if len(reviewed) > len(rows) {
		t.Fatalf("%d rounds carry a verdict but only %d rows exist", len(reviewed), len(rows))
	}
	if want := len(reviewed) + 1; len(rows) != want {
		t.Errorf("rounds table has %d rows; %d is required (%d reviewed + 1 pending)",
			len(rows), want, len(reviewed))
	}
	// At least one applied remediation must be unreviewed — the commit being
	// written is itself one — or the record could never be describing a
	// candidate under review.
	if remediationReviews := len(reviewed) - cfg.NonRemediationRounds; remediationReviews >= expected {
		t.Errorf("%d remediation reviews have run over %d remediation commits; at least one must be unreviewed",
			remediationReviews, expected)
	}

	// The claims the rounds keep getting wrong, checked in EVERY document that
	// carries the count — not only the review record. Scope matters: each claim
	// is read from the line that states it, because a first-match-in-the-file
	// search reads a historical quote as a live claim. Coverage matters more:
	// four consecutive rounds returned a P1 that was a figure in one of the
	// *other* documents, invisible to a gate that read only the review record.
	// A mechanism that covers one of five documents lets the class live in the
	// other four, so it is checked in all of them.
	want := reviewState{
		applied:     expected,
		reviews:     len(reviewed),
		outstanding: expected,
	}
	appliedPattern, reviewsPattern, outstandingPattern := cfg.stateLinePatterns()
	arr := [3]*regexp.Regexp{appliedPattern, reviewsPattern, outstandingPattern}
	for _, other := range cfg.CountCarryingDocuments {
		body, err := os.ReadFile(filepath.Join(root, other))
		if err != nil {
			t.Errorf("cannot read %s, which the count gate must cover: %v", other, err)
			continue
		}
		lines := stateLines(string(body), cfg.StateLinePrefix)
		if len(lines) != 1 {
			t.Errorf("%s carries %d %q lines; exactly one is required, so this document's state is machine-checkable",
				other, len(lines), cfg.StateLinePrefix)
			continue
		}
		state, ok := parseStateLine(lines[0], arr)
		if !ok {
			t.Errorf("%s's state line does not state all three figures (applied, reviews run, outstanding): %q",
				other, lines[0])
			continue
		}
		for _, figure := range []struct {
			name     string
			got, exp int
		}{
			{"applied remediations", state.applied, want.applied},
			{"reviews run", state.reviews, want.reviews},
			{"outstanding remediation", state.outstanding, want.outstanding},
		} {
			if figure.got != figure.exp {
				t.Errorf("%s states %d %s; %d remediation commits exist%s, so it must state %d",
					other, figure.got, figure.name, applied, inFlightSuffix, figure.exp)
			}
		}
	}
	// The rounds table must carry one row per review plus the pending one.
	//
	// The pending row must NOT name a commit: it reviews the commit being
	// written, which does not exist while it is being written, so a row carrying
	// an older SHA points the next reviewer at a range that omits the fix.
	//
	// It MUST name the outstanding ordinal, and that is a separate assertion from
	// the one above. One round weakened this commit by replacing it, so it is
	// asserted separately and explicitly rather than as a side effect.
	if !regexp.MustCompile(`(?m)^\|\s*\d+\s*\|[^\n]*pending`).MatchString(table) {
		t.Errorf("no pending row in the rounds table; one is required for the %d remediation", expected)
	}
	pendingRow := regexp.MustCompile(`(?m)^\|\s*\d+\s*\|\s*([^|]*?)\s*\|\s*([^|]*?)\s*\|[^\n]*pending`).FindStringSubmatch(table)
	if pendingRow == nil {
		t.Errorf("cannot read the pending row's candidate and scope cells")
	} else {
		if candidate := strings.TrimSpace(pendingRow[1]); candidate != "\u2014" && candidate != "-" {
			t.Errorf("pending row names commit %s, but a pending review cannot name the commit it reviews: it does not exist yet", candidate)
		}
		// Compared as a number, word or digits. A word-only comparison cannot
		// survive past ninety-nine, where this helper stops producing prose —
		// which would re-introduce the ceiling the ordinal assertion exists to
		// remove, in the very assertion added to remove it.
		// Scope cells carry a "(covering N–M)" note when one remediation spans
		// several commits. Read the ordinal *before* it: taking the first
		// number in the cell would read "covering 10" and report a mismatch
		// that is not there.
		scope := strings.TrimSpace(pendingRow[2])
		if paren := strings.Index(scope, "("); paren >= 0 {
			scope = strings.TrimSpace(scope[:paren])
		}
		scopeNumber, ok := readNumber(scope)
		if !ok {
			t.Errorf("pending row's scope %q carries no readable number; it must name the outstanding remediation, the %s",
				scope, ordinalWord(expected))
		} else if scopeNumber != expected {
			t.Errorf("pending row's scope names remediation %d; the outstanding one is the %s",
				scopeNumber, ordinalWord(expected))
		}
	}
}

// TestStateLineParsingRejectsEveryMutation is the battery: it covers the
// mutations a reviewer would actually apply. A wrong value must still PARSE,
// because that is what lets the gate compare it against the git history and
// report a mismatch; what must fail closed is a structurally broken line, since
// then there is nothing to compare. One round of one stage rejected this gate
// because a resolution cell claimed "verified by corrupting each in turn: every
// one fails" when only three of five documents had ever been corrupted, and the
// next found the fifth contributed no assertion at all because it never matched
// the pattern — so the battery is a test here rather than a claim in prose.
func TestStateLineParsingRejectsEveryMutation(t *testing.T) {
	for _, cfg := range stageReviewStates {
		t.Run(cfg.Stage, func(t *testing.T) {
			appliedPattern, reviewsPattern, outstandingPattern := cfg.stateLinePatterns()
			arr := [3]*regexp.Regexp{appliedPattern, reviewsPattern, outstandingPattern}

			// Every fixture below is BUILT from the configured marker and the
			// configured phrasing, so the battery is a property of the parser
			// rather than of one stage's vocabulary. The figures are
			// deliberately in the twenties: past twenty is the ceiling this
			// battery's reader had once, so fixtures beyond it would let a
			// reintroduced ceiling pass unnoticed here.
			line := func(applied, reviews, outstanding string) string {
				return cfg.StateLinePrefix + " " + applied + " " + cfg.AppliedPhrase +
					"; " + reviews + " " + cfg.ReviewsPhrase +
					"; the " + outstanding + " " + cfg.OutstandingPhrase + "."
			}
			valid := line("twenty-one", "twenty-two", "twenty-first")

			// The line as written must parse to exactly what it says.
			state, ok := parseStateLine(valid, arr)
			if !ok {
				t.Fatalf("the canonical state line does not parse: %q", valid)
			}
			if state.applied != 21 || state.reviews != 22 || state.outstanding != 21 {
				t.Fatalf("parsed %+v from %q, want applied=21 reviews=22 outstanding=21", state, valid)
			}

			t.Run("accepted forms", func(t *testing.T) {
				bold := func(figure string) string { return "**" + figure + "**" }
				for _, accepted := range []string{
					valid,
					// Markdown emphasis is tolerated, because the documents
					// bold their counts.
					line(bold("twenty-one"), bold("twenty-two"), bold("twenty-first")),
					// A list bullet before the marker is tolerated.
					"- " + valid,
					// A prefix on the same line is tolerated: only the figures
					// are read, and a line that does not OPEN with the marker
					// is not a state line at all.
					"Some prefix. " + valid,
				} {
					if _, ok := parseStateLine(accepted, arr); !ok {
						t.Errorf("did not parse, though it is a valid state line: %q", accepted)
					}
				}
			})

			t.Run("wrong values parse, and are therefore comparable", func(t *testing.T) {
				for name, tc := range map[string]struct {
					line  string
					which string
					want  int
				}{
					"applied wrong":     {line("twenty", "twenty-two", "twenty-first"), "applied", 20},
					"reviews wrong":     {line("twenty-one", "twenty", "twenty-first"), "reviews", 20},
					"outstanding wrong": {line("twenty-one", "twenty-two", "twentieth"), "outstanding", 20},
				} {
					got, ok := parseStateLine(tc.line, arr)
					if !ok {
						t.Errorf("%s: did not parse, so a wrong value would be invisible rather than reported", name)
						continue
					}
					var value int
					switch tc.which {
					case "applied":
						value = got.applied
					case "reviews":
						value = got.reviews
					case "outstanding":
						value = got.outstanding
					}
					if value != tc.want {
						t.Errorf("%s: read %s as %d, want %d", name, tc.which, value, tc.want)
					}
				}
			})

			t.Run("structurally broken lines fail closed", func(t *testing.T) {
				for name, broken := range map[string]string{
					"applied non-number":      line("many", "twenty-two", "twenty-first"),
					"applied deleted":         line("", "twenty-two", "twenty-first"),
					"reviews deleted":         line("twenty-one", "", "twenty-first"),
					"outstanding deleted":     cfg.StateLinePrefix + " twenty-one " + cfg.AppliedPhrase + "; twenty-two " + cfg.ReviewsPhrase + "; the " + cfg.OutstandingPhrase + ".",
					"digits instead of words": cfg.StateLinePrefix + " 21 " + cfg.AppliedPhrase + "; 22 " + cfg.ReviewsPhrase + "; the 21st " + cfg.OutstandingPhrase + ".",
					// The shape that defeated a whole-file first-match scan: a
					// foreign stage's sentence carrying all three figures.
					"prefix swallowing": "A different stage had four " + cfg.AppliedPhrase +
						"; the count was twenty-two " + cfg.ReviewsPhrase +
						"; the twenty-first " + cfg.OutstandingPhrase + ".",
				} {
					if _, ok := parseStateLine(broken, arr); ok {
						t.Errorf("%s: parsed %q, which must be rejected", name, broken)
					}
				}
			})

			t.Run("a mention of the marker is not a state line", func(t *testing.T) {
				// The review record explains that documents must carry a state
				// line, so the marker's own text appears in prose. Matching on
				// presence rather than position counted that explanation as a
				// second claim.
				doc := "Each document must carry a `" + cfg.StateLinePrefix + "` line.\n" + valid + "\n"
				lines := stateLines(doc, cfg.StateLinePrefix)
				if len(lines) != 1 {
					t.Fatalf("found %d state lines, want 1; a mention of the marker in prose was counted: %q", len(lines), lines)
				}
			})

			t.Run("quotes and foreign stages are not state lines", func(t *testing.T) {
				// These are the shapes that defeated whole-file first-match
				// scanning. They must contribute no state line at all, so they
				// cannot be read as the document's claim.
				doc := "| round 21 exit | twenty-one " + cfg.AppliedPhrase + " |\n" +
					"A different stage had four " + cfg.AppliedPhrase + ".\n" +
					"the count was twenty " + cfg.AppliedPhrase + " in the previous round\n" +
					valid + "\n"
				lines := stateLines(doc, cfg.StateLinePrefix)
				if len(lines) != 1 {
					t.Fatalf("found %d state lines, want exactly 1: %q", len(lines), lines)
				}
				if got, ok := parseStateLine(lines[0], arr); !ok || got.applied != 21 {
					t.Errorf("the surviving state line parsed as %+v (ok=%v); a preceding quote was read instead", got, ok)
				}
			})

			t.Run("missing and duplicated lines", func(t *testing.T) {
				if lines := stateLines("nothing here\njust prose\n", cfg.StateLinePrefix); len(lines) != 0 {
					t.Errorf("found %d state lines in a document with none", len(lines))
				}
				if lines := stateLines(valid+"\n"+valid+"\n", cfg.StateLinePrefix); len(lines) != 2 {
					t.Errorf("found %d state lines, want 2 so the caller can reject the duplication", len(lines))
				}
			})
		})
	}
}

// TestOccurrenceTableFiguresMatchTheirSource compares the occurrence table's
// prose against the table it sits beside, and the table's last row against the
// rounds table's most recent verdict. Both halves are derived: two rounds of
// one stage found the prose figures typed rather than derived, and three more
// found the enumeration short of the very rounds that motivated the commit —
// the last of them a P0 — because the checker compared the prose with the table
// and never the table with the rounds table.
func TestOccurrenceTableFiguresMatchTheirSource(t *testing.T) {
	for _, cfg := range stageReviewStates {
		t.Run(cfg.Stage, func(t *testing.T) {
			root := repoRoot
			raw, err := os.ReadFile(filepath.Join(root, cfg.Record))
			if err != nil {
				t.Fatalf("read %s: %v", cfg.Record, err)
			}
			doc := string(raw)

			rows := occurrenceRows(doc, cfg.OccurrenceAnchor)
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

			prose := regexp.MustCompile(occurrenceProsePattern).FindStringSubmatch(doc)
			if prose == nil {
				t.Fatalf("no occurrence prose matching the expected shape; the figures beside the table cannot be checked.\nWanted a sentence like:\n  Occurrences in rounds 10 to 24, twenty of them self-inflicted, and the twenty\n  consecutive self-inflicted ones are the last twenty rows.")
			}
			statedTotal, ok0 := parseNumber(prose[1])
			statedSelfInflicted, ok1 := parseNumber(prose[4])
			statedTailLength, ok2 := parseNumber(prose[5])
			statedTailRows, ok3 := parseNumber(prose[6])
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

			// The last row's remediation ordinal is the span's end.
			tableLastRound, err := strconv.Atoi(strings.TrimSpace(strings.Split(rows[len(rows)-1], "|")[1]))
			if err != nil {
				t.Fatalf("cannot read the last row's round: %v", err)
			}
			// The enumeration must reach the most recent review round. It reads
			// column TWO, not column one. Column one is the ordinal of the
			// remediation that introduced the row; column two is the review
			// round that found it. Comparing column one against a review round
			// was this check's own first version, and it failed immediately on
			// a correct table.
			lastVerdict := lastVerdictRound(doc, cfg.RoundsHeading)
			// Column two reads "28 (R28F1)" — the review round that found it,
			// plus the finding identifier. The leading integer is the round.
			reviewRoundCell := strings.TrimSpace(strings.Split(rows[len(rows)-1], "|")[2])
			reviewRoundField := strings.Fields(reviewRoundCell)
			if len(reviewRoundField) == 0 {
				t.Fatalf("the last occurrence row carries no review round: %q", rows[len(rows)-1])
			}
			tableLastReviewRound, err := strconv.Atoi(reviewRoundField[0])
			if err != nil {
				t.Fatalf("cannot read the last row's review round from %q: %v", reviewRoundCell, err)
			}
			if lastVerdict != 0 && tableLastReviewRound != lastVerdict {
				t.Errorf("the occurrence table's last row records review round %d, but round %d has returned a verdict. The enumeration claims to cover every occurrence of this fault in this stage, and the most recent rounds each found one",
					tableLastReviewRound, lastVerdict)
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
				{"first remediation in the span", spanFirst, tableFirstRound},
				{"last remediation in the span", spanLast, tableLastRound},
			} {
				if check.got != check.want {
					t.Errorf("the occurrence prose states %d %s; the table has %d. This is the finding that recurred across rounds: the figures beside the table were typed rather than derived",
						check.got, check.what, check.want)
				}
			}
		})
	}
}

// understatedCoveragePhrases are the exact phrasings that were true once and
// false later, each with the kind of claim it made. A document describing the
// checker may say what it covers, but must not claim the coverage is partial —
// six consecutive rounds of one stage rejected it for one shape: a document
// asserting something about the gate that the gate did not do. Editing each
// such statement by hand fixes it until the next mechanism change, which is how
// one round falsified the same claim one round after another had corrected it,
// so the statements are checked instead.
//
// "ungated" is legitimate in a sentence that scopes it to prose, so that match
// is allowed when the sentence says so.
var understatedCoveragePhrases = []struct {
	phrase string
	found  string
}{
	{"for this one file", "claimed the gate verified only the review record"},
	{"this one record", "claimed the gate verified only the review record"},
	{"the other four documents", "claimed the four other documents were unchecked"},
	{"are ungated", "claimed the other documents are unchecked"},
	{"is not gated", "claimed a document is unchecked"},
	{"only thing read", "claimed the state line was the gate's sole input (false: it also reads the rounds table and the pending row)"},
}

const coverageAllowedQualifier = "prose"

// TestNoDocumentUnderstatesTheGateCoverage reads every Markdown file under
// docs/ and fails on a live line claiming the checker covers less than it does,
// so the next coverage change cannot leave a stale claim standing.
//
// One region is exempt: a historical finding row quotes the claim as it stood
// when the finding was raised. That is evidence, not a live assertion, and
// rewriting it would destroy the audit trail those rounds preserved.
//
// A second exemption is narrower and exists only because the generated manifest
// lists this very phrase set, so a reader can see what the check catches: a
// phrase that appears ONLY inside inline code spans, and only in a generated
// manifest, is a quotation of the checker's own list rather than a claim about
// the checker. Outside a generated manifest an inline code span is matched as
// before, so nothing is weakened where prose actually lives.
func TestNoDocumentUnderstatesTheGateCoverage(t *testing.T) {
	root := repoRoot
	inlineCode := regexp.MustCompile("`[^`]*`")
	generated := map[string]bool{}
	for _, cfg := range stageReviewStates {
		generated[filepath.ToSlash(cfg.Manifest)] = true
	}
	err := filepath.Walk(filepath.Join(root, "docs"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		quotedList := generated[filepath.ToSlash(relative)]
		for _, line := range strings.Split(string(body), "\n") {
			if isQuotedFindingRow(line, stageReviewStates) {
				continue
			}
			for _, rule := range understatedCoveragePhrases {
				if !strings.Contains(line, rule.phrase) {
					continue
				}
				if rule.phrase == "are ungated" && strings.Contains(line, coverageAllowedQualifier) {
					continue
				}
				if quotedList && !strings.Contains(inlineCode.ReplaceAllString(line, ""), rule.phrase) {
					continue // the manifest quoting its own phrase list
				}
				t.Errorf("%s: a document understates what the checker covers — %q (this %s)",
					relative, strings.TrimSpace(line), rule.found)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
}

func isQuotedFindingRow(line string, cfgs []stageReviewState) bool {
	trimmed := strings.TrimSpace(line)
	for _, cfg := range cfgs {
		if strings.HasPrefix(trimmed, cfg.findingRowPrefix()) {
			return true
		}
	}
	return false
}

// TestRoundTalliesMatchTheirSections derives every rounds-table Findings tally
// from its own section, so a tally cannot be typed wrongly. Two bugs in this
// check were found by running it: it first read a column named "tally" while
// the table's column is "Findings", so the lookup was always empty and the check
// passed without ever running — a silent no-op — and it then compared the
// expected number by substring, which "4 P1, 6 P2, 1 P3" satisfies for an
// expected 4 P2 because the "4" of "4 P1" is present. Rounds before the
// configured boundary record only their P0 and P1 as rows and state their P2/P3
// counts in prose, so their tallies are not derivable and are skipped.
func TestRoundTalliesMatchTheirSections(t *testing.T) {
	for _, cfg := range stageReviewStates {
		t.Run(cfg.Stage, func(t *testing.T) {
			root := repoRoot
			raw, err := os.ReadFile(filepath.Join(root, cfg.Record))
			if err != nil {
				t.Fatalf("read the review record: %v", err)
			}
			doc := string(raw)

			table := roundsTable(doc, cfg.RoundsHeading)
			rows := roundRows(table)
			if len(rows) == 0 {
				t.Fatal("no rounds table rows found")
			}
			sections := roundSections(doc, cfg.findingRowPattern())
			if len(sections) == 0 {
				t.Fatal("no round sections found")
			}

			for _, row := range rows {
				number, err := strconv.Atoi(strings.TrimSpace(row["Round"]))
				if err != nil {
					continue
				}
				if number < cfg.FirstFullyEnumeratedRound {
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
				got := parseTally(row["Findings"])
				if got == nil {
					continue // an early round whose tally is prose ("5 P2 nits", "none")
				}
				for _, severity := range []string{"P0", "P1", "P2", "P3"} {
					if got[severity] != want[severity] {
						t.Errorf("rounds table row %d states %d %s in %q, but its section holds %d",
							number, got[severity], severity, row["Findings"], want[severity])
					}
				}
			}
		})
	}
}

// TestNoLiveRestatementOfTheStageFigures reads every count-carrying document for
// the live-claim shapes that actually caused a blocking finding, excluding the
// pointers and quoted examples that are the required replacement. This is
// deliberately NOT a general rule about locations: a location rule was written
// and deleted because it fired on eleven sentences of correct explanatory prose.
// What closes the class is the deletion of the restatements, done in the same
// change; this check catches the next author reintroducing one, and it is honest
// that a sufficiently different phrasing would evade it.
func TestNoLiveRestatementOfTheStageFigures(t *testing.T) {
	for _, cfg := range stageReviewStates {
		t.Run(cfg.Stage, func(t *testing.T) {
			root := repoRoot
			// A POINTER to the state line is the required replacement, not a
			// restatement, so it is excluded before matching — otherwise the
			// check fails on the very sentences that fix the defect it exists to
			// prevent. The pointer phrase wraps across lines in these documents,
			// so it is matched on its head ("named in the") as well as its tail.
			pointer := regexp.MustCompile(`(?i)named in the|state line's|stated (?:above|below)|\bstate line\b`)
			// Inline code spans are quoted examples, not claims: a review record
			// quotes a figure while explaining the gate that checks it.
			inlineCode := regexp.MustCompile("`[^`]*`")
			live := make([]*regexp.Regexp, 0, len(cfg.LiveClaimShapes))
			for _, shape := range cfg.LiveClaimShapes {
				live = append(live, regexp.MustCompile(shape))
			}
			// Regions where a figure is expected and is not a restatement to be
			// policed: the state line itself, and the historical record's own
			// tables and findings.
			allowed := func(line string) bool {
				trimmed := strings.TrimSpace(line)
				return strings.HasPrefix(trimmed, cfg.StateLinePrefix) ||
					strings.HasPrefix(trimmed, cfg.findingRowPrefix()) ||
					strings.HasPrefix(trimmed, "| ") // rounds and occurrence tables
			}

			for _, relative := range cfg.CountCarryingDocuments {
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
								"  Delete it and point at the state line instead.",
								relative, number+1, strings.TrimSpace(line))
							break
						}
					}
				}
			}
		})
	}
}

// --- the generated manifest -------------------------------------------------

//go:embed manifests/stage-review.tmpl
var stageReviewManifestTemplate string

// updateGeneratedManifests is set by `-update`, which `make manifest` passes.
// Without it the test compares the committed manifest with the generated one, so
// `make docs-check` — which runs this package — fails when it is stale.
var updateGeneratedManifests = flag.Bool("update", false,
	"rewrite the generated stage review manifests instead of checking them")

// TestGeneratedManifestsAreCurrent regenerates each configured stage's manifest
// and fails if the committed one differs. The manifest is the single place a
// reader is told what the checker does; it used to be five documents' prose, and
// nineteen rounds of one stage each found a place a hand-edit was missed.
func TestGeneratedManifestsAreCurrent(t *testing.T) {
	for _, cfg := range stageReviewStates {
		t.Run(cfg.Stage, func(t *testing.T) {
			generated, err := renderStageReviewManifest(cfg)
			if err != nil {
				t.Fatalf("render %s: %v", cfg.Manifest, err)
			}
			path := repoPath(cfg.Manifest)
			if *updateGeneratedManifests {
				if err := os.WriteFile(path, []byte(generated), 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}
			committed, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if string(committed) != generated {
				t.Errorf("%s is stale; run `make manifest` and commit the result. First difference:\n%s",
					cfg.Manifest, firstDifference(string(committed), generated))
			}
		})
	}
}

// firstDifference reports the first line that differs, so a staleness failure
// names a place rather than making the reader diff two files by hand.
func firstDifference(committed, generated string) string {
	was, now := strings.Split(committed, "\n"), strings.Split(generated, "\n")
	for i := 0; i < len(was) || i < len(now); i++ {
		var a, b string
		if i < len(was) {
			a = was[i]
		}
		if i < len(now) {
			b = now[i]
		}
		if a != b {
			return fmt.Sprintf("  line %d:\n    committed: %q\n    generated: %q", i+1, a, b)
		}
	}
	return "  (files differ only in trailing bytes)"
}

// checkerCheck is one check function in the manifest, with the first paragraph
// of its doc comment as its description. Reading the description from the source
// is the point: a manifest that restated the checks by hand is the defect this
// mechanism exists to remove.
type checkerCheck struct {
	Name   string
	Covers string
}

// renderStageReviewManifest produces the manifest for one configured stage.
func renderStageReviewManifest(cfg stageReviewState) (string, error) {
	file := thisCheckerFile()
	source, err := os.ReadFile(filepath.Join(repoRoot, file))
	if err != nil {
		return "", err
	}
	checks, err := checkerChecks(file)
	if err != nil {
		return "", err
	}
	parsed, err := template.New("manifest").Parse(stageReviewManifestTemplate)
	if err != nil {
		return "", err
	}
	// Paths are normalised to slashes so the committed manifest is identical on
	// every host; the configuration is written with filepath.Join for the checks
	// that open files, and a Windows-generated manifest would otherwise differ
	// from a Linux one and report itself stale.
	documents := make([]string, 0, len(cfg.CountCarryingDocuments))
	for _, document := range cfg.CountCarryingDocuments {
		documents = append(documents, filepath.ToSlash(document))
	}
	data := struct {
		Stage             string
		Record            string
		RecordBase        string
		Manifest          string
		StateLine         string
		SubjectPrefix     string
		AppliedPhrase     string
		ReviewsPhrase     string
		OutstandingPhrase string
		Candidate         string
		EarlyRounds       int
		FirstRound        int
		RoundsHeading     string
		OccurrenceHead    string
		Documents         []string
		Bookkeeping       []string
		LiveShapes        []string
		CheckerFile       string
		CheckerLines      int
		Checks            []checkerCheck
		ManifestTemplate  string
	}{
		Stage:             cfg.Stage,
		Record:            filepath.ToSlash(cfg.Record),
		RecordBase:        path.Base(filepath.ToSlash(cfg.Record)),
		Manifest:          filepath.ToSlash(cfg.Manifest),
		StateLine:         cfg.StateLinePrefix,
		SubjectPrefix:     cfg.SubjectPrefix,
		AppliedPhrase:     cfg.AppliedPhrase,
		ReviewsPhrase:     cfg.ReviewsPhrase,
		OutstandingPhrase: cfg.OutstandingPhrase,
		Candidate:         cfg.RejectedCandidate,
		EarlyRounds:       cfg.NonRemediationRounds,
		FirstRound:        cfg.FirstFullyEnumeratedRound,
		RoundsHeading:     cfg.RoundsHeading,
		OccurrenceHead:    cfg.OccurrenceAnchor,
		Documents:         documents,
		Bookkeeping:       cfg.BookkeepingSubjects,
		LiveShapes:        cfg.LiveClaimShapes,
		CheckerFile:       file,
		CheckerLines:      countLines(string(source)),
		Checks:            checks,
		ManifestTemplate:  filepath.ToSlash(filepath.Join(filepath.Dir(file), "manifests", "stage-review.tmpl")),
	}
	var out strings.Builder
	if err := parsed.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// thisCheckerFile is this file, relative to the repository root. It is derived
// rather than configured, so the manifest cannot name a file that was renamed.
func thisCheckerFile() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "unknown"
	}
	if root, err := filepath.Abs(repoRoot); err == nil {
		if relative, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(relative, "..") {
			return filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(filepath.Base(file))
}

// checkerChecks parses the checker source and returns every check function with
// the first paragraph of its doc comment.
func checkerChecks(file string) ([]checkerCheck, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, file), nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var checks []checkerCheck
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Doc == nil {
			continue
		}
		checks = append(checks, checkerCheck{
			Name:   fn.Name.Name,
			Covers: dropLeadingName(firstParagraph(fn.Doc.Text()), fn.Name.Name),
		})
	}
	return checks, nil
}

// firstParagraph collapses a doc comment's leading paragraph onto one line,
// dropping the section headings a longer comment may carry after it.
func firstParagraph(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		kept = append(kept, line)
	}
	joined := strings.Join(kept, " ")
	joined = strings.TrimSuffix(joined, ".")
	return strings.Join(strings.Fields(joined), " ")
}

// dropLeadingName removes the function name a Go doc comment opens with, so the
// manifest's coverage column reads as prose rather than repeating the heading
// beside it.
func dropLeadingName(summary, name string) string {
	return strings.TrimPrefix(summary, name+" ")
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n")
}

// TestResolutionCellsClaimingChecksCiteATest requires a resolution cell that claims
// what a check covers to name a test.
//
// Restored here rather than left deleted. It was defined, removed without mention
// anywhere in the repository, and round 28 found that the removal left a resolution
// cell asserting a check that no longer existed — the claim outlived the code. The
// check is the thing that would have caught it, and its absence is what let the
// dangling reference stand, so it earns its place back.
//
// The trigger names the GATE, not a pronoun: an earlier version accepted a bare
// "it", which matched any cell containing the word "it" anywhere and fired on two
// legitimate round-9 cells about a UI check covering a rendered string.
func TestResolutionCellsClaimingChecksCiteATest(t *testing.T) {
	claimsCoverage := regexp.MustCompile(`(?i)\b(verif(?:y|ies|ied)|assert(?:s|ed)?|check(?:s|ed)?|cover(?:s|ed|ing)?|gated|ungated)\b[^|]*\b(the gate|this gate|the count gate|its coverage|its scope)\b[^|]*`)
	namesATest := regexp.MustCompile("Test[A-Za-z0-9]+|`[a-z_]+_test\\.go`")

	for _, stage := range stageReviewStates {
		raw, err := os.ReadFile(filepath.Join(repoRoot, stage.Record))
		if err != nil {
			t.Skipf("read %s: %v", stage.Record, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "| 6.3-R") {
				continue
			}
			cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
			if len(cells) < 4 {
				continue
			}
			resolution := cells[3]
			if !claimsCoverage.MatchString(resolution) || namesATest.MatchString(resolution) {
				continue
			}
			t.Errorf("a resolution cell claims what a check covers without naming one: %.180s", resolution)
		}
	}
}
