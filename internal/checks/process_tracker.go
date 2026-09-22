package checks

import (
	"errors"
	"sync"
	"syscall"
	"time"
)

// processTracker records descendants while the direct child is running and
// also asks the host-specific implementation for processes carrying the
// unforgeable per-effect marker. The marker closes the re-parenting gap on
// hosts that expose process environments (Linux); ancestry polling covers the
// equivalent detached-session case on Darwin.
type processTracker struct {
	root   int
	marker string

	mu       sync.Mutex
	pids     map[int]bool
	scanErr  error
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newProcessTracker(root int, marker string) *processTracker {
	return &processTracker{root: root, marker: marker, pids: map[int]bool{root: true}, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
}

func (t *processTracker) scan() {
	t.mu.Lock()
	defer t.mu.Unlock()
	found, err := discoverCheckProcesses(t.pids, t.marker)
	if err != nil {
		t.scanErr = err
		return
	}
	for _, pid := range found {
		if pid > 1 {
			t.pids[pid] = true
		}
	}
}

func (t *processTracker) start() {
	t.scan()
	go func() {
		defer close(t.doneCh)
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.scan()
			case <-t.stopCh:
				t.scan()
				return
			}
		}
	}()
}

func (t *processTracker) stop() {
	t.stopOnce.Do(func() { close(t.stopCh) })
	<-t.doneCh
}

func (t *processTracker) signal(signal syscall.Signal) {
	t.scan()
	// The group handles ordinary descendants. Individual PIDs handle children
	// that called setsid/setpgid and therefore escaped that group.
	_ = syscall.Kill(-t.root, signal)
	t.mu.Lock()
	pids := make([]int, 0, len(t.pids))
	for pid := range t.pids {
		pids = append(pids, pid)
	}
	t.mu.Unlock()
	for _, pid := range pids {
		_ = syscall.Kill(pid, signal)
	}
}

func (t *processTracker) alive() bool {
	t.scan()
	if syscall.Kill(-t.root, 0) == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for pid := range t.pids {
		if syscall.Kill(pid, 0) == nil {
			return true
		}
	}
	return false
}

func (t *processTracker) reliable() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scanErr == nil
}

func (t *processTracker) failure() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scanErr != nil {
		return t.scanErr
	}
	return errors.New("tracked check processes did not reach a provably absent state")
}

func (t *processTracker) hasDescendants() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pids) > 1
}

func (t *processTracker) close() {
	closeProcessContainment(t.root)
}
