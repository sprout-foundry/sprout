package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// MCPHTTPClient represents an HTTP-based MCP client for remote servers
type MCPHTTPClient struct {
	config      MCPServerConfig
	httpClient  *http.Client
	logger      *utils.Logger
	running     bool
	initialized bool
	mu          sync.RWMutex
	nextID      int64
	sessionID   string // Mcp-Session-Id issued by the server at initialize
	// lastEventID is the id of the last SSE event consumed. The Streamable
	// HTTP spec allows a client to resume a broken stream by replaying it
	// with Last-Event-ID, so a dropped connection does not lose the frames
	// that were already sent.
	lastEventID string
}

// NewMCPHTTPClient creates a new HTTP MCP client
func NewMCPHTTPClient(config MCPServerConfig, logger *utils.Logger) *MCPHTTPClient {
	// Use a cookie jar to maintain session state
	jar, _ := cookiejar.New(nil)

	// A zero Timeout means "no timeout" to net/http, which would let a hung
	// server wedge a call forever. Fall back to the package default instead.
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultServerTimeout
	}

	return &MCPHTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: timeout,
			Jar:     jar, // Enable cookie handling for session management
		},
		logger:  logger,
		running: false,
		nextID:  1,
	}
}

// Start starts the HTTP MCP client (no-op for HTTP, just marks as running)
func (c *MCPHTTPClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	c.running = true
	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[>>] HTTP MCP client started for %s", c.config.URL))
	}
	return nil
}

// Stop stops the HTTP MCP client
func (c *MCPHTTPClient) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.running = false
	c.initialized = false
	c.sessionID = ""
	c.lastEventID = ""
	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[STOP] HTTP MCP client stopped for %s", c.config.URL))
	}
	return nil
}

// IsRunning checks if the client is running
func (c *MCPHTTPClient) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// GetName returns the server name
func (c *MCPHTTPClient) GetName() string {
	return c.config.Name
}

// GetConfig returns the server configuration
func (c *MCPHTTPClient) GetConfig() MCPServerConfig {
	return c.config
}

// sendRequest sends an HTTP request to the MCP server.
// The Streamable HTTP transport requires Accept to list both JSON and SSE,
// and servers may answer either way — an SSE response body is parsed down
// to the first JSON-RPC message carrying this request's ID.
//
// A server may end the session it issued at initialize (restart, idle
// reaping, TTL). The spec's signal for that is a 404 on a request that
// carried Mcp-Session-Id, so the session is re-established once and the
// request replayed rather than surfacing a bare 404 to the caller.
//
// The replay is limited to read-only methods. A tools/call may already have
// run on the server before the session was lost, and replaying it would
// perform the side effect twice (charge a card, send a message). For those
// the 404 is surfaced with a message saying the outcome is unknown.
func (c *MCPHTTPClient) sendRequest(ctx context.Context, method string, params interface{}) (*MCPMessage, error) {
	response, err := c.sendRequestOnce(ctx, method, params)
	if err == nil || method == "initialize" || !errors.Is(err, errMCPSessionExpired) {
		return response, err
	}

	if !isIdempotentMCPMethod(method) {
		return nil, fmt.Errorf("%w: server %s ended the MCP session during %s, which is not safe to retry — "+
			"the call may or may not have taken effect; re-run it only if a duplicate is acceptable",
			err, c.config.Name, method)
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[RECONNECT] Session for %s expired; re-initializing and retrying %s", c.config.Name, method))
	}
	if initErr := c.reinitialize(ctx); initErr != nil {
		return nil, fmt.Errorf("server %s ended the MCP session and re-initializing failed: %w", c.config.Name, initErr)
	}
	return c.sendRequestOnce(ctx, method, params)
}

// isIdempotentMCPMethod reports whether replaying a request is harmless.
// Everything under */list and */read is a pure read; anything else (notably
// tools/call) may mutate state on the server.
func isIdempotentMCPMethod(method string) bool {
	switch method {
	case "tools/list", "resources/list", "resources/templates/list",
		"prompts/list", "resources/read", "prompts/get", "ping":
		return true
	}
	return false
}

// errMCPSessionExpired marks a 404 on a session-carrying request. It is
// internal: sendRequest recovers from it, and it only escapes when the
// re-initialize itself fails.
var errMCPSessionExpired = errors.New("MCP session expired")

// reinitialize clears the dead session and runs the initialize handshake
// again. The initialized flag is cleared first so a concurrent caller that
// loses the race does not skip the handshake it still needs.
func (c *MCPHTTPClient) reinitialize(ctx context.Context) error {
	c.mu.Lock()
	c.sessionID = ""
	c.initialized = false
	c.mu.Unlock()
	return c.Initialize(ctx)
}

