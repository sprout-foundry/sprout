package gpulink

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

	"github.com/sprout-foundry/sprout/pkg/runner"
)

// Credentials identify a linked GPU.
type Credentials struct {
	LinkID string `json:"link_id"`
	APIKey string `json:"api_key"`
}

// Client speaks the GPU link's device flow to one platform.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient returns a client for baseURL.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: &http.Client{Timeout: 30 * time.Second}}
}

// StartLink asks the platform for a device code for linking a GPU. engine is
// shown on the approval screen; the platform never reaches it but through
// the tunnel.
func (c *Client) StartLink(ctx context.Context, name, goos, arch, engine string) (*runner.DeviceStart, error) {
	var out runner.DeviceStart
	status, err := c.post(ctx, "/gpus/device", map[string]string{"name": name, "os": goos, "arch": arch, "engine": engine}, &out)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("starting link: platform returned %d", status)
	}
	return &out, nil
}

// PollLink checks whether the person approved the link. It returns the
// link's credentials exactly once, on approval; the poll outcomes are the
// runner's (runner.ErrAuthorizationPending and the rest).
func (c *Client) PollLink(ctx context.Context, deviceCode string) (*Credentials, error) {
	var out Credentials
	status, err := c.post(ctx, "/runners/device/poll", map[string]string{"device_code": deviceCode}, &out)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		if out.LinkID == "" || out.APIKey == "" {
			return nil, errors.New("platform approved the link but returned no credentials")
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
		return nil, errors.New("a GPU with this name was linked to your account while the link was pending; run `sprout gpu link --name …` with a different name")
	case http.StatusBadRequest:
		return nil, errors.New("the platform no longer recognizes this link code; run `sprout gpu link` again")
	default:
		return nil, fmt.Errorf("polling link: platform returned %d", status)
	}
}

func (c *Client) post(ctx context.Context, path string, body, out any) (int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusOK && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decoding response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// EngineModels lists the models the engine at addr serves (GET /v1/models),
// to check it answers before linking and to show what it will offer.
func EngineModels(ctx context.Context, addr, engineKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if engineKey != "" {
		req.Header.Set("Authorization", "Bearer "+engineKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the engine answered %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("reading the engine's model list: %w", err)
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}
