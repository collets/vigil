package doccheck

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// loadClaimsStatus parses docs/STATUS as key=value pairs.
func loadClaimsStatus(root string) (map[string]string, error) {
	raw, err := readDoc(root, "docs/STATUS")
	if err != nil {
		return nil, err
	}
	parsed := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("malformed STATUS line %q", line)
		}
		parsed[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return parsed, nil
}

var statusMarkerClaims = regexp.MustCompile(`(?m)^<!-- vigil-status: stage=[^;]+; stage_accepted=(?:true|false); implementation_commit=[0-9a-f]{7,40} -->$`)

// C10: STATUS carries its required keys, the status-document list is
// non-empty, and every listed document carries exactly one matching marker.
// Values are read from STATUS, never hardcoded.
func checkStatusMarkersHarness(root string) error {
	status, err := loadClaimsStatus(root)
	if err != nil {
		return err
	}
	for _, key := range []string{"stage", "stage_accepted", "implementation_commit", "status_documents"} {
		if status[key] == "" {
			return fmt.Errorf("STATUS is missing required key %q", key)
		}
	}
	want := "<!-- vigil-status: stage=" + status["stage"] +
		"; stage_accepted=" + status["stage_accepted"] +
		"; implementation_commit=" + status["implementation_commit"] + " -->"
	var docs []string
	for _, doc := range strings.Split(status["status_documents"], ",") {
		doc = strings.TrimSpace(doc)
		if doc != "" {
			docs = append(docs, doc)
		}
	}
	if len(docs) == 0 {
		return fmt.Errorf("status_documents is empty")
	}
	for _, doc := range docs {
		raw, err := readDoc(root, doc)
		if err != nil {
			return err
		}
		markers := statusMarkerClaims.FindAllString(stripClaimsCode(raw), -1)
		if len(markers) != 1 {
			return fmt.Errorf("%s carries %d status markers, want exactly 1", doc, len(markers))
		}
		if markers[0] != want {
			return fmt.Errorf("%s marker does not match STATUS", doc)
		}
	}
	return nil
}

// C11: the accepted slice's diff resolves and is non-empty, and touches only
// the slice's own path groups. For 6.1 that is docs/, AGENTS.md and
// README.md; the range is the review record's own reviewed range, resolved
// through git rather than trusted from prose.
func checkSliceDiff(root string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is not available")
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	for _, rev := range []string{"27182de", "bad6139"} {
		if _, err := run("rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
			return fmt.Errorf("revision %s does not resolve", rev)
		}
	}
	out, err := run("diff", "--name-only", "27182de..bad6139")
	if err != nil {
		return fmt.Errorf("range 27182de..bad6139 does not resolve: %v", err)
	}
	if out == "" {
		return fmt.Errorf("accepted slice diff is empty")
	}
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if path == "AGENTS.md" || path == "README.md" || strings.HasPrefix(path, "docs/") {
			continue
		}
		return fmt.Errorf("accepted slice touches %q outside docs/, AGENTS.md and README.md", path)
	}
	return nil
}

var partitionPattern = regexp.MustCompile(`(\d+)((?:\s*\+\s*\d+)+)\s*=\s*(\d+)`)

// evaluatePartition computes the sum of an A + B + ... expression.
func evaluatePartition(expr string) (addends []int, total int, ok bool) {
	match := partitionPattern.FindStringSubmatch(expr)
	if match == nil {
		return nil, 0, false
	}
	numPattern := regexp.MustCompile(`\d+`)
	addends = nil
	for _, token := range numPattern.FindAllString(match[0], -1) {
		n, _ := strconv.Atoi(token)
		addends = append(addends, n)
	}
	if len(addends) < 3 {
		return nil, 0, false
	}
	total = addends[len(addends)-1]
	addends = addends[:len(addends)-1]
	return addends, total, true
}

