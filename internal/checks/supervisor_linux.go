//go:build linux

package checks

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func init() {
	if os.Getenv("VIGIL_CHECK_SUPERVISOR") == "1" {
		os.Exit(supervisorMain())
	}
}

func supervisorMain() int {
	interrupt := make(chan os.Signal, 2)
	signal.Notify(interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(interrupt)

	fd, err := strconv.Atoi(os.Getenv("VIGIL_CHECK_SUPERVISOR_CONFIG_FD"))
	if err != nil {
		return linuxContainmentFailureExit
	}
	file := os.NewFile(uintptr(fd), "supervisor-config")
	if file == nil {
		return linuxContainmentFailureExit
	}
	defer file.Close()
	unix.CloseOnExec(fd)
	proofFD, err := strconv.Atoi(os.Getenv("VIGIL_CHECK_SUPERVISOR_PROOF_FD"))
	if err != nil {
		return linuxContainmentFailureExit
	}
	proof := os.NewFile(uintptr(proofFD), "supervisor-proof")
	if proof == nil {
		return linuxContainmentFailureExit
	}
	defer proof.Close()
	unix.CloseOnExec(proofFD)
	readyFD, err := strconv.Atoi(os.Getenv("VIGIL_CHECK_SUPERVISOR_READY_FD"))
	if err != nil {
		return linuxContainmentFailureExit
	}
	ready := os.NewFile(uintptr(readyFD), "supervisor-ready")
	if ready == nil {
		return linuxContainmentFailureExit
	}
	defer ready.Close()
	unix.CloseOnExec(readyFD)
	var spec supervisorCommand
	if err := json.NewDecoder(file).Decode(&spec); err != nil || spec.Path == "" || len(spec.Args) == 0 {
		return linuxContainmentFailureExit
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return linuxContainmentFailureExit
	}
	if _, err := ready.Write([]byte(linuxContainmentReady)); err != nil {
		return linuxContainmentFailureExit
	}
	if err := ready.Close(); err != nil {
		return linuxContainmentFailureExit
	}
	command := exec.Command(spec.Path, spec.Args[1:]...)
	command.Dir, command.Env = spec.Dir, spec.Env
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		return linuxContainmentFailureExit
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	var terminationErr error
	select {
	case <-interrupt:
		terminationErr = terminateProcessTree(command.Process)
		<-finished
	case <-finished:
	}
	if err := cleanupDescendants(os.Getpid()); err != nil || terminationErr != nil {
		return linuxContainmentFailureExit
	}
	if command.ProcessState == nil {
		return linuxContainmentFailureExit
	}
	if _, err := proof.Write([]byte(linuxContainmentCompletion)); err != nil {
		return linuxContainmentFailureExit
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return linuxContainmentFailureExit
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	code := command.ProcessState.ExitCode()
	if code == linuxContainmentFailureExit {
		// Reserve the containment status for the supervisor itself.
		return linuxContainmentFailureExit - 1
	}
	return code
}

func terminateProcessTree(root *os.Process) error {
	var result error
	if err := root.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		result = errors.Join(result, err)
	}
	pids, err := linuxDescendants(os.Getpid())
	if err != nil {
		result = errors.Join(result, err)
	}
	for _, process := range pids {
		if err := signalObservedProcess(process, syscall.SIGTERM); err != nil {
			result = errors.Join(result, err)
		}
	}
	time.Sleep(250 * time.Millisecond)
	if err := root.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		result = errors.Join(result, err)
	}
	pids, err = linuxDescendants(os.Getpid())
	if err != nil {
		result = errors.Join(result, err)
	}
	for _, process := range pids {
		if err := signalObservedProcess(process, syscall.SIGKILL); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// Descendant retirement budgets.
//
// These are the pre-existing values, unchanged. They are now named durations
// rather than loop counts, which is the whole point: `100` attempts of a 10ms
// sleep is a one-second wall-clock budget in disguise, and a disguised one cannot
// be related to the budget it has to fit inside. As durations the relationship is
// expressible, and TestDescendantBudgetsFitTheSupervisorShutdown expresses it, so
// the two cannot drift apart again.
//
// They are deliberately NOT more generous. The failure that prompted this change
// was a teardown race, not an exhausted budget, so patience bought nothing; and
// raising them pushes this loop past processContainmentShutdownGrace, at which
// point the parent SIGKILLs the supervisor mid-cleanup and manufactures a fresh
// instance of the very symptom being fixed. A descendant that has ignored SIGTERM
// for a second will be SIGKILLed regardless, and SIGKILL is uncatchable, so all
// that remains after it is delivered is teardown and reaping, not cooperation.
const (
	descendantTerminateGrace = 1 * time.Second
	descendantKillSettle     = 2 * time.Second
	descendantPollInterval   = 10 * time.Millisecond
)

func cleanupDescendants(root int) error {
	termDeadline := time.Now().Add(descendantTerminateGrace)
	for {
		if err := reapAdopted(); err != nil {
			return err
		}
		pids, err := linuxDescendants(root)
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			return nil
		}
		for _, process := range pids {
			if err := signalObservedProcess(process, syscall.SIGTERM); err != nil {
				return err
			}
		}
		if !time.Now().Before(termDeadline) {
			break
		}
		time.Sleep(descendantPollInterval)
	}
	// SIGKILL phase. The snapshot and the signal are taken inside the loop, so
	// there is no window between "decide whom to kill" and "kill them".
	killDeadline := time.Now().Add(descendantKillSettle)
	for {
		if err := reapAdopted(); err != nil {
			return err
		}
		pids, err := linuxDescendants(root)
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			return nil
		}
		// Re-signal on every pass rather than only against the snapshot taken
		// before the loop. A descendant forked between that snapshot and signal
		// delivery would otherwise be detected for the whole settle window but
		// never signalled, and the longer that window is the longer it lives.
		// SIGKILL is idempotent and uncatchable, so repeating it costs nothing.
		for _, process := range pids {
			if err := signalObservedProcess(process, syscall.SIGKILL); err != nil {
				return err
			}
		}
		if !time.Now().Before(killDeadline) {
			break
		}
		time.Sleep(descendantPollInterval)
	}
	return errors.New("supervisor descendants did not retire")
}

