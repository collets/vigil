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

func cleanupDescendants(root int) error {
	for attempt := 0; attempt < 100; attempt++ {
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
		time.Sleep(10 * time.Millisecond)
	}
	pids, err := linuxDescendants(root)
	if err != nil {
		return err
	}
	for _, process := range pids {
		if err := signalObservedProcess(process, syscall.SIGKILL); err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 200; attempt++ {
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
		time.Sleep(10 * time.Millisecond)
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
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
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
	if errors.Is(err, os.ErrNotExist) {
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
