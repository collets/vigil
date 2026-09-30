package scenario

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// loopbackCredential returns the inherited loopback credential for the trusted
// application only. Callers must never print, persist or log the value.
func loopbackCredential() string { return os.Getenv("OPENAI_API_KEY") }

// The route probe is deliberately tiny and bounded: a metadata-only read of the
// prepared loopback endpoint, with a hard body cap and no redirect following, so
// a misconfigured OPENAI_BASE_URL cannot send the credential anywhere else.
const (
	routeProbeBodyLimit = 64 << 10
	routeProbeTimeout   = 5 * time.Second
	routeProbeRedirects = 0
)

// DeclaredEndpointURL returns the URL the walkthrough registers as its production
// endpoint.
//
// The endpoint is an *identity* the application reserves capacity against. With the
// synthetic fixture driver nothing ever connects to it, so the URL need not be
// reachable — it need only be a stable, credential-free loopback address.
//
// It previously came straight from the inherited OPENAI_BASE_URL, which made the
// walkthrough unreproducible on any machine without a prepared route configured:
// the endpoint registration failed on an empty URL. Native macOS validation is the
// case that exposed it. When a loopback route *is* configured, that route is used,
// so the report's recorded route identity stays meaningful; otherwise a documented
// placeholder is used and recorded as a placeholder rather than passed off as a
// route that was probed.
func DeclaredEndpointURL(route LocalRoute) (url string, placeholder bool) {
	if isLoopbackBaseURL(route.BaseURL) {
		return strings.TrimRight(route.BaseURL, "/"), false
	}
	return placeholderEndpointURL, true
}

// placeholderEndpointURL is the fixed loopback address registered when no route is
// configured. It is deliberately unmistakable and never contacted.
const placeholderEndpointURL = "http://127.0.0.1:1/v1"

func loopbackGet(url string) ([]byte, error) {
	if !isLoopbackBaseURL(url) {
		return nil, fmt.Errorf("refusing to contact a non-loopback route")
	}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The credential is referenced by the trusted application only. It is never
	// printed, logged or persisted; it is read here and discarded.
	if key := loopbackCredential(); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{
		Timeout: routeProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy: nil, // never route a named credential through an ambient proxy
			DialContext: (&net.Dialer{
				Timeout: routeProbeTimeout,
			}).DialContext,
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("route probe returned HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, routeProbeBodyLimit+1))
}

// parseModelList returns the single advertised model identifier, or an error
// when the response is not the exact bounded shape this probe expects. An
// unexpected shape is never treated as success.
func parseModelList(body []byte) (string, error) {
	if len(body) > routeProbeBodyLimit {
		return "", fmt.Errorf("route probe response exceeded %d bytes", routeProbeBodyLimit)
	}
	var document struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return "", fmt.Errorf("route probe response was not valid JSON")
	}
	ids := []string{}
	for _, entry := range document.Data {
		if strings.TrimSpace(entry.ID) != "" {
			ids = append(ids, entry.ID)
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("route advertised %d models; the scenario requires exactly one", len(ids))
	}
	return ids[0], nil
}
