package doccheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Stage 6.3's review record needed nine rounds to learn that prose discipline
// cannot keep a count correct, because each remediation is itself a new commit:
// correcting the count in remediation N makes it stale again in N+1. Rounds 15,
// 16, 17 and 18 each returned that same finding, and round 18 escalated to
// REJECTED because correcting a count had begun editing the audit trail of the
// rounds it was counting.
//
// This is the mechanical gate the record asked for from round 13 onward. It
// reads the commit history and derives the numbers rather than trusting them.
//
// The invariants, stated once so the arithmetic below is checkable:
//
//   - The **applied** count includes the commit being written, so it equals
//     the number of remediation commits on the branch after the rejected
//     candidate.
//   - Rounds 1–9 reviewed the eight checkpoints and then the candidate itself;
//     they are not remediation reviews. Every round from 10 onward reviews one
//     or more remediations, so the number of remediation reviews is the reviewed
//     count less 9.
//   - At least one applied remediation is always unreviewed: the commit being
//     written is itself one. There may be more than one, because a remediation
//     can be committed and then refined before the review that covers both —
//     which is what happened twice here when the gate below was itself wrong.
//   - The **outstanding** ordinal is the applied ordinal, because the
//     outstanding review is the one that will review the newest commit.
//   - The rounds table carries exactly one row per review plus one row for the
//     pending review, so rows = reviewed + 1.
//   - The gate runs in two states, and must be right in both. Committed, the
//     history holds every remediation. Authoring, the next remediation exists in
//     the working tree but not in history. So the document's count must equal the
//     history count, **or** that count plus one when the working tree carries an
//     uncommitted change. The tolerance is one and only one, and it exists only
//     while there is something uncommitted — which is what stops it from
//     becoming the off-by-one the gate exists to prevent.
const stage63RejectedCandidate = "f4196fb"

// stage63NonRemediationRounds is the number of early rounds that did not
// review a remediation: eight per-checkpoint rounds plus the candidate review.
const stage63NonRemediationRounds = 9

// stage63InFlightSuffix is set when the expected count is one ahead of the
// committed history because this record is being edited. Without it a failure
// message reads "18 remediation commits exist" when the history holds 17 and the
// eighteenth is in flight — which is the same class of misstatement this gate
// exists to catch, in the gate's own output.
var stage63InFlightSuffix = ""

// stage63Expected is the count the record must state, and why.
func stage63Expected(root, review string, applied int) int {
	if stage63RecordUncommitted(root, review) {
		stage63InFlightSuffix = fmt.Sprintf(", and a %s remediation is uncommitted, so %d are in total", stage63OrdinalWord(applied+1), applied+1)
		return applied + 1
	}
	return applied
}

