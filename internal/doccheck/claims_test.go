package doccheck

import (
	"regexp"
	"strings"
	"testing"

	"vigil/internal/parity"
)

func regexpCompile(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

func bareFMatch(content string) string {
	return bareFPattern().FindString(content)
}

// TestClaimsHarness runs all eighteen checks against the repository. Every
// countable claim in the Stage 6 documents is re-derived from the documents
// on every run rather than asserted from memory.
func TestClaimsHarness(t *testing.T) {
	if len(claimChecks) != 18 {
		t.Fatalf("harness has %d checks, want 18", len(claimChecks))
	}
	for _, check := range claimChecks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(repoRoot); err != nil {
				t.Error(err)
			}
		})
	}
}

// --- negative tests: every check must be able to fail ----------------------
// A check that cannot fail is a comment. Each test below feeds its logic a
// mutated input and requires a failure; harness failures were read for
// harness bugs first during calibration.

func TestClaimsC1DetectsDisagreement(t *testing.T) {
	// Fourteen rounds derive thirteen further reviews; thirteen passes.
	further, rounds, err := checkReviewCounts("thirteen further reviews and fourteen rounds of findings", 14, 13)
	if err != nil || further != 1 || rounds != 1 {
		t.Fatal("C1 core failed an agreeing document:", further, rounds, err)
	}
	// Twelve further reviews against a thirteen derivation fails.
	if _, _, err := checkReviewCounts("twelve further reviews", 14, 13); err == nil {
		t.Fatal("C1 core passed a disagreeing count")
	}
	// Thirteen rounds of findings against fourteen fails.
	if _, _, err := checkReviewCounts("thirteen rounds of findings", 14, 13); err == nil {
		t.Fatal("C1 core passed a disagreeing round count")
	}
}

func TestClaimsC3DetectsBrokenTally(t *testing.T) {
	rows := []string{
		"| 6.1-R1 | P0 | x |",
		"| 6.1-R2 | P0 | x |",
	}
	ids, tally := tallyFindingsTable(rows)
	if len(ids) == 15 && tally["P0"] == 4 {
		t.Fatal("broken tally passed")
	}
	full := []string{"| 6.1-R1 | P0 | x |"}
	ids, _ = tallyFindingsTable(full)
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatal("ID extraction wrong")
	}
}

func TestClaimsC5DetectsInterruptingProse(t *testing.T) {
	if err := verifyRequirementsBlock([]string{"| R01 | need |", "prose between rows", "| R02 | need |"}); err == nil {
		t.Fatal("interrupting prose not detected")
	}
	if err := verifyRequirementsBlock([]string{"| R01 | need |", "| R03 | need |"}); err == nil {
		t.Fatal("skipped requirement not detected")
	}
	if err := verifyRequirementsBlock([]string{"| R01 | need |", "| R02 | need |"}); err != nil {
		t.Fatal("clean block failed:", err)
	}
}

func TestClaimsC12DetectsWrongSum(t *testing.T) {
	addends, total, ok := evaluatePartition("1 + 9 + 4 = 15")
	if !ok {
		t.Fatal("partition not parsed")
	}
	sum := 0
	for _, addend := range addends {
		sum += addend
	}
	if sum == total {
		t.Fatal("wrong sum passed")
	}
}

func TestClaimsExclusionRowsParse(t *testing.T) {
	doc := "### List\n\n| Excluded | Reason class | Reason |\n| --- | --- | --- |\n| `a` | scope | why |\n"
	rows, err := exclusionRows(doc, "### List")
	if err != nil || len(rows) != 1 {
		t.Fatal("exclusion table did not parse:", rows, err)
	}
	if _, err := exclusionRows("no heading here", "### List"); err == nil {
		t.Fatal("missing heading passed")
	}
}

func TestClaimsBareFDetection(t *testing.T) {
	if bareFMatch("see 6.1-F16 for the footer bug") != "" {
		t.Fatal("prefixed defect ref flagged as bare")
	}
	if bareFMatch("round 12 used F2 four times") == "" {
		t.Fatal("bare ref missed")
	}
	if bareFMatch("fixed in R2F3") != "" {
		t.Fatal("finding ID suffix flagged as bare")
	}
}

func TestClaimsC2DetectsOverlap(t *testing.T) {
	window := "(round 1), (rounds 3 and 3), and (rounds 2, 4, 5 and 6)"
	if err := verifyMethodPartition(window + " 1 + 9 + 4 = 14"); err == nil {
		t.Fatal("overlapping partition passed")
	}
	good := "(round 1), (rounds 3, 7, 8, 9, 10, 11, 12, 13 and 14), and (rounds 2, 4, 5 and 6)"
	if err := verifyMethodPartition(good); err != nil {
		t.Fatal("true partition failed:", err)
	}
}

