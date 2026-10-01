package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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
	sessionID   string // Track session ID for GitHub MCP server
}

// NewMCPHTTPClient creates a new HTTP MCP client
func NewMCPHTTPClient(config MCPServerConfig, logger *utils.Logger) *MCPHTTPClient {
	// Use a cookie jar to maintain session state
	jar, _ := cookiejar.New(nil)

	return &MCPHTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
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
	c.sessionID = "" // Clear session ID
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

// sendRequest sends an HTTP request to the MCP server
func (c *MCPHTTPClient) sendRequest(ctx context.Context, method string, params interface{}) (*MCPMessage, error) {
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

	// Add auth headers based on resolved credentials
	if authHeaders, authErr := buildAuthHeaders(c.config.Name, &c.config); authErr != nil {
		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[WARN] Failed to build auth headers for %s: %v", c.config.Name, authErr))
		}
	} else {
		for headerName, headerValue := range authHeaders {
			req.Header.Set(headerName, headerValue)
		}
	}

	// Add session ID header if available (for subsequent requests after initialize)
	c.mu.RLock()
	if c.sessionID != "" && method != "initialize" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.RUnlock()

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[~] Sending MCP HTTP request: %s to %s", method, c.config.URL))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed HTTP request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed HTTP request with status %d: %s", resp.StatusCode, string(responseBody))
	}

	var response MCPMessage
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MCP response: %w", err)
	}

	// Extract session ID from response header if this is an initialize request
	if method == "initialize" {
		if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
			c.mu.Lock()
			c.sessionID = sessionID
			c.mu.Unlock()
			if c.logger != nil {
				c.logger.LogProcessStep(fmt.Sprintf("[key] Captured session ID: %s", sessionID))
			}
		}
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %w", response.Error.Code, response.Error)
	}

	return &response, nil
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

	// Session ID extraction happens in sendRequest now

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
