package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type hostingSpec struct {
	Provider      string
	APIBase       string
	Project       string
	HeadRef       string
	HeadOID       string
	BaseRef       string
	Title         string
	Body          string
	OperationID   string
	CredentialRef string
	Fixture       bool
}

type hostedDraft struct {
	ExternalID string
	URL        string
}

type hostingAdapter interface {
	List(context.Context) (hostedDraft, bool, error)
	Create(context.Context) (hostedDraft, error)
}

type hostingClient struct {
	spec   hostingSpec
	client *http.Client
	token  string
}

func fixtureLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host, _, err := net.SplitHostPort(u.Host)
	return err == nil && (host == "127.0.0.1" || host == "[::1]" || host == "::1")
}

func newHostingAdapter(spec hostingSpec) (hostingAdapter, error) {
	base := strings.TrimRight(spec.APIBase, "/")
	if spec.Provider != "github" && spec.Provider != "gitlab" || spec.Project == "" || spec.OperationID == "" ||
		len(spec.Title) == 0 || len(spec.Title) > 256 || len(spec.Body) > 8192 {
		return nil, errors.New("invalid bounded hosting request")
	}
	if spec.Fixture {
		if !fixtureLoopback(base) || spec.CredentialRef != "" {
			return nil, errors.New("fixture hosting requires credential-free loopback API")
		}
	} else if spec.Provider == "github" && base != "https://api.github.com" || spec.Provider == "gitlab" && base != "https://gitlab.com/api/v4" {
		return nil, errors.New("only official GitHub/GitLab API endpoints are supported")
	}
	var token string
	if !spec.Fixture {
		if !strings.HasPrefix(spec.CredentialRef, "env:") {
			return nil, errors.New("named hosting credential required")
		}
		name := strings.TrimPrefix(spec.CredentialRef, "env:")
		if name == "" || len(name) > 128 {
			return nil, errors.New("invalid hosting credential reference")
		}
		for _, r := range name {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return nil, errors.New("invalid hosting credential reference")
			}
		}
		token = os.Getenv(name)
		if token == "" {
			return nil, errors.New("named hosting credential unavailable")
		}
	}
	spec.APIBase = base
	return &hostingClient{spec: spec, token: token, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *hostingClient) request(ctx context.Context, method, endpoint string, body any) ([]byte, int, error) {
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil || len(b) > 16384 {
			return nil, 0, errors.New("hosting request exceeds limit")
		}
		input = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.spec.APIBase+endpoint, input)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.spec.Provider == "github" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	} else if c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, 0, errors.New("hosting API request failed or timed out")
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return nil, 0, errors.New("hosting API response exceeds limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("hosting API returned status %d", response.StatusCode)
	}
	return b, response.StatusCode, nil
}

func (c *hostingClient) endpoint() (string, error) {
	if c.spec.Provider == "github" {
		parts := strings.Split(c.spec.Project, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(c.spec.Project, "?%#\\") {
			return "", errors.New("GitHub project must be owner/repository")
		}
		return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/pulls", nil
	}
	if strings.ContainsAny(c.spec.Project, "?%#\\") || strings.HasPrefix(c.spec.Project, "/") || strings.HasSuffix(c.spec.Project, "/") {
		return "", errors.New("invalid GitLab project path")
	}
	return "/projects/" + url.PathEscape(c.spec.Project) + "/merge_requests", nil
}

func (c *hostingClient) marker() string {
	return "<!-- vigil-delivery-operation:" + c.spec.OperationID + " -->"
}

func (c *hostingClient) validURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return false
	}
	if c.spec.Fixture {
		return fixtureLoopback(c.spec.APIBase) && u.Scheme == "http" && u.Host == strings.TrimPrefix(c.spec.APIBase, "http://")
	}
	if c.spec.Provider == "github" {
		return u.Scheme == "https" && u.Host == "github.com"
	}
	return u.Scheme == "https" && u.Host == "gitlab.com"
}

