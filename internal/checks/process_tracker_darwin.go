//go:build darwin

package checks

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
)

type darwinProcessBoundary struct {
	kqueue       int
	reliable     bool
	forkObserved bool
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

// The fixed wrapper stops before exec, so NOTE_FORK is registered before
// approved code can fork. Current Darwin kernels do not support NOTE_TRACK;
// an observed fork that cannot be tied to a live group or discovered PID is
// therefore explicit containment uncertainty, never a completion proof.
func activateProcessContainment(pid int) error {
	var status unix.WaitStatus
	for {
		waited, err := unix.Wait4(pid, &status, unix.WUNTRACED, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if waited != pid {
			return fmt.Errorf("Darwin check boundary wait returned pid %d for %d", waited, pid)
		}
		break
	}
	// x/sys intentionally reports SIGSTOP as not Stopped on BSD. The kernel
	// wait encoding (low seven bits all set) is the authority we need here.
	if uint32(status)&0x7f != 0x7f {
		return fmt.Errorf("Darwin check boundary did not stop before exec (status=%#x exited=%t signaled=%t)", uint32(status), status.Exited(), status.Signaled())
	}
	kqueue, err := unix.Kqueue()
	if err != nil {
		return err
	}
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR, Fflags: unix.NOTE_FORK | unix.NOTE_EXIT}
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
				if event.Fflags&unix.NOTE_FORK != 0 {
					boundary.forkObserved = true
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

func unresolvedProcessFork(pid int) bool {
	darwinBoundaries.Lock()
	defer darwinBoundaries.Unlock()
	boundary := darwinBoundaries.items[pid]
	return boundary != nil && boundary.forkObserved
}

func discoverCheckProcesses(known map[int]bool, _ string) ([]int, error) {
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
	for changed := true; changed; {
		changed = false
		for _, process := range processes {
			pid := int(process.Proc.P_pid)
			parent := int(process.Eproc.Ppid)
			originalParent := int(process.Proc.P_oppid)
			if pid > 1 && (parents[parent] || parents[originalParent]) && !parents[pid] {
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
