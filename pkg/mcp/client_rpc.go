package mcp

// client_rpc.go — the MCP client RPC methods + transport: Initialize,
// the tools / resources / prompts accessors (ListTools, CallTool,
// ListResources, ReadResource, ListPrompts, GetPrompt), and the underlying
// sendRequest. Split out of client.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Initialize sends initialize request to server
func (c *MCPClient) Initialize(ctx context.Context) error {
	c.mutex.RLock()
	if c.initialized {
		c.mutex.RUnlock()
		return nil
	}
	c.mutex.RUnlock()

	initParams := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
			"prompts":   map[string]interface{}{},
		},
		"clientInfo": map[string]interface{}{
			"name":    "sprout",
			"version": "1.0.0",
		},
	}

	response, err := c.sendRequest(ctx, "initialize", initParams)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return fmt.Errorf("MCP server %s initialization error: %w", c.config.Name, response.Error)
	}

	c.mutex.Lock()
	c.initialized = true
	c.mutex.Unlock()

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[OK] Initialized MCP server: %s", c.config.Name))
	}

	return nil
}

// ListTools lists available tools from the server
func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	c.mutex.RLock()
	needsInit := !c.initialized
	c.mutex.RUnlock()
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize client: %w", err)
		}
	}

	response, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error listing tools from %s: %w", c.config.Name, response.Error)
	}

	var result struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}

	// Marshal and unmarshal properly to handle interface{} result
	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tools result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools list response: %w", err)
	}

	tools := make([]MCPTool, len(result.Tools))
	for i, tool := range result.Tools {
		tools[i] = MCPTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			ServerName:  c.config.Name,
		}
	}

	return tools, nil
}

// CallTool calls a tool on the server
func (c *MCPClient) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	c.mutex.RLock()
	needsInit := !c.initialized
	c.mutex.RUnlock()
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize client: %w", err)
		}
	}

	params := map[string]interface{}{
		"name":      request.Name,
		"arguments": request.Arguments,
	}

	response, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %s on %s: %w", request.Name, c.config.Name, err)
	}

	if response.Error != nil {
		return &MCPToolCallResult{
			IsError: true,
			Content: []MCPContent{{
				Type: "text",
				Text: response.Error.Message,
			}},
		}, nil
	}

	var result struct {
		Content []MCPContent `json:"content"`
		IsError bool         `json:"isError"`
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool call response: %w", err)
	}

	return &MCPToolCallResult{
		Content: result.Content,
		IsError: result.IsError,
	}, nil
}

// ListResources lists available resources from the server
func (c *MCPClient) ListResources(ctx context.Context) ([]MCPResource, error) {
	c.mutex.RLock()
	needsInit := !c.initialized
	c.mutex.RUnlock()
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize client: %w", err)
		}
	}

	response, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources from %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error listing resources from %s: %w", c.config.Name, response.Error)
	}

	var result struct {
		Resources []MCPResource `json:"resources"`
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resources result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse resources list response: %w", err)
	}

	// Set server name for each resource
	for i := range result.Resources {
		result.Resources[i].ServerName = c.config.Name
	}

	return result.Resources, nil
}

// ReadResource reads a resource from the server
func (c *MCPClient) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	c.mutex.RLock()
	needsInit := !c.initialized
	c.mutex.RUnlock()
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize client: %w", err)
		}
	}

	params := map[string]interface{}{
		"uri": uri,
	}

	response, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource %s from %s: %w", uri, c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error reading resource %s from %s: %w", uri, c.config.Name, response.Error)
	}

	var result struct {
		Contents []MCPContent `json:"contents"`
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse resource read response: %w", err)
	}

	if len(result.Contents) == 0 {
		return nil, fmt.Errorf("no content returned for resource %s", uri)
	}

	return &result.Contents[0], nil
}

// ListPrompts lists available prompts from the server
func (c *MCPClient) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	c.mutex.RLock()
	needsInit := !c.initialized
	c.mutex.RUnlock()
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize client: %w", err)
		}
	}

	response, err := c.sendRequest(ctx, "prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list prompts from %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error listing prompts from %s: %w", c.config.Name, response.Error)
	}

	var result struct {
		Prompts []MCPPrompt `json:"prompts"`
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal prompts result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse prompts list response: %w", err)
	}

	// Set server name for each prompt
	for i := range result.Prompts {
		result.Prompts[i].ServerName = c.config.Name
	}

	return result.Prompts, nil
}

// GetPrompt gets a prompt from the server
func (c *MCPClient) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	c.mutex.RLock()
	needsInit := !c.initialized
	c.mutex.RUnlock()
	if needsInit {
		if err := c.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("initialize client: %w", err)
		}
	}

	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	response, err := c.sendRequest(ctx, "prompts/get", params)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt %s from %s: %w", name, c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error getting prompt %s from %s: %w", name, c.config.Name, response.Error)
	}

	var result struct {
		Messages []MCPContent `json:"messages"`
	}

	resultBytes, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal prompt result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse prompt get response: %w", err)
	}

	if len(result.Messages) == 0 {
		return nil, fmt.Errorf("no messages returned for prompt %s", name)
	}

	return &result.Messages[0], nil
}

// sendRequest sends a JSON-RPC request and waits for the response
func (c *MCPClient) sendRequest(ctx context.Context, method string, params interface{}) (*MCPMessage, error) {
	c.reqMutex.Lock()
	c.messageID++
	id := fmt.Sprintf("req_%d", c.messageID)
	c.reqMutex.Unlock()

	message := MCPMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Create response channel
	responseChan := make(chan MCPMessage, 1)
	c.reqMutex.Lock()
	c.pendingReqs[id] = responseChan
	c.reqMutex.Unlock()

	// Ensure cleanup
	defer func() {
		c.reqMutex.Lock()
		delete(c.pendingReqs, id)
		c.reqMutex.Unlock()
	}()

	// Capture stdin under lock
	c.mutex.RLock()
	stdin := c.stdin
	c.mutex.RUnlock()
	if stdin == nil {
		return nil, fmt.Errorf("stdin not available: server not running")
	}

	// Send the message
	messageBytes, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := stdin.Write(append(messageBytes, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response with timeout
	timeout := 30 * time.Second
	if c.config.Timeout > 0 {
		timeout = c.config.Timeout
	}

	select {
	case response := <-responseChan:
		return &response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout after %s", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
