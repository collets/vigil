package quality_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"vigil/internal/policy"
)

func TestStage54FinalCleanEnvHelper(t *testing.T) {
	if os.Getenv("VIGIL_CLEAN_CHILD") != "1" {
		return
	}
	child := exec.Command("/bin/sleep", "20")
	child.Env = []string{}
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("VIGIL_PID_PATH"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		_ = child.Process.Kill()
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestStage54FinalCleanEnvironmentEscape(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 12; attempt++ {
		pidfile := filepath.Join(t.TempDir(), "pid")
		fixture := setupQuality(t, "pass", func(d *policy.CheckDefinition) {
			d.Argv = []string{executable, "-test.run=^TestStage54FinalCleanEnvHelper$"}
			d.Environment = []policy.EnvironmentVariable{{Name: "VIGIL_CLEAN_CHILD", Value: "1"}, {Name: "VIGIL_PID_PATH", Value: pidfile}}
			d.RequiredOutputs = nil
		})
		result, runErr := fixture.runCheck(context.Background(), nil)
		raw, err := os.ReadFile(pidfile)
		if err != nil {
			t.Fatal(err, runErr)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		alive := syscall.Kill(pid, 0) == nil
		_ = syscall.Kill(pid, syscall.SIGKILL)
		if alive && runErr == nil {
			t.Fatalf("attempt %d: empty-environment detached descendant survived a %s result", attempt, result.Status)
		}
	}
}

func TestStage54FinalRestrictiveUmask(t *testing.T) {
	fixture := setupQuality(t, "pass", nil)
	if err := os.Chmod(filepath.Join(fixture.root, "src/input.txt"), 0644); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0077)
	defer syscall.Umask(old)
	result, err := fixture.runCheck(context.Background(), nil)
	if err != nil || result.Status != "pass" {
		t.Fatalf("0644 source under umask 077 returned %s: %v", result.Status, err)
	}
}