func reapAdopted() error {
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil)
		if err == unix.EINTR {
			continue
		}
		if err == unix.ECHILD || pid == 0 {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type observedLinuxProcess struct {
	pid       int
	parent    int
	startTime string
}

// readProcessStat is a seam so the teardown race can be driven deterministically.
//
// A process caught mid-teardown -- present in the readdir snapshot, gone by the
// time its stat is read -- makes this read fail with ESRCH rather than ENOENT,
// because the kernel detaches the task from the PID hash before it invalidates the
// /proc dentry. That happens a few hundred times per minute of churn on a loaded
// machine and essentially never on an idle one, which is why the resulting failure
// looked like a flake. Reproducing it by spawning processes is a matter of luck, so
// the read is indirected and a test substitutes the errno.
var readProcessStat = func(pid string) ([]byte, error) {
	return os.ReadFile(filepath.Join("/proc", pid, "stat"))
}

func linuxDescendants(root int) ([]observedLinuxProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	all := make([]observedLinuxProcess, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		raw, err := readProcessStat(entry.Name())
		if err != nil {
			// The process was in the readdir snapshot but has gone by the time we
			// read it. Both ENOENT and ESRCH mean that, and neither is a defect:
			// skip it and let the next scan confirm. Returning the error here is
			// what turned a normal exit race into "descendant containment could
			// not be proven".
			if processGone(err) {
				continue
			}
			return nil, err
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			return nil, errors.New("invalid process status while cleaning descendants")
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 20 {
			return nil, errors.New("incomplete process status while cleaning descendants")
		}
		// A zombie has already terminated and released every resource it held. It
		// stays visible in /proc only until this process reaps it, and reapAdopted
		// has already drained those by the time this runs. Reporting one as a live
		// descendant asserts a containment failure for a process that cannot be
		// holding anything, and because reap-then-scan is inherently racy, the
		// window in which a freshly orphaned process is a not-yet-reaped zombie is
		// exactly the window that widens under load.
		if state := fields[0]; state == "Z" || state == "X" {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, errors.New("invalid parent process while cleaning descendants")
		}
		all = append(all, observedLinuxProcess{pid: pid, parent: parent, startTime: fields[19]})
	}
	found := map[int]bool{root: true}
	result := make([]observedLinuxProcess, 0)
	for changed := true; changed; {
		changed = false
		for _, item := range all {
			if found[item.parent] && !found[item.pid] {
				found[item.pid], changed = true, true
				if item.pid != root {
					result = append(result, item)
				}
			}
		}
	}
	return result, nil
}

// processGone reports whether err means "this process no longer exists", which is
// a success for containment purposes rather than a failure.
//
// Two different errnos mean exactly that, and tolerating only one of them turns an
// ordinary teardown race into a hard containment failure. ENOENT is what /proc
// returns once the directory entry is gone. ESRCH is what it returns for a process
// that is mid-teardown and was still present in the readdir snapshot the scan was
// built from -- the window between the scan and the read, which is precisely the
// window that widens under load. A process that has gone cannot be holding
// anything, so in both cases the correct answer is to stop tracking it rather than
// to fail the check.
func processGone(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func signalObservedProcess(process observedLinuxProcess, signal syscall.Signal) error {
	pidfd, err := unix.PidfdOpen(process.pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)
	current, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process.pid), "stat"))
	if processGone(err) {
		return nil
	}
	if err != nil {
		return err
	}
	end := strings.LastIndexByte(string(current), ')')
	if end < 0 {
		return errors.New("invalid process status while confirming descendant identity")
	}
	fields := strings.Fields(string(current[end+1:]))
	if len(fields) < 20 || fields[19] != process.startTime {
		return errors.New("descendant process identity changed before signalling")
	}
	err = unix.PidfdSendSignal(pidfd, unix.Signal(signal), nil, 0)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
