//go:build darwin

package checks

import (
	"bytes"
	"errors"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
)

type darwinProcessBoundary struct {
	kqueue   int
	reliable bool
}

var darwinBoundaries = struct {
	sync.Mutex
	items map[int]*darwinProcessBoundary
}{items: make(map[int]*darwinProcessBoundary)}

func prepareProcessContainment(command *exec.Cmd) error {
	if len(command.Args) == 0 {
		return errors.New("approved check command has no argv")
	}
	original := append([]string(nil), command.Args...)
	command.Path = "/bin/sh"
	command.Args = append([]string{"/bin/sh", "-c", `kill -STOP $$; exec "$@"`, "vigil-check-boundary"}, original...)
	return nil
}

// The fixed wrapper stops before exec. NOTE_TRACK is therefore registered
// before approved code can fork, and the kernel follows every descendant even
// if it creates a new process group/session or is re-parented.
func activateProcessContainment(pid int) error {
	var status unix.WaitStatus
	for {
		_, err := unix.Wait4(pid, &status, unix.WUNTRACED, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if !status.Stopped() {
		return errors.New("Darwin check boundary did not stop before exec")
	}
	kqueue, err := unix.Kqueue()
	if err != nil {
		return err
	}
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR, Fflags: unix.NOTE_FORK | unix.NOTE_TRACK | unix.NOTE_EXIT}
	if _, err := unix.Kevent(kqueue, []unix.Kevent_t{change}, nil, nil); err != nil {
		_ = unix.Close(kqueue)
		return err
	}
	darwinBoundaries.Lock()
	darwinBoundaries.items[pid] = &darwinProcessBoundary{kqueue: kqueue, reliable: true}
	darwinBoundaries.Unlock()
	if err := unix.Kill(pid, unix.SIGCONT); err != nil {
		closeProcessContainment(pid)
		return err
	}
	return nil
}

func closeProcessContainment(pid int) {
	darwinBoundaries.Lock()
	boundary := darwinBoundaries.items[pid]
	delete(darwinBoundaries.items, pid)
	darwinBoundaries.Unlock()
	if boundary != nil {
		_ = unix.Close(boundary.kqueue)
	}
}

func trackedDarwinProcesses(known map[int]bool) ([]int, error) {
	darwinBoundaries.Lock()
	defer darwinBoundaries.Unlock()
	var found []int
	for root, boundary := range darwinBoundaries.items {
		if !known[root] {
			continue
		}
		events := make([]unix.Kevent_t, 64)
		for {
			timeout := unix.Timespec{}
			n, err := unix.Kevent(boundary.kqueue, nil, events, &timeout)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				boundary.reliable = false
				return found, err
			}
			for _, event := range events[:n] {
				if event.Fflags&unix.NOTE_TRACKERR != 0 {
					boundary.reliable = false
				}
				if int(event.Ident) > 1 {
					found = append(found, int(event.Ident))
				}
			}
			if n < len(events) {
				break
			}
		}
		if !boundary.reliable {
			return found, errors.New("Darwin kernel could not track every check descendant")
		}
	}
	return found, nil
}

func discoverCheckProcesses(known map[int]bool, marker string) ([]int, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	found := make(map[int]bool)
	tracked, err := trackedDarwinProcesses(known)
	if err != nil {
		return nil, err
	}
	for _, pid := range tracked {
		found[pid] = true
	}
	parents := make(map[int]bool, len(known))
	for pid := range known {
		parents[pid] = true
	}
	marked := append([]byte(marker), 0)
	for _, process := range processes {
		pid := int(process.Proc.P_pid)
		if pid <= 1 {
			continue
		}
		arguments, readErr := unix.SysctlRaw("kern.procargs2", pid)
		if readErr == nil && bytes.Contains(arguments, marked) {
			parents[pid] = true
			found[pid] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, process := range processes {
			pid := int(process.Proc.P_pid)
			parent := int(process.Eproc.Ppid)
			if pid > 1 && parents[parent] && !parents[pid] {
				parents[pid] = true
				found[pid] = true
				changed = true
			}
		}
	}
	result := make([]int, 0, len(found))
	for pid := range found {
		result = append(result, pid)
	}
	return result, nil
}
