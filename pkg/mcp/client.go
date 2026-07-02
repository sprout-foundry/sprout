package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
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
	// Health check and reconnection fields
	healthInterval    time.Duration
	stopping          bool
	reconnecting      bool
	reconnectAttempt  int
	connectedAt       time.Time
	healthCheckCancel context.CancelFunc
	healthCheckCtx    context.Context

	// Sliding-window failure tracking
	failureTimestamps []time.Time
	disabled          bool
	disabledReason    string
}

// NewMCPClient creates a new MCP client for a server
func NewMCPClient(config MCPServerConfig, logger *utils.Logger) *MCPClient {
	// Default health check interval is 30 seconds
	healthInterval := 30 * time.Second
	if config.Timeout > 0 && config.Timeout < 60*time.Second {
		// If config timeout is reasonable, use a slightly longer health check interval
		healthInterval = config.Timeout * 2
	}

	return &MCPClient{
		config:         config,
		logger:         logger,
		pendingReqs:    make(map[string]chan MCPMessage),
		healthInterval: healthInterval,
	}
}

// Start starts the MCP server process.
// External callers are blocked during active reconnection to prevent
// concurrent process creation races. Use startInternal() for reconnection.
func (c *MCPClient) Start(ctx context.Context) error {
	c.mutex.Lock()
	if c.disabled {
		reason := c.disabledReason
		c.mutex.Unlock()
		return fmt.Errorf("MCP server %s is disabled: %s", c.config.Name, reason)
	}
	if c.reconnecting {
		c.mutex.Unlock()
		return fmt.Errorf("server %s is reconnecting, cannot start", c.config.Name)
	}
	c.mutex.Unlock()
	return c.startInternal(ctx)
}

// startInternal contains the process creation logic. It acquires the mutex
// internally so callers (both Start() and reconnect()) must NOT hold it.
func (c *MCPClient) startInternal(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.running {
		return fmt.Errorf("server %s is already running", c.config.Name)
	}
	if c.stopping {
		return fmt.Errorf("server %s is stopping, cannot start", c.config.Name)
	}

	// Reset disabled state on clean start
	c.disabled = false
	c.disabledReason = ""
	c.failureTimestamps = nil

	// Cancel any previous context to prevent goroutine leaks on retry
	if c.cancel != nil {
		c.cancel()
	}

	// Create context for the server process
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Set up the command
	c.cmd = exec.CommandContext(c.ctx, c.config.Command, c.config.Args...)

	// Resolve credential placeholders (Env + Credentials) for environment variables
	resolvedEnv, envErr := BuildFullEnvForServer(c.config.Name, &c.config)
	if envErr != nil {
		return fmt.Errorf("failed to resolve env vars for MCP server %s: %w", c.config.Name, envErr)
	}

	// Start with the parent process environment so PATH, SHELL, etc. are available
	c.cmd.Env = os.Environ()

	// Set / override environment variables (resolved secrets + non-secret config env)
	for key, value := range resolvedEnv {
		c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", key, value))
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

	// Initialize state for this connection
	c.running = true
	c.stopping = false
	c.reconnecting = false
	c.connectedAt = time.Now()

	// Increment restart count (tracks all starts, including manual Start() calls)
	c.restartCount++

	// Start message handling goroutines
	go c.handleMessages()
	go c.handleErrors()

	// Start health check if this is a new start (not already running health check)
	if c.healthCheckCancel == nil {
		c.startHealthCheck()
	}

	if c.logger != nil {
		action := "Started"
		if c.reconnectAttempt > 0 {
			action = fmt.Sprintf("Reconnected (attempt %d)", c.reconnectAttempt)
		}
		c.logger.LogProcessStep(fmt.Sprintf("[>>] %s MCP server: %s (restart #%d)", action, c.config.Name, c.restartCount))
	}

	return nil
}

// Stop stops the MCP server process
func (c *MCPClient) Stop(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.running && !c.reconnecting {
		return nil
	}

	// Signal that this is an intentional stop (not a crash).
	// Setting stopping=true prevents reconnect() from completing:
	//   - If reconnect() is in backoff wait, ctx.Done() fires and it returns.
	//   - If reconnect() already set running=false and is about to call
	//     startInternal(), startInternal() will see stopping=true and fail.
	//   - If reconnect() hasn't started cleanup yet, the guards in reconnect()
	//     will see stopping=true and return immediately.
	c.stopping = true
	c.reconnecting = false

	// Clear pending requests to unblock waiting callers
	c.reqMutex.Lock()
	for reqID, ch := range c.pendingReqs {
		close(ch)
		delete(c.pendingReqs, reqID)
	}
	c.reqMutex.Unlock()

	// Stop health check goroutine
	if c.healthCheckCancel != nil {
		c.healthCheckCancel()
		c.healthCheckCancel = nil
		c.healthCheckCtx = nil
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
					c.logger.LogProcessStep(fmt.Sprintf("[WARN] Failed to kill MCP server %s: %v", c.config.Name, err))
				}
			}
			<-done // Wait for the process to actually exit
		}
	}

	c.running = false
	c.initialized = false
	c.reconnectAttempt = 0 // Reset reconnect budget on clean stop
	c.stopping = false     // Reset so the client can be restarted with Start()

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[STOP] Stopped MCP server: %s", c.config.Name))
	}

	return nil
}

// IsRunning checks if the server is running
func (c *MCPClient) IsRunning() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.running
}

// IsDisabled checks if the server has been disabled due to excessive failures
func (c *MCPClient) IsDisabled() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.disabled
}

// isEnabled returns true if the client is not disabled and not stopping
func (c *MCPClient) isEnabled() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return !c.disabled && !c.stopping
}

// GetName returns the server name
func (c *MCPClient) GetName() string {
	return c.config.Name
}

// GetConfig returns the server configuration
func (c *MCPClient) GetConfig() MCPServerConfig {
	return c.config
}

// GetRestartCount returns how many times the underlying server has been
// started (including manual Start calls and reconnects). Safe for concurrent
// reads — startInternal increments under c.mutex.
func (c *MCPClient) GetRestartCount() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.restartCount
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
