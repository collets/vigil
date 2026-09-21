package boundary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Root         string   `json:"root"`
	Protected    []string `json:"protected"`
	Guarded      []string `json:"guarded_ancestors"`
	identity     workspace.Identity
	repositories []workspace.Identity
	seal         string
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

// IsProtectedCheckoutPath reports whether any component names application,
// repository or instruction state that an implementation writer must not
// target directly. Matching is case-insensitive to preserve the admission
// contract on both case-sensitive and case-insensitive filesystems.
func IsProtectedCheckoutPath(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return true
	}
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		if protectedName(component) {
			return true
		}
	}
	return false
}

func PlanCheckout(ctx context.Context, root string) (Checkout, error) {
	return PlanCheckoutWithRepositories(ctx, root, nil)
}

// PlanCheckoutWithRepositories admits explicitly enrolled, ordinary nested Git
// roots. Merely discovering a nested repository never authorizes it. Linked
// worktrees, submodules and external common-Git directories remain unsupported.
func PlanCheckoutWithRepositories(ctx context.Context, root string, nestedRoots []string) (Checkout, error) {
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
	enrolled := map[string]workspace.Identity{}
	for _, nestedRoot := range nestedRoots {
		nested, inspectErr := workspace.Inspect(ctx, nestedRoot)
		if inspectErr != nil {
			return Checkout{}, inspectErr
		}
		rel, relErr := filepath.Rel(id.Root, nested.Root)
		if relErr != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			return Checkout{}, errors.New("enrolled nested repository must be strictly inside the checkout")
		}
		if strings.ContainsAny(nested.Root, ",\r\n") {
			return Checkout{}, errors.New("enrolled nested repository path cannot be represented safely in a Docker mount")
		}
		nestedGit := filepath.Join(nested.Root, ".git")
		gitInfo, statErr := os.Lstat(nestedGit)
		if statErr != nil || !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 || nested.CommonGitPath != nestedGit {
			return Checkout{}, errors.New("enrolled nested repository must have an ordinary in-tree Git directory")
		}
		if _, duplicate := enrolled[rel]; duplicate {
			return Checkout{}, errors.New("duplicate enrolled nested repository")
		}
		enrolled[rel] = nested
		plan.repositories = append(plan.repositories, nested)
	}
	plan.Protected, plan.seal, err = scanCheckout(ctx, id.Root, enrolled)
	if err != nil {
		return Checkout{}, err
	}
	plan.Guarded = guardedAncestors(plan.Protected)
	if err := id.Validate(); err != nil {
		return Checkout{}, err
	}
	return plan, nil
}

// guardedAncestors returns every ordinary directory whose replacement could
// change a protected pathname. Each is mounted onto itself so it remains
// writable for implementation work while rename/unlink of the mountpoint fails.
func guardedAncestors(protected []string) []string {
	set := map[string]bool{}
	for _, path := range protected {
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			if !insidePath(parent, protected) {
				set[parent] = true
			}
		}
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.Count(result[i], string(filepath.Separator))
		right := strings.Count(result[j], string(filepath.Separator))
		if left != right {
			return left < right
		}
		return result[i] < result[j]
	})
	return result
}

