// Package platformauth is the sprout CLI's sign-in to a platform account
// (`sprout login`): a device-code flow approved in the browser mints an
// access token that can read who the account is, link this machine to it
// (as a runner or a GPU), and use the platform's MCP server for the person
// (their projects and tasks; no repository access). With it, `sprout runner
// link` and `sprout gpu link` link straight to the signed-in account instead
// of asking for a browser approval each time, and sprout's agent gets the
// platform's tools through the MCP server login adds (mcp.go).
//
// The platform side is the platform repo's internal/api/cli_login.go.
package platformauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/runner"
)

// credentialKey names the sign-in token in sprout's credential store.
const credentialKey = "sprout-platform-login" //nolint:gosec // G101: a credential-store entry *name*, not a credential value

// ErrNotSignedIn is a CLI with no sign-in (or one the platform refused).
var ErrNotSignedIn = errors.New("not signed in: run `sprout login`")

// Login is the saved sign-in. The token is not part of it: it lives in the
// credential store.
type Login struct {
	PlatformURL string `json:"platform_url"`
	Email       string `json:"email"`
}

func loginPath() (string, error) {
	base, err := configuration.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(base, "platform", "login.json"), nil
}

// Load returns the saved sign-in and its token; ok is false when the CLI is
// not signed in.
func Load() (*Login, string, bool) {
	path, err := loginPath()
	if err != nil {
		return nil, "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	var l Login
	if json.Unmarshal(data, &l) != nil || l.PlatformURL == "" {
		return nil, "", false
	}
	token, _, err := credentials.GetFromActiveBackend(credentialKey)
	if err != nil || token == "" {
		if fileToken, ferr := credentials.NewFileBackend().Get(credentialKey); ferr == nil && fileToken != "" {
			token = fileToken
		}
	}
	if token == "" {
		return nil, "", false
	}
	return &l, token, true
}

// SignedInTo returns the token when the CLI is signed in to platformURL.
func SignedInTo(platformURL string) (*Login, string, bool) {
	l, token, ok := Load()
	if !ok || !samePlatform(l.PlatformURL, platformURL) {
		return nil, "", false
	}
	return l, token, true
}

func samePlatform(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// Save stores the sign-in and its token.
func Save(l *Login, token string) (string, error) {
	path, err := loginPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("creating platform dir: %w", err)
	}
	source := "credential store"
	if err := credentials.SetToActiveBackend(credentialKey, token); err != nil {
		if ferr := credentials.NewFileBackend().Set(credentialKey, token); ferr != nil {
			return "", fmt.Errorf("storing the sign-in: %w; file fallback: %v", err, ferr)
		}
		source = "encrypted file (the OS keyring was unavailable)"
	}
	l.PlatformURL = strings.TrimRight(l.PlatformURL, "/")
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing the sign-in: %w", err)
	}
	return source, nil
}

// Remove forgets the sign-in on this machine.
func Remove() error {
	_ = credentials.DeleteFromActiveBackend(credentialKey)
	_ = credentials.NewFileBackend().Delete(credentialKey)
	path, err := loginPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Client speaks to one platform, with the sign-in token when it has one.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient returns a client for baseURL; token may be empty before login.
func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTPClient: &http.Client{Timeout: 30 * time.Second}}
}

// StartLogin asks the platform for a device code to sign this CLI in.
func (c *Client) StartLogin(ctx context.Context, name, goos, arch string) (*runner.DeviceStart, error) {
	var out runner.DeviceStart
	status, err := c.do(ctx, http.MethodPost, "/cli/device", map[string]string{"name": name, "os": goos, "arch": arch}, &out)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("starting sign-in: platform returned %d", status)
	}
	return &out, nil
}

// Granted is a successful sign-in.
type Granted struct {
	AccessToken  string `json:"access_token"`
	AccountEmail string `json:"account_email"`
}

// PollLogin checks whether the person approved the sign-in; the outcomes are
// the runner device flow's (runner.ErrAuthorizationPending and the rest).
func (c *Client) PollLogin(ctx context.Context, deviceCode string) (*Granted, error) {
	var out Granted
	status, err := c.do(ctx, http.MethodPost, "/runners/device/poll", map[string]string{"device_code": deviceCode}, &out)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		if out.AccessToken == "" {
			return nil, errors.New("platform approved the sign-in but returned no token")
		}
		return &out, nil
	case http.StatusPreconditionRequired:
		return nil, runner.ErrAuthorizationPending
	case http.StatusTooManyRequests:
		return nil, runner.ErrSlowDown
	case http.StatusForbidden:
		return nil, runner.ErrAccessDenied
	case http.StatusGone:
		return nil, runner.ErrExpiredToken
	case http.StatusConflict:
		return nil, errors.New("your account has the most access tokens allowed; revoke one under Account → Developer and sign in again")
	default:
		return nil, fmt.Errorf("polling sign-in: platform returned %d", status)
	}
}

// Account is who the CLI is signed in as.
type Account struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Plan   string `json:"plan"`
	// MCPAllowed is whether the account's plan includes the platform's MCP
	// server, which the sign-in token reaches.
	MCPAllowed bool `json:"mcp_allowed"`
}

// WhoAmI reads the signed-in account.
func (c *Client) WhoAmI(ctx context.Context) (*Account, error) {
	var out Account
	status, err := c.do(ctx, http.MethodGet, "/cli/whoami", nil, &out)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, ErrNotSignedIn
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("reading the account: platform returned %d", status)
	}
	return &out, nil
}

// LinkRequest describes the machine (and, for a GPU, its engine) to link.
type LinkRequest struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Mode   string `json:"mode,omitempty"`
	Engine string `json:"engine,omitempty"`
}

// Linked is a machine linked with the sign-in: the runner's or link's id,
// its key (shown once) and the account it landed on.
type Linked struct {
	RunnerID     string `json:"runner_id,omitempty"`
	LinkID       string `json:"link_id,omitempty"`
	APIKey       string `json:"api_key"`
	AccountEmail string `json:"account_email"`
}

// LinkRunner links this machine as a runner on the signed-in account.
func (c *Client) LinkRunner(ctx context.Context, req LinkRequest) (*Linked, error) {
	return c.link(ctx, "/cli/runners", req)
}

// LinkGPU links a GPU on this machine to the signed-in account.
func (c *Client) LinkGPU(ctx context.Context, req LinkRequest) (*Linked, error) {
	return c.link(ctx, "/cli/gpus", req)
}

func (c *Client) link(ctx context.Context, path string, req LinkRequest) (*Linked, error) {
	var out Linked
	var refusal struct {
		Error string `json:"error"`
	}
	status, body, err := c.doRaw(ctx, http.MethodPost, path, req)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusOK:
		if err := json.Unmarshal(body, &out); err != nil || out.APIKey == "" {
			return nil, errors.New("platform linked the machine but returned no key")
		}
		return &out, nil
	case status == http.StatusUnauthorized:
		return nil, ErrNotSignedIn
	case json.Unmarshal(body, &refusal) == nil && refusal.Error != "":
		return nil, errors.New(refusal.Error)
	default:
		return nil, fmt.Errorf("linking: platform returned %d", status)
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	status, data, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return status, err
	}
	if status == http.StatusOK && out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return status, fmt.Errorf("decoding response: %w", err)
		}
	}
	return status, nil
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}
