//go:build linux

package checks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type cancelOnReadyWriter struct {
	once   sync.Once
	cancel context.CancelFunc
}

func (w *cancelOnReadyWriter) Write(content []byte) (int, error) {
	if bytes.Contains(content, []byte("detached-ready")) {
		w.once.Do(w.cancel)
	}
	return len(content), nil
}

func TestIndependentSupervisorKilledHelper(t *testing.T) {
	if os.Getenv("VIGIL_REVIEW_KILL_SUPERVISOR") != "1" {
		return
	}
	if err := syscall.Kill(os.Getppid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestIndependentSupervisorDeathMustBeUncertain(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestIndependentSupervisorKilledHelper$")
	cmd.Env = []string{"VIGIL_REVIEW_KILL_SUPERVISOR=1"}
	runErr, contained := runContained(context.Background(), cmd, io.Discard)
	if contained {
		t.Fatalf("killed supervisor generated containment proof: err=%v status=%v", runErr, cmd.ProcessState)
	}
}

func TestIndependentSupervisorPipeDescriptors(t *testing.T) {
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		cmd := exec.Command("/bin/true")
		if err, contained := runContained(context.Background(), cmd, io.Discard); err != nil || !contained {
			t.Fatalf("run %s: %v %v", strconv.Itoa(i), err, contained)
		}
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) >= len(before)+12 {
		t.Fatalf("one config descriptor leaked per run: before=%d after=%d", len(before), len(after))
	}
}

func TestSupervisorCancellationHelper(t *testing.T) {
	if os.Getenv("VIGIL_REVIEW_CANCELLATION_HELPER") != "1" {
		return
	}
	child := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	child.Env = []string{"PATH=/usr/bin:/bin"}
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("detached-ready")
	_ = child.Wait()
}

func TestSupervisorEarlyCancellationHelper(t *testing.T) {
	if os.Getenv("VIGIL_REVIEW_EARLY_CANCELLATION_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestSupervisorEarlyCancellationIsBounded(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := exec.Command(executable, "-test.run=^TestSupervisorEarlyCancellationHelper$")
	command.Env = []string{"VIGIL_REVIEW_EARLY_CANCELLATION_HELPER=1"}
	started := time.Now()
	runErr, contained := runContained(ctx, command, io.Discard)
	if contained {
		t.Fatalf("pre-canceled readiness established containment: %v", runErr)
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("early cancellation exceeded its bound: %s", elapsed)
	}
}

func TestSupervisorSelfSignalHelper(t *testing.T) {
	raw := os.Getenv("VIGIL_REVIEW_SELF_SIGNAL")
	if raw == "" {
		return
	}
	signal, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.Signal(signal)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Second)
}

func TestSupervisorSignalExitCodeFidelity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL, syscall.SIGHUP, syscall.SIGINT} {
		command := exec.Command(executable, "-test.run=^TestSupervisorSelfSignalHelper$")
		command.Env = []string{"VIGIL_REVIEW_SELF_SIGNAL=" + strconv.Itoa(int(signal))}
		runErr, contained := runContained(context.Background(), command, io.Discard)
		if !contained {
			t.Fatalf("signal %v did not complete contained cleanup: %v", signal, runErr)
		}
		if got, want := command.ProcessState.ExitCode(), 128+int(signal); got != want {
			t.Fatalf("signal %v recorded exit code %d; want %d", signal, got, want)
		}
	}
}

func TestSupervisorCompletesCleanupDuringCancellation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &cancelOnReadyWriter{cancel: cancel}
	command := exec.Command(executable, "-test.run=^TestSupervisorCancellationHelper$")
	command.Env = []string{"VIGIL_REVIEW_CANCELLATION_HELPER=1"}
	started := time.Now()
	runErr, contained := runContained(ctx, command, output)
	if ctx.Err() == nil {
		t.Fatal("helper never triggered cancellation")
	}
	if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() && contained {
		t.Fatalf("signalled supervisor established containment after cancellation: %v", runErr)
	}
	if !contained && runErr == nil {
		t.Fatal("uncertain cancellation returned no containment error")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("supervisor cancellation exceeded its shutdown bound: %s", elapsed)
	}
}

func TestSupervisorConfigurationWriterRetiresAfterStartFailure(t *testing.T) {
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/true")
	command.Env = []string{"VIGIL_LARGE_CONFIG=" + strings.Repeat("x", 256<<10)}
	containment, err := prepareProcessContainment(command)
	if err != nil {
		t.Fatal(err)
	}
	command.Path = "/vigil-intentionally-missing-supervisor"
	started := time.Now()
	if err := command.Start(); err == nil {
		t.Fatal("invalid supervisor unexpectedly started")
	}
	containment.close()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("configuration writer did not retire promptly: %s", elapsed)
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before) {
		t.Fatalf("failed start leaked descriptors: before=%d after=%d", len(before), len(after))
	}
}
