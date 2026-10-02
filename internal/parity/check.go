package parity

import (
	"fmt"
	"sort"
	"strings"
)

// CheckCoverage verifies that every leaf command in the real command tree
// and every real envelope kind is classified exactly once. treeLeaves are
// space-separated command paths (for example "project pause"); kinds are the
// raw envelope kind names. It returns the failure rather than failing the
// test so the deliberate-unregistration demonstration can show the failure
// without touching the product.
func CheckCoverage(treeLeaves []string, kinds []string, entries []Entry) error {
	byPath := map[string]int{}
	for _, entry := range entries {
		byPath[entry.Path]++
		if entry.Class != ClassExpressible && entry.Class != ClassCompromise && entry.Class != ClassExcluded {
			return fmt.Errorf("entry %q has unknown class %q", entry.Path, entry.Class)
		}
		if entry.Status != StatusDone && entry.Status != StatusPlanned {
			return fmt.Errorf("entry %q has unknown status %q", entry.Path, entry.Status)
		}
	}
	for path, count := range byPath {
		if count > 1 {
			return fmt.Errorf("entry %q is classified %d times", path, count)
		}
	}
	// Note: this function rejects missing, duplicated and unclassified
	// entries, but a spurious extra row passes it on its own. The 85-row
	// count and 67/12/6 tally pins in the test are load-bearing parts of
	// the same check, not redundant restatements.
	for _, leaf := range treeLeaves {
		if byPath[leaf] != 1 {
			return fmt.Errorf("command %q is missing from the parity register", leaf)
		}
	}
	for _, kind := range kinds {
		if byPath["kind "+kind] != 1 {
			return fmt.Errorf("envelope kind %q is missing from the parity register", kind)
		}
	}
	return nil
}

// Tally counts the register by class. The headline figures (67 E / 12 C /
// 6 X over 85 rows) are asserted from this tally, so the numbers are derived
// from the table rather than restated from memory.
func Tally(entries []Entry) (e, c, x int) {
	for _, entry := range entries {
		switch entry.Class {
		case ClassExpressible:
			e++
		case ClassCompromise:
			c++
		case ClassExcluded:
			x++
		}
	}
	return e, c, x
}

// CheckLandedDone fails when an entry owned by a landed sub-stage is still
// planned, or when an excluded entry is not decided. The landed set is
// derived from LandedThrough by ordered comparison, so bumping the constant
// extends enforcement without touching this function.
func CheckLandedDone(entries []Entry) error {
	landed := func(owner string) bool {
		var stage, minor int
		if _, err := fmt.Sscanf(owner, "%d.%d", &stage, &minor); err != nil {
			return false
		}
		var landedStage, landedMinor int
		if _, err := fmt.Sscanf(LandedThrough, "%d.%d", &landedStage, &landedMinor); err != nil {
			return false
		}
		return stage < landedStage || (stage == landedStage && minor <= landedMinor)
	}
	for _, entry := range entries {
		if landed(entry.Owner) && entry.Status != StatusDone {
			return fmt.Errorf("entry %q is owned by landed sub-stage %s but still planned", entry.Path, entry.Owner)
		}
		if entry.Class == ClassExcluded && entry.Status != StatusDone {
			return fmt.Errorf("excluded entry %q is not decided", entry.Path)
		}
	}
	return nil
}

// ExclusionRow is one row of the closed exclusion list: the excluded
// capability, its reason class and its reason.
type ExclusionRow struct {
	Excluded string
	Class    string
	Reason   string
}

// ParseExclusionTable extracts the rows of a Markdown exclusion table: the
// contiguous block of pipe rows following the header. It returns the data
// rows only, so the header and delimiter never compare as entries.
func ParseExclusionTable(document string) []ExclusionRow {
	var rows []ExclusionRow
	inTable := false
	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			if len(rows) > 0 {
				break
			}
			continue
		}
		cells := splitRow(trimmed)
		if len(cells) < 3 {
			continue
		}
		if isDelimiter(cells) {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		rows = append(rows, ExclusionRow{Excluded: cells[0], Class: cells[1], Reason: cells[2]})
	}
	return rows
}

func splitRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func isDelimiter(cells []string) bool {
	for _, cell := range cells {
		stripped := strings.ReplaceAll(strings.ReplaceAll(cell, "-", ""), ":", "")
		if strings.TrimSpace(stripped) != "" {
			return false
		}
	}
	return true
}

// Normalise strips Markdown emphasis and backticks so a reason copied
// between documents with different quoting still compares equal only when
// the words match.
func normalise(value string) string {
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "`", "")
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// CheckExclusionMirror verifies the canonical list and its mirror carry the
// same entries with the same reason class and reason text. The baseline in
// requirements.md is canonical; the mirror is a copy.
func CheckExclusionMirror(canonical, mirror []ExclusionRow) error {
	if len(canonical) != 8 {
		return fmt.Errorf("canonical exclusion list has %d entries, want 8", len(canonical))
	}
	if len(mirror) != len(canonical) {
		return fmt.Errorf("mirror has %d entries, canonical has %d", len(mirror), len(canonical))
	}
	for i := range canonical {
		if normalise(canonical[i].Excluded) != normalise(mirror[i].Excluded) ||
			normalise(canonical[i].Class) != normalise(mirror[i].Class) ||
			normalise(canonical[i].Reason) != normalise(mirror[i].Reason) {
			return fmt.Errorf("mirror entry %d differs from canonical:\ncanonical: %+v\nmirror:    %+v", i+1, canonical[i], mirror[i])
		}
	}
	return nil
}

// CheckXAgainstExclusionList verifies every X row names a sanctioned
// exclusion and that the six command-mapped exclusions each have their row.
// The last two canonical entries name capabilities rather than commands and
// correctly have no register row.
func CheckXAgainstExclusionList(entries []Entry, canonical []ExclusionRow) error {
	sanctioned := map[string]bool{}
	for _, row := range canonical {
		sanctioned[normalise(row.Excluded)] = true
	}
	// Canonical command-mapped entries and the register paths that answer
	// them. completion and help are Cobra defaults absent from the built
	// tree; the table carries them as declared extras.
	mapped := map[string]string{
		"vigil tool-server":          "project tool-server",
		"apply kind operation.start": "kind operation.start",
		"vigil completion":           "completion",
		"vigil help":                 "help",
		"vigil hello":                "hello",
		"vigil spike":                "spike",
	}
	for name := range mapped {
		if !sanctioned[normalise(name)] {
			return fmt.Errorf("canonical exclusion %q is not sanctioned", name)
		}
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Class != ClassExcluded {
			continue
		}
		matched := ""
		for name, path := range mapped {
			if entry.Path == path {
				matched = name
				break
			}
		}
		if matched == "" {
			return fmt.Errorf("excluded entry %q names no sanctioned exclusion", entry.Path)
		}
		seen[matched] = true
	}
	for name := range mapped {
		if !seen[name] {
			return fmt.Errorf("sanctioned exclusion %q has no register row", name)
		}
	}
	return nil
}

// splitReason divides a register reason into its leading class sentence
// ("Scope." / "Mechanism.") and the remaining text.
func splitReason(reason string) (class, rest string) {
	if index := strings.Index(reason, ". "); index > 0 && index < 20 {
		return reason[:index+1], strings.TrimSpace(reason[index+1:])
	}
	return "", reason
}

// SortedPaths returns every entry path in order, for stable diagnostics.
func SortedPaths(entries []Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	sort.Strings(paths)
	return paths
}
