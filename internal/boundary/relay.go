package boundary

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const relayRequestLimit = 8 << 20
const relayResponseLimit = 16 << 20

// Relay holds a provider credential outside the worker. The worker receives a
// unique per-run token and can call only this selected model. It is a primitive:
// a production launcher must enforce its socket/lease/endpoint lifetime.
type Relay struct {
	base                        *url.URL
	model, token, key           string
	client                      *http.Client
	ctx                         context.Context
	cancel                      context.CancelFunc
	mu                          sync.Mutex
	active, closed              bool
	started, completed, aborted uint64
}

func NewRelay(parent context.Context, upstream, model, token, providerKey string) (*Relay, error) {
	u, err := url.Parse(upstream)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || model == "" || len(token) < 32 || providerKey == "" || strings.ContainsAny(providerKey, "\r\n") {
		return nil, errors.New("explicit provider URL, selected model, run token and credential required")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	ctx, cancel := context.WithCancel(parent)
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Relay{base: u, model: model, token: token, key: providerKey, client: client, ctx: ctx, cancel: cancel}, nil
}

// Close revokes new calls and cancels in-flight transport. Cancellation is not
// proof that a remote model server has finished inference.
func (p *Relay) Close() {
	p.mu.Lock()
	p.closed = true
	p.cancel()
	p.mu.Unlock()
	p.client.CloseIdleConnections()
}

type RelayStatus struct {
	Active, Closed              bool
	Started, Completed, Aborted uint64
}

func (p *Relay) Status() RelayStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return RelayStatus{p.active, p.closed, p.started, p.completed, p.aborted}
}

func relayError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message, "type": "vigil_relay"}})
}

func (p *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(credential), []byte(p.token)) != 1 || r.Header.Get("Authorization") == credential {
		relayError(w, http.StatusUnauthorized, "invalid run credential")
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" {
		relayError(w, 400, "noncanonical route")
		return
	}
	p.mu.Lock()
	closed := p.closed || p.ctx.Err() != nil
	p.mu.Unlock()
	if closed {
		relayError(w, 410, "run revoked")
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]string{{"id": p.model, "object": "model", "owned_by": "selected-provider"}}})
		return
	}
	if r.Method != http.MethodPost || (r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/v1/responses") {
		relayError(w, 403, "route not authorized")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, relayRequestLimit+1))
	if err != nil || len(body) > relayRequestLimit {
		relayError(w, 413, "request exceeds relay limit")
		return
	}
	if err = validateRelayBody(body, p.model); err != nil {
		relayError(w, 400, err.Error())
		return
	}
	p.mu.Lock()
	if p.closed || p.ctx.Err() != nil {
		p.mu.Unlock()
		relayError(w, 410, "run revoked")
		return
	}
	if p.active {
		p.mu.Unlock()
		relayError(w, 429, "one in-flight provider request per run")
		return
	}
	p.active = true
	p.started++
	p.mu.Unlock()
	complete := false
	defer func() {
		p.mu.Lock()
		p.active = false
		if complete {
			p.completed++
		} else {
			p.aborted++
		}
		p.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	u := *p.base
	u.Path += strings.TrimPrefix(r.URL.Path, "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		relayError(w, 502, "provider request unavailable")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.key)
	response, err := p.client.Do(req)
	if err != nil {
		relayError(w, 502, "provider transport failed")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Never forward provider error bodies, redirects, cookies or auth headers.
		relayError(w, 502, "provider rejected request")
		return
	}
	content := response.Header.Get("Content-Type")
	if !strings.HasPrefix(content, "application/json") && !strings.HasPrefix(content, "text/event-stream") {
		relayError(w, 502, "unsupported provider response")
		return
	}
	w.Header().Set("Content-Type", content)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	remaining := int64(relayResponseLimit)
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if int64(n) > remaining {
			panic(http.ErrAbortHandler)
		}
		if n > 0 {
			if _, err = w.Write(buffer[:n]); err != nil {
				return
			}
			remaining -= int64(n)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr == io.EOF {
			complete = true
			return
		}
		if readErr != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

func validateRelayBody(body []byte, model string) error {
	// Token validation rejects duplicate model/authority keys at every depth.
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("request nesting exceeds relay limit")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); ok {
			if delimiter == '{' {
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return err
					}
					name := key.(string)
					if seen[name] {
						return errors.New("duplicate request field")
					}
					seen[name] = true
					if name == "image_url" || name == "file_url" || name == "input_image" || name == "input_file" {
						return errors.New("remote media is not supported by this relay")
					}
					if err = value(depth + 1); err != nil {
						return err
					}
				}
			} else if delimiter == '[' {
				for d.More() {
					if err = value(depth + 1); err != nil {
						return err
					}
				}
			} else {
				return errors.New("invalid JSON")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return errors.New("invalid, ambiguous or unsupported request JSON")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing request data")
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil || request == nil {
		return errors.New("request object required")
	}
	allowed := strings.Fields("model messages input instructions tools tool_choice temperature top_p max_tokens max_completion_tokens max_output_tokens stream stream_options stop seed presence_penalty frequency_penalty response_format text reasoning reasoning_effort parallel_tool_calls metadata user store truncation service_tier verbosity n")
	for key := range request {
		found := false
		for _, a := range allowed {
			if key == a {
				found = true
				break
			}
		}
		if !found {
			return errors.New("unsupported provider request field")
		}
	}
	var selected string
	if json.Unmarshal(request["model"], &selected) != nil || selected != model {
		return errors.New("model not authorized")
	}
	if raw, ok := request["n"]; ok && string(bytes.TrimSpace(raw)) != "1" {
		return errors.New("multiple completions not authorized")
	}
	if raw, ok := request["store"]; ok && string(bytes.TrimSpace(raw)) != "false" {
		return errors.New("provider-side storage not authorized")
	}
	if raw, ok := request["tools"]; ok {
		var tools []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &tools) != nil {
			return errors.New("invalid tools")
		}
		for _, tool := range tools {
			if tool.Type != "function" {
				return errors.New("hosted tools not authorized")
			}
		}
	}
	return nil
}
