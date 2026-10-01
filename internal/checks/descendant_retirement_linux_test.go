//go:build linux

package checks

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A zombie descendant must not be reported as a live one.
//
// The supervisor is a PR_SET_CHILD_SUBREAPER, so an orphaned descendant is
// reparented to it and sits in /proc as a zombie until reapAdopted collects it.
// Reporting that zombie as a surviving descendant asserts a containment failure
// for a process that has already released every resource it held, and the
// reap-then-scan race that produces the window widens under load -- which is how
// this check came to fail on a saturated machine and pass on an idle one.
//
// The regression this guards is precise: before the fix, an unreaped zombie was
// indistinguishable from a live process, so the scan kept returning work until the
// budget expired and the caller reported "descendants did not retire".

func TestLinuxDescendantsExcludesZombies(t *testing.T) {
	// /bin/sleep is present wherever these tests run and exits immediately when
	// asked to, leaving an unreaped zombie that only this process can reap.
	child := exec.Command("/bin/sleep", "0")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	pid := child.Process.Pid
	t.Cleanup(func() {
		// Never leave a zombie behind for the rest of the suite.
		_ = reapAdopted()
	})

	// Wait for the child to exit but deliberately do not Wait on it, so it stays
	// a zombie for the assertion below.
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err := processState(pid)
		if err != nil {
			t.Fatalf("read state of %d: %v", pid, err)
		}
		if state == "Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d never reached the zombie state, last %q", pid, state)
		}
		time.Sleep(2 * time.Millisecond)
	}

	found, err := linuxDescendants(os.Getpid())
	if err != nil {
		t.Fatalf("linuxDescendants: %v", err)
	}
	for _, p := range found {
		if p.pid == pid {
			t.Fatalf("zombie descendant %d reported as live; a terminated process "+
				"holds no resources and must not fail a containment check", pid)
		}
	}
}

// A descendant that ignores SIGTERM must still be escalated to SIGKILL.
//
// This is the property the generous deadlines must not have cost us: raising the
// budgets must not turn into waiting forever, and it must not stop the escalation
// from happening at all. Without this test, a fix that simply slept longer would
// pass every other check here.
func TestCleanupDescendantsEscalatesToKill(t *testing.T) {
	if testing.Short() {
		t.Skip("deliberately waits out the SIGTERM grace period")
	}
	// A shell that traps SIGTERM and keeps running. SIGTERM is therefore
	// delivered, observed, and ignored; only SIGKILL can retire it.
	const script = `trap '' TERM; echo $$; while true; do sleep 0.05; done`
	child := exec.Command("/bin/sh", "-c", script)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatalf("start stubborn child: %v", err)
	}
	pid := child.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_, _ = child.Process.Wait()
	})

	// Give the shell time to install its trap, otherwise it may be killed by
	// SIGTERM before the trap exists and the test would pass for the wrong reason.
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	if err := cleanupDescendants(os.Getpid()); err != nil {
		t.Fatalf("cleanupDescendants did not retire a SIGTERM-ignoring descendant: %v", err)
	}
	elapsed := time.Since(start)

	// Escalation must have happened, so this must have cost at least the grace
	// period -- a fast return would mean the SIGTERM phase was skipped.
	if elapsed < descendantTerminateGrace {
		t.Fatalf("returned after %s, less than the %s SIGTERM grace period: the "+
			"grace period was not served, so the wait proves nothing",
			elapsed, descendantTerminateGrace)
	}
	if remaining, err := linuxDescendants(os.Getpid()); err == nil && len(remaining) != 0 {
		t.Fatalf("descendants still live after cleanup: %v", remaining)
	}
}

// The budgets are durations on purpose. An iteration count is a wall-clock
// budget in disguise, and a disguised one is what made this flake load-dependent.
func TestDescendantBudgetsAreDurations(t *testing.T) {
	if descendantTerminateGrace <= 0 || descendantKillSettle <= 0 || descendantPollInterval <= 0 {
		t.Fatal("descendant budgets must be positive durations")
	}
	if descendantPollInterval >= descendantTerminateGrace {
		t.Fatalf("poll interval %s must be well below the grace period %s, or the "+
			"grace period cannot be honoured", descendantPollInterval, descendantTerminateGrace)
	}
	// The pre-fix code allowed 100*10ms for SIGTERM and 200*10ms for SIGKILL and
	// still failed under load, so anything at or below that is not a fix.
	if descendantTerminateGrace <= time.Second {
		t.Fatalf("SIGTERM grace %s is no more generous than the pre-fix one second, "+
			"which was observed failing under load", descendantTerminateGrace)
	}
	if descendantKillSettle <= 2*time.Second {
		t.Fatalf("SIGKILL settle %s is no more generous than the pre-fix two seconds, "+
			"which was observed failing under load", descendantKillSettle)
	}
}

// The errno policy is the defect, so pin it directly.
//
// ENOENT and ESRCH both mean "this process is gone". The original code tolerated
// only ENOENT, so a /proc read that returned ESRCH for a process caught mid-teardown
// fell through to a hard error, the supervisor exited 125, and the caller reported
// "descendant containment could not be proven" for a process that had already gone.
// This is load-sensitive because the teardown window widens with load, and it is the
// most likely cause of the flake this change set out to remove.
func TestProcessGoneToleratesBothTeardownErrnos(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no such file", &os.PathError{Op: "open", Path: "/proc/1/stat", Err: syscall.ENOENT}, true},
		{"no such process", &os.PathError{Op: "open", Path: "/proc/1/stat", Err: syscall.ESRCH}, true},
		{"bare ENOENT", syscall.ENOENT, true},
		{"bare ESRCH", syscall.ESRCH, true},
		{"wrapped", &os.PathError{Op: "open", Path: "/proc/1/stat", Err: &os.LinkError{
			Op: "read", Old: "/proc/1/stat", New: "/proc/1/stat", Err: syscall.ESRCH}}, true},
		{"permission denied is a real error", &os.PathError{Op: "open", Path: "/proc/1/stat", Err: syscall.EACCES}, false},
		{"not a directory is a real error", &os.PathError{Op: "open", Path: "/proc/1/stat", Err: syscall.ENOTDIR}, false},
		{"io error is a real error", os.ErrInvalid, false},
		{"nil is not an error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := processGone(tc.err); got != tc.want {
				t.Fatalf("processGone(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

// Scan a process tree that is actively tearing down and require that the scan
// tolerates it. Rapid-exit children maximise the chance of catching one in the
// window between the readdir snapshot and the stat read, which is the race the
// errno policy exists to absorb.
func TestLinuxDescendantsToleratesRacingExits(t *testing.T) {
	for attempt := 0; attempt < 40; attempt++ {
		child := exec.Command("/bin/sleep", "0")
		if err := child.Start(); err != nil {
			t.Skipf("cannot spawn short-lived child: %v", err)
		}
		// Deliberately do not Wait: the child is in the process table, possibly
		// mid-exit, and may already be gone by the time the scan reads it.
		if _, err := linuxDescendants(os.Getpid()); err != nil {
			_ = reapAdopted()
			t.Fatalf("attempt %d: scan failed on a process that was exiting: %v", attempt, err)
		}
		_ = reapAdopted()
	}
}

func processState(pid int) (string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	s := string(raw)
	// The comm field is parenthesised and may itself contain spaces and
	// parentheses, so the fixed fields start after its closing ')'.
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return "", os.ErrInvalid
	}
	fields := strings.Fields(s[end+1:])
	if len(fields) == 0 {
		return "", os.ErrInvalid
	}
	return fields[0], nil
}