type githubDraft struct {
	Number  int    `json:"number"`
	Draft   bool   `json:"draft"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Head    struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}

type gitlabDraft struct {
	IID             int    `json:"iid"`
	Draft           bool   `json:"draft"`
	State           string `json:"state"`
	WebURL          string `json:"web_url"`
	Description     string `json:"description"`
	SourceBranch    string `json:"source_branch"`
	TargetBranch    string `json:"target_branch"`
	SHA             string `json:"sha"`
	SourceProjectID int    `json:"source_project_id"`
	TargetProjectID int    `json:"target_project_id"`
}

func (c *hostingClient) classifyGitHub(item githubDraft) (hostedDraft, bool, error) {
	if item.Head.Ref != c.spec.HeadRef || item.Base.Ref != c.spec.BaseRef {
		return hostedDraft{}, false, nil
	}
	if item.Head.Repo.FullName != c.spec.Project || item.Base.Repo.FullName != c.spec.Project ||
		item.Head.SHA != c.spec.HeadOID || !item.Draft || item.State != "open" || !strings.Contains(item.Body, c.marker()) ||
		item.Number < 1 || !c.validURL(item.HTMLURL) {
		return hostedDraft{}, false, errors.New("conflicting or unverifiable GitHub pull request for exact head/base")
	}
	return hostedDraft{ExternalID: strconv.Itoa(item.Number), URL: item.HTMLURL}, true, nil
}

func (c *hostingClient) classifyGitLab(item gitlabDraft) (hostedDraft, bool, error) {
	if item.SourceBranch != c.spec.HeadRef || item.TargetBranch != c.spec.BaseRef {
		return hostedDraft{}, false, nil
	}
	if item.SourceProjectID <= 0 || item.SourceProjectID != item.TargetProjectID || item.SHA != c.spec.HeadOID ||
		!item.Draft || item.State != "opened" || !strings.Contains(item.Description, c.marker()) ||
		item.IID < 1 || !c.validURL(item.WebURL) {
		return hostedDraft{}, false, errors.New("conflicting or unverifiable GitLab merge request for exact head/base")
	}
	return hostedDraft{ExternalID: strconv.Itoa(item.IID), URL: item.WebURL}, true, nil
}

func (c *hostingClient) List(ctx context.Context) (hostedDraft, bool, error) {
	endpoint, err := c.endpoint()
	if err != nil {
		return hostedDraft{}, false, err
	}
	var found hostedDraft
	matched := false
	for page := 1; page <= 5; page++ {
		query := url.Values{}
		query.Set("state", "all")
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))
		if c.spec.Provider == "github" {
			owner := strings.Split(c.spec.Project, "/")[0]
			query.Set("head", owner+":"+c.spec.HeadRef)
			query.Set("base", c.spec.BaseRef)
		} else {
			query.Set("scope", "all")
			query.Set("source_branch", c.spec.HeadRef)
			query.Set("target_branch", c.spec.BaseRef)
		}
		body, _, err := c.request(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
		if err != nil {
			return hostedDraft{}, false, err
		}
		count := 0
		if c.spec.Provider == "github" {
			var items []githubDraft
			if err := json.Unmarshal(body, &items); err != nil {
				return hostedDraft{}, false, errors.New("invalid GitHub list response")
			}
			count = len(items)
			for _, item := range items {
				candidate, ok, err := c.classifyGitHub(item)
				if err != nil || ok && matched {
					return hostedDraft{}, false, errors.New("ambiguous or conflicting GitHub pull requests")
				}
				if ok {
					found, matched = candidate, true
				}
			}
		} else {
			var items []gitlabDraft
			if err := json.Unmarshal(body, &items); err != nil {
				return hostedDraft{}, false, errors.New("invalid GitLab list response")
			}
			count = len(items)
			for _, item := range items {
				candidate, ok, err := c.classifyGitLab(item)
				if err != nil || ok && matched {
					return hostedDraft{}, false, errors.New("ambiguous or conflicting GitLab merge requests")
				}
				if ok {
					found, matched = candidate, true
				}
			}
		}
		if count > 100 {
			return hostedDraft{}, false, errors.New("hosting page exceeds limit")
		}
		if count < 100 {
			return found, matched, nil
		}
	}
	return hostedDraft{}, false, errors.New("hosting listing exceeds five bounded pages")
}

func (c *hostingClient) Create(ctx context.Context) (hostedDraft, error) {
	endpoint, err := c.endpoint()
	if err != nil {
		return hostedDraft{}, err
	}
	body := strings.TrimSpace(c.spec.Body) + "\n\n" + c.marker()
	var request any
	if c.spec.Provider == "github" {
		request = map[string]any{"title": c.spec.Title, "body": body, "head": c.spec.HeadRef, "base": c.spec.BaseRef, "draft": true}
	} else {
		request = map[string]any{"title": "Draft: " + c.spec.Title, "description": body,
			"source_branch": c.spec.HeadRef, "target_branch": c.spec.BaseRef, "remove_source_branch": false}
	}
	response, status, err := c.request(ctx, http.MethodPost, endpoint, request)
	if err != nil {
		return hostedDraft{}, err
	}
	if status != http.StatusCreated {
		return hostedDraft{}, errors.New("hosting create did not return 201 Created")
	}
	if c.spec.Provider == "github" {
		var item githubDraft
		if err := json.Unmarshal(response, &item); err != nil {
			return hostedDraft{}, errors.New("invalid GitHub creation response")
		}
		candidate, ok, err := c.classifyGitHub(item)
		if err != nil || !ok {
			return hostedDraft{}, errors.New("GitHub did not verify the exact draft request")
		}
		return candidate, nil
	}
	var item gitlabDraft
	if err := json.Unmarshal(response, &item); err != nil {
		return hostedDraft{}, errors.New("invalid GitLab creation response")
	}
	candidate, ok, err := c.classifyGitLab(item)
	if err != nil || !ok {
		return hostedDraft{}, errors.New("GitLab did not verify the exact draft request")
	}
	return candidate, nil
}
