package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func repositoryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = gitEnvironment()
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
	}
	run("init", "-q", "-b", "main", root)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("-C", root, "add", "tracked.txt")
	run("-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture")
	return root
}

func TestFingerprintCoversIndexTrackedAndUntrackedBytes(t *testing.T) {
	ctx := context.Background()
	root := repositoryFixture(t)
	clean, err := Fingerprint(ctx, root, nil)
	if err != nil || clean.Dirty {
		t.Fatal(clean, err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := Fingerprint(ctx, root, nil)
	if err != nil || !changed.Dirty || changed.ContentDigest == clean.ContentDigest {
		t.Fatal(changed, err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	untracked, err := Fingerprint(ctx, root, nil)
	if err != nil || !untracked.Dirty || len(untracked.DirtyPaths) != 2 {
		t.Fatal(untracked, err)
	}
	cmd := exec.Command("git", "-C", root, "add", "tracked.txt")
	cmd.Env = gitEnvironment()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	staged, err := Fingerprint(ctx, root, nil)
	if err != nil || !staged.Dirty || staged.IndexDigest == clean.IndexDigest {
		t.Fatal(staged, err)
	}
}

func TestPrepareBranchRejectsCollisionAndDrift(t *testing.T) {
	ctx := context.Background()
	root := repositoryFixture(t)
	observation, err := ObserveEnrollment(ctx, root, "refs/heads/main", "vigil/plan", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareBranch(ctx, observation.Identity, "vigil/plan", observation.BaseOID, observation.Baseline)
	if err != nil || prepared.HeadRef != "refs/heads/vigil/plan" || prepared.Dirty {
		t.Fatal(prepared, err)
	}
	if _, err = PrepareBranch(ctx, observation.Identity, "vigil/plan", observation.BaseOID, observation.Baseline); err != nil {
		t.Fatal("safe reconciliation", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("user bytes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareBranch(ctx, observation.Identity, "vigil/plan", observation.BaseOID, observation.Baseline); err == nil {
		t.Fatal("drift accepted")
	}
	b, _ := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if string(b) != "user bytes\n" {
		t.Fatal("user bytes changed")
	}
}
