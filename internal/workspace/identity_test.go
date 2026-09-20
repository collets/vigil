package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAliasesAndReplacement(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "Case")
	nested := filepath.Join(root, "child")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	a, err := Inspect(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Inspect(ctx, filepath.Join(alias, "child"))
	if err != nil {
		t.Fatal(err)
	}
	if !Overlap(a, b) {
		t.Fatal("symlink ancestor bypass")
	}
	if _, err := os.Stat(filepath.Join(base, "case")); err == nil {
		lower, err := Inspect(ctx, filepath.Join(base, "case"))
		if err != nil || !Overlap(lower, b) {
			t.Fatal("case alias bypass", err)
		}
	}
	composed := filepath.Join(base, "caf\u00e9")
	if err := os.Mkdir(composed, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "cafe\u0301")); err == nil {
		one, _ := Inspect(ctx, composed)
		two, err := Inspect(ctx, filepath.Join(base, "cafe\u0301"))
		if err != nil || !Overlap(one, two) {
			t.Fatal("Unicode alias bypass", err)
		}
	}
	if err := os.Rename(root, root+"-preserved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.Validate(); err == nil {
		t.Fatal("replacement not detected")
	}
}
func TestCommonGitIdentity(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	linked := filepath.Join(base, "linked worktree")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(b), err)
		}
	}
	git("init", "-q", repo)
	git("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture")
	git("-C", repo, "worktree", "add", "-q", "-b", "linked", linked)
	a, err := Inspect(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Inspect(ctx, linked)
	if err != nil {
		t.Fatal(err)
	}
	if a.CommonGit == "" || !Overlap(a, b) {
		t.Fatal("shared Git administration bypass")
	}
}

func TestIdentityIgnoresAmbientGitDirectory(t *testing.T) {
	root := t.TempDir()
	if b, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	before, err := Inspect(context.Background(), root)
	if err != nil || before.CommonGit == "" {
		t.Fatal(before, err)
	}
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "foreign"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	after, err := Inspect(context.Background(), root)
	if err != nil || after.CommonGit != before.CommonGit {
		t.Fatal("ambient Git environment redirected discovery", after, err)
	}
}
