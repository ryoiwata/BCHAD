package workflows

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ghClient makes authenticated calls to the GitHub REST API via net/http.
type ghClient struct {
	token      string
	httpClient *http.Client
}

func newGHClient(token string) *ghClient {
	return &ghClient{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type ghRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type ghRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type ghPR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

// getRepo returns basic repo metadata (default branch, etc.).
func (g *ghClient) getRepo(ctx context.Context, owner, repo string) (*ghRepoInfo, error) {
	var r ghRepoInfo
	if err := g.get(ctx, fmt.Sprintf("/repos/%s/%s", owner, repo), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// getBranchSHA returns the HEAD commit SHA for the given branch.
func (g *ghClient) getBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	var ref ghRef
	if err := g.get(ctx, fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", owner, repo, branch), &ref); err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

// createBranch creates a new branch from the given SHA. Returns nil if it already exists.
func (g *ghClient) createBranch(ctx context.Context, owner, repo, branch, sha string) error {
	body := map[string]string{
		"ref": "refs/heads/" + branch,
		"sha": sha,
	}
	err := g.doJSON(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), body, nil)
	if err != nil && strings.Contains(err.Error(), "422") {
		// 422 = branch already exists; treat as non-fatal.
		return nil
	}
	return err
}

// createFile creates a file in the repo, producing one commit on the given branch.
func (g *ghClient) createFile(ctx context.Context, owner, repo, path, commitMsg, content, branch string) error {
	body := map[string]interface{}{
		"message": commitMsg,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	return g.doJSON(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path), body, nil)
}

// createPR opens a pull request and returns its HTML URL.
func (g *ghClient) createPR(ctx context.Context, owner, repo, head, base, title, body string) (string, error) {
	req := map[string]interface{}{
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
		"draft": true,
	}
	var pr ghPR
	if err := g.doJSON(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), req, &pr); err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
}

func (g *ghClient) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	g.setHeaders(req)
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github GET %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (g *ghClient) doJSON(ctx context.Context, method, path string, body, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	g.setHeaders(req)
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("github %s %s: HTTP %d: %v", method, path, resp.StatusCode, errBody)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (g *ghClient) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
}

// parseGitHubRepo extracts owner and repo name from a GitHub HTTPS URL.
// Supports https://github.com/owner/repo and https://github.com/owner/repo.git.
func parseGitHubRepo(repoURL string) (owner, repo string, err error) {
	trimmed := strings.TrimPrefix(repoURL, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github URL %q: expected https://github.com/owner/repo", repoURL)
	}
	return parts[0], parts[1], nil
}
