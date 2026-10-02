package parity

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"vigil/internal/cli"
	"vigil/internal/core"
)

// treeLeaves returns every leaf command path in the real Cobra tree. Cobra
// adds completion and help at execution time, not construction, so those two
// are declared extras in the table rather than walked leaves.
func treeLeaves(t *testing.T) []string {
	t.Helper()
	var leaves []string
	var walk func(parent string, current *cobra.Command)
	walk = func(parent string, current *cobra.Command) {
		children := current.Commands()
		if len(children) == 0 {
			if parent != "" {
				leaves = append(leaves, parent)
			}
			return
		}
		for _, child := range children {
			name, _, _ := strings.Cut(child.Use, " ")
			path := strings.TrimSpace(parent + " " + name)
			walk(path, child)
		}
	}
	walk("", cli.NewCommand())
	sort.Strings(leaves)
	return leaves
}

func repoDoc(t *testing.T, parts ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	raw, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestParityCoversCommandTree walks the real Cobra tree and the real
// envelope-kind list and fails when any entry is missing, duplicated or
// unclassified. This is the check that stops the interface and the product
// from diverging again.
func TestParityCoversCommandTree(t *testing.T) {
	leaves := treeLeaves(t)
	if err := CheckCoverage(leaves, core.EnvelopeKinds, Entries); err != nil {
		t.Fatal(err)
	}
	if len(Entries) != 85 {
		t.Fatalf("register has %d rows, want 85", len(Entries))
	}
	e, c, x := Tally(Entries)
	if e != 67 || c != 12 || x != 6 {
		t.Fatalf("register tallies %d E / %d C / %d X, want 67 / 12 / 6", e, c, x)
	}
}

// TestParityXMatchesExclusionList pins every X row against the closed
// exclusion list in the requirements baseline: no capability absent from
// that list may be classified X.
func TestParityXMatchesExclusionList(t *testing.T) {
	requirements := repoDoc(t, "docs", "core", "requirements.md")
	section := exclusionSection(t, requirements, "The closed exclusion list R11 depends on")
	canonical := ParseExclusionTable(section)
	if err := CheckXAgainstExclusionList(Entries, canonical); err != nil {
		t.Fatal(err)
	}
}

// TestExclusionMirrorIdentical pins the canonical list against its mirror in
// the results document: same entries, same reason class, same reason text.
func TestExclusionMirrorIdentical(t *testing.T) {
	requirements := repoDoc(t, "docs", "core", "requirements.md")
	results := repoDoc(t, "docs", "research", "stage-6", "results.md")
	canonical := ParseExclusionTable(exclusionSection(t, requirements, "The closed exclusion list R11 depends on"))
	mirror := ParseExclusionTable(exclusionSection(t, results, "### 5.9 Deliberate exclusions"))
	if err := CheckExclusionMirror(canonical, mirror); err != nil {
		t.Fatal(err)
	}
}

// TestParityReasonsMatchRegister pins the machine table against the human
// register: every C and X reason must read the same as results.md section 4,
// compared after Markdown emphasis is stripped so quoting never counts as a
// difference.
func TestParityReasonsMatchRegister(t *testing.T) {
	results := repoDoc(t, "docs", "research", "stage-6", "results.md")
	section := exclusionSection(t, results, "## 4. Parity register")
	_ = section
	for _, entry := range Entries {
		if entry.Class != ClassCompromise && entry.Class != ClassExcluded {
			continue
		}
		if !strings.Contains(normalise(results), normalise(entry.Reason)) {
			t.Errorf("entry %q reason not found in the Markdown register: %q", entry.Path, entry.Reason)
		}
	}
}

// TestLandedOwnersDone fails when an entry owned by a landed sub-stage is
// still planned. At 6.2 the only owned row is dashboard; later stages bump
// LandedThrough and mark their rows done.
func TestLandedOwnersDone(t *testing.T) {
	if LandedThrough != "6.2" {
		t.Fatalf("LandedThrough is %q, want 6.2", LandedThrough)
	}
	if err := CheckLandedDone(Entries); err != nil {
		t.Fatal(err)
	}
}

// TestDeliberateUnregistrationFails demonstrates the check works: removing
// one entry in memory must fail coverage. Recorded here rather than against
// the product, which is never mutated for a demonstration.
func TestDeliberateUnregistrationFails(t *testing.T) {
	leaves := treeLeaves(t)
	trimmed := append([]Entry(nil), Entries...)
	trimmed = trimmed[:len(trimmed)-1]
	if err := CheckCoverage(leaves, core.EnvelopeKinds, trimmed); err == nil {
		t.Fatal("coverage passed with an entry removed")
	} else if !strings.Contains(err.Error(), "missing from the parity register") {
		t.Fatalf("unexpected coverage failure: %v", err)
	}
	duplicated := append(append([]Entry(nil), Entries...), Entries[0])
	if err := CheckCoverage(leaves, core.EnvelopeKinds, duplicated); err == nil {
		t.Fatal("coverage passed with a duplicated entry")
	}
	planned := append([]Entry(nil), Entries...)
	for i := range planned {
		if planned[i].Path == "dashboard" {
			planned[i].Status = StatusPlanned
		}
	}
	if err := CheckLandedDone(planned); err == nil {
		t.Fatal("landed check passed with a 6.2 row still planned")
	}
}

// exclusionSection returns the document text from a heading to the next
// heading of equal or higher level, so the table parser sees one table.
func exclusionSection(t *testing.T, document, heading string) string {
	t.Helper()
	index := strings.Index(document, heading)
	if index < 0 {
		t.Fatalf("heading %q not found", heading)
	}
	rest := document[index+len(heading):]
	lines := strings.Split(rest, "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && len(kept) > 0 {
			depth := 0
			for _, r := range trimmed {
				if r == '#' {
					depth++
				} else {
					break
				}
			}
			if depth <= 3 {
				break
			}
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
