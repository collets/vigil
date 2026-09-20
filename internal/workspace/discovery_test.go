package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoveryRecordsNestedAndSharedGitWithoutEnrollment(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = gitEnvironment()
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
	}
	git("init", "-q", root)
	git("-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "--allow-empty", "-qm", "fixture")
	git("init", "-q", filepath.Join(root, "nested"))
	git("-C", root, "worktree", "add", "-q", "-b", "linked", filepath.Join(root, "linked"))
	if err := os.Symlink(filepath.Join(root, "nested"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(root, "not-a-repository"))
	r, err := Discover(context.Background(), root)
	if err != nil || len(r.Repositories) != 3 || r.SkippedSymlinks != 1 {
		t.Fatal(r, err)
	}
	byRoot := map[string]Repository{}
	for _, repo := range r.Repositories {
		byRoot[repo.RelativeRoot] = repo
	}
	if byRoot["."].HeadOID == "" || byRoot["linked"].GitLayout != "gitfile" || byRoot["linked"].Identity.CommonGit != byRoot["."].Identity.CommonGit || byRoot["nested"].HeadOID != "" || len(byRoot["nested"].Issues) == 0 {
		t.Fatal(r)
	}
}

func TestDiscoveryDoesNotFollowGitSymlinkOrRunHooks(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	r, err := Discover(context.Background(), root)
	if err != nil || len(r.Repositories) != 1 || r.Repositories[0].GitLayout != "unsupported_symlink" {
		t.Fatal(r, err)
	}
	if err := os.Remove(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", root)
	cmd.Env = gitEnvironment()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	marker := filepath.Join(root, "hook-ran")
	hook := filepath.Join(root, ".git/hooks/query-fsmonitor")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ran > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", root, "config", "core.fsmonitor", hook)
	cmd.Env = gitEnvironment()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	if _, err := Discover(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("repository hook executed", err)
	}
}

func TestDiscoveryDoesNotOpenSpecialGitFile(t *testing.T) {
	root := t.TempDir()
	if b, err := exec.Command("mkfifo", filepath.Join(root, ".git")).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := Discover(ctx, root)
	if err != nil || len(r.Repositories) != 1 || r.Repositories[0].GitLayout != "unsupported_special" {
		t.Fatal(r, err)
	}
}
