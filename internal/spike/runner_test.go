package spike

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredResult(t *testing.T) {
	valid := `{"summary":"Changed message","files":["message.txt"]}`
	if _, err := validateResult(valid); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"", "```json\n" + valid + "\n```", valid + valid, `{"summary":"","files":["message.txt"]}`, `{"summary":"ok","files":["other"]}`, `{"summary":"ok","files":["message.txt"],"accepted":true}`, `{"summary":"ok","files":null}`} {
		if _, err := validateResult(text); err == nil {
			t.Errorf("accepted invalid result: %s", text)
		}
	}
}

func TestIndependentFixtureVerification(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-content", "instructions", "check", "extra", "ignored", "symlink", "remote", "commit"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + cwd, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
			ctx := context.Background()
			run := func(args ...string) string {
				t.Helper()
				out, err := command(ctx, cwd, env, args...)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			write := func(name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"README.md", "check.sh", "message.txt"} {
				b, err := os.ReadFile(filepath.Join("../../testdata/spike", name))
				if err != nil {
					t.Fatal(err)
				}
				write(name, string(b))
			}
			run("git", "init", "-q")
			run("git", "add", ".")
			commit := func() {
				run("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
			}
			commit()
			m := Manifest{Workspace: cwd, Baseline: run("git", "rev-parse", "HEAD")}
			if err := validateBaseline(ctx, m, env); err != nil {
				t.Fatal(err)
			}
			write("message.txt", "adapter spike ready\n")
			switch mode {
			case "wrong-content":
				write("message.txt", "incorrect\n")
			case "instructions":
				write("README.md", "changed\n")
			case "check":
				write("check.sh", "exit 0\n")
			case "extra":
				write("other.txt", "extra")
			case "ignored":
				write(".git/info/exclude", "hidden\n")
				write("hidden", "extra")
			case "symlink":
				os.Remove(filepath.Join(cwd, "message.txt"))
				if err := os.Symlink("README.md", filepath.Join(cwd, "message.txt")); err != nil {
					t.Fatal(err)
				}
			case "remote":
				run("git", "remote", "add", "origin", "https://example.invalid/fixture")
			case "commit":
				run("git", "add", ".")
				commit()
			}
			err := verifyFixture(ctx, m, env)
			if mode == "valid" && err != nil {
				t.Fatal(err)
			}
			if mode != "valid" && err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}

func TestJournalOmitsPayloadsAndBoundsEvidence(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "events")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	j := journal{f: f, remaining: 128, secrets: []string{"credential"}}
	if err := j.write(map[string]string{"value": "credential"}); err != nil {
		t.Fatal(err)
	}
	if err := j.write(strings.Repeat("x", 129)); err == nil {
		t.Fatal("evidence overflow accepted")
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "credential") || !json.Valid(b) {
		t.Fatal("secret or malformed record")
	}
}
