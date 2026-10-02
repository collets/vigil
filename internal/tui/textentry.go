package tui

import (
	"encoding/base64"
	"fmt"
)

// TextEntryLimit is the exact bound the clarification path has always
// enforced, in bytes: the mutator rejects answers over 4096 bytes, so the
// entry enforces the same unit at entry time rather than failing at submit.
const TextEntryLimit = 4096

// textEntry holds bounded input with a visible counter. The charset policy is
// clean(): control sequences never render. Empty submission is refused by the
// caller, which keeps the "cannot be empty" feedback next to the action that
// produced it.
type textEntry struct {
	text string
}

func (e *textEntry) add(fragment string) {
	if fragment == "" {
		return
	}
	for _, r := range fragment {
		next := e.text + string(r)
		if len(next) > TextEntryLimit {
			return
		}
		e.text = next
	}
}

func (e *textEntry) backspace() {
	if runes := []rune(e.text); len(runes) > 0 {
		e.text = string(runes[:len(runes)-1])
	}
}

func (e textEntry) counter() string {
	return fmt.Sprintf("%d/%d bytes", len(e.text), TextEntryLimit)
}

func (e textEntry) line() string {
	return "Answer (" + e.counter() + "): " + clean(e.text) + "  (Enter submit · Esc cancel locally · Ctrl+C quit)"
}

// encodeAnswer packs a submitted answer into the action string the mutator
// parses back. Base64 keeps the action colon-separated and bounded.
func encodeAnswer(answer string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(answer))
}

// entryPolicyLine states the charset rule where the operator can see it.
func entryPolicyLine() string {
	return "Bounded text: control characters are stripped, empty answers are refused."
}