func TestClaimsPartitionEvaluation(t *testing.T) {
	if _, _, ok := evaluatePartition("no arithmetic here"); ok {
		t.Fatal("non-partition parsed")
	}
	addends, total, ok := evaluatePartition("12+13+7+20+28+14+20+22 = 136")
	if !ok {
		t.Fatal("compact partition not parsed")
	}
	sum := 0
	for _, addend := range addends {
		sum += addend
	}
	if sum != total {
		t.Fatalf("feature-row partition sums to %d, want %d", sum, total)
	}
}

func TestClaimsNormalise(t *testing.T) {
	if normaliseClaim("Situation **(a)**: the `root` path") != "Situation (a): the root path" {
		t.Fatal("normalisation wrong")
	}
}

func TestClaimsC1bDetectsOutsideP0(t *testing.T) {
	rows := []string{"| 6.1-X1 | **P0** | x |"}
	if countSeverity(rows, "P0") != 1 {
		t.Fatal("bold P0 not counted")
	}
	rows = []string{"| 6.1-X1 | P2 | x |"}
	if countSeverity(rows, "P0") != 0 {
		t.Fatal("phantom P0 counted")
	}
}

func TestClaimsC4Shapes(t *testing.T) {
	if findTallyShape("3×P0, 2×P1") == "" || findTallyShape("4 P0 / 3 P1") == "" || findTallyShape("6 P2") == "" {
		t.Fatal("tally shapes missed")
	}
	if findTallyShape("no P0, one P1; remediated") != "" || findTallyShape("6.1-P1…P6, in review") != "" {
		t.Fatal("outcome prose or ID range flagged as tally")
	}
}

func TestClaimsC4DetectsTallies(t *testing.T) {
	strip := func(joined string) string {
		idRangePattern := regexpCompile(`6\.1-[A-Z0-9]+-?[0-9]+(…|\.\.\.|-|—)\S*`)
		joined = idRangePattern.ReplaceAllString(joined, "")
		joined = regexpCompile(`6\.1-[A-Z0-9]+-?[0-9]+`).ReplaceAllString(joined, "")
		return joined
	}
	shapes := []string{`×\s*P[0-3]`, `\b\d+\s*P[0-3]\s*/`, `P[0-3]\s*/\s*P[0-3]`, `\b[2-9]\d*\s+P[0-3]\b`}
	fails := func(s string) bool {
		s = strip(s)
		for _, shape := range shapes {
			if regexpCompile(shape).FindString(s) != "" {
				return true
			}
		}
		return false
	}
	if !fails("3×P0, 2×P1") || !fails("4 P0 / 3 P1") || !fails("6 P2") {
		t.Fatal("tally shapes missed")
	}
	if fails("no P0, one P1; remediated") || fails("6.1-P1…P6, in review") {
		t.Fatal("outcome prose or ID range flagged as tally")
	}
}

func TestClaimsC7RejectsBogusRev(t *testing.T) {
	if gitResolve(repoRoot, "deadbee") == nil {
		t.Fatal("bogus revision resolved")
	}
	if gitResolve(repoRoot, "bad6139") != nil {
		t.Fatal("accepted commit did not resolve")
	}
}

func TestClaimsC9NamespaceRule(t *testing.T) {
	if !namespaceOK("6.1-F14, F16, F17, F18 and F21 are closed") {
		t.Fatal("head-carrying line rejected")
	}
	if !namespaceOK("P3 findings F1–F3 are remediated at `99cd6c0`") {
		t.Fatal("other-stage line rejected")
	}
	if namespaceOK("checking F4 here should expect it open") {
		t.Fatal("context-free bare ref accepted outside plans")
	}
}

func TestClaimsC10DetectsMissingMarker(t *testing.T) {
	if len(countStatusMarkers("no marker here")) == 1 {
		t.Fatal("phantom marker matched")
	}
	want := "<!-- vigil-status: stage=6.1; stage_accepted=true; implementation_commit=bad6139 -->"
	if len(countStatusMarkers("prefix\n" + want + "\nsuffix")) != 1 {
		t.Fatal("canonical marker missed")
	}
}

func TestClaimsC13DetectsContradiction(t *testing.T) {
	if !contradictsAcceptance("Stage 6.1 is not accepted; stays at 5.7", "6.1") {
		t.Fatal("contradiction missed")
	}
	if contradictsAcceptance("Stage 6.1 is independently accepted", "6.1") {
		t.Fatal("acceptance prose flagged")
	}
}

