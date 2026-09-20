// Package harness implements the bounded, application-owned harness spike.
package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var ErrClosed = errors.New("harness transport closed")

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("native RPC error %d", e.Code) }

type Message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// IDKey preserves JSON-RPC's distinction between number 1 and string "1".
func IDKey(id json.RawMessage) (string, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(id))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return "", errors.New("invalid RPC id")
	}
	switch v := value.(type) {
	case string:
		return "s:" + v, nil
	case json.Number:
		if _, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return "n:" + string(v), nil
		}
	}
	return "", errors.New("RPC id must be a string or integer")
}

type ProcessConfig struct {
	Argv         []string
	Cwd          string
	Env          []string
	JSONRPC      bool
	FrameBytes   int
	TrafficBytes int64
	QueueSize    int
	PendingLimit int
	RPCTimeout   time.Duration
	Grace        time.Duration
	// Record must be bounded and must not retain arbitrary payloads or stderr.
	Record func(direction string, message Message, size int) error
}

type writeJob struct {
	ctx     context.Context
	message Message
	result  chan error
}

type Transport struct {
	cfg                   ProcessConfig
	cmd                   *exec.Cmd
	stdin, stdout, stderr *os.File
	messages              chan Message
	writes                chan writeJob
	done, exited          chan struct{}
	readDone              chan struct{}
	mu                    sync.Mutex
	err                   error
	closing               bool
	next                  int64
	pending               map[string]chan Message
	traffic               int64
	stderrBytes           int64
	exitErr               error
	once                  sync.Once
	closeOnce             sync.Once
	wg                    sync.WaitGroup
}

func Start(ctx context.Context, cfg ProcessConfig) (*Transport, error) {
	if len(cfg.Argv) == 0 || cfg.FrameBytes < 128 || cfg.TrafficBytes < int64(cfg.FrameBytes) || cfg.QueueSize < 1 || cfg.PendingLimit < 1 || cfg.RPCTimeout <= 0 || cfg.Grace <= 0 {
		return nil, errors.New("invalid process configuration")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t := &Transport{cfg: cfg, messages: make(chan Message, cfg.QueueSize), writes: make(chan writeJob), done: make(chan struct{}), exited: make(chan struct{}), pending: make(map[string]chan Message)}
	t.readDone = make(chan struct{})
	// Own pipes rather than Cmd.StdoutPipe: Wait must not truncate a final frame.
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, err
	}
	t.stdin, t.stdout, t.stderr = inW, outR, errR
	t.cmd = exec.Command(cfg.Argv[0], cfg.Argv[1:]...)
	t.cmd.Dir = cfg.Cwd
	t.cmd.Env = cfg.Env
	t.cmd.Stdin = inR
	t.cmd.Stdout = outW
	t.cmd.Stderr = errW
	setProcessGroup(t.cmd)
	err = t.cmd.Start()
	inR.Close()
	outW.Close()
	errW.Close()
	if err != nil {
		inW.Close()
		outR.Close()
		errR.Close()
		return nil, err
	}
	t.wg.Add(3)
	go t.read()
	go t.drainStderr()
	go t.write()
	go func() {
		err := t.cmd.Wait()
		t.mu.Lock()
		t.exitErr = err
		t.mu.Unlock()
		close(t.exited)
		// Drain the final frames, but do not wait indefinitely on inherited pipes.
		select {
		case <-t.readDone:
		case <-time.After(t.cfg.Grace):
			t.fail(errors.New("harness exited with open output pipes"))
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			t.fail(ctx.Err())
		case <-t.done:
		}
	}()
	return t, nil
}

func (t *Transport) Messages() <-chan Message { return t.messages }
func (t *Transport) Done() <-chan struct{}    { return t.done }
func (t *Transport) Err() error               { t.mu.Lock(); defer t.mu.Unlock(); return t.err }
func (t *Transport) StderrBytes() int64       { t.mu.Lock(); defer t.mu.Unlock(); return t.stderrBytes }

func (t *Transport) fail(err error) {
	t.once.Do(func() {
		t.mu.Lock()
		if !t.closing || (err != ErrClosed && err != io.EOF) {
			t.err = err
		}
		t.mu.Unlock()
		close(t.done)
		t.stdin.Close()
		signalGroup(t.cmd, true)
	})
}

func (t *Transport) record(direction string, m Message, size int) error {
	t.mu.Lock()
	t.traffic += int64(size)
	over := t.traffic > t.cfg.TrafficBytes
	t.mu.Unlock()
	if over {
		return errors.New("harness traffic limit exceeded")
	}
	if t.cfg.Record != nil {
		return t.cfg.Record(direction, m, size)
	}
	return nil
}

func (t *Transport) read() {
	defer t.wg.Done()
	defer close(t.readDone)
	s := bufio.NewScanner(t.stdout)
	s.Buffer(make([]byte, min(4096, t.cfg.FrameBytes)), t.cfg.FrameBytes)
	for s.Scan() {
		line := s.Bytes()
		var m Message
		if err := json.Unmarshal(line, &m); err != nil {
			t.fail(errors.New("malformed harness JSON"))
			return
		}
		if m.JSONRPC != "" && m.JSONRPC != "2.0" {
			t.fail(errors.New("invalid JSON-RPC version"))
			return
		}
		if m.Method != "" && (m.Result != nil || m.Error != nil) {
			t.fail(errors.New("mixed RPC request/response"))
			return
		}
		if len(m.ID) > 0 {
			if _, err := IDKey(m.ID); err != nil {
				t.fail(err)
				return
			}
		}
		if m.Method == "" && (len(m.ID) == 0 || (m.Result == nil) == (m.Error == nil)) {
			t.fail(errors.New("invalid RPC response"))
			return
		}
		if err := t.record("receive", m, len(line)); err != nil {
			t.fail(err)
			return
		}
		if m.Method == "" {
			key, _ := IDKey(m.ID)
			t.mu.Lock()
			ch := t.pending[key]
			delete(t.pending, key)
			t.mu.Unlock()
			if ch != nil {
				ch <- m
			} // one-element channel; late/duplicate replies never block.
			continue
		}
		select {
		case t.messages <- m:
		case <-t.done:
			return
		default:
			t.fail(errors.New("harness event queue overflow"))
			return
		}
	}
	if errors.Is(s.Err(), os.ErrClosed) {
		t.fail(ErrClosed)
	} else if s.Err() != nil {
		t.fail(errors.New("harness frame limit or read failure"))
	} else {
		t.fail(io.EOF)
	}
}

