package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if os.Getenv("VIGIL_TEST_HELPER") != "1" {
		return
	}
	mode := os.Getenv("VIGIL_TEST_MODE")
	if mode == "blocked" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	send := func(m any) { b, _ := json.Marshal(m); fmt.Println(string(b)) }
	if mode == "malformed" {
		fmt.Println("not json")
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if mode == "oversize" {
		fmt.Println(strings.Repeat("x", 4096))
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if mode == "stderr" {
		fmt.Fprint(os.Stderr, strings.Repeat("sensitive", 200000))
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if mode == "flood" {
		for i := 0; i < 20; i++ {
			send(map[string]any{"method": "event", "params": map[string]int{"i": i}})
		}
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if mode == "exit" {
		os.Exit(4)
	}
	if mode == "held-pipes" {
		child := exec.Command("/bin/sh", "-c", "sleep 30")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if strings.HasPrefix(mode, "hermes") {
		send(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "gateway.ready", "payload": map[string]any{}}})
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var held []Message
	for scanner.Scan() {
		var m Message
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(2)
		}
		if m.Method == "" {
			if strings.HasPrefix(mode, "bidirectional") {
				send(Message{ID: json.RawMessage("1"), Result: json.RawMessage(`{"done":true}`)})
			}
			continue
		}
		if m.Method == "initialized" {
			continue
		}
		if mode == "reverse" {
			held = append(held, m)
			if len(held) == 2 {
				for i := 1; i >= 0; i-- {
					send(Message{ID: held[i].ID, Result: held[i].Params})
				}
			}
			continue
		}
		if strings.HasPrefix(mode, "bidirectional") {
			id := json.RawMessage(`"1"`)
			if mode == "bidirectional-number" {
				id = json.RawMessage(`1`)
			}
			send(Message{ID: id, Method: "approval", Params: json.RawMessage(`{}`)})
			continue
		}
		if mode == "late" && string(m.ID) == "1" {
			time.Sleep(80 * time.Millisecond)
		}
		if strings.HasPrefix(mode, "codex") || strings.HasPrefix(mode, "hermes") {
			fakeProtocol(mode, m, send)
			continue
		}
		send(Message{ID: m.ID, Result: m.Params})
	}
	if mode == "malformed-at-close" {
		fmt.Println("not json")
	}
	os.Exit(0)
}

func testTransport(t *testing.T, mode string, edit func(*ProcessConfig)) *Transport {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := ProcessConfig{Argv: []string{exe, "-test.run=^TestProcessHelper$"}, Env: []string{"VIGIL_TEST_HELPER=1", "VIGIL_TEST_MODE=" + mode}, Cwd: t.TempDir(), FrameBytes: 1 << 20, TrafficBytes: 4 << 20, QueueSize: 32, PendingLimit: 8, RPCTimeout: time.Second, Grace: 20 * time.Millisecond}
	if edit != nil {
		edit(&cfg)
	}
	tr, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	return tr
}
func TestConcurrentOutOfOrderRPC(t *testing.T) {
	tr := testTransport(t, "reverse", nil)
	var wg sync.WaitGroup
	for _, label := range []string{"first", "second"} {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			var r struct {
				Label string `json:"label"`
			}
			if err := tr.Call(context.Background(), "echo", map[string]string{"label": label}, &r); err != nil {
				t.Error(err)
			} else if r.Label != label {
				t.Errorf("wrong correlation: %s", r.Label)
			}
		}(label)
	}
	wg.Wait()
}
func TestServerRequestDoesNotBlockPendingRPC(t *testing.T) {
	for _, mode := range []string{"bidirectional", "bidirectional-number"} {
		t.Run(mode, func(t *testing.T) { testBidirectional(t, mode) })
	}
}
func testBidirectional(t *testing.T, mode string) {
	tr := testTransport(t, mode, nil)
	done := make(chan error, 1)
	go func() { done <- tr.Call(context.Background(), "ask", nil, nil) }()
	select {
	case m := <-tr.Messages():
		if mode == "bidirectional" && string(m.ID) != `"1"` {
			t.Fatal("lost string id")
		}
		if err := tr.Respond(context.Background(), m.ID, map[string]string{"choice": "deny"}, nil); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request blocked")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a, _ := IDKey(json.RawMessage(`1`))
	b, _ := IDKey(json.RawMessage(`"1"`))
	if a == b {
		t.Fatal("id namespaces conflated")
	}
}
func TestMalformedFinalFrameCannotBecomeSuccessfulClose(t *testing.T) {
	tr := testTransport(t, "malformed-at-close", nil)
	if err := tr.Call(context.Background(), "echo", nil, nil); err != nil {
		t.Fatal(err)
	}
	tr.Close()
	if tr.Err() == nil {
		t.Fatal("malformed final frame was ignored")
	}
}
func TestLateReplyCannotResolveAnotherCall(t *testing.T) {
	tr := testTransport(t, "late", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := tr.Call(ctx, "echo", map[string]string{"value": "old"}, nil); err == nil {
		t.Fatal("missing timeout")
	}
	var r struct {
		Value string `json:"value"`
	}
	if err := tr.Call(context.Background(), "echo", map[string]string{"value": "new"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Value != "new" {
		t.Fatal("late response applied to new call")
	}
}
func TestTransportFailures(t *testing.T) {
	for _, mode := range []string{"malformed", "oversize", "stderr", "flood", "exit", "held-pipes"} {
		t.Run(mode, func(t *testing.T) {
			tr := testTransport(t, mode, func(c *ProcessConfig) { c.FrameBytes = 1024; c.TrafficBytes = 4096; c.QueueSize = 1 })
			select {
			case <-tr.Done():
				if tr.Err() == nil {
					t.Fatal("failure not retained")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("failure not detected")
			}
		})
	}
}
func TestBlockedWriteAndBoundedShutdown(t *testing.T) {
	tr := testTransport(t, "blocked", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := tr.Call(ctx, "large", strings.Repeat("x", 256<<10), nil); err == nil {
		t.Fatal("expected write timeout")
	}
	tr.Close()
	if time.Since(start) > time.Second {
		t.Fatal("shutdown not bounded")
	}
}
func TestCancelledRPC(t *testing.T) {
	tr := testTransport(t, "blocked", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tr.Call(ctx, "cancelled", nil, nil); err == nil {
		t.Fatal("cancelled call succeeded")
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.pending) != 0 {
		t.Fatal("pending request leaked")
	}
}
func TestInvalidIDs(t *testing.T) {
	for _, id := range []string{`null`, `true`, `1.5`, `{}`, `[]`} {
		if _, err := IDKey(json.RawMessage(id)); err == nil {
			t.Errorf("accepted %s", id)
		}
	}
}