// The seal covers entry identity, mode, size and mtime, not a content-addressed
// checkpoint. It catches normal intervening edits/replacements before launch;
// it cannot establish exclusion against a hostile host process racing Docker.
func insidePath(path string, parents []string) bool {
	for _, parent := range parents {
		if path == parent || strings.HasPrefix(path, parent+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func scanCheckout(ctx context.Context, root string, enrolled map[string]workspace.Identity) ([]string, string, error) {
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
		if protectedName(entry.Name()) && !insidePath(rel, protected) {
			if strings.ContainsAny(rel, ",\r\n") {
				return errors.New("protected checkout path cannot be represented safely in a Docker mount")
			}
			if strings.EqualFold(entry.Name(), ".git") && !strings.EqualFold(rel, ".git") {
				parent := filepath.Dir(rel)
				if _, ok := enrolled[parent]; !ok {
					return errors.New("nested Git repository is not explicitly enrolled")
				}
			}
			protected = append(protected, rel)
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
	enrolled := map[string]workspace.Identity{}
	for _, repository := range p.repositories {
		if err := repository.Validate(); err != nil {
			return err
		}
		rel, err := filepath.Rel(p.Root, repository.Root)
		if err != nil {
			return err
		}
		enrolled[rel] = repository
	}
	protected, seal, err := scanCheckout(ctx, p.Root, enrolled)
	if err != nil {
		return err
	}
	guarded := guardedAncestors(protected)
	if seal != p.seal || strings.Join(protected, "\x00") != strings.Join(p.Protected, "\x00") || strings.Join(guarded, "\x00") != strings.Join(p.Guarded, "\x00") {
		return errors.New("checkout changed after admission; inspect again before launch")
	}
	return nil
}

// ObservedMount is the security-relevant subset of Docker's effective mount
// inspection. Stage 5.2 must call ValidateMounts after create and before start.
type ObservedMount struct {
	Source      string
	Destination string
	ReadWrite   bool
}

func (p Checkout) ValidateMounts(ctx context.Context, observed []ObservedMount) error {
	return p.ValidateMountsForRole(ctx, "implementation", observed)
}

func (p Checkout) ValidateMountsForRole(ctx context.Context, role string, observed []ObservedMount) error {
	if err := p.Validate(ctx); err != nil {
		return err
	}
	rootWritable := true
	if role == "review" {
		rootWritable = false
	} else if role != "implementation" {
		return errors.New("unsupported checkout mount role")
	}
	expected := map[string]ObservedMount{"/work": {Source: p.Root, Destination: "/work", ReadWrite: rootWritable}}
	for _, path := range p.Guarded {
		destination := "/work/" + filepath.ToSlash(path)
		expected[destination] = ObservedMount{Source: filepath.Join(p.Root, path), Destination: destination, ReadWrite: rootWritable}
	}
	for _, path := range p.Protected {
		destination := "/work/" + filepath.ToSlash(path)
		expected[destination] = ObservedMount{Source: filepath.Join(p.Root, path), Destination: destination, ReadWrite: false}
	}
	seen := map[string]bool{}
	for _, mount := range observed {
		destination := filepath.Clean(mount.Destination)
		if seen[destination] {
			return errors.New("duplicate effective mount destination")
		}
		seen[destination] = true
		want, required := expected[destination]
		insideWork := destination == "/work" || strings.HasPrefix(destination, "/work/")
		if insideWork && !required {
			return fmt.Errorf("unexpected effective mount shadows checkout path %s", destination)
		}
		if !required {
			continue
		}
		actualInfo, actualErr := os.Stat(mount.Source)
		wantedInfo, wantedErr := os.Stat(want.Source)
		if actualErr != nil || wantedErr != nil || !os.SameFile(actualInfo, wantedInfo) || mount.ReadWrite != want.ReadWrite {
			return fmt.Errorf("effective mount mismatch at %s", destination)
		}
	}
	for destination := range expected {
		if !seen[destination] {
			return fmt.Errorf("required effective mount missing at %s", destination)
		}
	}
	return nil
}

func (p Checkout) MountArgs(ctx context.Context) ([]string, error) {
	return p.MountArgsForRole(ctx, "implementation")
}

func (p Checkout) MountArgsForRole(ctx context.Context, role string) ([]string, error) {
	if err := p.Validate(ctx); err != nil {
		return nil, err
	}
	rootMount := "type=bind,src=" + p.Root + ",dst=/work,bind-recursive=disabled"
	if role == "review" {
		rootMount = "type=bind,src=" + p.Root + ",dst=/work,readonly,bind-recursive=readonly,bind-propagation=rprivate"
	} else if role != "implementation" {
		return nil, errors.New("unsupported checkout mount role")
	}
	args := []string{"--mount", rootMount}
	for _, path := range p.Guarded {
		mount := "type=bind,src=" + filepath.Join(p.Root, path) + ",dst=/work/" + filepath.ToSlash(path) + ",bind-recursive=disabled"
		if role == "review" {
			mount = "type=bind,src=" + filepath.Join(p.Root, path) + ",dst=/work/" + filepath.ToSlash(path) + ",readonly,bind-recursive=readonly,bind-propagation=rprivate"
		}
		args = append(args, "--mount", mount)
	}
	for _, path := range p.Protected {
		args = append(args, "--mount", "type=bind,src="+filepath.Join(p.Root, path)+",dst=/work/"+filepath.ToSlash(path)+",readonly,bind-recursive=readonly,bind-propagation=rprivate")
	}
	return args, nil
}

// QualificationLayout and QualificationRoots expose only the immutable
// security-relevant shape needed to bind a prepared execution to qualification.
func (p Checkout) QualificationLayout() string {
	if len(p.repositories) == 0 {
		return "ordinary_single_repository"
	}
	return "ordinary_enrolled_nested_repositories"
}

func (p Checkout) QualificationRoots() []string {
	roots := []string{p.Root}
	for _, repository := range p.repositories {
		roots = append(roots, repository.Root)
	}
	sort.Strings(roots)
	return roots
}

// QualificationMountPlanDigest binds exact canonical sources, destinations and
// access modes produced by this validated checkout plan.
func (p Checkout) QualificationMountPlanDigest(ctx context.Context, role string) (string, error) {
	args, err := p.MountArgsForRole(ctx, role)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Layout string   `json:"layout"`
		Roots  []string `json:"roots"`
		Args   []string `json:"mount_args"`
	}{p.QualificationLayout(), p.QualificationRoots(), args})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}