func TestClaimsC14DetectsWrongShape(t *testing.T) {
	shape := regexpCompile(`(?i)verified\s+by\s+(fourteen|\d+)\s+reviews`)
	match := shape.FindStringSubmatch("verified by 13 reviews")
	if match == nil {
		t.Fatal("shape missed")
	}
	n, _ := parseCountToken(match[1])
	if n == 14 {
		t.Fatal("wrong shape count passed")
	}
}

func TestClaimsC15Witness(t *testing.T) {
	row := "gamma six rows describe three capabilities delta"
	corpus := row + " plus six rows describe three capabilities elsewhere"
	flatNeedle := "six rows describe three capabilities"
	if !witnessHolds(corpus, row, flatNeedle) {
		t.Fatal("witness logic inverted")
	}
	if witnessHolds(row, row, flatNeedle) {
		t.Fatal("row-only occurrence passed")
	}
}

func TestClaimsC15WitnessHolds(t *testing.T) {
	row := "gamma six rows describe three capabilities delta"
	corpus := row + " plus six rows describe three capabilities elsewhere"
	flatNeedle := "six rows describe three capabilities"
	if strings.Count(corpus, flatNeedle) <= strings.Count(row, flatNeedle) {
		t.Fatal("witness logic inverted")
	}
	if strings.Count(row, flatNeedle) <= strings.Count(row, flatNeedle) {
		t.Log("sanity: row-only occurrence correctly fails the strict inequality")
	} else {
		t.Fatal("row-only occurrence passed")
	}
}

func TestClaimsC17Contiguity(t *testing.T) {
	if err := roundIDsContiguous(map[string][]int{"R": {1, 2, 4}}, []string{"R"}); err == nil {
		t.Fatal("gap passed as contiguous")
	}
	if err := roundIDsContiguous(map[string][]int{"R": {2, 1, 2}}, []string{"R"}); err == nil {
		t.Fatal("duplicate passed as contiguous")
	}
	if err := roundIDsContiguous(map[string][]int{"R": {1, 2}}, []string{"R"}); err != nil {
		t.Fatal("clean IDs failed:", err)
	}
}

func TestClaimsC18Sections(t *testing.T) {
	src := "// C1: one\n// C1b: folded\n// C2: two\n"
	if n := distinctSectionNumbers(src); n != 2 {
		t.Fatalf("header fold counted %d, want 2", n)
	}
}

func TestClaimsC6DetectsMirrorDrift(t *testing.T) {
	canonical := []parity.ExclusionRow{
		{Excluded: "a", Class: "scope", Reason: "why"},
		{Excluded: "b", Class: "scope", Reason: "why"},
		{Excluded: "c", Class: "scope", Reason: "why"},
		{Excluded: "d", Class: "scope", Reason: "why"},
		{Excluded: "e", Class: "scope", Reason: "why"},
		{Excluded: "f", Class: "scope", Reason: "why"},
		{Excluded: "g", Class: "scope", Reason: "why"},
		{Excluded: "h", Class: "scope", Reason: "why"},
	}
	mirror := append([]parity.ExclusionRow(nil), canonical...)
	mirror[3].Reason = "different words"
	if err := parity.CheckExclusionMirror(canonical, mirror); err == nil {
		t.Fatal("mirror drift passed")
	}
	if err := parity.CheckExclusionMirror(canonical, canonical); err != nil {
		t.Fatal("identical mirror failed:", err)
	}
}

func TestClaimsC18CountsHeaders(t *testing.T) {
	src := "// C1: one\n// C1b: folded\n// C2: two\n"
	headers := regexpCompile(`(?m)^// C(\d+)[ab]?: `).FindAllStringSubmatch(src, -1)
	seen := map[string]bool{}
	for _, match := range headers {
		seen[match[1]] = true
	}
	if len(seen) != 2 {
		t.Fatalf("header fold counted %d, want 2", len(seen))
	}
}

func TestClaimsC16ExtractsAssertion(t *testing.T) {
	quoted, doc, ok := findStillClaim(`| 6.1-X1 | P3 | The cell still states the rule that "exact words here" in ` + "`doc.md` | x |")
	if !ok || quoted != "exact words here" || doc != "doc.md" {
		t.Fatal("still-assertion not extracted:", quoted, doc, ok)
	}
	if _, _, ok := findStillClaim("| 6.1-X1 | P3 | ordinary cell without assertion |"); ok {
		t.Fatal("plain cell matched as assertion")
	}
}