func TestStage63ReviewRecordCountsMatchHistory(t *testing.T) {
	root := repoRoot
	review := filepath.Join("docs", "research", "stage-6", "6.3-review.md")
	shas, err := stage63RemediationCommits(root)
	if err != nil {
		t.Skipf("git history unavailable (%v); this gate reads the repository", err)
	}
	if len(shas) == 0 {
		t.Fatal("no remediation commits found; the history walk is broken")
	}
	applied := len(shas)
	// A remediation being authored exists in the working tree and not yet in
	// history, so the record's count may legitimately be one ahead — but only
	// while *this record* is uncommitted. Keying the tolerance on the whole
	// tree instead made a correct record fail whenever any tracked file was
	// dirty, which would fire on every unrelated edit; keying it on the record
	// ties the tolerance to the only file whose count can change.
	expected := stage63Expected(root, review, applied)

	raw, err := os.ReadFile(filepath.Join(root, review))
	if err != nil {
		// 6.3 closes by moving this record to docs/history/. The gate has
		// nothing left to check at that point, and failing closed would leave
		// `make check` red for the life of the repository over a Stage 6.3
		// artifact. Skipping with a named reason is the honest outcome: the
		// check was live while the slice was open, and it goes with the slice.
		// The gate must be DELETED in the same change that moves the record, or
		// this skip becomes permanent silence.
		t.Skipf("%s is gone (%v): 6.3 is closed and its review record archived, so there is no count left to verify", review, err)
	}
	table := stage63RoundsTable(string(raw))

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
	if remediationReviews := len(reviewed) - stage63NonRemediationRounds; remediationReviews >= expected {
		t.Errorf("%d remediation reviews have run over %d remediation commits; at least one must be unreviewed",
			remediationReviews, expected)
	}
	// The claims the rounds keep getting wrong, checked in EVERY document that
	// carries the count — not only the review record.
	//
	// Scope matters: each claim is read from the SECTION that states it, because a
	// first-match-in-the-file search reads a historical quote as a live claim.
	//
	// Coverage matters more. Four consecutive rounds returned a P1 that was a
	// figure in one of the *other* four documents, invisible to a gate that read
	// only the review record. A mechanism that covers one of five documents lets
	// the class live in the other four, so it is checked in all five.
	_ = raw

	// The header no longer restates any figure. It used to carry the applied count
	// and the outstanding ordinal, and those two restatements were the whole of
	// round 25's first P1: a commit advanced the state line and left the header
	// behind, so the record's own first paragraph contradicted its own fourth.
	//
	// Deleting the restatements is the fix round 25 prescribed over extending a
	// check to police them. `TestOutstandingOrdinalClaimsAreConsistent` now reads
	// every ordinal the record presents as outstanding wherever it appears, so a
	// future reintroduction would be caught — but the reason to delete rather than
	// police is that a number stated once cannot go stale.

	// 2. EVERY count-carrying document must carry exactly one state line, and
	//    that line must state the three figures correctly.
	//
	//    Round 23 rejected this gate because the previous version listed the plan
	//    among the documents to check, but the plan never matched the number
	//    pattern, so the loop skipped it and the plan contributed no assertion at
	//    all — while the resolution cell claimed all five documents were verified
	//    by corrupting each in turn. Only three had ever been corrupted.
	//
	//    Requiring the line closes that: a document without one fails, so coverage
	//    cannot silently drop to four. Reading only the marked line also closes
	//    round 23's other finding, that a table row placed above the live claim
	//    satisfied a whole-file first-match scan.
	want := stage63State{
		applied:     expected,
		reviews:     len(reviewed),
		outstanding: expected,
	}
	for _, other := range stage63CountCarryingDocuments {
		body, err := os.ReadFile(filepath.Join(root, other))
		if err != nil {
			t.Errorf("cannot read %s, which the count gate must cover: %v", other, err)
			continue
		}
		lines := stage63StateLines(string(body))
		if len(lines) != 1 {
			t.Errorf("%s carries %d %q lines; exactly one is required, so this document's state is machine-checkable",
				other, len(lines), stage63StateLinePrefix)
			continue
		}
		state, ok := stage63ParseStateLine(lines[0])
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
					other, figure.got, figure.name, applied, stage63InFlightSuffix, figure.exp)
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
	// the one above. Round 20 weakened this commit by replacing it, so it is
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
		// which would re-introduce the ceiling round 20 found, in the very
		// assertion added to remove it.
		// Scope cells carry a "(covering N–M)" note when one remediation spans
		// several commits. Read the ordinal *before* it: taking the first
		// number in the cell would read "covering 10" and report a mismatch
		// that is not there.
		scope := strings.TrimSpace(pendingRow[2])
		if paren := strings.Index(scope, "("); paren >= 0 {
			scope = strings.TrimSpace(scope[:paren])
		}
		scopeNumber, ok := stage63ReadNumber(scope)
		if !ok {
			t.Errorf("pending row's scope %q carries no readable number; it must name the outstanding remediation, the %s",
				scope, stage63OrdinalWord(expected))
		} else if scopeNumber != expected {
			t.Errorf("pending row's scope names remediation %d; the outstanding one is the %s",
				scopeNumber, stage63OrdinalWord(expected))
		}
	}
}

