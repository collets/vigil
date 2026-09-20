package spike

import (
	"encoding/json"
	"testing"
)

func TestApprovalFixtureIsExactAndNotAnAllowlist(t *testing.T) {
	for _, command := range []string{"printf 'vigil-approval-probe\\n'", `/usr/bin/zsh -lc 'printf '\''vigil-approval-probe\n'\'''`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if !safeApprovalFixture(raw) {
			t.Fatalf("literal fixture rejected: %s", command)
		}
	}
	for _, command := range []string{"printf 'vigil-approval-probe\\n'; touch other", "echo vigil-approval-probe", "/usr/bin/zsh -lc 'printf x; git push'", "printf 'vigil-approval-probe$(whoami)'"} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if safeApprovalFixture(raw) {
			t.Fatalf("non-fixture command accepted: %s", command)
		}
	}
}
