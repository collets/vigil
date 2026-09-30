package scenario

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// gitFixtureIdentity is the fixed, obviously-fake identity the disposable
// repository uses. It is never the operator's identity and never a real address.
const (
	FixtureAuthorName  = "Vigil Scenario Fixture"
	FixtureAuthorEmail = "fixture@invalid"
)

// git runs one bounded Git command inside the disposable repository with
// ambient configuration removed. The scenario never runs hooks, filters,
// status from a status alias, or any command that could read or write outside
// the disposable root.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"GIT_CONFIG_GLOBAL=" + os.Getenv("GIT_CONFIG_NOSYSTEM"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_AUTHOR_NAME=" + FixtureAuthorName,
		"GIT_AUTHOR_EMAIL=" + FixtureAuthorEmail,
		"GIT_COMMITTER_NAME=" + FixtureAuthorName,
		"GIT_COMMITTER_EMAIL=" + FixtureAuthorEmail,
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	stdout, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(stdout)), nil
}

// InitRepository creates the disposable Git repository, commits the fixture
// content, and records the base commit. The committed content deliberately
// includes the injected defect, so the review/repair cycle has something real
// to find.
func InitRepository(ctx context.Context, root string) (string, error) {
	if _, err := git(ctx, root, "init", "-q", "-b", "main", "."); err != nil {
		return "", err
	}
	if _, err := git(ctx, root, "add", "--all", "."); err != nil {
		return "", err
	}
	if _, err := git(ctx, root, "commit", "-q", "-m", "fixture: stage 5.7 scenario base"); err != nil {
		return "", err
	}
	return git(ctx, root, "rev-parse", "HEAD")
}

// InitBareRemote creates the local bare remote used by the delivery rehearsal.
// It is a plain directory with no network listener and no credential.
func InitBareRemote(ctx context.Context, path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if _, err := git(ctx, path, "init", "-q", "--bare", "-b", "main", "."); err != nil {
		return err
	}
	return nil
}

// AddRemote registers the local bare remote under a fixture name.
func AddRemote(ctx context.Context, root, name, url string) error {
	if _, err := git(ctx, root, "remote", "add", name, url); err != nil {
		return err
	}
	return nil
}

// RefValue reads one exact ref in the bare remote.
func RefValue(ctx context.Context, bare, ref string) (string, error) {
	value, err := git(ctx, bare, "rev-parse", "--verify", "--quiet", ref)
	if errors.Is(err, nil) && value == "" {
		return "", fmt.Errorf("ref %s is not present in the bare remote", ref)
	}
	return value, err
}

// ListRefs returns every ref in a repository or bare remote, for the
// no-unintended-refs boundary check.
func ListRefs(ctx context.Context, dir string) ([]string, error) {
	output, err := git(ctx, dir, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return nil, err
	}
	refs := []string{}
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			refs = append(refs, line)
		}
	}
	return refs, nil
}

// WorktreeStatus returns the porcelain status lines of the disposable
// repository, used to assert that a delivery operation never touched the user's
// index, worktree or HEAD.
func WorktreeStatus(ctx context.Context, root string) ([]string, error) {
	output, err := git(ctx, root, "status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// WorktreeDigest returns a stable digest of the working tree's file contents,
// excluding `.git` and the in-repository `.vigil` view.
//
// This is the observation that actually answers "did the delivery rehearsal change
// anything the operator can see in their files". Porcelain status cannot answer it
// once a commit legitimately clears a pending change, because the status going
// from one modified line to none is the point of committing rather than a
// disturbance — but a digest of the bytes is unchanged either way, and that is the
// property worth asserting.
func WorktreeDigest(ctx context.Context, root string) (string, error) {
	output, err := git(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	names := []string{}
	for _, name := range strings.Split(output, "\x00") {
		if name == "" || strings.HasPrefix(name, ".git/") || strings.HasPrefix(name, ".vigil/") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	digest := sha256.New()
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "%s\x00%d\x00", name, len(raw))
		digest.Write(raw)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// HeadCommit reads the current HEAD of the disposable repository.
func HeadCommit(ctx context.Context, root string) (string, error) {
	return git(ctx, root, "rev-parse", "HEAD")
}

// BranchName reads the current symbolic branch of the disposable repository.
func BranchName(ctx context.Context, root string) (string, error) {
	return git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
}

// RequireBareRemoteUnderRoot refuses a bare remote outside the agent-owned
// scenario root, so cleanup can never remove an operator's repository.
func RequireBareRemoteUnderRoot(scenarioRoot, bare string) error {
	rel, err := filepath.Rel(scenarioRoot, bare)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("bare remote must live inside the agent-owned scenario root")
	}
	return nil
}
