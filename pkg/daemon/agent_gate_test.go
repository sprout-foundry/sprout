//go:build !js

package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAgentService is a scriptable AgentService for the gate test. It
// emulates the daemon owning agent state: sessions, one-shot queries,
// streaming, and tool dispatch.
type stubAgentService struct {
	sessions []SessionInfo
	active   string
	queries  int64
	tools    int64
}

func newStubAgentService() *stubAgentService {
	return &stubAgentService{
		sessions: []SessionInfo{{ID: "s-default", Name: "default", Active: true}},
		active:   "s-default",
	}
}

func (s *stubAgentService) ListSessions(context.Context) ([]SessionInfo, error) {
	return s.sessions, nil
}

func (s *stubAgentService) CreateSession(_ context.Context, name string) (*SessionInfo, error) {
	sess := SessionInfo{ID: fmt.Sprintf("s-%d", len(s.sessions)+1), Name: name, Active: false}
	s.sessions = append(s.sessions, sess)
	return &sess, nil
}

func (s *stubAgentService) SwitchSession(_ context.Context, id string) (*SessionInfo, error) {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Active = true
			s.active = id
			return &s.sessions[i], nil
		}
	}
	return nil, fmt.Errorf("session %q not found", id)
}

func (s *stubAgentService) Query(_ context.Context, prompt, _ string) (string, error) {
	atomic.AddInt64(&s.queries, 1)
	return "daemon answer: " + prompt, nil
}

func (s *stubAgentService) StreamQuery(_ context.Context, prompt, _ string, emit func(StreamEvent) error) error {
	for _, chunk := range []string{"a", "b", "c"} {
		if err := emit(StreamEvent{Type: "delta", Content: chunk}); err != nil {
			return err
		}
	}
	atomic.AddInt64(&s.queries, 1)
	return nil
}

func (s *stubAgentService) ExecuteTool(_ context.Context, name string, args map[string]any) (*ToolResult, error) {
	atomic.AddInt64(&s.tools, 1)
	return &ToolResult{Content: fmt.Sprintf("tool %s ran with %d args", name, len(args))}, nil
}

// TestAgentSocketGate is the SP-136 P4 gate test: a full CLI session over
// the daemon agent socket — session management, one-shot query, streaming,
// tool execution — with the daemon owning all state.
func TestAgentSocketGate(t *testing.T) {
	svc := newStubAgentService()
	sockPath := shortSocketPath(t, "agent")

	srv := &AgentServer{SocketPath: sockPath, Service: svc}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	client, err := NewAgentClient(sockPath)
	require.NoError(t, err, "client must connect to the daemon agent socket")
	defer client.Close()

	// --- Session management ---
	sessions, err := client.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1, "default session exists")

	created, err := client.CreateSession(ctx, "review")
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "review", created.Name)
	assert.False(t, created.Active)

	switched, err := client.SwitchSession(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, switched)
	assert.True(t, switched.Active, "switched session becomes active")

	_, err = client.SwitchSession(ctx, "does-not-exist")
	require.Error(t, err, "unknown session must error")

	// --- One-shot query (the daemon owns the agent; CLI is presentation) ---
	result, err := client.Query(ctx, "hello daemon", "/tmp/project")
	require.NoError(t, err)
	assert.Equal(t, "daemon answer: hello daemon", result)

	// --- Streaming ---
	var deltas []string
	err = client.StreamQuery(ctx, "stream me", "/tmp/project", func(ev StreamEvent) error {
		if ev.Type == "delta" {
			deltas = append(deltas, ev.Content)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, deltas, "stream delivered all chunks in order")

	// --- Tool execution ---
	tool, err := client.ExecuteTool(ctx, "run_bash", map[string]any{"command": "echo hi"})
	require.NoError(t, err)
	require.NotNil(t, tool)
	assert.Contains(t, tool.Content, "run_bash")

	// The daemon (stub) served all three query-ish ops and one tool op.
	assert.Equal(t, int64(2), atomic.LoadInt64(&svc.queries), "one-shot + stream each count as a query")
	assert.Equal(t, int64(1), atomic.LoadInt64(&svc.tools), "exactly one tool execution")
}

// TestAgentClient_DialFailure verifies the thin client surfaces a clear
// error when the daemon is unreachable, so the CLI falls back to in-process.
func TestAgentClient_DialFailure(t *testing.T) {
	_, err := NewAgentClient(filepath.Join(t.TempDir(), "missing.sock"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial daemon socket")
}

// TestAgentServer_ServiceErrorPropagation verifies service errors surface to
// the client as response errors, not dropped connections.
func TestAgentServer_ServiceErrorPropagation(t *testing.T) {
	svc := &failingAgentService{}
	sockPath := shortSocketPath(t, "agent")
	srv := &AgentServer{SocketPath: sockPath, Service: svc}
	ctx := context.Background()
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() { srv.Close() })

	client, err := NewAgentClient(sockPath)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Query(ctx, "boom", "/tmp/project")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected failure")
}

type failingAgentService struct{}

func (f *failingAgentService) ListSessions(context.Context) ([]SessionInfo, error) {
	return nil, errors.New("injected failure")
}
func (f *failingAgentService) CreateSession(context.Context, string) (*SessionInfo, error) {
	return nil, errors.New("injected failure")
}
func (f *failingAgentService) SwitchSession(context.Context, string) (*SessionInfo, error) {
	return nil, errors.New("injected failure")
}
func (f *failingAgentService) Query(context.Context, string, string) (string, error) {
	return "", errors.New("injected failure")
}
func (f *failingAgentService) StreamQuery(context.Context, string, string, func(StreamEvent) error) error {
	return errors.New("injected failure")
}
func (f *failingAgentService) ExecuteTool(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, errors.New("injected failure")
}
