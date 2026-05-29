package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// MCPClient implements the MCPServer interface for subprocess-based MCP servers
type MCPClient struct {
	config             MCPServerConfig
	cmd                *exec.Cmd
	stdin              io.WriteCloser
	stdout             io.ReadCloser
	stderr             io.ReadCloser
	running            bool
	initialized        bool
	mutex              sync.RWMutex
	logger             *utils.Logger
	messageID          int64
	pendingReqs        map[string]chan MCPMessage
	reqMutex           sync.RWMutex
	restartCount       int
	ctx                context.Context
	cancel             context.CancelFunc
	// Health check and reconnection fields
	healthInterval     time.Duration
	stopping           bool
	reconnecting       bool
	reconnectAttempt   int
	connectedAt        time.Time
	healthCheckCancel  context.CancelFunc
	healthCheckCtx     context.Context

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

// handleMessages handles incoming messages from the server
func (c *MCPClient) handleMessages() {
	// Capture stdout under lock — Stop() / tests may swap it to nil concurrently.
	c.mutex.RLock()
	stdout := c.stdout
	c.mutex.RUnlock()
	if stdout == nil {
		return
	}
	scanner := bufio.NewScanner(stdout)
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
				c.logger.LogProcessStep(fmt.Sprintf("[WARN] Failed to parse MCP message from %s: %v", c.config.Name, err))
			}
			continue
		}

		// Handle responses to our requests
		if message.ID != nil {
			idStr := fmt.Sprintf("%v", message.ID)
			c.reqMutex.RLock()
			if responseChan, exists := c.pendingReqs[idStr]; exists {
				c.reqMutex.RUnlock()
				// Protect against send on closed channel if reconnect/Stop
				// closes the channel between our RUnlock and this send.
				func() {
					defer func() {
						recover() //nolint:errcheck // safe to swallow send-on-closed-channel
					}()
					select {
					case responseChan <- message:
					default:
					}
				}()
			} else {
				c.reqMutex.RUnlock()
			}
		}
		// Handle notifications/events (ID is nil)
		// Could be extended to handle server notifications in the future
	}

	// Scanner ended - check if this was unexpected (process died)
	if err := scanner.Err(); err != nil {
		c.triggerReconnect("stdout scanner ended unexpectedly", err)
	} else {
		// Scanner ended without error (EOF)
		c.triggerReconnect("stdout closed unexpectedly (EOF)", nil)
	}
}

// triggerReconnect checks if a reconnection should be attempted after the
// message handler exits unexpectedly, and spawns the reconnect goroutine.
func (c *MCPClient) triggerReconnect(reason string, err error) {
	c.mutex.RLock()
	stopping := c.stopping
	running := c.running
	c.mutex.RUnlock()

	if stopping || !running {
		return
	}

	if c.logger != nil {
		if err != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[WARN] MCP server %s %s: %v", c.config.Name, reason, err))
		} else {
			c.logger.LogProcessStep(fmt.Sprintf("[WARN] MCP server %s %s", c.config.Name, reason))
		}
	}

	c.mutex.Lock()
	if c.running && !c.stopping {
		clientCtx := c.ctx
		go c.reconnect(clientCtx)
	}
	c.mutex.Unlock()
}

// handleErrors handles stderr output from the server
func (c *MCPClient) handleErrors() {
	// Capture stderr under lock — Stop() / tests may swap it to nil concurrently.
	c.mutex.RLock()
	stderr := c.stderr
	c.mutex.RUnlock()
	if stderr == nil {
		return
	}
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[STDERR] MCP server %s stderr: %s", c.config.Name, line))
		}
	}
}

// ping sends a ping request to check if the server is responsive
func (c *MCPClient) ping(ctx context.Context) error {
	c.mutex.RLock()
	stdin := c.stdin
	c.mutex.RUnlock()

	if stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	_, err := c.sendRequest(ctx, "ping", nil)
	return err
}

