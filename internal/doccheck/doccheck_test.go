// Package doccheck holds the documentation consistency gate.
//
// It reads docs/STATUS as the single source of truth for current state and fails
// when the documentation drifts from the code, the schema, or itself. Run it with
// `make docs-check`; it is also part of `make check`, so it runs in every gate.
package doccheck

import (
	"bufio"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"vigil/internal/cli"
)

const (
	repoRoot    = "../.."
	statusFile  = "docs/STATUS"
	markdownExt = ".md"
)

// status is the parsed contents of docs/STATUS.
type status map[string]string

func loadStatus(t *testing.T) status {
	t.Helper()
	file, err := os.Open(filepath.Join(repoRoot, statusFile))
	if err != nil {
		t.Fatalf("open %s: %v", statusFile, err)
	}
	defer file.Close()
	parsed := status{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("%s: malformed line %q (want key=value)", statusFile, line)
		}
		parsed[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", statusFile, err)
	}
	for _, required := range []string{
		"stage", "stage_accepted", "implementation_commit", "review_record",
		"project_migrations", "coordination_migrations", "index",
		"status_documents", "cli_reference",
	} {
		if parsed[required] == "" {
			t.Errorf("%s is missing required key %q", statusFile, required)
		}
	}
	return parsed
}

func repoPath(parts ...string) string {
	return filepath.Join(append([]string{repoRoot}, parts...)...)
}

// markdownFiles returns every tracked markdown file in the repository.
func markdownFiles(t *testing.T) []string {
	t.Helper()
	var found []string
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".cache", ".tools", "bin", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, markdownExt) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	sort.Strings(found)
	return found
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// --- relative markdown links resolve ---------------------------------------

var linkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

func TestDocsRelativeLinksResolve(t *testing.T) {
	for _, file := range markdownFiles(t) {
		content := readFile(t, file)
		for _, match := range linkPattern.FindAllStringSubmatch(content, -1) {
			target := strings.TrimSpace(match[1])
			if target == "" || strings.HasPrefix(target, "#") {
				continue
			}
			if index := strings.IndexAny(target, "# "); index >= 0 {
				target = target[:index]
			}
			if target == "" {
				continue
			}
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.Join(filepath.Dir(file), filepath.FromSlash(target))
			if _, err := os.Stat(resolved); err != nil {
				line := lineOf(content, match[0])
				t.Errorf("%s:%d: broken link %q", file, line, match[1])
			}
		}
	}
}

func lineOf(content, needle string) int {
	for index, line := range strings.Split(content, "\n") {
		if strings.Contains(line, needle) {
			return index + 1
		}
	}
	return 0
}

// --- no orphan documents ----------------------------------------------------

// TestDocsIndexHasNoOrphans requires every markdown file under docs/ to be
// reachable by filename from the index, so a new document cannot be added
// without being placed in the documentation map.
func TestDocsIndexHasNoOrphans(t *testing.T) {
	current := loadStatus(t)
	index := readFile(t, repoPath(current["index"]))
	for _, file := range markdownFiles(t) {
		relative, err := filepath.Rel(repoRoot, file)
		if err != nil {
			t.Fatal(err)
		}
		slash := filepath.ToSlash(relative)
		if !strings.HasPrefix(slash, "docs/") || slash == current["index"] {
			continue
		}
		// The index may spell a path relative to docs/ or to the repository
		// root. A single entry may cover a whole subdirectory (a directory of
		// retained review probes, for example), but a document sitting directly
		// in docs/ must be named individually, otherwise one incidental
		// occurrence of "docs/" would satisfy every top-level document.
		underDocs := strings.TrimPrefix(slash, "docs/")
		named := strings.Contains(index, slash) || strings.Contains(index, underDocs)
		if dir := path.Dir(underDocs); dir != "." && strings.Contains(index, dir+"/") {
			named = true
		}
		if named {
			continue
		}
		t.Errorf("%s is not listed in %s; add it to the documentation index", slash, current["index"])
	}
}

// --- CLI reference completeness --------------------------------------------

// TestDocsCLIRefIsComplete walks the real Cobra command tree and requires every
// command to appear in the CLI reference. This is the check that stops the CLI
// and its documentation from diverging.
func TestDocsCLIRefIsComplete(t *testing.T) {
	current := loadStatus(t)
	reference := readFile(t, repoPath(current["cli_reference"]))
	root := cli.NewCommand()
	for _, command := range flatten(root) {
		needle := command
		if !strings.Contains(needle, " ") {
			needle = "vigil " + needle
		}
		if !strings.Contains(reference, needle) {
			t.Errorf("command %q is not documented in %s", command, current["cli_reference"])
		}
	}
}

// flatten returns every non-root command as a space separated path, using the
// first word of each Use string as the command name.
func flatten(command *cobra.Command) []string {
	var paths []string
	var walk func(parent string, current *cobra.Command)
	walk = func(parent string, current *cobra.Command) {
		for _, child := range current.Commands() {
			name, _, _ := strings.Cut(child.Use, " ")
			path := strings.TrimSpace(parent + " " + name)
			paths = append(paths, path)
			walk(path, child)
		}
	}
	walk("", command)
	sort.Strings(paths)
	return paths
}

// --- README structure block matches internal/ -------------------------------