// sendRequestOnce performs one HTTP round trip. See sendRequest for the
// session-recovery wrapper.
func (c *MCPHTTPClient) sendRequestOnce(ctx context.Context, method string, params interface{}) (*MCPMessage, error) {
	// Only lock for the ID increment, not the entire method
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	request := MCPMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.config.URL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Required by the Streamable HTTP transport: the server may answer with
	// plain JSON or open an SSE stream, and many reject requests that do not
	// advertise both.
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")

	// Configured headers first (explicit wins), then resolved credentials,
	// then the built-in auth heuristic, then an OAuth token when the server
	// has a completed login.
	if headers, headerErr := buildRequestHeaders(c.config.Name, &c.config); headerErr != nil {
		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[WARN] Failed to build request headers for %s: %v", c.config.Name, headerErr))
		}
	} else {
		for headerName, headerValue := range headers {
			req.Header.Set(headerName, headerValue)
		}
	}
	if oauthToken, oauthErr := EnsureFreshAccessToken(ctx, c.config.Name); oauthErr == nil && oauthToken != "" {
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+oauthToken)
		}
	}

	// Add session ID header if available (for subsequent requests after
	// initialize). Last-Event-ID is deliberately NOT sent here: it names a
	// position in one logical stream, and carrying it onto an unrelated
	// request would ask the server to replay or skip frames from a different
	// stream. It is only set by the resumption path below.
	c.mu.RLock()
	sessionID := c.sessionID
	c.mu.RUnlock()
	if sessionID != "" && method != "initialize" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[~] Sending MCP HTTP request: %s to %s", method, c.config.URL))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed HTTP request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Session ID capture must happen for both response shapes.
	if method == "initialize" {
		if newSessionID := resp.Header.Get("Mcp-Session-Id"); newSessionID != "" {
			c.mu.Lock()
			c.sessionID = newSessionID
			c.mu.Unlock()
			if c.logger != nil {
				c.logger.LogProcessStep(fmt.Sprintf("[key] Captured session ID: %s", newSessionID))
			}
		}
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusAccepted:
		// A notification (no ID) — nothing to read back.
		return &MCPMessage{JSONRPC: "2.0"}, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("server %s rejected the request with 401 Unauthorized — this server requires OAuth (use mcp_refresh login) or a credential the server accepts (check the Authorization/X-*-Token headers)", c.config.Name)
	case resp.StatusCode == http.StatusNotFound && method == "initialize":
		return nil, fmt.Errorf("server %s returned 404 for %s — the MCP endpoint path may be wrong (e.g. /mcp vs /sse)", c.config.Name, c.config.URL)
	case resp.StatusCode == http.StatusNotFound && sessionID != "":
		return nil, fmt.Errorf("%w: server %s answered 404 to %s with session %q", errMCPSessionExpired, c.config.Name, method, sessionID)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("failed HTTP request with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var response *MCPMessage
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		response, err = c.parseSSEResponse(responseBody, id)
	} else {
		response, err = parseJSONResponse(responseBody)
	}
	if err != nil {
		return nil, err
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %w", response.Error.Code, response.Error)
	}

	return response, nil
}

// sendNotification sends an id-less JSON-RPC notification. Servers answer
// 202 Accepted; any 2xx is success, and errors are returned but non-fatal
// callers decide.
func (c *MCPHTTPClient) sendNotification(ctx context.Context, method string) (*MCPMessage, error) {
	request := MCPMessage{
		JSONRPC: "2.0",
		Method:  method,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.config.URL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	if headers, headerErr := buildRequestHeaders(c.config.Name, &c.config); headerErr == nil {
		for headerName, headerValue := range headers {
			req.Header.Set(headerName, headerValue)
		}
	}
	if oauthToken, oauthErr := EnsureFreshAccessToken(ctx, c.config.Name); oauthErr == nil && oauthToken != "" {
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+oauthToken)
		}
	}

	c.mu.RLock()
	sessionID := c.sessionID
	c.mu.RUnlock()
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed HTTP request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("notification %s rejected with status %d", method, resp.StatusCode)
	}
	return &MCPMessage{JSONRPC: "2.0"}, nil
}

// parseJSONResponse decodes a single JSON-RPC message body.
func parseJSONResponse(body []byte) (*MCPMessage, error) {
	var response MCPMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MCP response: %w", err)
	}
	return &response, nil
}

