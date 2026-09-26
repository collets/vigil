package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vigil/internal/store"
	modeltools "vigil/internal/tools"
)

func TestInitializeAndNotification(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	server := &Server{Handler: &modeltools.Handler{}, SessionID: "fixture-session"}
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	result, ok := response["result"].(map[string]any)
	if !ok || result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("unexpected initialize response: %s", output.String())
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("notification produced a response: %s", output.String())
	}
}

func TestRejectsDuplicateAndOversizedFrames(t *testing.T) {
	server := &Server{Handler: &modeltools.Handler{}, SessionID: "fixture-session"}
	for name, input := range map[string]string{
		"duplicate": `{"jsonrpc":"2.0","id":1,"method":"initialize","method":"tools/list"}` + "\n",
		"oversized": strings.Repeat("x", modeltools.MaxPayload+1) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			err := server.Serve(context.Background(), strings.NewReader(input), &output)
			if name == "duplicate" && (err != nil || !strings.Contains(output.String(), `"code":-32600`)) {
				t.Fatalf("duplicate field was not rejected: error=%v response=%s", err, output.String())
			}
			if name == "oversized" && (err == nil || !strings.Contains(err.Error(), "oversized")) {
				t.Fatalf("oversized frame was not rejected: %v", err)
			}
		})
	}
}

func TestInputSchemasMatchApplicationArgumentNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		required []string
	}{
		{"plan.read", []string{"id", "revision"}},
		{"task.read", []string{"id", "revision"}},
		{"artifact.read", []string{"id", "offset", "length"}},
		{"task.report_blocked", []string{"command_id", "task_id", "expected_task_revision", "reason", "missing_facts", "evidence_ids"}},
	} {
		schema := inputSchema(tc.name)
		got, _ := schema["required"].([]string)
		if len(got) != len(tc.required) {
			t.Fatalf("%s required=%v", tc.name, got)
		}
		for index := range got {
			if got[index] != tc.required[index] {
				t.Fatalf("%s required=%v", tc.name, got)
			}
		}
	}
}

func TestToolCallParamsAcceptOnlyReservedTransportMetadata(t *testing.T) {
	for name, raw := range map[string]string{
		"reserved-meta": `{"name":"project.read","arguments":{"limit":1},"_meta":{"progressToken":1}}`,
		"unknown":       `{"name":"project.read","arguments":{"limit":1},"authority":"grant"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var params toolCallParams
			err := store.Decode([]byte(raw), &params)
			if name == "reserved-meta" && err != nil {
				t.Fatal(err)
			}
			if name == "unknown" && err == nil {
				t.Fatal("unknown transport field accepted")
			}
		})
	}
}
