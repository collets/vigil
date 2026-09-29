package scenario

import (
	"os"
	"path/filepath"
	"sync"
)

// readRepoFile reads a repository-relative file. The scenario runs from the
// repository root by default, and falls back to walking up from the working
// directory so it also works from a subdirectory or an agent-owned worktree.
func readRepoFile(relative string) (string, error) {
	if raw, err := os.ReadFile(relative); err == nil {
		return string(raw), nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for range 6 {
		candidate := filepath.Join(dir, relative)
		if raw, err := os.ReadFile(candidate); err == nil {
			return string(raw), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}

var (
	platformOnce sync.Once
	platformErr  error
)
