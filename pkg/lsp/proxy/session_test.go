package proxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIdleProcess starts a process that stays alive but never writes to stdout,
// standing in for a language server without any canned replies. `cat` would
// echo stdin back and pollute the session channels. Tests drive the server side
// by calling registry.route directly — the same entry point readLoop uses.
func newIdleProcess(t *testing.T) *LSPProcess {
	t.Helper()
	proc, err := StartLSPProcess(context.Background(), "/", "sleep", []string{"60"})
	require.NoError(t, err)
	t.Cleanup(func() { proc.Close() })
	return proc
}

func recv(t *testing.T, ch <-chan string) (string, bool) {
	t.Helper()
	select {
	case msg, ok := <-ch:
		return msg, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session message")
		return "", false
	}
}

func expectNothing(t *testing.T, ch <-chan string) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("expected no message, got %q", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func idOf(t *testing.T, raw string) string {
	t.Helper()
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &obj))
	return string(obj["id"])
}

func TestSessionIDIsolation(t *testing.T) {
	t.Run("two sessions using the same client id get their own replies", func(t *testing.T) {
		proc := newIdleProcess(t)
		a, err := proc.NewSession()
		require.NoError(t, err)
		b, err := proc.NewSession()
		require.NoError(t, err)

		// Both clients naturally start at id 1 — the collision this guards.
		require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":1,"method":"textDocument/hover"}`))
		require.NoError(t, b.Send(`{"jsonrpc":"2.0","id":1,"method":"textDocument/completion"}`))

		// The server answers each upstream id it was given.
		proc.registry.route(`{"jsonrpc":"2.0","id":1,"result":"hover-for-a"}`)
		proc.registry.route(`{"jsonrpc":"2.0","id":2,"result":"completion-for-b"}`)

		msgA, ok := recv(t, a.Out())
		require.True(t, ok)
		assert.Contains(t, msgA, "hover-for-a")
		assert.Equal(t, "1", idOf(t, msgA), "client id must be restored")

		msgB, ok := recv(t, b.Out())
		require.True(t, ok)
		assert.Contains(t, msgB, "completion-for-b")
		assert.Equal(t, "1", idOf(t, msgB))

		// Neither session may see the other's reply.
		expectNothing(t, a.Out())
		expectNothing(t, b.Out())
	})

	t.Run("string client ids round-trip", func(t *testing.T) {
		proc := newIdleProcess(t)
		s, err := proc.NewSession()
		require.NoError(t, err)

		require.NoError(t, s.Send(`{"jsonrpc":"2.0","id":"req-abc","method":"textDocument/hover"}`))
		proc.registry.route(`{"jsonrpc":"2.0","id":1,"result":"ok"}`)

		msg, ok := recv(t, s.Out())
		require.True(t, ok)
		assert.Equal(t, `"req-abc"`, idOf(t, msg))
	})
}

func TestSessionNotificationsBroadcast(t *testing.T) {
	proc := newIdleProcess(t)
	a, err := proc.NewSession()
	require.NoError(t, err)
	b, err := proc.NewSession()
	require.NoError(t, err)

	// Diagnostics have no id and are relevant to every open client.
	proc.registry.route(`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{}}`)

	msgA, ok := recv(t, a.Out())
	require.True(t, ok)
	assert.Contains(t, msgA, "publishDiagnostics")

	msgB, ok := recv(t, b.Out())
	require.True(t, ok)
	assert.Contains(t, msgB, "publishDiagnostics")
}

func TestSessionServerRequestGoesToOneClient(t *testing.T) {
	proc := newIdleProcess(t)
	a, err := proc.NewSession()
	require.NoError(t, err)
	b, err := proc.NewSession()
	require.NoError(t, err)

	// A server-initiated request must be answered exactly once. Broadcasting it
	// would produce two responses to a single request.
	proc.registry.route(`{"jsonrpc":"2.0","id":9001,"method":"workspace/configuration","params":{}}`)

	msgA, ok := recv(t, a.Out())
	require.True(t, ok)
	assert.Contains(t, msgA, "workspace/configuration")
	expectNothing(t, b.Out())
}

