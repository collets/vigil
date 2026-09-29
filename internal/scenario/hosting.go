package scenario

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FakeHosting is a credential-free loopback stand-in for a hosting provider's
// draft API. It exists so the delivery rehearsal can drive the real
// `draft-prepare`/`draft-execute` production path — including the application's
// exact head/base/operation-marker verification — without contacting GitHub or
// GitLab and without holding a token.
//
// It is emphatically not a hosted service: it binds one loopback port for the
// lifetime of one run, creates nothing outside the scenario root, and records
// every request so the report can state exactly what was sent.
type FakeHosting struct {
	Provider string
	Project  string
	BaseRef  string

	listener net.Listener
	server   *http.Server
	mu       sync.Mutex
	items    []map[string]any
	posts    int
	requests []string
	base     string
	headSHA  string
	baseSHA  string
	closed   bool
}

// NewFakeHosting starts a loopback hosting stand-in for one provider.
func NewFakeHosting(provider, project, baseRef string) (*FakeHosting, error) {
	if provider != "github" && provider != "gitlab" {
		return nil, fmt.Errorf("unsupported fake hosting provider %q", provider)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	hosting := &FakeHosting{Provider: provider, Project: project, BaseRef: baseRef, listener: listener, base: "http://" + listener.Addr().String()}
	mux := http.NewServeMux()
	mux.HandleFunc("/", hosting.serve)
	hosting.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = hosting.server.Serve(listener) }()
	return hosting, nil
}

// Base returns the loopback API base the production binary must be pointed at.
func (f *FakeHosting) Base() string { return f.base }

// Posts returns how many creation requests were received. It must be exactly one
// for a successful rehearsal, because a retry must never POST twice.
func (f *FakeHosting) Posts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.posts
}

// CreatedURL returns the URL the stand-in reported for the created draft.
func (f *FakeHosting) CreatedURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.items) == 0 {
		return ""
	}
	for _, item := range f.items {
		if url, ok := item["html_url"].(string); ok && url != "" {
			return url
		}
		if url, ok := item["web_url"].(string); ok && url != "" {
			return url
		}
	}
	return ""
}

// Close stops the loopback listener. It never touches anything outside the
// scenario process.
func (f *FakeHosting) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	_ = f.server.Close()
}

func (f *FakeHosting) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		f.serveList(w)
	case http.MethodPost:
		f.serveCreate(w, r)
	default:
		http.Error(w, `{"message":"unsupported"}`, http.StatusMethodNotAllowed)
	}
}

func (f *FakeHosting) serveList(w http.ResponseWriter) {
	f.mu.Lock()
	items := append([]map[string]any(nil), f.items...)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (f *FakeHosting) serveCreate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		http.Error(w, `{"message":"invalid body"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.posts++
	number := len(f.items) + 1
	item := f.buildItem(number, payload)
	f.items = append(f.items, item)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(item)
}

// buildItem echoes back exactly what the application asked for, so the client's
// own verification is the thing under test rather than a pre-cooked answer.
func (f *FakeHosting) buildItem(number int, payload map[string]any) map[string]any {
	head := fmt.Sprintf("%v", payload["head"])
	if f.Provider == "gitlab" {
		head = fmt.Sprintf("%v", payload["source_branch"])
	}
	base := fmt.Sprintf("%v", payload["base"])
	if f.Provider == "gitlab" {
		base = fmt.Sprintf("%v", payload["target_branch"])
	}
	body := stringOf(payload["body"])
	if body == "" {
		body = stringOf(payload["description"])
	}
	title := stringOf(payload["title"])
	item := map[string]any{
		"head_ref": head,
		"base_ref": base,
		"body":     body,
	}
	if f.Provider == "github" {
		item["number"] = number
		item["draft"] = true
		item["state"] = "open"
		item["html_url"] = f.base + "/" + f.Project + "/pull/" + strconv.Itoa(number)
		item["head"] = map[string]any{"ref": head, "sha": f.headSHA, "repo": map[string]any{"full_name": f.Project}}
		item["base"] = map[string]any{"ref": base, "sha": f.baseSHA, "repo": map[string]any{"full_name": f.Project}}
		return item
	}
	item["iid"] = number
	item["draft"] = true
	item["state"] = "opened"
	item["web_url"] = f.base + "/" + f.Project + "/-/merge_requests/" + strconv.Itoa(number)
	item["source_branch"] = head
	item["target_branch"] = base
	item["source_project_id"] = 1
	item["target_project_id"] = 1
	item["sha"] = f.headSHA
	item["description"] = body
	item["title"] = title
	return item
}

// stringOf renders a decoded JSON value as the string the client sent, so a
// missing field stays empty rather than becoming the literal "<nil>".
func stringOf(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

// headSHA and baseSHA are the exact object identities the fake provider reports.
// The rehearsal sets them from the observed delivery intent so the stand-in can
// only ever confirm the precise head/base the application approved.
func (f *FakeHosting) SetIdentities(headSHA, baseSHA string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headSHA, f.baseSHA = headSHA, baseSHA
}

// RequestLog returns every request path the stand-in received.
func (f *FakeHosting) RequestLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// APIPathFor returns the endpoint path the production client will address for
// this provider and project, so the rehearsal can assert the real path was used.
func (f *FakeHosting) APIPathFor() (string, error) {
	if f.Provider == "github" {
		parts := strings.Split(f.Project, "/")
		if len(parts) != 2 {
			return "", fmt.Errorf("GitHub project must be owner/repository")
		}
		return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/pulls", nil
	}
	return "/projects/" + url.PathEscape(f.Project) + "/merge_requests", nil
}
