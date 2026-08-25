// Package agent_api: HTTP transport for Ollama local API (split from ollama_local.go)
package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ollamaClient is the interface our OllamaLocalClient talks to.
type ollamaClient interface {
	List(ctx context.Context) (*localOllamaListResponse, error)
	Show(ctx context.Context, name string) (*localOllamaShowResponse, error)
	Chat(ctx context.Context, req *localOllamaChatRequest, fn func(*localOllamaChatResponse) error) error
}

type ollamaClientFactory func() (ollamaClient, error)

// httpOllamaClient is a minimal net/http-backed implementation of ollamaClient.
// It replaces the upstream github.com/ollama/ollama/api client (which
// transitively pulls in 8 Dependabot-flagged CVEs).
type httpOllamaClient struct {
	baseURL string
	http    *http.Client
}

func newHTTPClientAt(baseURL string) *httpOllamaClient {
	return &httpOllamaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

func (c *httpOllamaClient) List(ctx context.Context) (*localOllamaListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("build list request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama list read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama list status %d: %s", resp.StatusCode, string(body))
	}

	var out localOllamaListResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("ollama list decode: %w", err)
	}
	return &out, nil
}

func (c *httpOllamaClient) Show(ctx context.Context, name string) (*localOllamaShowResponse, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, fmt.Errorf("marshal show request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build show request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama show: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("ollama show read error response: %w", readErr)
		}
		return nil, fmt.Errorf("ollama show status %d: %s", resp.StatusCode, string(respBody))
	}

	var out localOllamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama show decode: %w", err)
	}
	return &out, nil
}

func (c *httpOllamaClient) Chat(ctx context.Context, req *localOllamaChatRequest, fn func(*localOllamaChatResponse) error) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama chat status %d: %s", resp.StatusCode, string(respBody))
	}

	streaming := req.Stream != nil && *req.Stream

	if streaming {
		return readChatNDJSON(resp.Body, fn)
	}

	var single localOllamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&single); err != nil {
		return fmt.Errorf("ollama chat decode: %w", err)
	}
	if fn != nil {
		if err := fn(&single); err != nil {
			return fmt.Errorf("chat response callback: %w", err)
		}
	}
	return nil
}

// readChatNDJSON consumes the newline-delimited JSON streaming body from
// Ollama, invoking fn for each parsed chunk.
func readChatNDJSON(r io.Reader, fn func(*localOllamaChatResponse) error) error {
	scanner := bufio.NewScanner(r)
	// Allow large lines (vision responses can exceed 64K).
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk localOllamaChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("ollama chat ndjson decode: %w", err)
		}
		if fn != nil {
			if err := fn(&chunk); err != nil {
				return fmt.Errorf("chat chunk callback: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ollama chat read: %w", err)
	}
	return nil
}
