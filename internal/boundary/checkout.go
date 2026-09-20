package boundary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"vigil/internal/workspace"
)

// Checkout is a conservative admission plan for a single ordinary Git checkout.
// It does not authorize launch. The trusted controller must hold ownership,
// validate immediately before Docker create, and verify the actual mounts.
// Unsupported aliases/layouts are rejected rather than partially protected.
type Checkout struct {
	Root      string   `json:"root"`
	Protected []string `json:"protected"`
	identity  workspace.Identity
	seal      string
}

var protectedNames = []string{".git", ".gitmodules", "AGENTS.md", "AGENTS.override.md", "CLAUDE.md", "GEMINI.md", ".agents", ".codex", ".hermes"}

func protectedName(name string) bool {
	for _, p := range protectedNames {
		if strings.EqualFold(name, p) {
			return true
		}
	}
	return false
}

func PlanCheckout(ctx context.Context, root string) (Checkout, error) {
	id, err := workspace.Inspect(ctx, root)
	if err != nil {
		return Checkout{}, err
	}
	if strings.ContainsAny(id.Root, ",\r\n") {
		return Checkout{}, errors.New("checkout path cannot be represented safely in a Docker mount")
	}
	git := filepath.Join(id.Root, ".git")
	info, err := os.Lstat(git)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || id.CommonGitPath != git {
		return Checkout{}, errors.New("boundary requires an ordinary Git root; linked worktrees, submodules and external Git directories are unsupported")
	}
	plan := Checkout{Root: id.Root, identity: id}
	plan.Protected, plan.seal, err = scanCheckout(ctx, id.Root)
	if err != nil {
		return Checkout{}, err
	}
	if err := id.Validate(); err != nil {
		return Checkout{}, err
	}
	return plan, nil
}

// The seal covers entry identity, mode, size and mtime, not a content-addressed
// checkpoint. It catches normal intervening edits/replacements before launch;
// it cannot establish exclusion against a hostile host process racing Docker.
func scanCheckout(ctx context.Context, root string) ([]string, string, error) {
	var protected []string
	hash := sha256.New()
	var device uint64
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 {
			return errors.New("checkout exceeds admission scan limit")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("symlinks and special files require a separately qualified checkout layout")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("checkout filesystem identity unavailable")
		}
		if count == 1 {
			device = uint64(stat.Dev)
		}
		if uint64(stat.Dev) != device {
			return errors.New("nested filesystem mounts are unsupported")
		}
		if info.Mode().IsRegular() && stat.Nlink != 1 {
			return errors.New("hard-linked checkout files are unsupported")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		insideProtected := protectedName(parts[0])
		if len(parts) == 1 && protectedName(rel) {
			protected = append(protected, rel)
		}
		if len(parts) > 1 && !insideProtected && protectedName(entry.Name()) {
			return errors.New("nested Git or instruction paths require a separately qualified checkout layout")
		}
		fmt.Fprintf(hash, "%q\x00%d:%d:%d:%d:%d\n", rel, stat.Dev, stat.Ino, info.Mode(), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Strings(protected)
	return protected, hex.EncodeToString(hash.Sum(nil)), nil
}

func (p Checkout) Validate(ctx context.Context) error {
	if p.Root == "" || p.Root != p.identity.Root || p.seal == "" {
		return errors.New("uninitialized checkout plan")
	}
	if err := p.identity.Validate(); err != nil {
		return err
	}
	protected, seal, err := scanCheckout(ctx, p.Root)
	if err != nil {
		return err
	}
	if seal != p.seal || strings.Join(protected, "\x00") != strings.Join(p.Protected, "\x00") {
		return errors.New("checkout changed after admission; inspect again before launch")
	}
	return nil
}

func (p Checkout) MountArgs(ctx context.Context) ([]string, error) {
	if err := p.Validate(ctx); err != nil {
		return nil, err
	}
	args := []string{"--mount", "type=bind,src=" + p.Root + ",dst=/work,bind-recursive=disabled"}
	for _, path := range p.Protected {
		args = append(args, "--mount", "type=bind,src="+filepath.Join(p.Root, path)+",dst=/work/"+path+",readonly,bind-recursive=readonly,bind-propagation=rprivate")
	}
	return args, nil
}