func (t *Transport) drainStderr() {
	defer t.wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := t.stderr.Read(buf)
		if n > 0 {
			t.mu.Lock()
			t.stderrBytes += int64(n)
			t.mu.Unlock()
			if e := t.record("stderr", Message{}, n); e != nil {
				t.fail(e)
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (t *Transport) write() {
	defer t.wg.Done()
	for {
		select {
		case <-t.done:
			return
		case job := <-t.writes:
			if err := job.ctx.Err(); err != nil {
				job.result <- err
				continue
			}
			m := job.message
			if t.cfg.JSONRPC {
				m.JSONRPC = "2.0"
			}
			data, err := json.Marshal(m)
			if err == nil && len(data) >= t.cfg.FrameBytes {
				err = errors.New("outgoing frame limit exceeded")
			}
			if err == nil {
				err = t.record("send", m, len(data))
			}
			if err == nil {
				deadline := time.Now().Add(t.cfg.RPCTimeout)
				if d, ok := job.ctx.Deadline(); ok && d.Before(deadline) {
					deadline = d
				}
				err = t.stdin.SetWriteDeadline(deadline)
				if err == nil {
					cancelled := make(chan struct{})
					stop := context.AfterFunc(job.ctx, func() {
						_ = t.stdin.SetWriteDeadline(time.Now())
						close(cancelled)
					})
					_, err = t.stdin.Write(append(data, '\n'))
					if !stop() {
						<-cancelled
					}
				}
			}
			job.result <- err
			if err != nil {
				t.mu.Lock()
				closing := t.closing
				t.mu.Unlock()
				if closing {
					t.fail(ErrClosed)
				} else {
					t.fail(errors.New("harness write failed; delivery uncertain"))
				}
				return
			}
		}
	}
}

func (t *Transport) send(ctx context.Context, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, t.cfg.RPCTimeout)
	defer cancel()
	job := writeJob{ctx: ctx, message: m, result: make(chan error, 1)}
	select {
	case <-t.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	case t.writes <- job:
	}
	select {
	case err := <-job.result:
		return err
	case <-t.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Transport) Call(ctx context.Context, method string, params any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, t.cfg.RPCTimeout)
	defer cancel()
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	t.mu.Lock()
	if len(t.pending) >= t.cfg.PendingLimit {
		t.mu.Unlock()
		return errors.New("pending RPC limit exceeded")
	}
	t.next++
	id := json.RawMessage(strconv.FormatInt(t.next, 10))
	key, _ := IDKey(id)
	ch := make(chan Message, 1)
	t.pending[key] = ch
	t.mu.Unlock()
	defer func() { t.mu.Lock(); delete(t.pending, key); t.mu.Unlock() }()
	if err = t.send(ctx, Message{ID: id, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s acknowledgement uncertain: %w", method, ctx.Err())
	case <-t.done:
		// Prefer an already received response to a following EOF.
		select {
		case m := <-ch:
			if m.Error != nil {
				return m.Error
			}
			if out != nil {
				return json.Unmarshal(m.Result, out)
			}
			return nil
		default:
			return ErrClosed
		}
	}
}

func (t *Transport) Notify(ctx context.Context, method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return t.send(ctx, Message{Method: method, Params: p})
}
func (t *Transport) Respond(ctx context.Context, id json.RawMessage, result any, rpcErr *RPCError) error {
	if _, err := IDKey(id); err != nil {
		return err
	}
	m := Message{ID: id, Error: rpcErr}
	if rpcErr == nil {
		r, err := json.Marshal(result)
		if err != nil {
			return err
		}
		m.Result = r
	}
	return t.send(ctx, m)
}

// Close bounds shutdown of the owned process group, including a child holding pipes.
// It cannot establish quiescence of children which detached from this group.
func (t *Transport) Close() {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closing = true
		t.mu.Unlock()
		t.stdin.Close()
		forced := false
		select {
		case <-t.exited:
		case <-time.After(t.cfg.Grace):
			forced = true
			signalGroup(t.cmd, false)
		}
		select {
		case <-t.exited:
		case <-time.After(t.cfg.Grace):
			signalGroup(t.cmd, true)
		}
		// Kill remaining group members even if the direct child exited first.
		signalGroup(t.cmd, true)
		// Inspect buffered final frames before declaring normal closure. A malformed
		// frame after completion must still invalidate the fixture result.
		select {
		case <-t.readDone:
		case <-time.After(t.cfg.Grace):
			t.fail(errors.New("harness output drain unconfirmed"))
		}
		t.fail(ErrClosed)
		t.stdout.Close()
		t.stderr.Close()
		t.wg.Wait()
		select {
		case <-t.exited:
			t.mu.Lock()
			if !forced && t.exitErr != nil && t.err == nil {
				t.err = errors.New("harness exited unsuccessfully")
			}
			t.mu.Unlock()
		case <-time.After(t.cfg.Grace):
			t.mu.Lock()
			t.err = errors.New("process reap unconfirmed")
			t.mu.Unlock()
		}
	})
}
