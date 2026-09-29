package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"vigil/internal/workspace"
)

const maxCommittedFileBytes = 16 << 20

type boundedGitWriter struct{ bytes.Buffer }

func (w *boundedGitWriter) Write(p []byte) (int, error) {
	if w.Len()+len(p) > 16<<20 {
		return 0, errors.New("Git output exceeds 16 MiB")
	}
	return w.Buffer.Write(p)
}

// deliveryGit never inherits ambient GIT_* overrides, global/system config,
// credential helpers, hooks, filters or external diff programs. Commands are
// argument arrays, not shell fragments. Push has a separate route below.
func deliveryGit(ctx context.Context, root string, extra []string, input []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	argv := append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull,
		"-c", "credential.helper=", "-c", "diff.external=", "-c", "core.attributesFile=" + os.DevNull}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}, extra...)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr boundedGitWriter
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("controlled Git %s failed: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func exactCommitPath(name string) bool {
	if name == "" || len(name) > 4096 || name == "." || path.IsAbs(name) || strings.ContainsAny(name, "\x00\r\n") || strings.Contains(name, "\\") {
		return false
	}
	clean := path.Clean(name)
	if clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".git" || part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func gitOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}

func safeCommittedBytes(root *os.Root, name string) ([]byte, string, bool, error) {
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		ancestor := strings.Join(parts[:i], "/")
		info, err := root.Lstat(ancestor)
		if err != nil || !info.IsDir() {
			return nil, "", false, errors.New("commit path has a missing or non-directory ancestor")
		}
	}
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := root.Readlink(name)
		if err != nil || len(target) > maxCommittedFileBytes {
			return nil, "", false, errors.New("invalid commit symlink")
		}
		return []byte(target), "120000", true, nil
	}
	if !info.Mode().IsRegular() || info.Size() > maxCommittedFileBytes || info.Size() < 0 {
		return nil, "", false, errors.New("commit path is not a bounded regular file or symlink")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, "", false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxCommittedFileBytes+1))
	if err != nil || len(b) > maxCommittedFileBytes || int64(len(b)) != info.Size() {
		return nil, "", false, errors.New("commit file changed or exceeds limit")
	}
	mode := "100644"
	if info.Mode().Perm()&0111 != 0 {
		mode = "100755"
	}
	return b, mode, true, nil
}

// buildCommitTree uses a private temporary index. Preview writes only to a
// temporary alternate object database; effect mode writes objects to the
// enrolled repository after exact approval. Neither mode edits the user index,
// worktree, HEAD or any checked-out branch.
func buildCommitTree(ctx context.Context, record RepositoryRecord, parent string, paths []string, effect bool) (string, error) {
	if err := record.Identity.Validate(); err != nil {
		return "", err
	}
	if !gitOID(parent) || len(paths) == 0 || len(paths) > 1000 {
		return "", errors.New("bounded exact commit parent and paths required")
	}
	tmp, err := os.MkdirTemp("", "vigil-commit-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // agent-owned, private temporary objects/index only
	index := filepath.Join(tmp, "index")
	gitEnv := []string{"GIT_INDEX_FILE=" + index}
	if !effect {
		objects := filepath.Join(tmp, "objects")
		if err := os.Mkdir(objects, 0700); err != nil {
			return "", err
		}
		gitEnv = append(gitEnv, "GIT_OBJECT_DIRECTORY="+objects,
			"GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(record.Identity.CommonGitPath, "objects"))
	}
	if _, err := deliveryGit(ctx, record.Root, gitEnv, nil, "read-tree", parent); err != nil {
		return "", err
	}
	opened, err := os.OpenRoot(record.Root)
	if err != nil {
		return "", err
	}
	defer opened.Close()
	seen := map[string]bool{}
	for _, name := range paths {
		if !exactCommitPath(name) || seen[name] {
			return "", errors.New("commit paths must be unique exact repository-relative names")
		}
		seen[name] = true
		b, mode, present, err := safeCommittedBytes(opened, name)
		if err != nil {
			return "", err
		}
		if !present {
			if _, err := deliveryGit(ctx, record.Root, gitEnv, nil, "update-index", "--force-remove", "--", name); err != nil {
				return "", err
			}
			continue
		}
		oid, err := deliveryGit(ctx, record.Root, gitEnv, b, "hash-object", "-w", "--no-filters", "--stdin")
		if err != nil || !gitOID(oid) {
			return "", errors.New("failed to create exact no-filter commit blob")
		}
		if _, err := deliveryGit(ctx, record.Root, gitEnv, nil, "update-index", "--add", "--cacheinfo", mode+","+oid+","+name); err != nil {
			return "", err
		}
	}
	tree, err := deliveryGit(ctx, record.Root, gitEnv, nil, "write-tree")
	if err != nil || !gitOID(tree) {
		return "", errors.New("failed to build exact commit tree")
	}
	prior, err := deliveryGit(ctx, record.Root, gitEnv, nil, "rev-parse", "--verify", parent+"^{tree}")
	if err != nil || !gitOID(prior) || prior == tree {
		return "", errors.New("selected paths produce no commit change")
	}
	return tree, nil
}

func acceptedBaselineForRepository(ctx context.Context, scopeManifest string, record RepositoryRecord) (workspace.Baseline, error) {
	var accepted []struct {
		ID       string             `json:"id"`
		Revision int                `json:"revision"`
		Identity workspace.Identity `json:"identity"`
		Observed workspace.Baseline `json:"observed"`
	}
	if err := json.Unmarshal([]byte(scopeManifest), &accepted); err != nil {
		return workspace.Baseline{}, err
	}
	for _, candidate := range accepted {
		if candidate.ID == record.ID && candidate.Revision == record.Revision && reflect.DeepEqual(candidate.Identity, record.Identity) {
			// Normalize the stored exclusion set before comparing: a stored
			// acceptance that predates an application-owned exclusion (e.g.
			// .vigil) must not be invalidated by the forced set alone.
			normalized, err := workspace.NormalizeExclusions(record.Root, candidate.Observed.Exclusions)
			if err != nil {
				return workspace.Baseline{}, errors.New("accepted task fingerprint exclusions are invalid")
			}
			expected := candidate.Observed
			expected.Exclusions = normalized
			observed, err := workspace.Fingerprint(ctx, record.Root, candidate.Observed.Exclusions)
			if err != nil || !reflect.DeepEqual(observed, expected) {
				return workspace.Baseline{}, errors.New("repository no longer matches accepted task fingerprint")
			}
			return expected, nil
		}
	}
	return workspace.Baseline{}, errors.New("accepted task scope lacks enrolled repository revision")
}