// C12: every stated partition whose total equals a review count actually sums
// to it. A quoted stale figure is excluded by construction, not by an
// allow-list of cells: the arithmetic is evaluated, not looked up.
func checkPartitionsSum(root string) error {
	files, err := allMarkdown(root)
	if err != nil {
		return err
	}
	checked := 0
	for _, file := range files {
		rel, err := filepathRel(root, file)
		if err != nil {
			return err
		}
		if !(strings.HasPrefix(rel, "docs/research/stage-6/") || strings.HasPrefix(rel, "docs/plans/stage-6/") || strings.HasPrefix(rel, "docs/process/") || strings.HasPrefix(rel, "docs/core/") || rel == "AGENTS.md") {
			continue
		}
		raw, err := osReadFile(file)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(stripClaimsCode(raw), "\n") {
			for _, expr := range partitionPattern.FindAllString(line, -1) {
				addends, total, ok := evaluatePartition(expr)
				if !ok {
					continue
				}
				checked++
				sum := 0
				for _, addend := range addends {
					sum += addend
				}
				if sum != total {
					return fmt.Errorf("%s: partition %q sums to %d", rel, expr, sum)
				}
			}
		}
	}
	if checked == 0 {
		return fmt.Errorf("no partitions found to check")
	}
	return nil
}

// C13: no live status document contradicts acceptance while STATUS records
// it. Scoped to the status-document list, where live guidance lives; review
// records legitimately quote superseded state.
func checkAcceptanceProse(root string) error {
	status, err := loadClaimsStatus(root)
	if err != nil {
		return err
	}
	if status["stage_accepted"] != "true" {
		return fmt.Errorf("STATUS does not record acceptance")
	}
	stage := status["stage"]
	var docs []string
	for _, doc := range strings.Split(status["status_documents"], ",") {
		doc = strings.TrimSpace(doc)
		if doc != "" {
			docs = append(docs, doc)
		}
	}
	if len(docs) == 0 {
		return fmt.Errorf("no status documents to check")
	}
	denied := regexp.MustCompile(`(?i)stage ` + regexp.QuoteMeta(stage) + ` is not accepted|not accepted[^.]{0,40}stage ` + regexp.QuoteMeta(stage))
	for _, doc := range docs {
		raw, err := readDoc(root, doc)
		if err != nil {
			return err
		}
		if denied.MatchString(stripClaimsCode(raw)) {
			return fmt.Errorf("%s contradicts the recorded acceptance of stage %s", doc, stage)
		}
	}
	return nil
}

type claimShape struct {
	name    string
	file    string
	pattern *regexp.Regexp
	expect  func() int
}

// C14: a registry of tight claim shapes for every live review-state count,
// each with a derived expected value. No prose heuristics, no allow-list of
// cells to maintain.
func checkClaimShapes(root string) error {
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
	tables, err := findingsTables(record)
	if err != nil {
		return err
	}
	shapes := []claimShape{
		{name: "verdict rounds", file: "6.2 plan", pattern: regexp.MustCompile(`(?i)verified\s+by\s+(fourteen|\d+)\s+reviews`), expect: func() int { return rounds }},
		{name: "restating rounds", file: "6.2 plan", pattern: regexp.MustCompile(`(?i)(fourteen|\d+)\s+review\s+rounds\s+established`), expect: func() int { return rounds }},
		{name: "findings tables", file: "review record", pattern: regexp.MustCompile(`findings tables in the (fourteen|\d+) sections`), expect: func() int { return len(tables) }},
	}
	plan, err := readDoc(root, "docs/plans/stage-6/6.2-interface-architecture-and-parity-register.md")
	if err != nil {
		return err
	}
	checked := 0
	for _, shape := range shapes {
		var content string
		switch shape.file {
		case "6.2 plan":
			content = stripClaimsCode(plan)
		default:
			content = stripClaimsCode(record)
		}
		match := shape.pattern.FindStringSubmatch(content)
		if match == nil {
			return fmt.Errorf("shape %q not found", shape.name)
		}
		n, ok := parseCountToken(match[1])
		if !ok {
			return fmt.Errorf("shape %q has unparsable count", shape.name)
		}
		checked++
		if n != shape.expect() {
			return fmt.Errorf("shape %q states %d, derived %d", shape.name, n, shape.expect())
		}
	}
	if checked != len(shapes) {
		return fmt.Errorf("only %d of %d shapes checked", checked, len(shapes))
	}
	return nil
}

var replacementVerbs = regexp.MustCompile(`(?i)(restated|rewritten|corrected|replaced|now reads?|recast)(\s+\S+){0,3}\s+"([^"]{8,})"`)
var ellipsisSplitter = regexp.MustCompile(`…|\.\.\.`)

