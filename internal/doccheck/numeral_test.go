package doccheck

import (
	"strconv"
	"testing"
)

// TestNumeralParsingHasNoCeiling is the round 20 regression. The gate's first
// version compared number *words* from a table covering 0..20, so the
// twenty-first commit after the rejected candidate failed with a message
// reporting that the document "says 1" — and no spelling could satisfy it.
// Comparing integers while still READING a fixed table moves the ceiling rather
// than removing it, which is exactly what round 20 found by mutation.
//
// This test is why the claim "no ceiling" is checkable rather than asserted.
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
		got, ok := stage63ParseNumber(tc.word)
		if !ok {
			t.Errorf("%q did not parse", tc.word)
			continue
		}
		if got != tc.want {
			t.Errorf("%q parsed to %d, want %d", tc.word, got, tc.want)
		}
	}
	// Every ordinal this helper produces for the range the gate can reach must
	// parse back, or a correct record would fail on correct English.
	for n := 0; n <= 99; n++ {
		word := stage63OrdinalWord(n)
		got, ok := stage63ParseNumber(word)
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
		if got, ok := stage63ReadNumber(stage63OrdinalWord(n)); !ok || got != n {
			t.Errorf("%d does not round-trip: got %d, ok=%v", n, got, ok)
		}
		if got, ok := stage63ReadNumber(strconv.Itoa(n)); !ok || got != n {
			t.Errorf("digits for %d do not read back", n)
		}
	}
	// Nonsense must fail closed rather than parse to something plausible.
	for _, bad := range []string{"", "   ", "banana", "twenty first", "hundred", "and", "one hundred and", "the third", "twenty-first-"} {
		if got, ok := stage63ParseNumber(bad); ok {
			t.Errorf("%q parsed to %d but should not", bad, got)
		}
	}
}
