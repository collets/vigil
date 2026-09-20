// Package workspace provides read-only repository and filesystem identities.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Node struct {
	Path string `json:"path"`
	Key  string `json:"key"`
}
type Identity struct {
	Root          string `json:"root"`
	Key           string `json:"key"`
	Ancestors     []Node `json:"ancestors"`
	CommonGit     string `json:"common_git,omitempty"`
	CommonGitPath string `json:"common_git_path,omitempty"`
}

func node(path string) (Node, error) {
	i, err := os.Stat(path)
	if err != nil {
		return Node{}, err
	}
	if !i.IsDir() {
		return Node{}, errors.New("workspace root must be an existing directory")
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return Node{}, errors.New("filesystem identity unavailable")
	}
	return Node{Path: path, Key: fmt.Sprintf("%d:%d", s.Dev, s.Ino)}, nil
}
func Inspect(ctx context.Context, path string) (Identity, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Identity{}, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return Identity{}, err
	}
	n, err := node(path)
	if err != nil {
		return Identity{}, err
	}
	id := Identity{Root: path, Key: n.Key}
	for ancestor := path; ; ancestor = filepath.Dir(ancestor) {
		n, err := node(ancestor)
		if err != nil {
			return Identity{}, err
		}
		id.Ancestors = append(id.Ancestors, n)
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--git-common-dir")
	// The caller's Git shell environment must not redirect identity discovery
	// to an unrelated repository or inject configuration into the probe.
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0")
	if b, err := cmd.Output(); err == nil {
		gitPath := strings.TrimSpace(string(b))
		if !filepath.IsAbs(gitPath) {
			gitPath = filepath.Join(path, gitPath)
		}
		gitPath, err = filepath.EvalSymlinks(gitPath)
		if err != nil {
			return Identity{}, err
		}
		n, err = node(gitPath)
		if err != nil {
			return Identity{}, err
		}
		id.CommonGit = n.Key
		id.CommonGitPath = gitPath
	} else if ctx.Err() != nil {
		return Identity{}, ctx.Err()
	}
	return id, nil
}
func Overlap(a, b Identity) bool {
	if a.CommonGit != "" && a.CommonGit == b.CommonGit {
		return true
	}
	for _, n := range a.Ancestors {
		if n.Key == b.Key {
			return true
		}
	}
	for _, n := range b.Ancestors {
		if n.Key == a.Key {
			return true
		}
	}
	return false
}
func (id Identity) Validate() error {
	for _, prior := range id.Ancestors {
		now, err := node(prior.Path)
		if err != nil {
			return err
		}
		if now.Key != prior.Key {
			return errors.New("workspace identity changed; reconciliation required")
		}
	}
	if id.CommonGit != "" {
		now, err := node(id.CommonGitPath)
		if err != nil {
			return err
		}
		if now.Key != id.CommonGit {
			return errors.New("Git administration directory replaced")
		}
	}
	return nil
}