// stage63RoundsTable extracts the rounds table, so the occurrence table lower
// in the document — which also has numbered rows — is not counted as rounds.
func stage63RoundsTable(doc string) string {
	start := strings.Index(doc, "## Rounds")
	if start < 0 {
		return doc
	}
	rest := doc[start:]
	if end := strings.Index(rest[1:], "\n## "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

// stage63RecordUncommitted reports whether the review record itself has
// uncommitted changes, which is what makes a count one ahead of the history
// legitimate rather than merely wrong.
func stage63RecordUncommitted(root, review string) bool {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--", review).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func stage63RemediationCommits(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "log", "--format=%H %s", stage63RejectedCandidate+"..HEAD").Output()
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && stage63IsRemediation(parts[1]) {
			shas = append(shas, parts[0])
		}
	}
	return shas, nil
}

// stage63IsRemediation decides whether a commit after the rejected candidate is a
// remediation. The rule is inverted deliberately: everything counts as a
// remediation EXCEPT three named bookkeeping commits.
//
// An earlier draft enumerated the *remediation* subject prefixes instead. That
// under-counted the moment a remediation was committed with a subject the list
// had not seen — which happened at once, on the commit that introduced this file.
// A whitelist of exclusions fails closed toward under-counting the thing being
// measured; a whitelist of inclusions fails toward the same place. The exclusions
// are a closed, checkable set, so the inversion is the safer shape.
func stage63IsRemediation(subject string) bool {
	for _, excluded := range stage63BookkeepingSubjects {
		if subject == excluded {
			return false
		}
	}
	return true
}

// stage63BookkeepingSubjects are the three commits between the rejected
// candidate and the first review round. They record evidence and next actions
// and answer no round, so they are not remediations.
var stage63BookkeepingSubjects = []string{
	"stage 6.3: record native macOS evidence and the checkpoint review record",
	"stage 6.3: point the next action at the candidate review",
	"stage 6.3: record that the candidate review dispatch was rate limited",
}

// The 0..20 tables that once stood here were the defect round 20 rejected: the
// comparison had moved to integers while the *reading* still stopped at twenty, so
// the twenty-first commit failed with a message claiming the document said "1".
// stage63ParseNumber replaced them and they are gone. The comment that announced
// their deletion then claimed they were "kept as the documented history of that
// defect", which was false of the same commit — round 22 — so the record of the
// defect lives here and nowhere else, as this sentence.
// stage63CountCarryingDocuments are the documents that state the applied
// remediation count. Every one of them is checked: rounds 19 to 22 each returned
// a P1 that was a stale figure in one of these files while the gate read only the
// review record, which is the same failure four times with a mechanism in place.
//
// Round 23 then found the mechanism's own gap: the previous version listed the
// plan in this slice, but the plan never matched the "N remediations applied"
// pattern, so the loop skipped it and the plan contributed ZERO assertions while
// the resolution cell claimed all five were verified. Coverage must therefore be
// *derived and required*, not declared and skipped — hence the state line below,
// which each document must carry, and a failure when one does not.
var stage63CountCarryingDocuments = []string{
	filepath.Join("docs", "research", "stage-6", "6.3-review.md"),
	filepath.Join("docs", "research", "stage-6", "results.md"),
	filepath.Join("docs", "process", "next-steps.md"),
	filepath.Join("docs", "plans", "stage-6", "6.3-main-dashboard.md"),
	filepath.Join("docs", "plans", "stage-6", "stage-6.md"),
}

// stage63StateLinePrefix marks the single line in each of the above documents
// that carries the machine-checkable current state.
//
// Rounds 20 and 23 both rejected this gate for reading a document's first match
// for a number pattern, which a historical quote or a table row defeats: round 20
// in the review record, round 23 in results.md, where a table cell placed above
// the live claim satisfied the check while the real figure was wrong. A line with
// an explicit marker cannot be confused with prose — a quote of a count elsewhere
// in the document is simply not a state line, and a document missing its state
// line fails rather than being silently skipped.
const stage63StateLinePrefix = "Stage 6.3 review state:"

// stage63State is the set of figures every count-carrying document states.
type stage63State struct {
	applied     int
	reviews     int
	outstanding int
}

var stage63AppliedPattern = regexp.MustCompile(`([a-z]+(?:[- ][a-z]+)*)[*_` + "`" + `]*\s+remediations applied`)
var stage63ReviewsPattern = regexp.MustCompile(`([a-z]+(?:[- ][a-z]+)*)[*_` + "`" + `]*\s+reviews run`)
var stage63OutstandingPattern = regexp.MustCompile(`[Tt]he\s+[*_` + "`" + `]*([a-z]+(?:[- ][a-z]+)*)[*_` + "`" + `]*\s+remediation is outstanding`)

// stage63ParseStateLine reads the figures from one state line.
//
// It fails closed: all three must be present and readable, so a document that
// states only some of them is reported rather than partially accepted. Markdown
// emphasis is tolerated because these documents bold their counts.
func stage63ParseStateLine(line string) (stage63State, bool) {
	var state stage63State
	ok := true
	read := func(pattern *regexp.Regexp) int {
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			ok = false
			return 0
		}
		value, parsed := stage63ParseNumber(strings.Trim(match[1], "*_` "))
		if !parsed {
			ok = false
			return 0
		}
		return value
	}
	state.applied = read(stage63AppliedPattern)
	state.reviews = read(stage63ReviewsPattern)
	state.outstanding = read(stage63OutstandingPattern)
	return state, ok
}

