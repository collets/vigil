//go:build linux

package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const linuxContainmentFailureExit = 125
const linuxContainmentCompletion = "vigil-check-cleanup-complete-v1\n"
const linuxContainmentReady = "vigil-check-supervisor-ready-v1\n"
const linuxContainmentReadinessTimeout = 4 * time.Second

type processContainment struct {
	configRead  *os.File
	configWrite *os.File
	proofRead   *os.File
	proofWrite  *os.File
	readyRead   *os.File
	readyWrite  *os.File
	writeDone   chan error
}

type supervisorCommand struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
	Env  []string `json:"env"`
}

func prepareProcessContainment(command *exec.Cmd) (*processContainment, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	config, err := json.Marshal(supervisorCommand{Path: command.Path, Args: command.Args, Dir: command.Dir, Env: command.Env})
	if err != nil {
		return nil, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	proofRead, proofWrite, err := os.Pipe()
	if err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, err
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		_ = read.Close()
		_ = write.Close()
		_ = proofRead.Close()
		_ = proofWrite.Close()
		return nil, err
	}
	boundary := &processContainment{
		configRead: read, configWrite: write,
		proofRead: proofRead, proofWrite: proofWrite,
		readyRead: readyRead, readyWrite: readyWrite,
		writeDone: make(chan error, 1),
	}
	// The supervisor is a subreaper. Detached descendants are adopted by it,
	// rather than init, and can therefore be synchronously killed and reaped.
	configFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, read)
	proofFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, proofWrite)
	readyFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, readyWrite)
	command.Env = append(command.Env, "VIGIL_CHECK_SUPERVISOR=1")
	command.Path = executable
	command.Args = []string{executable, "-vigil-check-supervisor"}
	command.Env = append(command.Env,
		"VIGIL_CHECK_SUPERVISOR_CONFIG_FD="+strconv.Itoa(configFD),
		"VIGIL_CHECK_SUPERVISOR_PROOF_FD="+strconv.Itoa(proofFD),
		"VIGIL_CHECK_SUPERVISOR_READY_FD="+strconv.Itoa(readyFD),
	)
	go func() {
		_, writeErr := write.Write(config)
		boundary.writeDone <- errors.Join(writeErr, write.Close())
	}()
	return boundary, nil
}

func (p *processContainment) started(ctx context.Context) error {
	_ = p.configRead.Close()
	p.configRead = nil
	_ = p.proofWrite.Close()
	p.proofWrite = nil
	_ = p.readyWrite.Close()
	p.readyWrite = nil
	if err := p.waitForConfigWriter(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("supervisor readiness canceled: %w", err)
	}
	type readinessResult struct {
		proof []byte
		err   error
	}
	readinessDone := make(chan readinessResult, 1)
	go func() {
		ready, err := io.ReadAll(io.LimitReader(p.readyRead, int64(len(linuxContainmentReady)+1)))
		readinessDone <- readinessResult{proof: ready, err: err}
	}()
	readinessTimer := time.NewTimer(linuxContainmentReadinessTimeout)
	defer readinessTimer.Stop()
	var result readinessResult
	select {
	case result = <-readinessDone:
	case <-ctx.Done():
		_ = p.readyRead.Close()
		result = <-readinessDone
		return errors.Join(fmt.Errorf("supervisor readiness canceled: %w", ctx.Err()), result.err)
	case <-readinessTimer.C:
		_ = p.readyRead.Close()
		result = <-readinessDone
		return errors.Join(errors.New("supervisor readiness timed out"), result.err)
	}
	ready, err := result.proof, result.err
	if err != nil {
		return fmt.Errorf("read supervisor readiness: %w", err)
	}
	_ = p.readyRead.Close()
	p.readyRead = nil
	if !bytes.Equal(ready, []byte(linuxContainmentReady)) {
		return errors.New("supervisor readiness is missing or malformed")
	}
	return nil
}

// The configuration writer is a goroutine in this process writing to a pipe the
// supervisor reads, so it has no natural deadline; this bound is a hang-detector
// and nothing more. It is deliberately not generous. A check's own timeout is
// enforced by started's caller and a longer wait here would let a check that has
// already blown its deadline sit in this function for far longer than the timeout
// it exceeded, converting a fast failure into a slow one. The pre-existing one
// second is kept for that reason.
const configWriterRetireTimeout = time.Second

func (p *processContainment) waitForConfigWriter() error {
	if p.writeDone == nil {
		return nil
	}
	select {
	case err := <-p.writeDone:
		p.writeDone = nil
		p.configWrite = nil
		return err
	case <-time.After(configWriterRetireTimeout):
		_ = p.configWrite.Close()
		err := <-p.writeDone
		p.writeDone = nil
		p.configWrite = nil
		return errors.Join(errors.New("supervisor configuration writer did not retire"), err)
	}
}

func (p *processContainment) completed(command *exec.Cmd) error {
	if command.ProcessState == nil {
		return errors.New("supervisor has no terminal process state")
	}
	if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); !ok || status.Signaled() {
		return errors.New("supervisor did not exit normally")
	}
	if command.ProcessState.ExitCode() == linuxContainmentFailureExit {
		return errors.New("supervisor reported containment failure")
	}
	proof, err := io.ReadAll(io.LimitReader(p.proofRead, int64(len(linuxContainmentCompletion)+1)))
	if err != nil {
		return fmt.Errorf("read supervisor cleanup proof: %w", err)
	}
	if !bytes.Equal(proof, []byte(linuxContainmentCompletion)) {
		return errors.New("supervisor cleanup proof is missing or malformed")
	}
	return nil
}

func (p *processContainment) close() {
	if p.configRead != nil {
		_ = p.configRead.Close()
		p.configRead = nil
	}
	if p.proofWrite != nil {
		_ = p.proofWrite.Close()
		p.proofWrite = nil
	}
	if p.readyWrite != nil {
		_ = p.readyWrite.Close()
		p.readyWrite = nil
	}
	if p.readyRead != nil {
		_ = p.readyRead.Close()
		p.readyRead = nil
	}
	if p.configWrite != nil {
		_ = p.configWrite.Close()
	}
	if p.writeDone != nil {
		<-p.writeDone
		p.writeDone = nil
	}
	if p.proofRead != nil {
		_ = p.proofRead.Close()
		p.proofRead = nil
	}
}

func (*processContainment) authoritative() bool      { return true }
func processContainmentShutdownGrace() time.Duration { return 4 * time.Second }
func activateProcessContainment(_ int) error         { return nil }
func closeProcessContainment(_ int)                  {}
func unresolvedProcessFork(_ int) bool               { return false }

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
