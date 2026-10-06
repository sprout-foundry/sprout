// Package runner implements `sprout runner`: a user's own machine executing
// the browser build's escalated work for their platform account (SP-159).
// The platform side of the protocol is documented in the platform repo's
// docs/runners/PROTOCOL.md.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Modes a runner executes workspaces in (SP-159 §159c).
const (
	ModeContainer = "container"
	ModeNative    = "native"
	ModeBareMetal = "bare-metal"
)

// ValidMode reports whether m is a known execution mode.
func ValidMode(m string) bool {
	return m == ModeContainer || m == ModeNative || m == ModeBareMetal
}

// DeviceStart is the platform's answer to a link request.
type DeviceStart struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

// Credentials identify a linked runner.
type Credentials struct {
	RunnerID string `json:"runner_id"`
	APIKey   string `json:"api_key"`
}

// Heartbeat reports liveness plus what the runner can do. The platform only
// reflects Mode; it never sets it.
type Heartbeat struct {
	Status        string   `json:"status"`
	RunningTasks  int      `json:"running_tasks"`
	Mode          string   `json:"mode"`
	Sandbox       string   `json:"sandbox"`
	RunnerVersion string   `json:"runner_version"`
	Toolchains    []string `json:"toolchains,omitempty"`
	// DirectURL is where the platform reaches the host server now; empty
	// for a relayed runner.
	DirectURL string `json:"direct_url,omitempty"`
	// Relayed reports that the runner serves the platform over its tunnel.
	Relayed bool `json:"relayed,omitempty"`
}

// WorkspaceTask is a start/stop/destroy instruction for one workspace.
// Secrets arrive over the runner-key-authenticated channel and are never
// written to disk by the runner.
type WorkspaceTask struct {
	WorkspaceID    string            `json:"workspace_id"`
	UserID         string            `json:"user_id"`
	RepoURL        string            `json:"repo_url"`
	Action         string            `json:"action"`
	ContainerID    string            `json:"container_id"`
	TxnSecret      string            `json:"txn_secret,omitempty"`
	GitToken       string            `json:"git_token,omitempty"`
	LLMProvider    string            `json:"llm_provider,omitempty"`
	LLMKey         string            `json:"llm_key,omitempty"`
	UserEnv        map[string]string `json:"user_env,omitempty"`
	PlatformAPIURL string            `json:"platform_api_url,omitempty"`
}

// WorkspaceResult reports a started (or failed) workspace.
type WorkspaceResult struct {
	WorkspaceID   string `json:"workspace_id"`
	ContainerID   string `json:"container_id"`
	Port          int    `json:"port"`
	ConnectionURL string `json:"connection_url,omitempty"`
	Status        string `json:"status"`
}

// Device-flow poll outcomes the caller acts on.
var (
	ErrAuthorizationPending = errors.New("authorization pending")
	ErrSlowDown             = errors.New("slow down")
	ErrAccessDenied         = errors.New("link request denied on the platform")
	ErrExpiredToken         = errors.New("link code expired")
)

// Client speaks the runner protocol to one platform.
type Client struct {
	BaseURL    string
	Creds      Credentials
	HTTPClient *http.Client
}

// NewClient returns a client for baseURL; creds may be empty before linking.
func NewClient(baseURL string, creds Credentials) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Creds:      creds,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// StartLink asks the platform for a device code for this machine. mode is
// shown on the approval screen; the platform never stores or sets it.
func (c *Client) StartLink(ctx context.Context, name, goos, arch, mode string) (*DeviceStart, error) {
	var out DeviceStart
	status, err := c.do(ctx, http.MethodPost, "/runners/device", false,
		map[string]string{"name": name, "os": goos, "arch": arch, "mode": mode}, &out)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("starting link: platform returned %d", status)
	}
	return &out, nil
}

// PollLink checks whether the user approved the link. It returns the
// runner's credentials exactly once, on approval.
func (c *Client) PollLink(ctx context.Context, deviceCode string) (*Credentials, error) {
	var out Credentials
	status, err := c.do(ctx, http.MethodPost, "/runners/device/poll", false,
		map[string]string{"device_code": deviceCode}, &out)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		if out.RunnerID == "" || out.APIKey == "" {
			return nil, errors.New("platform approved the link but returned no credentials")
		}
		return &out, nil
	case http.StatusPreconditionRequired:
		return nil, ErrAuthorizationPending
	case http.StatusTooManyRequests:
		return nil, ErrSlowDown
	case http.StatusForbidden:
		return nil, ErrAccessDenied
	case http.StatusGone:
		return nil, ErrExpiredToken
	case http.StatusConflict:
		return nil, errors.New("another runner on your account took this name while the link was pending; run `sprout runner link --name …` with a different name")
	case http.StatusBadRequest:
		return nil, errors.New("the platform no longer recognizes this link code; run `sprout runner link` again")
	default:
		return nil, fmt.Errorf("polling link: platform returned %d", status)
	}
}

// SendHeartbeat reports liveness and capability.
func (c *Client) SendHeartbeat(ctx context.Context, hb Heartbeat) error {
	return c.expectOK(ctx, http.MethodPost, "/runners/"+c.Creds.RunnerID+"/heartbeat", hb, nil, "heartbeat")
}

// PollWorkspaceTasks fetches pending workspace instructions.
func (c *Client) PollWorkspaceTasks(ctx context.Context) ([]WorkspaceTask, error) {
	var out []WorkspaceTask
	if err := c.expectOK(ctx, http.MethodGet, "/runners/"+c.Creds.RunnerID+"/workspace-tasks", nil, &out, "polling workspace tasks"); err != nil {
		return nil, err
	}
	return out, nil
}

// SubmitWorkspaceResult reports a workspace start outcome.
func (c *Client) SubmitWorkspaceResult(ctx context.Context, r WorkspaceResult) error {
	return c.expectOK(ctx, http.MethodPost, "/runners/"+c.Creds.RunnerID+"/workspace-result", r, nil, "workspace result")
}

// UpdateWorkspaceStatus reports a workspace's status change.
func (c *Client) UpdateWorkspaceStatus(ctx context.Context, workspaceID, status string) error {
	return c.expectOK(ctx, http.MethodPost, "/runners/"+c.Creds.RunnerID+"/workspace/"+workspaceID+"/status",
		map[string]string{"status": status}, nil, "workspace status")
}

func (c *Client) expectOK(ctx context.Context, method, path string, body, out any, what string) error {
	status, err := c.do(ctx, method, path, true, body, out)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if status != http.StatusOK {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return fmt.Errorf("%s: platform rejected this runner's key (%d); run `sprout runner link` again", what, status)
		}
		return fmt.Errorf("%s: platform returned %d", what, status)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, authed bool, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authed {
		req.Header.Set("X-Runner-Key", c.Creds.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if out != nil && resp.StatusCode == http.StatusOK && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decoding response: %w", err)
		}
	}
	return resp.StatusCode, nil
}