// stage63StateLines returns every state line in a document.
//
// A state line must BEGIN with the marker, optionally after a list bullet. The
// first version matched the marker anywhere on the line, and immediately counted
// two state lines in the review record — because a resolution cell *mentions* the
// marker while explaining that documents must carry one. A mention of the marker
// is prose; only a line that opens with it is a claim. Requiring the position
// rather than the presence is what makes the check usable inside a document that
// explains itself.
func stage63StateLines(doc string) []string {
	var found []string
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimSpace(trimmed)
		if strings.HasPrefix(trimmed, stage63StateLinePrefix) {
			found = append(found, line)
		}
	}
	return found
}

// stage63HeaderBound is the heading that ends the review record's header block.
// It is referenced by name in the record's own findings, which is deliberate: a
// quoted heading inside the body must not be able to satisfy the bound, so the
// bound is the FIRST such heading and the record keeps its quotes elsewhere. If
// this heading is renamed, the header scope fails closed and the gate says so.
const stage63HeaderBound = "## Identifier note"

// stage63Section returns the text of one section of the record: from the heading
// matching `from` up to the first heading matching `to`. An empty bound means the
// start or the end of the document.
func stage63Section(doc, from, to string) string {
	start := 0
	if from != "" {
		idx := strings.Index(doc, from)
		if idx < 0 {
			return ""
		}
		start = idx
	}
	rest := doc[start:]
	if to == "" {
		return rest
	}
	// A missing bound returns "" rather than the whole document, which fails closed
	// and reports "states no applied count" — true and actionable.
	//
	// This holds only while the bound string occurs ONCE. Round 22 found the limit:
	// a resolution cell that quoted the bound heading verbatim put a second
	// occurrence in the document, so renaming the real heading no longer made the
	// bound missing and the scope widened to the whole file again, reading a
	// historical quote as the live count. The quotes that caused it are reworded;
	// a future session adding a verbatim quote of the heading reintroduces it.
	end := strings.Index(rest, to)
	if end <= 0 {
		return ""
	}
	return rest[:end]
}

// English numerals, in both cardinal and ordinal form. The record spells its
// counts in words, so the gate must be able to read words — but a fixed table
// puts a ceiling on the count, which is exactly the defect round 20 found: the
// comparison had moved to integers while the *reading* was still a 0..20 table,
// so the twenty-first commit failed with a message claiming the document said
// "1". A compositional parser has no ceiling.
var stage63Cardinals = map[string]int{
	"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
	"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
	"thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19,
	"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60,
	"seventy": 70, "eighty": 80, "ninety": 90,
}

