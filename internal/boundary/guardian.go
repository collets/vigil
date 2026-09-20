package boundary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

// Lease is renewed atomically by the host controller in a read-only container
// mount. It carries no credentials. The hard deadline cannot be renewed.
type Lease struct {
	RunID     string `json:"run_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// Guard runs only as container PID 1. Its worker has a different, non-root UID,
// so it cannot stop the guardian to evade the deadline. Exiting PID 1 terminates
// the PID namespace, including detached descendants. The host still has to
// inspect the stopped container before releasing workspace/inference ownership.
// This primitive is not a qualified harness or production launcher.
func Guard(runID, leasePath string, uid, gid uint32, limit time.Duration, argv []string) error {
	if runtime.GOOS != "linux" || os.Getpid() != 1 || os.Getuid() != 0 {
		return errors.New("guardian requires root PID 1 inside a Linux container")
	}
	if runID == "" || len(runID) > 128 || uid == 0 || gid == 0 || limit <= 0 || limit > 90*time.Minute || len(argv) == 0 {
		return errors.New("invalid guardian configuration")
	}
	valid := func() bool {
		f, err := os.OpenFile(leasePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return false
		}
		defer f.Close()
		i, err := f.Stat()
		if err != nil || !i.Mode().IsRegular() || i.Size() > 1024 {
			return false
		}
		var lease Lease
		d := json.NewDecoder(io.LimitReader(f, 1025))
		d.DisallowUnknownFields()
		if d.Decode(&lease) != nil || d.Decode(new(any)) != io.EOF {
			return false
		}
		remaining := time.Until(time.UnixMilli(lease.ExpiresAt))
		return lease.RunID == runID && remaining > 0 && remaining <= 10*time.Second
	}
	if !valid() {
		return errors.New("controller lease absent, invalid or expired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, NoSetGroups: true}, Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return errors.New("worker hard deadline reached")
		case <-signals:
			return errors.New("guardian termination requested")
		case <-ticker.C:
			if !valid() {
				return errors.New("controller lease lost")
			}
		}
	}
}
