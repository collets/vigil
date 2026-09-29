package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FixtureFile is one materialized file of the disposable scenario repository,
// recorded with its exact content digest so the manifest proves which bytes the
// walkthrough ran against.
type FixtureFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Digest string `json:"digest"`
}

// Fixture is the complete deterministic content of the disposable scenario
// repository. It is a closed, in-code definition rather than a template
// directory so the exact bytes under test are reviewable in the diff and can
// never drift from the manifest.
//
// The repository is deliberately small: one objective task, one deterministic
// check command, one explicitly manual criterion, and one deterministically
// injected defect that a fresh reviewer is expected to find.
type Fixture struct {
	Files []FixtureFile
}

// Fixture constants that the rest of the scenario refers to. They are paths
// inside the disposable repository, never absolute host paths.
const (
	// FixtureMarker is the committed disposable-fixture marker every fixture
	// path requires before any command may act on the repository.
	FixtureMarker = ".vigil-disposable-fixture"
	// SourcePath is the single agent-owned file the task implements, and the only
	// file any attempt in this scenario writes. Keeping it to one file matters:
	// the production runner requires a reported changed-path set to match the
	// checkout exactly, and it validates that set against the accumulated dirty
	// paths, so a rehearsal spanning several files would not be exercising the
	// real single-attempt contract.
	SourcePath = "src/greeting.txt"
	// CheckScript is the deterministic check the task's criteria require.
	CheckScript = "scripts/check.sh"
	// SpecPath is the Markdown specification the planning step imports.
	SpecPath = "spec.md"
	// FixtureRemoteName is the local bare remote used for the delivery rehearsal.
	FixtureRemoteName = "fixture-origin"
)

// ExpectedGreeting is the exact content the specification requires.
const ExpectedGreeting = "hello from vigil stage 5.7\n"

// CommittedGreeting is what the fixture repository starts with: the wrong,
// previous-stage content. The implementation attempt replaces it.
const CommittedGreeting = "hello from vigil stage 5.6\n"

// InjectedDefectGreeting is what the implementation attempt writes. It is the
// specified greeting *with a trailing space*, which is a real defect: the
// specification requires the line with no trailing whitespace.
//
// The injected defect is deliberately invisible to the deterministic check, which
// compares the trimmed line. That is what makes the review/repair cycle
// meaningful rather than a restatement of the check: the check passes, a fresh
// reviewer session finds the real formatting defect, the repair removes it, and
// fresh check and review evidence is then required. Its origin is this scenario's
// fixture and is labelled as such in the report — it is not a reviewer result
// invented to reach a preferred outcome.
const InjectedDefectGreeting = "hello from vigil stage 5.7 \n"

// checkScript is the fixture's own deterministic check. It always writes its
// required output describing what it observed and exits nonzero when the artifact
// does not match, so the reported status stays an honest function of the exit
// code rather than being masked by a missing-output condition.
//
// It compares the trimmed line on purpose: the trailing whitespace that the
// reviewer reports is outside what an automated check is asked to judge.
func checkScript() string {
	return "#!/bin/sh\n" +
		"# Deterministic objective check for the Stage 5.7 scenario task.\n" +
		"#\n" +
		"# It always writes its required output describing what it observed, and\n" +
		"# exits nonzero when the artifact does not match the specification. The\n" +
		"# comparison is on the trimmed line: trailing whitespace is deliberately\n" +
		"# outside what this automated check judges, and inside what a fresh\n" +
		"# reviewer reports.\n" +
		"set -eu\n" +
		"mkdir -p build\n" +
		"expected='" + strings.TrimSuffix(ExpectedGreeting, "\n") + "'\n" +
		"raw=$(cat " + SourcePath + " 2>/dev/null || true)\n" +
		"actual=$(printf '%s' \"$raw\" | tr -d '\\r' | sed -e 's/[[:space:]]*$//')\n" +
		"if [ \"$actual\" = \"$expected\" ]; then\n" +
		"  printf 'greeting verified\\n' > build/result.txt\n" +
		"  echo PASS\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf 'greeting mismatch\\n' > build/result.txt\n" +
		"echo \"greeting mismatch\" >&2\n" +
		"echo \"expected: $expected\" >&2\n" +
		"echo \"actual:   $actual\" >&2\n" +
		"exit 1\n"
}

// fixtureFiles is the closed content definition of the disposable repository.
// Every entry is a repository-relative regular file.
func fixtureFiles() map[string]string {
	return map[string]string{
		FixtureMarker: "disposable fixture for autonomous Stage 5.7 qualification\n",
		"README.md": "# Stage 5.7 disposable scenario\n\n" +
			"Agent-owned disposable fixture. It carries no real remote, no credential and\n" +
			"no user work. Every repository under qualification is a throwaway copy.\n",
		SpecPath: "# Greeting specification\n\n" +
			"Implement a single greeting artifact.\n\n" +
			"## Behavior\n\n" +
			"`" + SourcePath + "` must contain exactly one line, with no trailing\n" +
			"whitespace:\n\n" +
			"```\n" + ExpectedGreeting + "```\n\n" +
			"## Verification\n\n" +
			"`" + CheckScript + "` is the deterministic check. It must exit zero and write\n" +
			"`build/result.txt`.\n",
		SourcePath:  CommittedGreeting,
		CheckScript: checkScript(),
	}
}

// Materialize writes the fixture into root, creating a private directory. It
// refuses to touch a root that already exists and is not empty, so it can never
// overwrite anything the operator owns.
func (f Fixture) Materialize(root string) error {
	if info, err := os.Lstat(root); err == nil {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return fmt.Errorf("inspect fixture root: %w", readErr)
		}
		if len(entries) > 0 {
			return fmt.Errorf("refusing to write the scenario fixture into a non-empty directory")
		}
		if !info.IsDir() {
			return fmt.Errorf("fixture root is not a directory")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect fixture root: %w", err)
	}
	for path, content := range fixtureFiles() {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			return err
		}
	}
	// The check script is created executable, so the committed fixture is
	// directly runnable and the baseline can be executed as committed.
	if err := os.Chmod(filepath.Join(root, filepath.FromSlash(CheckScript)), 0o700); err != nil {
		return err
	}
	return nil
}

// NewFixture returns the fixture description with every file digested in a
// stable order, so a manifest records exactly which content was used.
func NewFixture() Fixture {
	contents := fixtureFiles()
	paths := make([]string, 0, len(contents))
	for path := range contents {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]FixtureFile, 0, len(paths))
	for _, path := range paths {
		raw := []byte(contents[path])
		files = append(files, FixtureFile{Path: path, Bytes: len(raw), Digest: Digest(raw)})
	}
	return Fixture{Files: files}
}

// Digest is the manifest's content digest function: SHA-256 over raw bytes,
// matching the digest format the persisted core records.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// DigestOf digests a string.
func DigestOf(value string) string { return Digest([]byte(value)) }