// TestDocsStructureBlockListsEveryPackage requires every Go package directory
// under internal/ to appear in the README structure block.
func TestDocsStructureBlockListsEveryPackage(t *testing.T) {
	readme := readFile(t, repoPath("README.md"))
	entries, err := os.ReadDir(repoPath("internal"))
	if err != nil {
		t.Fatalf("read internal: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !strings.Contains(readme, "internal/"+entry.Name()) {
			t.Errorf("package internal/%s is not listed in the README structure block", entry.Name())
		}
	}
}

// --- migration counts -------------------------------------------------------

// TestDocsMigrationCountsMatch compares the migration counts recorded in STATUS
// with the migrations actually present.
func TestDocsMigrationCountsMatch(t *testing.T) {
	current := loadStatus(t)
	for _, check := range []struct{ key, glob string }{
		{"project_migrations", "project-*.sql"},
		{"coordination_migrations", "coordination-*.sql"},
	} {
		matches, err := filepath.Glob(repoPath("internal", "store", "migrations", check.glob))
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := strconv.Atoi(current[check.key])
		if err != nil {
			t.Errorf("%s: %s is not a number: %v", statusFile, check.key, err)
			continue
		}
		if len(matches) != claimed {
			t.Errorf("%s: %s=%d but %d migration files are present in internal/store/migrations (%s)",
				statusFile, check.key, claimed, len(matches), check.glob)
		}
	}
}

// --- status documents agree with STATUS ------------------------------------

// TestDocsStatusDocumentsAgree requires every document that carries stage status
// to name the current implementation commit. A status flip applied to only some
// documents is the failure this catches.
func TestDocsStatusDocumentsAgree(t *testing.T) {
	current := loadStatus(t)
	commit := current["implementation_commit"]
	if !commitExists(t, commit) {
		t.Fatalf("%s: implementation_commit %q does not exist in this repository", statusFile, commit)
	}
	for _, relative := range strings.Split(current["status_documents"], ",") {
		relative = strings.TrimSpace(relative)
		if relative == "" {
			continue
		}
		raw, err := os.ReadFile(repoPath(relative))
		if err != nil {
			t.Errorf("%s: status document listed in %s does not exist: %v", statusFile, statusFile, err)
			continue
		}
		if !strings.Contains(string(raw), commit) {
			t.Errorf("%s does not mention the current implementation commit %s from %s; a status change must reach every status document",
				relative, commit, statusFile)
		}
	}
}

// --- commit ranges resolve, and next-action text is current -----------------

var rangePattern = regexp.MustCompile(`\b([0-9a-f]{7,40})\.\.([0-9a-f]{7,40})\b`)

// TestDocsCommitRangesResolve requires every A..B range named in the
// documentation to name two real commits with A an ancestor of B.
// requireGit skips the git-dependent checks when git is unavailable, so a missing
// toolchain produces a clear skip rather than a wall of false "not a commit"
// failures.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available; skipping commit cross-reference checks")
	}
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Run(); err != nil {
		t.Skipf("%s is not a git working tree; skipping commit cross-reference checks", repoRoot)
	}
}

func TestDocsCommitRangesResolve(t *testing.T) {
	requireGit(t)
	for _, file := range markdownFiles(t) {
		content := readFile(t, file)
		for _, match := range rangePattern.FindAllStringSubmatch(content, -1) {
			from, to := match[1], match[2]
			if !commitExists(t, from) {
				t.Errorf("%s:%d: range start %q is not a commit in this repository", file, lineOf(content, match[0]), from)
				continue
			}
			if !commitExists(t, to) {
				t.Errorf("%s:%d: range end %q is not a commit in this repository", file, lineOf(content, match[0]), to)
				continue
			}
			if !isAncestor(t, from, to) {
				t.Errorf("%s:%d: range %q is not an ancestor-ordered range in this repository", file, lineOf(content, match[0]), match[0])
			}
		}
	}
}

// TestDocsNextActionRangeIsCurrent requires live next-action guidance to name a
// commit that is not already superseded. A stale next-action range is a real and
// repeated failure mode in this repository: it sends the next session to re-review
// a range that was already reviewed.
//
// The check is scoped to the status documents, which is where live guidance lives.
// Review records legitimately quote superseded ranges when describing a finding,
// so scanning them would report the finding text itself.
func TestDocsNextActionRangeIsCurrent(t *testing.T) {
	requireGit(t)
	current := loadStatus(t)
	head := current["implementation_commit"]
	action := regexp.MustCompile(`(?i)(next concrete action|next action|the next step)`)
	for _, relative := range strings.Split(current["status_documents"], ",") {
		relative = strings.TrimSpace(relative)
		if relative == "" {
			continue
		}
		content := readFile(t, repoPath(relative))
		for index, line := range strings.Split(content, "\n") {
			if !action.MatchString(line) {
				continue
			}
			for _, match := range rangePattern.FindAllStringSubmatch(line, -1) {
				end := match[2]
				if !commitExists(t, end) || !commitExists(t, head) {
					continue
				}
				if end != head && isAncestor(t, end, head) {
					t.Errorf("%s:%d: next-action text names the superseded range %q; the current implementation commit is %s",
						relative, index+1, match[0], head)
				}
			}
		}
	}
}

func commitExists(t *testing.T, revision string) bool {
	t.Helper()
	return exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", revision+"^{commit}").Run() == nil
}

func isAncestor(t *testing.T, from, to string) bool {
	t.Helper()
	return exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", from, to).Run() == nil
}
