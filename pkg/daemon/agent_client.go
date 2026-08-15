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

		c.conn.mu.Lock()
		payload, err := json.Marshal(req)
		if err == nil {
			deadline := time.Now().Add(DefaultRemoteSocketTimeout)
			if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
				deadline = d
			}
			err = c.conn.conn.SetDeadline(deadline)
			if err == nil {
				_, err = c.conn.rw.Write(append(payload, '\n'))
			}
			if err == nil {
				err = c.conn.rw.Flush()
			}
			if err == nil {
				var line []byte
				line, err = c.conn.rw.ReadBytes('\n')
				if err == nil {
					var resp AgentResponse
					err = json.Unmarshal(line, &resp)
					if err == nil {
						if resp.ID != req.ID {
							err = fmt.Errorf("agent response ID mismatch: got %q want %q", resp.ID, req.ID)
						} else if resp.Error != "" {
							err = errors.New(resp.Error)
						} else {
							c.conn.mu.Unlock()
							return &resp, nil
						}
					}
				}
			}
		}
		c.conn.mu.Unlock()

		// Connection failed — drop it, retry once with a fresh dial.
		_ = c.conn.close()
		c.conn = nil
		if attempt == 0 {
			continue
		}
		return nil, err
	}
	return nil, errors.New("agent client: unreachable")
}

// Query runs a one-shot query on the daemon and returns the final response.
// workDir is the caller's working directory, required so the daemon (a
// single long-lived process that may serve many different projects over its
// lifetime) scopes tool execution to the right one.
func (c *AgentClient) Query(ctx context.Context, prompt, workDir string) (string, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpQuery, Prompt: prompt, WorkDir: workDir})
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

// ExecuteTool invokes a tool on the daemon.
func (c *AgentClient) ExecuteTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	resp, err := c.do(ctx, AgentRequest{Op: AgentOpExecuteTool, Tool: name, ToolArgs: args})
	if err != nil {
		return nil, err
	}
	return resp.Tool, nil
}

// StreamQuery is Query with streamed events instead of a single result. The
// call returns after the terminal "done"/"error" event.
func (c *AgentClient) StreamQuery(ctx context.Context, prompt, workDir string, emit func(StreamEvent) error) error {
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

	req := AgentRequest{Op: AgentOpStreamQuery, Prompt: prompt, WorkDir: workDir}
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
