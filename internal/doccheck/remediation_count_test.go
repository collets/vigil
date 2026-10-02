package doccheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	// while something is uncommitted.
	expected := applied
	if stage63WorkingTreeDirty(root) {
		expected++
	}

	raw, err := os.ReadFile(filepath.Join(root, review))
	if err != nil {
		t.Fatal(err)
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
	// The three claims the rounds keep getting wrong. Each is asserted against
	// its own sentence rather than the whole document, because a bare
	// substring test passes on a number that appears in a historical finding
	// — which is how the count stayed wrong for four rounds.
	doc := string(raw)
	// Each claim names its own required spelling: the applied count is a
	// cardinal ("ten remediations applied") and the outstanding ordinal is an
	// ordinal ("the tenth remediation"). Comparing either against the other's
	// spelling is itself the near-miss this gate exists to catch, so the two are
	// checked separately and each against its own word.
	claims := []struct {
		what  string
		form  *regexp.Regexp
		spell string
	}{
		{"applied count in the status line", regexp.MustCompile(`(?i)\b([a-z]+) remediations applied`), stage63Spell(expected)},
		{"outstanding ordinal in the status line", regexp.MustCompile(`(?i)follow-up review of the \*\*([a-z]+)\*\* remediation`), stage63Ordinal(expected)},
		{"outstanding ordinal in the closing section", regexp.MustCompile(`(?i)the \*\*([a-z]+)\*\* remediation has not`), stage63Ordinal(expected)},
	}
	for _, claim := range claims {
		found := claim.form.FindStringSubmatch(doc)
		if found == nil {
			t.Errorf("review record states no %s; %d remediation commits exist: %s",
				claim.what, expected, strings.Join(shas, " "))
			continue
		}
		if claim.spell != found[1] {
			t.Errorf("review record's %s says %q but %d remediation commits exist, so it must say %q",
				claim.what, found[1], expected, claim.spell)
		}
	}
	// The rounds table must carry exactly one row per review plus the pending
	// one, and the pending row must name the applied ordinal.
	if !regexp.MustCompile(`(?m)^\|\s*\d+\s*\|.*` + stage63Ordinal(expected) + ` remediation.*\|\s*\**pending`).MatchString(table) {
		t.Errorf("no pending row for the %s remediation in the rounds table", stage63Ordinal(expected))
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

// stage63WorkingTreeDirty reports whether anything is uncommitted, which is what
// makes a count one ahead of the history legitimate rather than merely wrong.
func stage63WorkingTreeDirty(root string) bool {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
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

func stage63Spell(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return ""
}

func stage63Ordinal(n int) string {
	word := stage63Spell(n)
	// English is not regular here: fifth, ninth and twelfth each end in a way
	// that neither "-y becomes -ieth" nor a bare suffix produces. The three
	// irregulars are listed because they are listed, not because a rule was
	// found that covers them.
	switch word {
	case "five":
		return "fifth"
	case "nine":
		return "ninth"
	case "twelve":
		return "twelfth"
	case "":
		return ""
	}
	if strings.HasSuffix(word, "y") {
		return strings.TrimSuffix(word, "y") + "ieth"
	}
	return word + "th"
}