// C15: a remediation cell's quoted replacement — a double-quoted span after
// a replacement verb — must exist outside that cell's own row. Covers only
// the quoted case; a broad version produced false positives because a cell
// is usually a description rather than a quotation.
func checkQuotedReplacements(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	tables, err := findingsTables(record)
	if err != nil {
		return err
	}
	// The witness may live in another document (a correction to the
	// register is witnessed by the register), so the corpus is every
	// document, with the row's own occurrences subtracted.
	files, err := allMarkdown(root)
	if err != nil {
		return err
	}
	var corpus strings.Builder
	for _, file := range files {
		raw, err := osReadFile(file)
		if err != nil {
			return err
		}
		corpus.WriteString(strings.Join(strings.Fields(stripClaimsCode(raw)), " "))
		corpus.WriteString("\n")
	}
	flatCorpus := corpus.String()
	candidates := 0
	for _, table := range tables {
		for _, row := range table {
			for _, match := range replacementVerbs.FindAllStringSubmatch(row, -1) {
				quoted := match[3]
				parts := ellipsisSplitter.Split(quoted, -1)
				needle := ""
				for _, part := range parts {
					if len(strings.TrimSpace(part)) > len(needle) {
						needle = strings.TrimSpace(part)
					}
				}
				if len(needle) < 8 {
					continue
				}
				candidates++
				flatRow := strings.Join(strings.Fields(row), " ")
				flatNeedle := strings.Join(strings.Fields(needle), " ")
				if strings.Count(flatCorpus, flatNeedle) <= strings.Count(flatRow, flatNeedle) {
					return fmt.Errorf("quoted replacement has no witness outside its row: %q", needle)
				}
			}
		}
	}
	if candidates == 0 {
		return fmt.Errorf("no quoted replacements found; the check is unexercised")
	}
	return nil
}

// documentedNonChanges are finding cells that deliberately preserve
// erroneous wording as a recorded lesson rather than applying a fix.
var documentedNonChanges = []string{
	"Stage 6 remains conditional on finishing the accepted Stage 5 milestone",
}

// C16: a Finding cell's "still reads X" must be gone from the document it
// names — the shape of an unapplied fix. Narrowed to cells that name a
// document and assert the text is still present, with deliberate
// non-changes excluded.
func checkStillReadsGone(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	tables, err := findingsTables(record)
	if err != nil {
		return err
	}
	stillPattern := regexp.MustCompile(`(?i)still\s+(reads?|said|holds|claims?|contains?)\s+"([^"]{8,})"`)
	docPattern := regexp.MustCompile("(`?[a-zA-Z0-9_./-]+\\.md`?)")
	scanned := 0
	for _, table := range tables {
		for _, row := range table {
			scanned++
			match := stillPattern.FindStringSubmatch(row)
			if match == nil {
				continue
			}
			quoted := match[2]
			for _, kept := range documentedNonChanges {
				if strings.Contains(quoted, kept) || strings.Contains(kept, quoted) {
					return fmt.Errorf("non-change exclusion hit unexpectedly for %q", quoted)
				}
			}
			docMatch := docPattern.FindStringSubmatch(row)
			if docMatch == nil {
				continue
			}
			named := strings.Trim(docMatch[1], "`")
			named = strings.TrimPrefix(named, "docs/research/stage-6/")
			candidates := []string{named, "docs/research/stage-6/" + named, "docs/" + named}
			var content string
			found := false
			for _, candidate := range candidates {
				if raw, err := readDoc(root, candidate); err == nil {
					content = raw
					found = true
					break
				}
			}
			if !found {
				continue
			}
			if strings.Contains(content, quoted) {
				return fmt.Errorf("text asserted fixed still present in %s: %q", named, quoted)
			}
		}
	}
	if scanned == 0 {
		return fmt.Errorf("no finding cells scanned")
	}
	return nil
}

var findingIDPattern = regexp.MustCompile(`6\.1-(R14|R2F|[RGHIJKLMNOPQ])-?([0-9]+)`)

