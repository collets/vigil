package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func fakeHostedItem(provider, baseURL, body string, draft bool) map[string]any {
	if provider == "github" {
		return map[string]any{"number": 1, "draft": draft, "state": "open", "html_url": baseURL + "/pull/1", "body": body,
			"head": map[string]any{"ref": "branch", "sha": strings.Repeat("a", 40), "repo": map[string]string{"full_name": "fixture/project"}},
			"base": map[string]any{"ref": "main", "repo": map[string]string{"full_name": "fixture/project"}}}
	}
	return map[string]any{"iid": 1, "draft": draft, "state": "opened", "web_url": baseURL + "/merge_requests/1", "description": body,
		"source_branch": "branch", "target_branch": "main", "sha": strings.Repeat("a", 40), "source_project_id": 7, "target_project_id": 7}
}

func fakeHostingSpec(provider, baseURL string) hostingSpec {
	return hostingSpec{Provider: provider, APIBase: baseURL, Project: "fixture/project", HeadRef: "branch",
		HeadOID: strings.Repeat("a", 40), BaseRef: "main", Title: "Fixture", Body: "Verified work", OperationID: "operation-one", Fixture: true}
}

func TestHostingAdapterAdversarialResponses(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		t.Run(provider, func(t *testing.T) {
			ctx := context.Background()
			var mutex sync.Mutex
			mode := "duplicate"
			setMode := func(value string) { mutex.Lock(); mode = value; mutex.Unlock() }
			var posted []byte
			posts := 0
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts++
					posted, _ = io.ReadAll(r.Body)
					if mode == "lost-response" {
						hijacker, ok := w.(http.Hijacker)
						if !ok {
							t.Error("fake server cannot simulate lost response")
							return
						}
						conn, _, err := hijacker.Hijack()
						if err == nil {
							_ = conn.Close()
						}
						return
					}
					var input map[string]any
					_ = json.Unmarshal(posted, &input)
					content, _ := input["body"].(string)
					if provider == "gitlab" {
						content, _ = input["description"].(string)
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(fakeHostedItem(provider, server.URL, content, mode != "not-draft"))
					return
				}
				switch mode {
				case "rate-limit":
					w.WriteHeader(http.StatusTooManyRequests)
				case "duplicate":
					item := fakeHostedItem(provider, server.URL, "<!-- vigil-delivery-operation:operation-one -->", true)
					_ = json.NewEncoder(w).Encode([]any{item, item})
				case "pages":
					items := make([]any, 100)
					for i := range items {
						items[i] = map[string]any{"number": i + 1, "iid": i + 1, "head": map[string]string{"ref": "unrelated"}, "source_branch": "unrelated"}
					}
					_ = json.NewEncoder(w).Encode(items)
				case "lost-response":
					if posted == nil {
						_, _ = w.Write([]byte("[]"))
						return
					}
					var input map[string]any
					_ = json.Unmarshal(posted, &input)
					content, _ := input["body"].(string)
					if provider == "gitlab" {
						content, _ = input["description"].(string)
					}
					_ = json.NewEncoder(w).Encode([]any{fakeHostedItem(provider, server.URL, content, true)})
				default:
					_, _ = w.Write([]byte("[]"))
				}
			}))
			defer server.Close()
			adapter, err := newHostingAdapter(fakeHostingSpec(provider, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := adapter.List(ctx); err == nil {
				t.Fatal("duplicate exact candidates accepted")
			}
			setMode("rate-limit")
			if _, _, err := adapter.List(ctx); err == nil {
				t.Fatal("rate-limit response accepted")
			}
			setMode("pages")
			if _, _, err := adapter.List(ctx); err == nil {
				t.Fatal("unbounded pagination accepted")
			}
			setMode("not-draft")
			if _, err := adapter.Create(ctx); err == nil {
				t.Fatal("non-draft creation response accepted")
			}
			setMode("lost-response")
			if _, err := adapter.Create(ctx); err == nil {
				t.Fatal("lost POST response reported success")
			}
			if item, found, err := adapter.List(ctx); err != nil || !found || item.ExternalID != "1" {
				t.Fatalf("lost response not reconcilable: %#v %v %v", item, found, err)
			}
			mutex.Lock()
			finalPosts := posts
			mutex.Unlock()
			if finalPosts != 2 {
				t.Fatalf("fake adapter issued unexpected POST count: %d", finalPosts)
			}
		})
	}
}
