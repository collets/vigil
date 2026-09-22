//go:build linux

package checks

import (
	"encoding/json"
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
	fd, err := strconv.Atoi(os.Getenv("VIGIL_CHECK_SUPERVISOR_CONFIG_FD"))
	if err != nil {
		return linuxContainmentFailureExit
	}
	file := os.NewFile(uintptr(fd), "supervisor-config")
	if file == nil {
		return linuxContainmentFailureExit
	}
	defer file.Close()
	var spec supervisorCommand
	if err := json.NewDecoder(file).Decode(&spec); err != nil || spec.Path == "" || len(spec.Args) == 0 {
		return linuxContainmentFailureExit
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
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
	interrupt := make(chan os.Signal, 2)
	signal.Notify(interrupt, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-interrupt:
		terminateProcessTree(command.Process.Pid)
		<-finished
	case <-finished:
	}
	signal.Stop(interrupt)
	if !cleanupDescendants(os.Getpid()) {
		return linuxContainmentFailureExit
	}
	if command.ProcessState == nil {
		return linuxContainmentFailureExit
	}
	code := command.ProcessState.ExitCode()
	if code < 0 {
		return 128 + (-code)
	}
	if code == linuxContainmentFailureExit {
		// Reserve the containment status for the supervisor itself.
		return linuxContainmentFailureExit - 1
	}
	return code
}

func terminateProcessTree(root int) {
	_ = syscall.Kill(-root, syscall.SIGTERM)
	for _, pid := range linuxDescendants(os.Getpid()) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	time.Sleep(250 * time.Millisecond)
	_ = syscall.Kill(-root, syscall.SIGKILL)
	for _, pid := range linuxDescendants(os.Getpid()) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func cleanupDescendants(root int) bool {
	for attempt := 0; attempt < 100; attempt++ {
		reapAdopted()
		pids := linuxDescendants(root)
		if len(pids) == 0 {
			return true
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, pid := range linuxDescendants(root) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	for attempt := 0; attempt < 200; attempt++ {
		reapAdopted()
		if len(linuxDescendants(root)) == 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func reapAdopted() {
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil)
		if err != nil || pid == 0 {
			return
		}
	}
}

func linuxDescendants(root int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	type proc struct{ pid, parent int }
	all := make([]proc, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 2 {
			continue
		}
		parent, err := strconv.Atoi(fields[1])
		if err == nil {
			all = append(all, proc{pid, parent})
		}
	}
	found := map[int]bool{root: true}
	result := make([]int, 0)
	for changed := true; changed; {
		changed = false
		for _, item := range all {
			if found[item.parent] && !found[item.pid] {
				found[item.pid], changed = true, true
				if item.pid != root {
					result = append(result, item.pid)
				}
			}
		}
	}
	return result
}
