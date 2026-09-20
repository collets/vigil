package workspace

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Repository struct {
	RelativeRoot string   `json:"relative_root"`
	Identity     Identity `json:"identity"`
	GitLayout    string   `json:"git_layout"`
	HeadOID      string   `json:"head_oid,omitempty"`
	HeadRef      string   `json:"head_ref,omitempty"`
	Issues       []string `json:"issues"`
}

type Discovery struct {
	Root            Identity     `json:"root"`
	Repositories    []Repository `json:"repositories"`
	SkippedSymlinks int          `json:"skipped_symlinks"`
	Visited         int          `json:"visited"`
}

// Discover inventories existing roots only. It does not run status, filters,
// hooks, repository scripts, submodule updates or any Git mutation. Enrollment,
// base selection and dirty-work preservation remain separate human decisions.
func Discover(ctx context.Context, root string) (Discovery, error) {
	var result Discovery
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	id, err := inspectDirectory(root)
	if err != nil {
		return result, err
	}
	result.Root = id
	result.Repositories = []Repository{}
	err = filepath.WalkDir(id.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result.Visited++
		if result.Visited > 100000 {
			return errors.New("repository discovery exceeds 100000 entries; narrow the project root")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			result.SkippedSymlinks++
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != id.Root && strings.EqualFold(entry.Name(), ".git") {
			return filepath.SkipDir
		}
		gitPath := filepath.Join(path, ".git")
		info, err := os.Lstat(gitPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(result.Repositories) >= 100 {
			return errors.New("repository discovery exceeds 100 roots")
		}
		rel, err := filepath.Rel(id.Root, path)
		if err != nil {
			return err
		}
		repo := Repository{RelativeRoot: rel, Issues: []string{}}
		repo.Identity, err = inspectDirectory(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			repo.GitLayout = "unsupported_symlink"
			repo.Issues = append(repo.Issues, "Git administration symlink requires explicit qualification")
			result.Repositories = append(result.Repositories, repo)
			return nil
		}
		if info.IsDir() {
			repo.GitLayout = "directory"
		} else if info.Mode().IsRegular() {
			repo.GitLayout = "gitfile"
		} else {
			repo.GitLayout = "unsupported_special"
			repo.Issues = append(repo.Issues, "Git administration path is a special file")
			result.Repositories = append(result.Repositories, repo)
			return nil
		}
		repo.Identity, err = Inspect(ctx, path)
		if err != nil {
			return err
		}
		if repo.Identity.CommonGit == "" {
			repo.Issues = append(repo.Issues, "Git metadata is invalid or unreadable")
		} else {
			repo.HeadOID, err = gitObservation(ctx, path, "rev-parse", "--verify", "HEAD^{commit}")
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				repo.Issues = append(repo.Issues, "HEAD commit is unavailable (possibly unborn)")
			}
			repo.HeadRef, err = gitObservation(ctx, path, "symbolic-ref", "--quiet", "HEAD")
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
		}
		if repo.GitLayout == "gitfile" {
			repo.Issues = append(repo.Issues, "Linked checkout or submodule: not enrolled or execution-qualified")
		}
		if rel == "." {
			result.Root = repo.Identity
		}
		result.Repositories = append(result.Repositories, repo)
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := id.Validate(); err != nil {
		return result, err
	}
	for _, repo := range result.Repositories {
		if repo.Identity.Root != "" {
			if err := repo.Identity.Validate(); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func gitEnvironment() []string {
	var env []string
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			env = append(env, variable)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0")
}

func gitObservation(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	cmd.Env = gitEnvironment()
	var output gitOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", errors.New("Git observation unavailable")
	}
	return strings.TrimSpace(output.String()), nil
}

type gitOutput struct{ strings.Builder }

func (b *gitOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8192 {
		return 0, errors.New("Git observation exceeds limit")
	}
	return b.Builder.Write(p)
}
