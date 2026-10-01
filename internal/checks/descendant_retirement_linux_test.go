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
		t.Skipf("cannot spawn a short-lived child here: %v", err)
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
		t.Skipf("cannot spawn a shell here: %v", err)
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

// The budgets must fit inside the parent's patience, or the fix manufactures the
// symptom it exists to remove.
//
// The parent gives the supervisor processContainmentShutdownGrace to finish after
// signalling it, and then SIGKILLs it. If this loop's worst case reaches that
// grace, the parent kills the supervisor *mid-cleanup*, `completed()` sees a
// signalled exit, and the caller reports "descendant containment could not be
// proven" for a tree that was about to clean up correctly.
//
// This is the relationship that made raising the budgets a mistake: at 5s + 5s the
// loop needed 10.25s against a 4s grace. The values are back at their originals,
// and this test is what stops anyone from raising them again without meeting it.
func TestDescendantBudgetsFitTheSupervisorShutdown(t *testing.T) {
	// One scan's worth of slack, so the last iteration's sleep cannot push the
	// total over the edge. Measured at well under a millisecond per scan.
	const scanAllowance = 500 * time.Millisecond
	worst := descendantTerminateGrace + descendantKillSettle + scanAllowance
	grace := processContainmentShutdownGrace()
	if worst >= grace {
		t.Fatalf("descendant retirement can take %s (terminate %s + settle %s + scan "+
			"allowance %s) but the parent only allows %s before it SIGKILLs the "+
			"supervisor; raise the parent's grace rather than the budgets here, or the "+
			"parent will kill the supervisor mid-cleanup and report containment "+
			"unproven for a tree that was retiring correctly",
			worst, descendantTerminateGrace, descendantKillSettle, scanAllowance, grace)
	}
	if descendantPollInterval >= descendantTerminateGrace {
		t.Fatalf("poll interval %s must be well below the SIGTERM grace %s, or the "+
			"grace period cannot be honoured", descendantPollInterval, descendantTerminateGrace)
	}
	// The budgets must not shrink below the values that were in place before this
	// change either; a smaller wait is a weaker check, not a stricter one.
	if descendantTerminateGrace < time.Second {
		t.Fatalf("SIGTERM grace %s is below the pre-existing one second, which "+
			"weakens the check rather than fixing it", descendantTerminateGrace)
	}
	if descendantKillSettle < 2*time.Second {
		t.Fatalf("SIGKILL settle %s is below the pre-existing two seconds, which "+
			"weakens the check rather than fixing it", descendantKillSettle)
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

// The ESRCH tolerance is the claimed root cause, so drive it into the scan
// deterministically rather than hoping a spawned process lands in the window.
//
// Spawning churn reproduces the errno only a few hundred times per minute under
// load, and never on an idle machine, which is exactly why the bug presented as a
// flake. The read is indirected through readProcessStat, so the race is injected
// instead of waited for. This is the test that makes the claim falsifiable.
func TestLinuxDescendantsSkipsProcessVanishedMidTeardown(t *testing.T) {
	// A real, live child, so there is a genuine entry for the scan to find.
	child := exec.Command("/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Skipf("cannot spawn a child here: %v", err)
	}
	pid := child.Process.Pid
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	})

	// Sanity: the child is found while its stat reads normally.
	found, err := linuxDescendants(os.Getpid())
	if err != nil {
		t.Fatalf("baseline scan: %v", err)
	}
	if !containsPID(found, pid) {
		t.Skipf("child %d not visible in the scan on this system; cannot exercise the race", pid)
	}

	// Now make every stat read fail the way a mid-teardown process does, and
	// require the scan to tolerate it rather than fail. Pre-fix this returned the
	// error, which surfaced as "descendant containment could not be proven".
	real := readProcessStat
	t.Cleanup(func() { readProcessStat = real })
	var injected int
	readProcessStat = func(string) ([]byte, error) {
		injected++
		return nil, &os.PathError{Op: "open", Path: "/proc/x/stat", Err: syscall.ESRCH}
	}
	if _, err := linuxDescendants(os.Getpid()); err != nil {
		t.Fatalf("scan failed on a process that vanished mid-teardown: %v", err)
	}
	if injected == 0 {
		t.Fatal("the seam was never called, so the test proved nothing")
	}
}

// The scan must still report a genuine read failure, or the tolerance above has
// become a blanket "ignore errors" and the check no longer means anything.
func TestLinuxDescendantsStillReportsRealReadFailures(t *testing.T) {
	real := readProcessStat
	t.Cleanup(func() { readProcessStat = real })
	readProcessStat = func(string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: "/proc/x/stat", Err: syscall.EACCES}
	}
	if _, err := linuxDescendants(os.Getpid()); err == nil {
		t.Fatal("a permission failure must still fail the scan; tolerating it would " +
			"make the containment check report success without having looked")
	}
}

func containsPID(list []observedLinuxProcess, pid int) bool {
	for _, p := range list {
		if p.pid == pid {
			return true
		}
	}
	return false
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
