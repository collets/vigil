package boundary

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const relayTestToken = "fixture-token-01234567890123456789012345"

func TestRelayCredentialAndRouteBoundary(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-provider-secret" || r.Header.Get("X-Forwarded-Host") != "" || r.URL.Path != "/v1/chat/completions" {
			t.Error("provider request not isolated")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "provider-cookie=private")
		io.WriteString(w, "data: {\"message\":\"ok\"}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	relay, err := NewRelay(context.Background(), upstream.URL+"/v1", "selected", relayTestToken, "fixture-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Forwarded-Host", "attacker")
		w := httptest.NewRecorder()
		relay.ServeHTTP(w, r)
		return w
	}
	w := call("GET", "/v1/models", relayTestToken, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "selected") || calls.Load() != 0 {
		t.Fatal("model metadata bypass", w.Code, w.Body.String())
	}
	for _, c := range []struct{ method, path, token, body string }{
		{"POST", "/v1/chat/completions", "wrong", `{"model":"selected"}`},
		{"POST", "/v1/chat/completions", relayTestToken, `{"model":"other"}`},
		{"POST", "/v1/chat/completions", relayTestToken, `{"model":"other","model":"selected"}`},
		{"POST", "/v1/chat/completions", relayTestToken, `{"model":"selected","n":2}`},
		{"POST", "/v1/responses", relayTestToken, `{"model":"selected","tools":[{"type":"mcp"}]}`},
		{"POST", "/v1/responses", relayTestToken, `{"model":"selected","store":true}`},
		{"POST", "/v1/responses", relayTestToken, `{"model":"selected","background":true}`},
		{"POST", "/v1/responses", relayTestToken, `{"model":"selected","messages":[{"image_url":"http://outside"}]}`},
		{"POST", "/v1/files", relayTestToken, `{"model":"selected"}`},
		{"POST", "/v1/chat/completions?url=http://outside", relayTestToken, `{"model":"selected"}`},
		{"POST", "/v1/%63hat/completions", relayTestToken, `{"model":"selected"}`},
	} {
		w := call(c.method, c.path, c.token, c.body)
		if w.Code < 400 {
			t.Errorf("allowed unauthorized request: %+v", c)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("denied requests reached provider")
	}
	w = call("POST", "/v1/chat/completions", relayTestToken, `{"model":"selected","messages":[],"stream":true,"tools":[{"type":"function","function":{"name":"fixture"}}]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "[DONE]") || w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("invalid forwarding", w.Code, w.Body.String())
	}
	status := relay.Status()
	if calls.Load() != 1 || status.Completed != 1 || status.Active {
		t.Fatal("relay accounting", status)
	}
}

func TestRelayRedirectAndProviderErrorsDoNotLeak(t *testing.T) {
	var escaped atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1) }))
	defer target.Close()
	for _, code := range []int{302, 401, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(code)
				io.WriteString(w, "private-provider-error")
			}))
			defer upstream.Close()
			p, err := NewRelay(context.Background(), upstream.URL+"/v1", "selected", relayTestToken, "private-provider-key")
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"selected"}`))
			r.Header.Set("Authorization", "Bearer "+relayTestToken)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != 502 || strings.Contains(w.Body.String(), "private") || w.Header().Get("Location") != "" || escaped.Load() != 0 {
				t.Fatal("provider detail or redirect escaped", w.Code, w.Body.String())
			}
		})
	}
}

func TestRelayRevocationCancelsActiveRequest(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(5 * time.Second):
		}
	}))
	defer upstream.Close()
	p, err := NewRelay(context.Background(), upstream.URL+"/v1", "selected", relayTestToken, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"selected"}`))
		r.Header.Set("Authorization", "Bearer "+relayTestToken)
		return r
	}
	done := make(chan struct{})
	go func() { defer close(done); p.ServeHTTP(httptest.NewRecorder(), request()) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not reached")
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, request())
	if w.Code != 429 {
		t.Fatal("concurrent inference admitted", w.Code)
	}
	p.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("request not cancelled")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream transport not cancelled")
	}
	w = httptest.NewRecorder()
	p.ServeHTTP(w, request())
	if w.Code != 410 {
		t.Fatal("revoked relay admitted request", w.Code)
	}
	s := p.Status()
	if s.Started != 1 || s.Aborted != 1 || s.Active || !s.Closed {
		t.Fatal(s)
	}
}

func TestRelayRejectsDeepOrOversizedRequest(t *testing.T) {
	deep := `{"model":"selected","messages":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`
	if !json.Valid([]byte(deep)) {
		t.Fatal("bad fixture")
	}
	if validateRelayBody([]byte(deep), "selected") == nil {
		t.Fatal("unbounded nesting")
	}
	p, err := NewRelay(context.Background(), "http://127.0.0.1:1/v1", "selected", relayTestToken, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(strings.Repeat(" ", relayRequestLimit+1)))
	r.Header.Set("Authorization", "Bearer "+relayTestToken)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal("unbounded input", w.Code)
	}
}

func TestRelayOutputFloodAbortsTransport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		block := strings.Repeat("x", 32<<10)
		for n := 0; n < (relayResponseLimit/(32<<10))+2; n++ {
			if _, err := io.WriteString(w, block); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	p, err := NewRelay(context.Background(), upstream.URL+"/v1", "selected", relayTestToken, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	server := httptest.NewServer(p)
	defer server.Close()
	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"selected","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+relayTestToken)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	count, readErr := io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if readErr == nil || count > relayResponseLimit {
		t.Fatal("oversized output appeared complete", count, readErr)
	}
	if s := p.Status(); s.Completed != 0 || s.Aborted != 1 {
		t.Fatal("output flood accounting", s)
	}
}