// startHealthCheck starts the health check goroutine
func (c *MCPClient) startHealthCheck() {
	// Derive health check context from client's context
	healthCtx, healthCancel := context.WithCancel(c.ctx)
	c.healthCheckCtx = healthCtx
	c.healthCheckCancel = healthCancel

	go func() {
		ticker := time.NewTicker(c.healthInterval)
		defer ticker.Stop()

		for {
			select {
			case <-healthCtx.Done():
				// Health check stopped
				return
			case <-ticker.C:
				c.mutex.RLock()
				running := c.running && !c.stopping
				c.mutex.RUnlock()

				if !running {
					// Server not running or stopping, skip health check
					continue
				}

				// Send ping
				ctx, cancel := context.WithTimeout(healthCtx, 10*time.Second)
				if err := c.ping(ctx); err != nil {
					cancel()
					// Health check failed, trigger reconnection
					c.mutex.Lock()
					if c.running && !c.stopping {
						if c.logger != nil {
							c.logger.LogProcessStep(fmt.Sprintf("[WARN] Health check failed for MCP server %s: %v", c.config.Name, err))
						}
						go c.reconnect(healthCtx)
					}
					c.mutex.Unlock()
				} else {
					cancel()
					// Health check passed, check if we should reset backoff
					c.mutex.Lock()
					if c.reconnectAttempt > 0 && time.Since(c.connectedAt) > 2*time.Minute {
						// Connection has been stable for 2 minutes, reset backoff and failure history
						if c.logger != nil {
							c.logger.LogProcessStep(fmt.Sprintf("[OK] Connection stable for MCP server %s, resetting backoff and failure history", c.config.Name))
						}
						c.reconnectAttempt = 0
						c.failureTimestamps = nil
					}
					c.mutex.Unlock()
				}
			}
		}
	}()
}

