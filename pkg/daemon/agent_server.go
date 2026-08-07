//go:build !js

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Agent socket protocol (SP-136 P4): full CLI-on-daemon.
//
// The daemon owns agent state, conversation history, tool dispatch, and the
// embedding index. The CLI is a presentation layer: it connects to the
// daemon's agent socket, sends a query, and renders the response/stream.
//
// Wire format: one JSON object per line, same ID-echo convention as the
// embedding protocol. Ops:
//
//	{"id":"1","op":"list_sessions"}                        → ListSessionsResponse
//	{"id":"2","op":"create_session","session_name":"x"}    → SessionInfo
//	{"id":"3","op":"switch_session","session_id":"s1"}     → SessionInfo
//	{"id":"4","op":"query","prompt":"..."}                 → QueryResponse (one-shot)
//	{"id":"5","op":"stream_query","prompt":"..."}          → stream of StreamEvents (newline JSON)
//	{"id":"6","op":"execute_tool","tool":"name","tool_args":{...}} → ToolResponse
//
// One-shot and stream are mutually exclusive per connection op; stream_query
// holds the connection until the run completes, emitting one event per line.

// AgentOp identifies a protocol operation.
type AgentOp string

const (
	AgentOpListSessions  AgentOp = "list_sessions"
	AgentOpCreateSession AgentOp = "create_session"
	AgentOpSwitchSession AgentOp = "switch_session"
	AgentOpQuery         AgentOp = "query"
	AgentOpStreamQuery   AgentOp = "stream_query"
	AgentOpExecuteTool   AgentOp = "execute_tool"
)

// SessionInfo describes an agent session.
type SessionInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	Active    bool   `json:"active,omitempty"`
}

// StreamEvent is one chunk of a streaming query run.
type StreamEvent struct {
	Type    string `json:"type"` // "delta" | "tool" | "done" | "error"
	Content string `json:"content,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ToolResult is the outcome of an ExecuteTool call.
type ToolResult struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// AgentRequest is a single protocol request.
type AgentRequest struct {
	ID          string         `json:"id"`
	Op          AgentOp        `json:"op"`
	Prompt      string         `json:"prompt,omitempty"`
	SessionName string         `json:"session_name,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	ToolArgs    map[string]any `json:"tool_args,omitempty"`
}

// AgentResponse is a single protocol response (non-streaming ops).
type AgentResponse struct {
	ID       string           `json:"id"`
	Error    string           `json:"error,omitempty"`
	Sessions []SessionInfo    `json:"sessions,omitempty"`
	Session  *SessionInfo     `json:"session,omitempty"`
	Result   string           `json:"result,omitempty"`
	Tool     *ToolResult      `json:"tool,omitempty"`
}

// AgentService is the daemon-side capability for CLI-on-daemon (SP-136 P4).
type AgentService interface {
	// ListSessions returns known sessions.
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	// CreateSession creates a named session.
	CreateSession(ctx context.Context, name string) (*SessionInfo, error)
	// SwitchSession activates an existing session.
	SwitchSession(ctx context.Context, sessionID string) (*SessionInfo, error)
	// Query runs a one-shot query and returns the final response.
	Query(ctx context.Context, prompt string) (string, error)
	// StreamQuery runs a query and emits stream events.
	StreamQuery(ctx context.Context, prompt string, emit func(StreamEvent) error) error
	// ExecuteTool invokes a tool by name with args.
	ExecuteTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)
}

// AgentServer serves the SP-136 P4 agent socket protocol.
type AgentServer struct {
	// SocketPath is the Unix socket path to listen on.
	SocketPath string
	// Service backs all operations.
	Service AgentService
	// Logger receives request/error logs.
	Logger *slog.Logger

	ln   net.Listener
	mu   sync.Mutex
	conns map[net.Conn]struct{}
	done chan struct{}
	once sync.Once
}