// C17: every round's finding IDs are unique and contiguous from 1, and the
// verdict table's stated ranges equal the rows that exist.
func checkFindingIDs(root string) error {
	record, err := readDoc(root, "docs/research/stage-6/6.1-review.md")
	if err != nil {
		return err
	}
	// Contiguity is judged on the table rows, which carry one occurrence
	// each; verdict-table range citations repeat IDs outside tables and are
	// checked separately below.
	tables, err := findingsTables(record)
	if err != nil {
		return err
	}
	tableIDs := map[string][]int{}
	for _, table := range tables {
		for _, row := range table {
			cells := strings.Split(strings.Trim(row, "|"), "|")
			if len(cells) == 0 {
				continue
			}
			match := findingIDPattern.FindStringSubmatch(strings.TrimSpace(cells[0]))
			if match == nil {
				continue
			}
			n, _ := strconv.Atoi(match[2])
			tableIDs[match[1]] = append(tableIDs[match[1]], n)
		}
	}
	expectedRounds := []string{"R", "R2F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R14"}
	if len(tableIDs) != len(expectedRounds) {
		return fmt.Errorf("findings cover %d rounds, want %d", len(tableIDs), len(expectedRounds))
	}
	for _, round := range expectedRounds {
		ids, ok := tableIDs[round]
		if !ok {
			return fmt.Errorf("round %s has no findings table", round)
		}
		sort.Ints(ids)
		for i, id := range ids {
			if id != i+1 {
				return fmt.Errorf("round %s IDs are not contiguous from 1", round)
			}
		}
		seen := map[int]bool{}
		for _, id := range ids {
			if seen[id] {
				return fmt.Errorf("round %s duplicates ID %d", round, id)
			}
			seen[id] = true
		}
	}
	rows, err := verdictRows(record)
	if err != nil {
		return err
	}
	rangePattern := regexp.MustCompile(`6\.1-(R14|R2F|[RGHIJKLMNOPQ])-?([0-9]+)[^\d]{1,3}?(R14|R2F|[RGHIJKLMNOPQ])-?([0-9]+)`)
	for _, row := range rows {
		for _, match := range rangePattern.FindAllStringSubmatch(row, -1) {
			if match[1] != match[3] {
				continue
			}
			round := match[1]
			from, _ := strconv.Atoi(match[2])
			to, _ := strconv.Atoi(match[4])
			ids := tableIDs[round]
			if len(ids) == 0 {
				return fmt.Errorf("verdict range names round %s with no table", round)
			}
			if from != 1 || to != len(ids) {
				return fmt.Errorf("verdict range %s%d..%s%d does not equal the %d rows present", match[1], from, match[3], to, len(ids))
			}
		}
	}
	return nil
}

// C18: the harness has eighteen sections. The count is derived from the
// harness's own section headers in source, not from the registry list —
// deriving it from the list made the check self-consistent and blind, since
// a section never enumerated would be invisible to it.
func checkHarnessSelfCount(root string) error {
	raw, err := readDoc(root, "internal/doccheck/claims.go")
	if err != nil {
		return err
	}
	registry := regexp.MustCompile(`\{name: "C\d+: `).FindAllString(raw, -1)
	if len(registry) == 0 {
		return fmt.Errorf("no check registrations found in claims.go")
	}
	checks1, err := readDoc(root, "internal/doccheck/claims_checks1.go")
	if err != nil {
		return err
	}
	checks2, err := readDoc(root, "internal/doccheck/claims_checks2.go")
	if err != nil {
		return err
	}
	combined := raw + checks1 + checks2
	sectionHeaders := regexp.MustCompile(`(?m)^// C(\d+)[ab]?: `).FindAllStringSubmatch(combined, -1)
	seen := map[string]bool{}
	for _, match := range sectionHeaders {
		seen[match[1]] = true
	}
	if len(seen) != 18 {
		return fmt.Errorf("harness has %d distinct section numbers, want 18", len(seen))
	}
	// Eighteen sections, enumerated as seventeen items with 1b folded into
	// item 1: the registry carries eighteen entries while the C1 entry runs
	// both halves. The count is derived from the source text, not from the
	// slice, so a registration that never lands is still visible.
	if len(registry) != 18 {
		return fmt.Errorf("harness registry has %d checks, want 18", len(registry))
	}
	return nil
}
