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
		resp := response{JSONRPC: "2.0", ID: req.ID}
		if req.JSONRPC != "2.0" || req.Method != "tools/call" {
			resp.Error = &rpcError{Code: -32601, Message: "unsupported method"}
		} else {
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := store.Decode(req.Params, &params); err != nil {
				resp.Error = &rpcError{Code: -32602, Message: err.Error()}
			} else {
				result, err := s.Handler.Call(ctx, s.SessionID, params.Name, params.Arguments)
				if err != nil {
					resp.Error = &rpcError{Code: -32000, Message: err.Error()}
				} else {
					var value any
					if err := json.Unmarshal(result, &value); err != nil {
						return err
					}
					resp.Result = value
				}
			}
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