// Start begins listening. Returns once bound.
func (s *AgentServer) Start(ctx context.Context) error {
	if s.SocketPath == "" {
		return errors.New("agent server: empty socket path")
	}
	if s.Service == nil {
		return errors.New("agent server: nil service")
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}

	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o700); err != nil {
		return fmt.Errorf("agent server: create socket dir: %w", err)
	}
	_ = os.Remove(s.SocketPath)

	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("agent server: listen %s: %w", s.SocketPath, err)
	}
	if err := os.Chmod(s.SocketPath, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("agent server: chmod socket: %w", err)
	}

	s.ln = ln
	s.conns = make(map[net.Conn]struct{})
	s.done = make(chan struct{})

	go s.acceptLoop(ctx)
	s.Logger.Info("agent socket server started", slog.String("socket", s.SocketPath))
	return nil
}

func (s *AgentServer) acceptLoop(ctx context.Context) {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if isClosedErr(err) {
				return
			}
			s.Logger.Warn("agent server accept error", slog.Any("err", err))
			continue
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handleConn(ctx, conn)
	}
}

func (s *AgentServer) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	for {
		line, err := rw.ReadBytes('\n')
		if err != nil {
			return
		}
		var req AgentRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeAgentError(rw, "", fmt.Sprintf("malformed request: %v", err))
			continue
		}

		// Stream query is handled specially: it emits events until done,
		// then returns; the connection stays open for the next request.
		if req.Op == AgentOpStreamQuery {
			s.serveStreamQuery(ctx, rw, req)
			continue
		}

		resp := s.dispatch(ctx, req)
		payload, err := json.Marshal(resp)
		if err != nil {
			s.writeAgentError(rw, req.ID, fmt.Sprintf("marshal response: %v", err))
			continue
		}
		if _, err := rw.Write(append(payload, '\n')); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
	}
}

func (s *AgentServer) serveStreamQuery(ctx context.Context, rw *bufio.ReadWriter, req AgentRequest) {
	emit := func(ev StreamEvent) error {
		payload, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := rw.Write(append(payload, '\n')); err != nil {
			return err
		}
		return rw.Flush()
	}

	runErr := s.Service.StreamQuery(ctx, req.Prompt, emit)
	done := StreamEvent{Type: "done"}
	if runErr != nil {
		done = StreamEvent{Type: "error", Error: runErr.Error()}
	}
	_ = emit(done)
}

func (s *AgentServer) dispatch(ctx context.Context, req AgentRequest) AgentResponse {
	resp := AgentResponse{ID: req.ID}

	switch req.Op {
	case AgentOpListSessions:
		sessions, err := s.Service.ListSessions(ctx)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Sessions = sessions

	case AgentOpCreateSession:
		sess, err := s.Service.CreateSession(ctx, req.SessionName)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Session = sess

	case AgentOpSwitchSession:
		sess, err := s.Service.SwitchSession(ctx, req.SessionID)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Session = sess

	case AgentOpQuery:
		result, err := s.Service.Query(ctx, req.Prompt)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Result = result

	case AgentOpExecuteTool:
		tool, err := s.Service.ExecuteTool(ctx, req.Tool, req.ToolArgs)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.Tool = tool

	default:
		resp.Error = fmt.Sprintf("unknown agent op %q", req.Op)
	}
	return resp
}

func (s *AgentServer) writeAgentError(rw *bufio.ReadWriter, id, errMsg string) {
	payload, _ := json.Marshal(AgentResponse{ID: id, Error: errMsg})
	_, _ = rw.Write(append(payload, '\n'))
	_ = rw.Flush()
}

// Wait blocks until the server stops accepting.
func (s *AgentServer) Wait() {
	if s.done != nil {
		<-s.done
	}
}

// Close stops the server and drops connections.
func (s *AgentServer) Close() error {
	var firstErr error
	s.once.Do(func() {
		if s.ln != nil {
			firstErr = s.ln.Close()
			_ = os.Remove(s.SocketPath)
		}
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.conns = nil
		s.mu.Unlock()
	})
	return firstErr
}
