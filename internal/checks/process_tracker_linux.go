//go:build linux

package checks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func prepareProcessContainment(_ *exec.Cmd) error { return nil }
func activateProcessContainment(_ int) error      { return nil }
func closeProcessContainment(_ int)               {}
func unresolvedProcessFork(_ int) bool            { return false }

func discoverCheckProcesses(known map[int]bool, marker string) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	type process struct{ pid, parent int }
	processes := make([]process, 0)
	found := make(map[int]bool)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue // the process may have exited between enumeration and read
		}
		closeName := strings.LastIndexByte(string(stat), ')')
		if closeName < 0 {
			return nil, errors.New("invalid process status while proving containment")
		}
		fields := strings.Fields(string(stat[closeName+1:]))
		if len(fields) < 2 {
			return nil, errors.New("incomplete process status while proving containment")
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, errors.New("invalid parent process while proving containment")
		}
		processes = append(processes, process{pid: pid, parent: parent})
		environment, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err == nil {
			for _, variable := range strings.Split(string(environment), "\x00") {
				if variable == marker {
					found[pid] = true
					break
				}
			}
		}
	}
	parents := make(map[int]bool, len(known)+len(found))
	for pid := range known {
		parents[pid] = true
	}
	for pid := range found {
		parents[pid] = true
	}
	for changed := true; changed; {
		changed = false
		for _, process := range processes {
			if parents[process.parent] && !parents[process.pid] {
				parents[process.pid] = true
				found[process.pid] = true
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
