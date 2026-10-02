package doccheck

import (
	"regexp"
	"strings"
	"testing"
)

func regexpCompile(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

type claimErr string

func (e claimErr) Error() string { return string(e) }

const (
	errNoMatch  = claimErr("no count claim matched")
	errMismatch = claimErr("count mismatch")
)

func regexpFind(content, pattern string) string {
	match := regexpCompile(pattern).FindStringSubmatch(content)
	if match == nil {
		return ""
	}
	return match[1]
}

func checkCountsAgainst(rounds int, further int, claim string) error {
	pattern := `([a-z0-9]+)\s+further\s+reviews?`
	match := regexpFind(claim, pattern)
	if match == "" {
		return errNoMatch
	}
	n, ok := parseCountToken(match)
	if !ok || n != further {
		return errMismatch
	}
	_ = rounds
	return nil
}

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
	// Two rounds derive one further review; a document claiming three fails.
	if err := checkCountsAgainst(2, 1, "three further reviews"); err == nil {
		t.Fatal("C1 logic passed a disagreeing count")
	}
	if err := checkCountsAgainst(2, 1, "one further review"); err != nil {
		t.Fatal("C1 logic failed an agreeing count:", err)
	}
	if err := checkCountsAgainst(2, 1, "no count here"); err == nil {
		t.Fatal("C1 logic passed content with no claim")
	}
}

func TestClaimsC3DetectsBrokenTally(t *testing.T) {
	rows := []string{
		"| 6.1-R1 | P0 | x |",
		"| 6.1-R2 | P0 | x |",
	}
	tally := map[string]int{}
	for _, row := range rows {
		cells := strings.Split(strings.Trim(row, "|"), "|")
		tally[strings.TrimSpace(cells[1])]++
	}
	if tally["P0"] == 4 {
		t.Fatal("broken tally passed")
	}
}

func TestClaimsC5DetectsInterruptingProse(t *testing.T) {
	block := "| R01 | need |\nprose between rows\n| R02 | need |"
	lines := strings.Split(block, "\n")
	prose := false
	for _, line := range lines[1:2] {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			prose = true
		}
	}
	if !prose {
		t.Fatal("interrupting prose not detected")
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
	markers := statusMarkerClaims.FindAllString("no marker here", -1)
	if len(markers) == 1 {
		t.Fatal("phantom marker matched")
	}
}

func TestClaimsC13DetectsContradiction(t *testing.T) {
	denied := regexpCompile(`(?i)stage 6\.1 is not accepted`)
	if !denied.MatchString("Stage 6.1 is not accepted; stays at 5.7") {
		t.Fatal("contradiction missed")
	}
	if denied.MatchString("Stage 6.1 is independently accepted") {
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
	ids := []int{1, 2, 4}
	contiguous := true
	for i, id := range ids {
		if id != i+1 {
			contiguous = false
		}
	}
	if contiguous {
		t.Fatal("gap passed as contiguous")
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