// parseSSEResponse extracts the JSON-RPC response with the matching ID from
// an SSE stream body. Per the Streamable HTTP spec the response stream
// carries "data:" frames, each a JSON-RPC message; a request's response is
// the frame whose id matches. Server-pushed requests (no id) and unrelated
// notifications are skipped.
//
// The id of the last frame consumed is recorded on the client so a later
// request can send Last-Event-ID and let a server that supports replay
// resume the stream instead of restarting it.
func (c *MCPHTTPClient) parseSSEResponse(body []byte, wantID int64) (*MCPMessage, error) {
	msg, lastID, err := parseSSEStream(body, wantID)
	if lastID != "" {
		c.mu.Lock()
		c.lastEventID = lastID
		c.mu.Unlock()
	}
	return msg, err
}

// parseSSEResponse is the client-free form, kept for callers that only need
// the decoded message (tests, one-shot probes).
func parseSSEResponse(body []byte, wantID int64) (*MCPMessage, error) {
	msg, _, err := parseSSEStream(body, wantID)
	return msg, err
}

// parseSSEStream decodes an SSE body, returning the reply to wantID (if
// present) and the id of the last event seen.
func parseSSEStream(body []byte, wantID int64) (*MCPMessage, string, error) {
	var (
		lastEventID string
		matched     *MCPMessage
	)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if id, ok := strings.CutPrefix(line, "id:"); ok {
			lastEventID = strings.TrimLeft(id, " ")
			continue
		}
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // event/retry fields and comments are irrelevant here
		}
		payload = strings.TrimLeft(payload, " ")

		var msg MCPMessage
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			continue // not a JSON-RPC frame (keepalive, comment, custom event)
		}
		if msg.ID == nil {
			continue // server-initiated request or notification
		}
		if idNum, ok := msg.ID.(float64); ok && int64(idNum) == wantID && matched == nil {
			m := msg
			matched = &m
		}
	}
	if matched == nil {
		return nil, lastEventID, fmt.Errorf("SSE response carried no reply for request id %d", wantID)
	}
	return matched, lastEventID, nil
}

// Initialize sends initialize request to the server
func (c *MCPHTTPClient) Initialize(ctx context.Context) error {
	// Check state with lock, but don't hold it during the HTTP call
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}

	if !c.running {
		c.mu.Unlock()
		return fmt.Errorf("client not started")
	}
	c.mu.Unlock()

	params := map[string]interface{}{
		"protocolVersion": "2025-06-18",
		"capabilities": map[string]interface{}{
			"roots": map[string]interface{}{
				"listChanged": false,
			},
		},
		"clientInfo": map[string]interface{}{
			"name":    "sprout",
			"version": "1.0.0",
		},
	}

	_, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	// The Streamable HTTP handshake: after initialize, the client must
	// notify the server before calling tools. Servers that enforce the
	// sequence reject tools/list with "Server not initialized" otherwise.
	_, err = c.sendNotification(ctx, "notifications/initialized")
	if err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}

	// Set initialized state with lock
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[OK] HTTP MCP client initialized for %s", c.config.URL))
	}
	return nil
}

// ListTools lists available tools from the server
func (c *MCPHTTPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started")
	}
	needsInit := !c.initialized
	c.mu.RUnlock()

	// Auto-initialize if needed
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize client: %w", err)
		}
	}

	response, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}

	var tools []MCPTool
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		toolsData, ok := resultMap["tools"]
		if !ok {
			return nil, fmt.Errorf("tools field not found in response")
		}

		toolsBytes, err := json.Marshal(toolsData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tools data: %w", err)
		}

		if err := json.Unmarshal(toolsBytes, &tools); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tools: %w", err)
		}

		// Set server name for all tools
		for i := range tools {
			tools[i].ServerName = c.config.Name
		}
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[search] Listed %d tools from HTTP MCP server %s", len(tools), c.config.Name))
	}
	return tools, nil
}

// CallTool calls a tool on the server
func (c *MCPHTTPClient) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started")
	}
	needsInit := !c.initialized
	c.mu.RUnlock()

	// Auto-initialize if needed
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize client: %w", err)
		}
	}

	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	params := map[string]interface{}{
		"name":      request.Name,
		"arguments": request.Arguments,
	}

	response, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}

	var result MCPToolCallResult
	if response.Result != nil {
		resultBytes, err := json.Marshal(response.Result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		if err := json.Unmarshal(resultBytes, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[tool] Called tool %s on HTTP MCP server %s", request.Name, c.config.Name))
	}
	return &result, nil
}
