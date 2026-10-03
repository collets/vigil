package doccheck

import "testing"

// TestStateLineParsingRejectsEveryMutation is the battery round 22 asked for and
// round 23 said was asserted rather than run.
//
// Round 22 rejected this gate after a resolution cell claimed "verified by
// corrupting each in turn: every one fails" when only three of five documents had
// ever been corrupted. Round 23 then found the fifth document contributed no
// assertion at all, because it never matched the pattern. So the battery is a
// test here, and it covers the mutations a reviewer would actually apply:
//
//   - corrupt each figure in turn, to a wrong value and to a non-number
//   - delete a figure entirely
//   - delete the whole state line
//   - duplicate it
//   - prepend a historical quote of a count, the shape that defeated round 23
//   - prepend a different stage's count, which defeated the whole-file scan
//
// Each must be rejected. A gate that passes any of these is the gate that let
// five rejections through.
func TestStateLineParsingRejectsEveryMutation(t *testing.T) {
	const valid = "Stage 6.3 review state: twenty-one remediations applied; twenty-two reviews run; the twenty-first remediation is outstanding."

	// The line as written must parse to exactly what it says.
	state, ok := stage63ParseStateLine(valid)
	if !ok {
		t.Fatalf("the canonical state line does not parse: %q", valid)
	}
	if state.applied != 21 || state.reviews != 22 || state.outstanding != 21 {
		t.Fatalf("parsed %+v from %q, want applied=21 reviews=22 outstanding=21", state, valid)
	}

	t.Run("accepted forms", func(t *testing.T) {
		for _, line := range []string{
			valid,
			"Stage 6.3 review state: **twenty-one** remediations applied; **twenty-two** reviews run; the **twenty-first** remediation is outstanding.",
			"- Stage 6.3 review state: twenty-one remediations applied; twenty-two reviews run; the twenty-first remediation is outstanding.",
			"Some prefix. Stage 6.3 review state: twenty-one remediations applied; twenty-two reviews run; the twenty-first remediation is outstanding.",
		} {
			if _, ok := stage63ParseStateLine(line); !ok {
				t.Errorf("did not parse, though it is a valid state line: %q", line)
			}
		}
	})

	// A wrong VALUE must still parse — that is what lets the gate compare it
	// against the git history and report a mismatch. What must fail closed is a
	// line that is structurally broken, because then there is nothing to compare.
	t.Run("wrong values parse, and are therefore comparable", func(t *testing.T) {
		for name, tc := range map[string]struct {
			line  string
			which string
			want  int
		}{
			"applied wrong": {
				"Stage 6.3 review state: twenty remediations applied; twenty-two reviews run; the twenty-first remediation is outstanding.",
				"applied", 20},
			"reviews wrong": {
				"Stage 6.3 review state: twenty-one remediations applied; twenty reviews run; the twenty-first remediation is outstanding.",
				"reviews", 20},
			"outstanding wrong": {
				"Stage 6.3 review state: twenty-one remediations applied; twenty-two reviews run; the twentieth remediation is outstanding.",
				"outstanding", 20},
		} {
			got, ok := stage63ParseStateLine(tc.line)
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
		for name, line := range map[string]string{
			"applied non-number":      "Stage 6.3 review state: many remediations applied; twenty-two reviews run; the twenty-first remediation is outstanding.",
			"applied deleted":         "Stage 6.3 review state: remediations applied; twenty-two reviews run; the twenty-first remediation is outstanding.",
			"reviews deleted":         "Stage 6.3 review state: twenty-one remediations applied; reviews run; the twenty-first remediation is outstanding.",
			"outstanding deleted":     "Stage 6.3 review state: twenty-one remediations applied; twenty-two reviews run; the remediation is outstanding.",
			"digits instead of words": "Stage 6.3 review state: 21 remediations applied; 22 reviews run; the 21st remediation is outstanding.",
			"prefix swallowing":       "Stage 6.2 had four remediations applied; the count was twenty-two reviews run; the twenty-first remediation is outstanding.",
		} {
			if _, ok := stage63ParseStateLine(line); ok {
				t.Errorf("%s: parsed %q, which must be rejected", name, line)
			}
		}
	})

	t.Run("a mention of the marker is not a state line", func(t *testing.T) {
		// The review record explains that documents must carry a state line, so
		// the marker's own text appears in prose. Matching on presence rather
		// than position counted that explanation as a second claim.
		doc := "Each document must carry a `Stage 6.3 review state:` line.\n" + valid + "\n"
		lines := stage63StateLines(doc)
		if len(lines) != 1 {
			t.Fatalf("found %d state lines, want 1; a mention of the marker in prose was counted: %q", len(lines), lines)
		}
	})

	t.Run("quotes and foreign stages are not state lines", func(t *testing.T) {
		// These are the shapes that defeated whole-file first-match scanning. They
		// must contribute no state line at all, so they cannot be read as the
		// document's claim.
		doc := "| round 21 exit | twenty-one remediations applied |\n" +
			"Stage 6.2 had four remediations applied.\n" +
			"the count was twenty remediations applied in the previous round\n" +
			valid + "\n"
		lines := stage63StateLines(doc)
		if len(lines) != 1 {
			t.Fatalf("found %d state lines, want exactly 1: %q", len(lines), lines)
		}
		if got, ok := stage63ParseStateLine(lines[0]); !ok || got.applied != 21 {
			t.Errorf("the surviving state line parsed as %+v (ok=%v); a preceding quote was read instead", got, ok)
		}
	})

	t.Run("missing and duplicated lines", func(t *testing.T) {
		if lines := stage63StateLines("nothing here\njust prose\n"); len(lines) != 0 {
			t.Errorf("found %d state lines in a document with none", len(lines))
		}
		if lines := stage63StateLines(valid + "\n" + valid + "\n"); len(lines) != 2 {
			t.Errorf("found %d state lines, want 2 so the caller can reject the duplication", len(lines))
		}
	})
}