func TestSessionHandshakeSharing(t *testing.T) {
	t.Run("second initialize is answered from cache, not forwarded", func(t *testing.T) {
		proc := newIdleProcess(t)
		a, err := proc.NewSession()
		require.NoError(t, err)

		require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		proc.registry.route(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"hoverProvider":true}}}`)

		msgA, ok := recv(t, a.Out())
		require.True(t, ok)
		assert.Contains(t, msgA, "hoverProvider")

		// A second client joins after the handshake completed. gopls rejects a
		// repeat initialize (-32600), so it must be served from cache instead.
		b, err := proc.NewSession()
		require.NoError(t, err)
		require.NoError(t, b.Send(`{"jsonrpc":"2.0","id":77,"method":"initialize","params":{}}`))

		msgB, ok := recv(t, b.Out())
		require.True(t, ok)
		assert.Contains(t, msgB, "hoverProvider")
		assert.Equal(t, "77", idOf(t, msgB), "replayed with the joiner's own id")
	})

	t.Run("initialize arriving mid-handshake waits for the shared reply", func(t *testing.T) {
		proc := newIdleProcess(t)
		a, err := proc.NewSession()
		require.NoError(t, err)
		b, err := proc.NewSession()
		require.NoError(t, err)

		require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		require.NoError(t, b.Send(`{"jsonrpc":"2.0","id":5,"method":"initialize","params":{}}`))

		// Only one initialize went upstream, so only one reply comes back.
		proc.registry.route(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`)

		msgA, ok := recv(t, a.Out())
		require.True(t, ok)
		assert.Equal(t, "1", idOf(t, msgA))

		msgB, ok := recv(t, b.Out())
		require.True(t, ok)
		assert.Equal(t, "5", idOf(t, msgB))
	})

	t.Run("a failed handshake is not cached", func(t *testing.T) {
		proc := newIdleProcess(t)
		a, err := proc.NewSession()
		require.NoError(t, err)

		require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		proc.registry.route(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"nope"}}`)

		msgA, ok := recv(t, a.Out())
		require.True(t, ok)
		assert.Contains(t, msgA, "nope")

		proc.registry.mu.Lock()
		state := proc.registry.handshake
		proc.registry.mu.Unlock()
		assert.Equal(t, handshakeNone, state, "a later client must be able to retry")
	})

	t.Run("only the first initialized notification is forwarded", func(t *testing.T) {
		proc := newIdleProcess(t)
		require.True(t, proc.registry.claimInitialized())
		assert.False(t, proc.registry.claimInitialized())
	})
}

func TestSessionShutdownDoesNotKillSharedServer(t *testing.T) {
	proc := newIdleProcess(t)
	a, err := proc.NewSession()
	require.NoError(t, err)

	require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":3,"method":"shutdown"}`))

	msg, ok := recv(t, a.Out())
	require.True(t, ok)
	assert.Equal(t, "3", idOf(t, msg))
	assert.Contains(t, msg, `"result":null`)

	// `exit` would terminate the server other clients are still using.
	require.NoError(t, a.Send(`{"jsonrpc":"2.0","method":"exit"}`))
	assert.True(t, proc.Healthy(), "shared process must survive one client leaving")
}

func TestSessionCancelRequestRewrite(t *testing.T) {
	proc := newIdleProcess(t)
	a, err := proc.NewSession()
	require.NoError(t, err)

	require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":42,"method":"textDocument/hover"}`))

	upstream, ok := a.lookupUpstream(json.RawMessage("42"))
	require.True(t, ok)

	out, ok := a.rewriteCancel(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":42}}`)
	require.True(t, ok)

	var obj struct {
		Params struct {
			ID json.RawMessage `json:"id"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &obj))
	assert.Equal(t, string(upstream), string(obj.Params.ID),
		"cancel must name the id the server actually knows")
}

func TestSessionCloseDropsPendingWork(t *testing.T) {
	proc := newIdleProcess(t)
	a, err := proc.NewSession()
	require.NoError(t, err)
	b, err := proc.NewSession()
	require.NoError(t, err)

	require.NoError(t, a.Send(`{"jsonrpc":"2.0","id":1,"method":"textDocument/hover"}`))
	a.Close()

	// The reply for a departed client must not be handed to a surviving one.
	proc.registry.route(`{"jsonrpc":"2.0","id":1,"result":"orphaned"}`)
	expectNothing(t, b.Out())

	_, ok := <-a.Out()
	assert.False(t, ok, "closed session channel should be closed")

	assert.NotPanics(t, func() { a.Close() }, "Close must be idempotent")
}

func TestSessionRejectedOnDeadProcess(t *testing.T) {
	proc, err := StartLSPProcess(context.Background(), "/", "cat", []string{})
	require.NoError(t, err)
	proc.Close()

	_, err = proc.NewSession()
	require.Error(t, err, "attaching to an exited server must fail loudly")
}

func TestProcessExitClosesSessions(t *testing.T) {
	proc, err := StartLSPProcess(context.Background(), "/", "cat", []string{})
	require.NoError(t, err)

	s, err := proc.NewSession()
	require.NoError(t, err)

	proc.Close()

	_, ok := recv(t, s.Out())
	assert.False(t, ok, "session channel should close when the process exits")
}
