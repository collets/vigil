//go:build darwin

package checks

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func discoverCheckProcesses(known map[int]bool, marker string) ([]int, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	found := make(map[int]bool)
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
