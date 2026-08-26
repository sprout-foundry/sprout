//go:build !js

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AgentClient is the CLI-side thin client for the daemon's agent socket
// (SP-136 P4). The daemon owns agent state, conversation history, and tool
// dispatch; the CLI renders responses. Falls back to in-process execution at
// the caller's discretion when the socket is unavailable.
type AgentClient struct {
	socketPath string
	conn       *remoteConn
}

// NewAgentClient dials the daemon agent socket. Returns an error when the
// daemon is not reachable — callers then fall back to in-process execution.
func NewAgentClient(socketPath string) (*AgentClient, error) {
	conn, err := dialRemote(socketPath)
	if err != nil {
		return nil, err
	}
	return &AgentClient{socketPath: socketPath, conn: conn}, nil
}

// Close closes the underlying connection.
func (c *AgentClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.close()
}

// do sends a request and reads the response, re-dialing once on a stale
// connection (daemon restarted).
func (c *AgentClient) do(ctx context.Context, req AgentRequest) (*AgentResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if c.conn == nil {
			conn, err := dialRemote(c.socketPath)
			if err != nil {
				return nil, err
			}
			c.conn = conn
		}

		resp, err := c.exchange(ctx, req)
		if resp != nil {
			// A complete, ID-matched response is a successful protocol
			// exchange even when it carries an error — the error IS the
			// answer. Retrying would re-execute the whole request on the
			// daemon (double LLM cost, repeated tool side effects).
			return resp, err
		}

		// Transport-level failure — drop the connection, retry once with a
		// fresh dial (the daemon may have restarted).
		_ = c.conn.close()
		c.conn = nil
		if attempt == 0 {
			continue
		}
		return nil, err
	}
	return nil, errors.New("agent client: unreachable")
}

// exchange performs one request/response round-trip on the current
// connection. It returns (nil, err) on transport failure (retryable) and
// (resp, nil) or (resp, err) once a complete, ID-matched response arrives
// (terminal — never retried).
func (c *AgentClient) exchange(ctx context.Context, req AgentRequest) (*AgentResponse, error) {
	c.conn.mu.Lock()
	defer c.conn.mu.Unlock()

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal agent request: %w", err)
	}
	deadline := time.Now().Add(DefaultRemoteSocketTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set socket deadline: %w", err)
	}
	if _, err := c.conn.rw.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("write agent request: %w", err)
	}
	if err := c.conn.rw.Flush(); err != nil {
		return nil, fmt.Errorf("flush agent request: %w", err)
	}

	line, err := c.conn.rw.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read agent response: %w", err)
	}
	var resp AgentResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("decode agent response: %w", err)
	}
	if resp.ID != req.ID {
		return nil, fmt.Errorf("agent response ID mismatch: got %q want %q", resp.ID, req.ID)
	}
	if resp.Error != "" {
		return &resp, errors.New(resp.Error)
	}
	return &resp, nil
}

// Query runs a one-shot query on the daemon and returns the final response.
// workDir is the caller's working directory, required so the daemon (a
// single long-lived process that may serve many different projects over its
// lifetime) scopes tool execution to the right one.
func (c *AgentClient) Query(ctx context.Context, prompt, workDir string, opts QueryOptions) (string, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpQuery, Prompt: prompt, WorkDir: workDir, Options: &opts})
	if err != nil {
		return "", err
	}
	return resp.Result, nil
}

// ListSessions returns known sessions.
func (c *AgentClient) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpListSessions})
	if err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

// CreateSession creates a named session.
func (c *AgentClient) CreateSession(ctx context.Context, name string) (*SessionInfo, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpCreateSession, SessionName: name})
	if err != nil {
		return nil, err
	}
	return resp.Session, nil
}

// SwitchSession activates an existing session.
func (c *AgentClient) SwitchSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpSwitchSession, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	return resp.Session, nil
}

// ExecuteTool invokes a tool on the daemon, scoped to the caller's workDir.
func (c *AgentClient) ExecuteTool(ctx context.Context, name string, args map[string]any, workDir string) (*ToolResult, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpExecuteTool, Tool: name, ToolArgs: args, WorkDir: workDir})
	if err != nil {
		return nil, err
	}
	return resp.Tool, nil
}

// StreamQuery is Query with streamed events instead of a single result. The
// call returns after the terminal "done"/"error" event.
func (c *AgentClient) StreamQuery(ctx context.Context, prompt, workDir string, opts QueryOptions, emit func(StreamEvent) error) error {
	if err := c.ensureConn(); err != nil {
		return err
	}

	deadline := time.Now().Add(DefaultRemoteSocketTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set socket deadline: %w", err)
	}

	req := AgentRequest{Op: AgentOpStreamQuery, Prompt: prompt, WorkDir: workDir, Options: &opts}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal stream request: %w", err)
	}

	c.conn.mu.Lock()
	defer c.conn.mu.Unlock()
	if _, err := c.conn.rw.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write stream request: %w", err)
	}
	if err := c.conn.rw.Flush(); err != nil {
		return fmt.Errorf("flush stream request: %w", err)
	}

	for {
		line, err := c.conn.rw.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("read stream event: %w", err)
		}
		var ev StreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("decode stream event: %w", err)
		}
		if ev.Type == "done" {
			return nil
		}
		if ev.Type == "error" {
			return errors.New(ev.Error)
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
}

func (c *AgentClient) ensureConn() error {
	if c.conn != nil {
		return nil
	}
	conn, err := dialRemote(c.socketPath)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}
