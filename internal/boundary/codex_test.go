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

func codexImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv("VIGIL_CODEX_IMAGE")
	if os.Getenv("VIGIL_TEST_DOCKER") != "1" || image == "" {
		t.Skip("set VIGIL_TEST_DOCKER=1 and VIGIL_CODEX_IMAGE=sha256:... for Codex image metadata tests")
	}
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 || os.Getuid() == 0 {
		t.Fatal("immutable image ID and non-root host required")
	}
	return image
}

func TestCodexImageMetadataAndPrivateHome(t *testing.T) {
	image := codexImage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v: %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if version := docker("image", "inspect", "--format", `{{index .Config.Labels "vigil.codex-version"}}`, image); version != "0.155.1" {
		t.Fatal("unrecognized Codex image version", version)
	}
	native := filepath.Join(t.TempDir(), "native")
	if err := os.Mkdir(native, 0700); err != nil {
		t.Fatal(err)
	}
	template, err := filepath.Abs("../../config/boundary/codex.toml")
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(native, "config.toml"), config, 0600); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	output := docker("run", "--rm", "--pull=never", "--label=vigil.probe=stage5", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=256m", "--cpus=0.5", "--restart=no", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=8m,mode=1777", "--mount", "type=bind,src="+native+",dst=/native/codex,bind-recursive=disabled", "--mount", "type=bind,src="+work+",dst=/work,bind-recursive=disabled", "--entrypoint=/bin/sh", image, "-c", `/opt/codex/codex --version; /opt/codex/codex features list | grep '^multi_agent.*false$'; test ! -e /native/codex/auth.json`)
	if !strings.Contains(output, "codex-cli 0.155.1") || !strings.Contains(output, "multi_agent") {
		t.Fatal("Codex image did not apply the pinned metadata profile", output)
	}
	if _, err := os.Lstat(filepath.Join(native, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("metadata probe created an authentication file")
	}
}
