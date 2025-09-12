package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/alantheprice/ledit/pkg/utils"
)

// MCPHTTPClient represents an HTTP-based MCP client for remote servers
type MCPHTTPClient struct {
	config       MCPServerConfig
	httpClient   *http.Client
	logger       *utils.Logger
	running      bool
	initialized  bool
	mu           sync.RWMutex
	nextID       int64
}

// NewMCPHTTPClient creates a new HTTP MCP client
func NewMCPHTTPClient(config MCPServerConfig, logger *utils.Logger) *MCPHTTPClient {
	return &MCPHTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
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
		c.logger.LogProcessStep(fmt.Sprintf("🚀 HTTP MCP client started for %s", c.config.URL))
	}
	return nil
}

// Stop stops the HTTP MCP client
func (c *MCPHTTPClient) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.running = false
	c.initialized = false
	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("🛑 HTTP MCP client stopped for %s", c.config.URL))
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
	
	// Add GitHub token authentication if available
	if token, exists := c.config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"]; exists && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("🔄 Sending MCP HTTP request: %s to %s", method, c.config.URL))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var response MCPMessage
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}

	return &response, nil
}

// Initialize sends initialize request to the server
func (c *MCPHTTPClient) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	if !c.running {
		return fmt.Errorf("client not started")
	}

	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"roots": map[string]interface{}{
				"listChanged": false,
			},
		},
		"clientInfo": map[string]interface{}{
			"name":    "ledit",
			"version": "1.0.0",
		},
	}

	_, err := c.sendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	c.initialized = true
	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("✅ HTTP MCP client initialized for %s", c.config.URL))
	}
	return nil
}

// ListTools lists available tools from the server
func (c *MCPHTTPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

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
		c.logger.LogProcessStep(fmt.Sprintf("🔍 Listed %d tools from HTTP MCP server %s", len(tools), c.config.Name))
	}
	return tools, nil
}

// CallTool calls a tool on the server
func (c *MCPHTTPClient) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
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
		c.logger.LogProcessStep(fmt.Sprintf("🔧 Called tool %s on HTTP MCP server %s", request.Name, c.config.Name))
	}
	return &result, nil
}

// ListResources lists available resources from the server
func (c *MCPHTTPClient) ListResources(ctx context.Context) ([]MCPResource, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	response, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("resources/list request failed: %w", err)
	}

	var resources []MCPResource
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		resourcesData, ok := resultMap["resources"]
		if !ok {
			return nil, fmt.Errorf("resources field not found in response")
		}

		resourcesBytes, err := json.Marshal(resourcesData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal resources data: %w", err)
		}

		if err := json.Unmarshal(resourcesBytes, &resources); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resources: %w", err)
		}

		// Set server name for all resources
		for i := range resources {
			resources[i].ServerName = c.config.Name
		}
	}

	return resources, nil
}

// ReadResource reads a resource from the server
func (c *MCPHTTPClient) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	params := map[string]interface{}{
		"uri": uri,
	}

	response, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return nil, fmt.Errorf("resources/read request failed: %w", err)
	}

	var contents []MCPContent
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		contentsData, ok := resultMap["contents"]
		if !ok {
			return nil, fmt.Errorf("contents field not found in response")
		}

		contentsBytes, err := json.Marshal(contentsData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal contents data: %w", err)
		}

		if err := json.Unmarshal(contentsBytes, &contents); err != nil {
			return nil, fmt.Errorf("failed to unmarshal contents: %w", err)
		}
	}

	if len(contents) == 0 {
		return nil, fmt.Errorf("no content returned")
	}

	return &contents[0], nil
}

// ListPrompts lists available prompts from the server
func (c *MCPHTTPClient) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	response, err := c.sendRequest(ctx, "prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("prompts/list request failed: %w", err)
	}

	var prompts []MCPPrompt
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		promptsData, ok := resultMap["prompts"]
		if !ok {
			return nil, fmt.Errorf("prompts field not found in response")
		}

		promptsBytes, err := json.Marshal(promptsData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal prompts data: %w", err)
		}

		if err := json.Unmarshal(promptsBytes, &prompts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal prompts: %w", err)
		}

		// Set server name for all prompts
		for i := range prompts {
			prompts[i].ServerName = c.config.Name
		}
	}

	return prompts, nil
}

// GetPrompt gets a prompt from the server
func (c *MCPHTTPClient) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	response, err := c.sendRequest(ctx, "prompts/get", params)
	if err != nil {
		return nil, fmt.Errorf("prompts/get request failed: %w", err)
	}

	var messages []MCPContent
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		messagesData, ok := resultMap["messages"]
		if !ok {
			return nil, fmt.Errorf("messages field not found in response")
		}

		messagesBytes, err := json.Marshal(messagesData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal messages data: %w", err)
		}

		if err := json.Unmarshal(messagesBytes, &messages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal messages: %w", err)
		}
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages returned")
	}

	return &messages[0], nil
}