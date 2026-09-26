package mcp

// client.go — the MCP client lifecycle: the MCPClient type, NewMCPClient,
// the start / stop paths (Start, startInternal, Stop), the running /
// disabled status accessors (IsRunning, IsDisabled, isEnabled, GetName,
// GetConfig, GetRestartCount). The RPC methods + the sendRequest transport
// live in client_rpc.go.

import (
	"context"
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
