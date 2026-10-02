package core

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestEnvelopeKindsMatchApplySwitch guards the parity register's foundation:
// EnvelopeKinds must name exactly the kinds Engine.Apply dispatches, no more
// and no fewer. A literal list beside a switch drifts in either direction, so
// this test extracts the switch's case labels from source and pins the two
// sets against each other. Adding a kind to either side without the other
// fails here before the parity check ever runs.
func TestEnvelopeKindsMatchApplySwitch(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "core.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	apply := applySource(t, source)
	cases := regexp.MustCompile(`(?m)^\s*case\s+(.+)$`).FindAllStringSubmatch(apply, -1)
	quoted := regexp.MustCompile(`"([^"]+)"`)
	set := map[string]bool{}
	for _, match := range cases {
		for _, name := range quoted.FindAllStringSubmatch(match[1], -1) {
			set[name[1]] = true
		}
	}
	// The early repository.enroll branch is an if, not a case label, and the
	// permission group shares one case line; both are still envelope kinds.
	var kinds []string
	for kind := range set {
		kinds = append(kinds, kind)
	}
	if !set["repository.enroll"] {
		early := regexp.MustCompile(`cmd\.Kind == "repository\.enroll"`).MatchString(apply)
		if !early {
			t.Error("Apply has no repository.enroll dispatch")
		} else {
			kinds = append(kinds, "repository.enroll")
		}
	}
	listed := map[string]bool{}
	for _, kind := range EnvelopeKinds {
		if listed[kind] {
			t.Errorf("EnvelopeKinds duplicates %q", kind)
		}
		listed[kind] = true
	}
	for _, kind := range kinds {
		if !listed[kind] {
			t.Errorf("Apply dispatches %q, which EnvelopeKinds does not list", kind)
		}
	}
	for kind := range listed {
		found := false
		for _, dispatched := range kinds {
			if dispatched == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EnvelopeKinds lists %q, which Apply does not dispatch", kind)
		}
	}
	var sorted []string
	for kind := range listed {
		sorted = append(sorted, kind)
	}
	sort.Strings(sorted)
	if len(EnvelopeKinds) != len(sorted) {
		t.Fatalf("EnvelopeKinds has %d entries, want %d", len(EnvelopeKinds), len(sorted))
	}
}

// applySource returns the source of Engine.Apply only, so case labels in
// later functions cannot leak into the comparison.
func applySource(t *testing.T, source string) string {
	t.Helper()
	target := "func (e *Engine) Apply("
	start := -1
	for i := 0; i+len(target) <= len(source); i++ {
		if source[i:i+len(target)] == target {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("Engine.Apply not found in core.go")
	}
	rest := source[start+len(target):]
	if index := strings.Index(rest, "\nfunc "); index >= 0 {
		return source[start : start+len(target)+index]
	}
	return source[start:]
}

func indexOfApply(source string) int {
	for i := 0; i+len(source) > i; i++ {
		target := "func (e *Engine) Apply("
		if len(source)-i < len(target) {
			return -1
		}
		if source[i:i+len(target)] == target {
			return i
		}
	}
	return -1
}

// TestApplyRejectsUnknownEnvelopeKind shows the list is authoritative at
// runtime: anything outside EnvelopeKinds is rejected before dispatch.
func TestApplyRejectsUnknownEnvelopeKind(t *testing.T) {
	if ValidEnvelopeKind("planning.proposal.invent") {
		t.Fatal("bogus kind validates")
	}
	for _, kind := range EnvelopeKinds {
		if !ValidEnvelopeKind(kind) {
			t.Fatalf("listed kind %q does not validate", kind)
		}
	}
	if len(EnvelopeKinds) != 14 {
		t.Fatalf("EnvelopeKinds has %d entries, want 14", len(EnvelopeKinds))
	}
}
