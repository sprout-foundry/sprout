package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

// MCPClient implements the MCPServer interface for subprocess-based MCP servers
type MCPClient struct {
	config       MCPServerConfig
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	running      bool
	initialized  bool
	mutex        sync.RWMutex
	logger       *utils.Logger
	messageID    int64
	pendingReqs  map[string]chan MCPMessage
	reqMutex     sync.RWMutex
	restartCount int
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewMCPClient creates a new MCP client for a server
func NewMCPClient(config MCPServerConfig, logger *utils.Logger) *MCPClient {
	return &MCPClient{
		config:      config,
		logger:      logger,
		pendingReqs: make(map[string]chan MCPMessage),
	}
}

// Start starts the MCP server process
func (c *MCPClient) Start(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.running {
		return fmt.Errorf("server %s is already running", c.config.Name)
	}

	// Create context for the server process
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Set up the command
	c.cmd = exec.CommandContext(c.ctx, c.config.Command, c.config.Args...)

	// Set environment variables
	if c.config.Env != nil {
		for key, value := range c.config.Env {
			c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Set working directory
	if c.config.WorkingDir != "" {
		c.cmd.Dir = c.config.WorkingDir
	}

	// Set up pipes
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start MCP server %s: %w", c.config.Name, err)
	}

	c.running = true
	c.restartCount++

	// Start message handling goroutines
	go c.handleMessages()
	go c.handleErrors()

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("🚀 Started MCP server: %s", c.config.Name))
	}

	return nil
}

// Stop stops the MCP server process
func (c *MCPClient) Stop(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.running {
		return nil
	}

	// Cancel the context to signal shutdown
	if c.cancel != nil {
		c.cancel()
	}

	// Close pipes
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}

	// Kill the process if it doesn't exit gracefully
	if c.cmd != nil && c.cmd.Process != nil {
		// Give it a moment to exit gracefully
		done := make(chan error, 1)
		go func() {
			done <- c.cmd.Wait()
		}()

		select {
		case <-done:
			// Process exited gracefully
		case <-time.After(5 * time.Second):
			// Force kill after timeout
			if err := c.cmd.Process.Kill(); err != nil {
				if c.logger != nil {
					c.logger.LogProcessStep(fmt.Sprintf("⚠️ Failed to kill MCP server %s: %v", c.config.Name, err))
				}
			}
			<-done // Wait for the process to actually exit
		}
	}

	c.running = false
	c.initialized = false

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("🛑 Stopped MCP server: %s", c.config.Name))
	}

	return nil
}

// IsRunning checks if the server is running
func (c *MCPClient) IsRunning() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.running
}

// GetName returns the server name
func (c *MCPClient) GetName() string {
	return c.config.Name
}

// GetConfig returns the server configuration
func (c *MCPClient) GetConfig() MCPServerConfig {
	return c.config
}

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
			"name":    "ledit",
			"version": "1.0.0",
		},
	}

	response, err := c.sendRequest(ctx, "initialize", initParams)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP server %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return fmt.Errorf("MCP server %s initialization error: %s", c.config.Name, response.Error.Message)
	}

	c.mutex.Lock()
	c.initialized = true
	c.mutex.Unlock()

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("✅ Initialized MCP server: %s", c.config.Name))
	}

	return nil
}

// ListTools lists available tools from the server
func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	response, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error listing tools from %s: %s", c.config.Name, response.Error.Message)
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
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
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
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	response, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources from %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error listing resources from %s: %s", c.config.Name, response.Error.Message)
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
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
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
		return nil, fmt.Errorf("error reading resource %s from %s: %s", uri, c.config.Name, response.Error.Message)
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
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	response, err := c.sendRequest(ctx, "prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list prompts from %s: %w", c.config.Name, err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("error listing prompts from %s: %s", c.config.Name, response.Error.Message)
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
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
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
		return nil, fmt.Errorf("error getting prompt %s from %s: %s", name, c.config.Name, response.Error.Message)
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

	// Send the message
	messageBytes, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := c.stdin.Write(append(messageBytes, '\n')); err != nil {
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
		return nil, fmt.Errorf("request timeout after %v", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// handleMessages handles incoming messages from the server
func (c *MCPClient) handleMessages() {
	scanner := bufio.NewScanner(c.stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Skip lines that don't start with { (not JSON)
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			// Non-JSON output (warnings, logs, etc.) - skip silently
			continue
		}

		var message MCPMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			if c.logger != nil {
				c.logger.LogProcessStep(fmt.Sprintf("⚠️ Failed to parse MCP message from %s: %v", c.config.Name, err))
			}
			continue
		}

		// Handle responses to our requests
		if message.ID != nil {
			idStr := fmt.Sprintf("%v", message.ID)
			c.reqMutex.RLock()
			if responseChan, exists := c.pendingReqs[idStr]; exists {
				c.reqMutex.RUnlock()
				select {
				case responseChan <- message:
				default:
				}
			} else {
				c.reqMutex.RUnlock()
			}
		}
		// Handle notifications/events (ID is nil)
		// Could be extended to handle server notifications in the future
	}
}

// handleErrors handles stderr output from the server
func (c *MCPClient) handleErrors() {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("🔍 MCP server %s stderr: %s", c.config.Name, line))
		}
	}
}
