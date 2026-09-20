package boundary

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkoutFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet", root)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("protected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "edit.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCheckoutAdmission(t *testing.T) {
	ctx := context.Background()
	root := checkoutFixture(t)
	plan, err := PlanCheckout(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	args, err := plan.MountArgs(ctx)
	if err != nil || len(plan.Protected) != 2 || !strings.Contains(strings.Join(args, " "), "dst=/work/.git,readonly") {
		t.Fatal(plan, args, err)
	}
	if err := os.WriteFile(filepath.Join(root, "edit.txt"), []byte("human change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if plan.Validate(ctx) == nil {
		t.Fatal("intervening human edit was not detected")
	}
	plan, err = PlanCheckout(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	plan.Protected = nil
	if _, err = plan.MountArgs(ctx); err == nil {
		t.Fatal("mutable mount list bypassed admission")
	}
}

func TestCheckoutRejectsAliasesAndNestedInstructions(t *testing.T) {
	for _, mode := range []string{"instruction-hardlink", "git-hardlink", "symlink", "nested-git", "nested-instruction", "worktree", "fifo"} {
		t.Run(mode, func(t *testing.T) {
			root := checkoutFixture(t)
			var err error
			switch mode {
			case "instruction-hardlink":
				err = os.Link(filepath.Join(root, "AGENTS.md"), filepath.Join(root, "alias"))
			case "git-hardlink":
				err = os.Link(filepath.Join(root, ".git/config"), filepath.Join(root, "alias"))
			case "symlink":
				err = os.Symlink(".git/config", filepath.Join(root, "alias"))
			case "nested-git", "nested-instruction":
				err = os.Mkdir(filepath.Join(root, "child"), 0700)
				if err == nil {
					name := ".git"
					if mode == "nested-instruction" {
						name = "AGENTS.md"
					}
					err = os.WriteFile(filepath.Join(root, "child", name), []byte("nested"), 0600)
				}
			case "worktree":
				outside := filepath.Join(t.TempDir(), "git")
				err = os.Rename(filepath.Join(root, ".git"), outside)
				if err == nil {
					err = os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+outside+"\n"), 0600)
				}
			case "fifo":
				err = exec.Command("mkfifo", filepath.Join(root, "pipe")).Run()
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := PlanCheckout(context.Background(), root); err == nil {
				t.Fatal("unsupported layout admitted")
			}
		})
	}
}

func TestCheckoutDetectsParentReplacement(t *testing.T) {
	root := checkoutFixture(t)
	p, err := PlanCheckout(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	old := root + "-old"
	if err = os.Rename(root, old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(old) })
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if p.Validate(context.Background()) == nil {
		t.Fatal("replaced checkout admitted")
	}
}

func TestDockerCheckoutGitProtection(t *testing.T) {
	image := hermesImage(t) // Pinned qualification image includes real Git, no model.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := checkoutFixture(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(b))
		}
		return string(b)
	}
	git("add", "edit.txt", "AGENTS.md")
	git("-c", "user.name=Vigil fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture")
	before := git("rev-parse", "HEAD")
	plan, err := PlanCheckout(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	mounts, err := plan.MountArgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	script := `set -eu
printf 'allowed edit\n' > /work/edit.txt
if git -C /work add edit.txt 2>/dev/null; then exit 10; fi
if git -C /work -c user.name=fixture -c user.email=fixture@invalid commit --allow-empty -qm bypass 2>/dev/null; then exit 11; fi
if git -C /work config vigil.bypass yes 2>/dev/null; then exit 12; fi
if git --git-dir=/work/.git --work-tree=/work update-ref refs/heads/bypass HEAD 2>/dev/null; then exit 13; fi
if echo bypass >> /work/.git/HEAD 2>/dev/null; then exit 14; fi
if mv /work/.git /work/moved 2>/dev/null; then exit 15; fi
if mv /work /moved 2>/dev/null; then exit 16; fi
if rm /work/AGENTS.md 2>/dev/null; then exit 17; fi
if echo bypass >> /work/AGENTS.md 2>/dev/null; then exit 18; fi
if ln /work/.git/HEAD /work/new-alias 2>/dev/null; then exit 20; fi
if ln /work/AGENTS.md /work/instruction-alias 2>/dev/null; then exit 21; fi
ln -s .git/HEAD /work/symlink-alias
if echo bypass >> /work/symlink-alias 2>/dev/null; then exit 22; fi
git clone -q --no-hardlinks /work /tmp/alternate
git -C /tmp/alternate -c user.name=fixture -c user.email=fixture@invalid commit --allow-empty -qm alternative
if git -C /tmp/alternate push -q /work/.git HEAD:refs/heads/bypass 2>/dev/null; then exit 19; fi
echo git-protection-ok`
	args := []string{"create", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=128m", "--cpus=0.5", "--restart=no", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=16m,mode=1777", "--env", "GIT_CONFIG_GLOBAL=/dev/null", "--env", "GIT_CONFIG_NOSYSTEM=1", "--env", "GIT_OPTIONAL_LOCKS=0", "--entrypoint=/bin/sh"}
	args = append(args, mounts...)
	args = append(args, image, "-c", script)
	docker := func(args ...string) string {
		t.Helper()
		c, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		b, err := exec.CommandContext(c, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v: %s", args[0], err, b)
		}
		return strings.TrimSpace(string(b))
	}
	id := docker(args...)
	t.Cleanup(func() { docker("rm", "-f", id) })
	if out := docker("start", "-a", id); !strings.Contains(out, "git-protection-ok") {
		t.Fatal(out)
	}
	if docker("inspect", "--format", "{{.State.Running}} {{.State.Pid}}", id) != "false 0" {
		t.Fatal("probe boundary still active")
	}
	if git("rev-parse", "HEAD") != before || strings.TrimSpace(git("for-each-ref", "--format=%(refname)", "refs/heads/bypass")) != "" {
		t.Fatal("protected Git refs changed")
	}
	if b, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err != nil || string(b) != "protected\n" {
		t.Fatal("instructions changed", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "edit.txt")); err != nil || string(b) != "allowed edit\n" {
		t.Fatal("approved checkout write failed", err)
	}
}
