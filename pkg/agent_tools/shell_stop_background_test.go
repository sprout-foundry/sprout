//go:build !js

// Tests for handleStopBackground in the shell command handler.
// Covers both TerminalManager (WebUI) and BackgroundProcessManager (CLI) paths.

package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TerminalManager path — WebUI mode
// ---------------------------------------------------------------------------

func TestHandleStopBackground_TerminalManagerSuccess(t *testing.T) {
	t.Parallel()

	tm := &mockTerminalManager{
		stopBackgroundFunc: func(sessionID string) error {
			if sessionID != "bg-npm-dev-aabbccdd" {
				t.Errorf("expected sessionID 'bg-npm-dev-aabbccdd', got %q", sessionID)
			}
			return nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "bg-npm-dev-aabbccdd")

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Background session bg-npm-dev-aabbccdd stopped.")
}

func TestHandleStopBackground_TerminalManagerError(t *testing.T) {
	t.Parallel()

	tm := &mockTerminalManager{
		stopBackgroundFunc: func(sessionID string) error {
			return fmt.Errorf("session %s not found", sessionID)
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "bg-missing")

	require.Error(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "stop background")
	assert.Contains(t, result.Output, "not found")
}

// ---------------------------------------------------------------------------
// BackgroundProcessManager path — CLI mode (TerminalManager absent)
// ---------------------------------------------------------------------------

func TestHandleStopBackground_BPMFallback(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start a long-running process so we have something to stop.
	sessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)
	assert.True(t, bpm.IsActive(sessionID))

	// Put BPM in context (no TerminalManager).
	ctx := WithBackgroundProcessManager(context.Background(), bpm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, sessionID)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, fmt.Sprintf("Background session %s stopped.", sessionID))
	assert.False(t, bpm.IsActive(sessionID))
}

func TestHandleStopBackground_BPMError(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Put BPM in context (no TerminalManager).
	ctx := WithBackgroundProcessManager(context.Background(), bpm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "nonexistent-session")

	require.Error(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "stop background")
	assert.Contains(t, result.Output, "not found")
}

// ---------------------------------------------------------------------------
// Neither manager available
// ---------------------------------------------------------------------------

func TestHandleStopBackground_NoManager(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "bg-session-123")

	require.Error(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "stop_background requires WebUI terminal manager or CLI background process manager")
}

// ---------------------------------------------------------------------------
// TerminalManager takes priority over BackgroundProcessManager
// ---------------------------------------------------------------------------

func TestHandleStopBackground_TerminalManagerTakesPriority(t *testing.T) {
	t.Parallel()

	// Create a real BPM with a running process — it should NOT be used.
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()
	bpmSessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	// Create a mock TerminalManager that succeeds.
	tmCalled := false
	tm := &mockTerminalManager{
		stopBackgroundFunc: func(sessionID string) error {
			tmCalled = true
			return nil
		},
	}

	// Put BOTH in context — TerminalManager should take priority.
	ctx := WithBackgroundProcessManager(context.Background(), bpm)
	ctx = WithTerminalManager(ctx, tm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "bg-npm-dev-aabbccdd")

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, "Background session bg-npm-dev-aabbccdd stopped.")

	// TerminalManager was called.
	assert.True(t, tmCalled, "TerminalManager should have been used, not BPM")

	// BPM process should still be running (was not touched).
	assert.True(t, bpm.IsActive(bpmSessionID))
}

// ---------------------------------------------------------------------------
// TerminalManager error does NOT fall back to BPM
// ---------------------------------------------------------------------------

func TestHandleStopBackground_TerminalManagerErrorNoFallback(t *testing.T) {
	t.Parallel()

	// Create a real BPM with a running process.
	bpm := NewBackgroundProcessManager()
	defer bpm.Close()
	bpmSessionID, err := bpm.Start(context.Background(), "sleep 60", "")
	require.NoError(t, err)

	// TerminalManager returns an error — the handler should NOT fall through to BPM.
	tm := &mockTerminalManager{
		stopBackgroundFunc: func(sessionID string) error {
			return fmt.Errorf("session not found")
		},
	}

	ctx := WithBackgroundProcessManager(context.Background(), bpm)
	ctx = WithTerminalManager(ctx, tm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "bg-missing")

	require.Error(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "not found")

	// BPM process should still be running — no fallback occurred.
	assert.True(t, bpm.IsActive(bpmSessionID))
}

// ---------------------------------------------------------------------------
// OutputWriter integration
// ---------------------------------------------------------------------------

func TestHandleStopBackground_OutputWriter(t *testing.T) {
	t.Parallel()

	tm := &mockTerminalManager{
		stopBackgroundFunc: func(sessionID string) error { return nil },
	}
	ctx := WithTerminalManager(context.Background(), tm)

	var buf strings.Builder
	env := ToolEnv{OutputWriter: &buf}

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, env, "bg-session-1")

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, buf.String(), result.Output)
}

// ---------------------------------------------------------------------------
// TokenUsage is reported
// ---------------------------------------------------------------------------

func TestHandleStopBackground_TokenUsage(t *testing.T) {
	t.Parallel()

	tm := &mockTerminalManager{
		stopBackgroundFunc: func(sessionID string) error { return nil },
	}
	ctx := WithTerminalManager(context.Background(), tm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, "bg-session-1")

	require.NoError(t, err)
	assert.Greater(t, result.TokenUsage, int64(0), "should report token usage for non-empty output")
}

// ---------------------------------------------------------------------------
// BPM with already-exited process (Stop is a no-op)
// ---------------------------------------------------------------------------

func TestHandleStopBackground_BPMStoppedProcess(t *testing.T) {
	t.Parallel()

	bpm := NewBackgroundProcessManager()
	defer bpm.Close()

	// Start a fast command that exits immediately.
	sessionID, err := bpm.Start(context.Background(), "echo done", "")
	require.NoError(t, err)

	// Wait for it to exit.
	require.Eventually(t, func() bool {
		return !bpm.IsActive(sessionID)
	}, 3*time.Second, 100*time.Millisecond)

	ctx := WithBackgroundProcessManager(context.Background(), bpm)

	h := &shellCommandHandler{}
	result, err := h.handleStopBackground(ctx, ToolEnv{}, sessionID)

	// Stop on an already-exited process is a no-op and returns success.
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Output, fmt.Sprintf("Background session %s stopped.", sessionID))
}
