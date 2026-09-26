// Package mcp is a deliberately small JSON-RPC transport over the bounded
// application tool handlers. It owns no authorization or workflow state.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"

	"vigil/internal/store"
	modeltools "vigil/internal/tools"
)

type Server struct {
	Handler   *modeltools.Handler
	SessionID string
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	// _meta is reserved MCP transport metadata supplied by native SDKs. It is
	// bounded by the frame limit, ignored, and never reaches application tools.
	Meta json.RawMessage `json:"_meta,omitempty"`
}

// Serve processes newline-delimited JSON-RPC messages. Native launch code must
// inject SessionID; no request field can replace it.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s == nil || s.Handler == nil || s.SessionID == "" {
		return errors.New("handler and injected session required")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), modeltools.MaxPayload+1)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var req request
		if err := store.Decode(line, &req); err != nil {
			if writeErr := encoder.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: err.Error()}}); writeErr != nil {
				return writeErr
			}
			continue
		}
		if req.JSONRPC != "2.0" {
			if err := encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "invalid JSON-RPC version"}}); err != nil {
				return err
			}
			continue
		}
		if req.Method == "notifications/initialized" && len(req.ID) == 0 {
			continue
		}
		resp := response{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "vigil", "version": "0.1"}}
		case "tools/list":
			caps, err := s.Handler.Capabilities(ctx, s.SessionID)
			if err != nil {
				resp.Error = &rpcError{Code: -32000, Message: err.Error()}
				break
			}
			definitions := make([]map[string]any, 0, len(caps))
			for _, name := range caps {
				definitions = append(definitions, map[string]any{"name": name, "description": "Bounded Vigil application handler; authority is injected server-side.", "inputSchema": inputSchema(name)})
			}
			resp.Result = map[string]any{"tools": definitions}
		case "tools/call":
			var params toolCallParams
			if err := store.Decode(req.Params, &params); err != nil {
				resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			} else {
				result, err := s.Handler.Call(ctx, s.SessionID, params.Name, params.Arguments)
				if err != nil {
					resp.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true}
				} else {
					if err := s.Handler.AuditCall(ctx, s.SessionID, req.ID, params.Name, params.Arguments, result); err != nil {
						resp.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": "tool result audit failed"}}, "isError": true}
					} else {
						resp.Result = map[string]any{"content": []map[string]string{{"type": "text", "text": string(result)}}, "isError": false}
					}
				}
			}
		default:
			resp.Error = &rpcError{Code: -32601, Message: "unsupported method"}
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.New("invalid or oversized MCP frame: " + err.Error())
	}
	return nil
}

func inputSchema(name string) map[string]any {
	properties := map[string]any{}
	required := []string{}
	switch name {
	case "project.read":
		properties["cursor"] = map[string]string{"type": "string"}
		properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": modeltools.MaxList}
	case "plan.read":
		properties["id"] = map[string]string{"type": "string"}
		properties["revision"] = map[string]any{"type": "integer", "minimum": 1}
		properties["cursor"] = map[string]string{"type": "string"}
		properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": modeltools.MaxList}
		required = []string{"id", "revision"}
	case "task.read":
		properties["id"] = map[string]string{"type": "string"}
		properties["revision"] = map[string]any{"type": "integer", "minimum": 1}
		required = []string{"id", "revision"}
	case "artifact.read":
		properties["id"] = map[string]string{"type": "string"}
		properties["offset"] = map[string]any{"type": "integer", "minimum": 0}
		properties["length"] = map[string]any{"type": "integer", "minimum": 1, "maximum": modeltools.MaxExcerpt}
		required = []string{"id", "offset", "length"}
	case "task.report_blocked":
		properties["command_id"] = map[string]string{"type": "string"}
		properties["task_id"] = map[string]string{"type": "string"}
		properties["expected_task_revision"] = map[string]any{"type": "integer", "minimum": 1}
		properties["reason"] = map[string]any{"type": "string", "maxLength": 4096}
		properties["missing_facts"] = stringArray(50)
		properties["evidence_ids"] = stringArray(50)
		required = []string{"command_id", "task_id", "expected_task_revision", "reason", "missing_facts", "evidence_ids"}
	case "plan.reorder":
		properties["command_id"] = map[string]string{"type": "string"}
		properties["expected_project_revision"] = map[string]any{"type": "integer", "minimum": 1}
		properties["plan_id"] = map[string]string{"type": "string"}
		properties["expected_plan_revision"] = map[string]any{"type": "integer", "minimum": 1}
		properties["tasks"] = stringArray(modeltools.MaxAffected)
		properties["reason"] = map[string]string{"type": "string"}
		required = []string{"command_id", "expected_project_revision", "plan_id", "expected_plan_revision", "tasks", "reason"}
	case "plan.propose_change":
		properties["command_id"] = map[string]string{"type": "string"}
		properties["expected_project_revision"] = map[string]any{"type": "integer", "minimum": 1}
		// The handler applies the authoritative recursively closed Go contract.
		// This transport schema also closes the outer proposal object while its
		// nested plan remains data validated by that same handler.
		properties["proposal"] = map[string]any{"type": "object"}
		required = []string{"command_id", "expected_project_revision", "proposal"}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}

func stringArray(max int) map[string]any {
	return map[string]any{"type": "array", "maxItems": max, "items": map[string]string{"type": "string"}}
}