var stage63Ordinals = map[string]int{
	"zeroth": 0, "first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5,
	"sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9, "tenth": 10,
	"eleventh": 11, "twelfth": 12, "thirteenth": 13, "fourteenth": 14,
	"fifteenth": 15, "sixteenth": 16, "seventeenth": 17, "eighteenth": 18,
	"nineteenth": 19, "twentieth": 20, "thirtieth": 30, "fortieth": 40,
	"fiftieth": 50, "sixtieth": 60, "seventieth": 70, "eightieth": 80,
	"ninetieth": 90,
}

// stage63ParseNumber reads an English numeral, cardinal or ordinal, compound or
// simple: "eighteen", "twentieth", "twenty-one", "twenty-first",
// "one hundred and four". ok is false for anything it cannot read, which fails
// closed with a message naming the offending text.
func stage63ParseNumber(text string) (int, bool) {
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
	if len(fields) > 1 && !strings.ContainsAny(lowered, "-\u2011") && !stage63HasWord(fields, "hundred") {
		for _, field := range fields {
			if _, isOrdinal := stage63Ordinals[field]; isOrdinal {
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
			value, ok := stage63Cardinals[field]
			if !ok {
				// An ordinal is accepted as the final word only; the caller
				// anchors on it, so "twenty first" cannot be misread.
				value, ok = stage63Ordinals[field]
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

// stage63OrdinalWord renders n as an ordinal. Tens are regular ("twenty" + unit),
// so a compound is the cardinal tens word, a hyphen, and the unit's ordinal form;
// the unit's own irregulars — first, second, third, fifth, eighth, ninth, twelfth —
// are the only words that are not the cardinal with "th" appended.
func stage63OrdinalWord(n int) string {
	if n < 0 || n > 99 {
		return strconv.Itoa(n)
	}
	// Below twenty every number is a single word, so it is looked up directly
	// rather than split into a tens part and a unit part — which is what made
	// nineteen render as "ten-ninth".
	if word, ok := stage63OrdinalSingle(n); ok {
		return word
	}
	unit := n % 10
	tens := n - unit
	// A round ten has no unit part: twentieth, not twenty-zeroth.
	if unit == 0 {
		if word, ok := stage63OrdinalSingle(n); ok {
			return word
		}
		return strconv.Itoa(n)
	}
	if tens == 0 {
		return stage63CardinalUnit(unit) + "th"
	}
	unitWord := stage63IrregularOrdinals[unit]
	if unitWord == "" {
		unitWord = stage63CardinalUnit(unit) + "th"
	}
	return stage63CardinalUnit(tens) + "-" + unitWord
}

// stage63ReadNumber finds the first number in a fragment, spelled or in digits.
// The record spells its counts in words, but past ninety-nine a human would write
// digits, and both must be readable for the gate to keep working.
func stage63ReadNumber(text string) (int, bool) {
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
		if value, ok := stage63ParseNumber(strings.Trim(field, "*`,.;:()")); ok {
			return value, true
		}
	}
	return 0, false
}

// stage63HasWord reports whether any token is the given word.
func stage63HasWord(fields []string, word string) bool {
	for _, field := range fields {
		if field == word {
			return true
		}
	}
	return false
}

var stage63IrregularOrdinals = map[int]string{
	1: "first", 2: "second", 3: "third", 5: "fifth", 8: "eighth", 9: "ninth", 12: "twelfth",
}

var stage63UnitCardinals = map[int]string{
	1: "one", 2: "two", 3: "three", 4: "four", 5: "five", 6: "six", 7: "seven",
	8: "eight", 9: "nine", 10: "ten", 11: "eleven", 12: "twelve",
	20: "twenty", 30: "thirty", 40: "forty", 50: "fifty", 60: "sixty",
	70: "seventy", 80: "eighty", 90: "ninety",
}

func stage63CardinalUnit(n int) string {
	if word, ok := stage63UnitCardinals[n]; ok {
		return word
	}
	return strconv.Itoa(n)
}

func stage63OrdinalSingle(n int) (string, bool) {
	for word, value := range stage63Ordinals {
		if value == n {
			return word, true
		}
	}
	return "", false
}