// reconnect attempts to reconnect to the MCP server with exponential backoff
func (c *MCPClient) reconnect(ctx context.Context) {
	// Track failures in 60s window for adaptive backoff (set inside lock, used after unlock)
	var failuresIn60s int

	c.mutex.Lock()

	// Record this failure and check sliding windows
	c.failureTimestamps = append(c.failureTimestamps, time.Now())
	now := time.Now()
	var pruned []time.Time
	for _, t := range c.failureTimestamps {
		if now.Sub(t) <= 24*time.Hour {
			pruned = append(pruned, t)
		}
	}
	c.failureTimestamps = pruned

	// Check 24-hour window: if >= 10 failures, disable the server
	failuresIn24h := len(c.failureTimestamps)
	if failuresIn24h >= 10 {
		c.disabled = true
		c.disabledReason = "10 failures in 24 hours"
		c.reconnecting = false
		c.mutex.Unlock()
		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[DISABLED] MCP server %s disabled after %d failures in 24 hours", c.config.Name, failuresIn24h))
		}
		return
	}

	// Count 60-second window failures for adaptive backoff
	failuresIn60s = 0
	for _, t := range c.failureTimestamps {
		if now.Sub(t) <= 60*time.Second {
			failuresIn60s++
		}
	}

	if c.stopping || c.reconnecting || c.reconnectAttempt >= c.getMaxRestarts() {
		c.mutex.Unlock()
		if c.logger != nil {
			if c.stopping {
				c.logger.LogProcessStep(fmt.Sprintf("[INFO] Skipping reconnect for %s (server stopping)", c.config.Name))
			} else if c.reconnecting {
				c.logger.LogProcessStep(fmt.Sprintf("[INFO] Skipping reconnect for %s (reconnect already in progress)", c.config.Name))
			} else {
				c.logger.LogProcessStep(fmt.Sprintf("[ERROR] Max reconnect attempts (%d) reached for MCP server %s", c.getMaxRestarts(), c.config.Name))
			}
		}
		return
	}

	c.reconnecting = true
	c.reconnectAttempt++
	attempt := c.reconnectAttempt
	c.mutex.Unlock()
	defer func() {
		c.mutex.Lock()
		c.reconnecting = false
		c.mutex.Unlock()
	}()

	// Calculate backoff delay
	delay := c.calculateBackoff(attempt)
	// Boost backoff for rapid failure patterns (>3 failures in 60s)
	if failuresIn60s > 3 {
		delay = delay * 2
	}
	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[RECONNECT] Attempting reconnect %d/%d for MCP server %s in %v", attempt, c.getMaxRestarts(), c.config.Name, delay))
	}

	// Wait for backoff delay
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[RECONNECT] Reconnect cancelled for MCP server %s", c.config.Name))
		}
		return
	}

	// Terminate old process and clean up before restarting.
	// Nil out fields under lock to prevent double-close races with concurrent Stop().
	c.mutex.Lock()
	oldCancel := c.cancel
	oldStdin := c.stdin
	oldStdout := c.stdout
	oldStderr := c.stderr
	c.stdin = nil
	c.stdout = nil
	c.stderr = nil
	c.cancel = nil
	// Clear health check state so startInternal() will create a fresh one (MUST_FIX #3)
	c.healthCheckCancel = nil
	c.healthCheckCtx = nil
	c.mutex.Unlock()

	// Cancel the old context to signal old goroutines to stop
	if oldCancel != nil {
		oldCancel()
	}
	// Close old pipes to unblock old goroutines (safe to call nil-check since we nulled above)
	if oldStdin != nil {
		oldStdin.Close()
	}
	if oldStdout != nil {
		oldStdout.Close()
	}
	if oldStderr != nil {
		oldStderr.Close()
	}
	// Brief sleep to let old goroutines detect EOF and exit
	time.Sleep(50 * time.Millisecond)

	// Mark as not running before attempting restart
	// Clear pending requests to prevent stale response delivery
	c.reqMutex.Lock()
	for id, ch := range c.pendingReqs {
		close(ch)
		delete(c.pendingReqs, id)
	}
	c.reqMutex.Unlock()

	c.mutex.Lock()
	c.running = false
	c.initialized = false
	c.mutex.Unlock()

	// Start the server again via startInternal (bypasses reconnecting guard).
	// This will increment restartCount and create a new health check goroutine.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// startInternal() sets connectedAt = time.Now(), which the health check
	// uses for the 2-minute stability reset of failureTimestamps.
	if err := c.startInternal(ctx); err != nil {
		c.mutex.Lock()
		c.running = false
		c.mutex.Unlock()

		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[ERROR] Reconnect attempt %d failed for MCP server %s: %v", attempt, c.config.Name, err))
		}

		// Don't retry here - the health check will trigger another attempt if needed
		return
	}

	// Re-initialize the server after successful connection
	if err := c.Initialize(ctx); err != nil {
		c.mutex.Lock()
		c.running = false
		c.mutex.Unlock()

		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[ERROR] Failed to initialize after reconnect for MCP server %s: %v", c.config.Name, err))
		}
		return
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[OK] Successfully reconnected and initialized MCP server %s (attempt %d)", c.config.Name, attempt))
	}

	// Reset reconnect budget after successful reconnect+initialize so that
	// subsequent crashes get a fresh retry budget instead of accumulating
	// toward the max-restarts cap.
	c.mutex.Lock()
	c.reconnectAttempt = 0
	c.mutex.Unlock()
}

// calculateBackoff calculates exponential backoff delay
func (c *MCPClient) calculateBackoff(attempt int) time.Duration {
	// Start with 1 second, double each attempt up to max 5 minutes
	backoff := time.Duration(1<<uint(attempt-1)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	return backoff
}

// getMaxRestarts returns the maximum number of restart attempts
func (c *MCPClient) getMaxRestarts() int {
	if c.config.MaxRestarts > 0 {
		return c.config.MaxRestarts
	}
	return 3 // default, matching pkg/mcp/config.go
}
