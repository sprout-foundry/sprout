package git

// pull_request_platform.go — opening a pull/merge request through the
// Sprout Foundry platform from inside one of its workspaces. The platform
// knows the workspace's repository and holds its owner's credential for the
// repository's host, so this works on every host the platform supports
// (GitHub, GitLab, Gitea/Forgejo) without the host's token ever
// being in the workspace.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// platformWorkspace is the platform workspace this process runs in, read from
// the environment the platform sets (WORKSPACE_ID, WORKSPACE_TOKEN,
// PLATFORM_API_URL). ok is false outside a platform workspace.
func platformWorkspace() (apiURL, workspaceID, token string, ok bool) {
	apiURL = strings.TrimRight(os.Getenv("PLATFORM_API_URL"), "/")
	workspaceID = os.Getenv("WORKSPACE_ID")
	token = os.Getenv("WORKSPACE_TOKEN")
	return apiURL, workspaceID, token, apiURL != "" && workspaceID != "" && token != ""
}

type platformPullRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch,omitempty"`
	Draft      bool   `json:"draft,omitempty"`
}

type platformPullResponse struct {
	PullRequest struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
	} `json:"pull_request"`
	Error       string `json:"error"`
	Code        string `json:"code"`
	ExistingURL string `json:"existing_url"`
}

// createPRViaPlatform asks the platform to open the change request
// (POST {PLATFORM_API_URL}/internal/workspace/{id}/pulls). The head branch
// must already be pushed.
func createPRViaPlatform(ctx context.Context, apiURL, workspaceID, token, head, base, title, body string, draft bool) (*PullRequestResult, error) {
	payload, err := json.Marshal(platformPullRequest{Title: title, Body: body, HeadBranch: head, BaseBranch: base, Draft: draft})
	if err != nil {
		return nil, fmt.Errorf("marshal pull request: %w", err)
	}
	endpoint := apiURL + "/internal/workspace/" + workspaceID + "/pulls"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-Token", token)
	resp, err := prHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach the platform: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out platformPullResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		msg := out.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if out.ExistingURL != "" {
			msg += " " + out.ExistingURL
		}
		return nil, httpError(resp.StatusCode, fmt.Sprintf("platform could not open the change request (HTTP %d): %s", resp.StatusCode, msg))
	}
	if out.PullRequest.HTMLURL == "" {
		return nil, fmt.Errorf("platform answered without a change request URL")
	}
	state := out.PullRequest.State
	if state == "" {
		state = "open"
	}
	return &PullRequestResult{URL: out.PullRequest.HTMLURL, Number: out.PullRequest.Number, State: state}, nil
}
