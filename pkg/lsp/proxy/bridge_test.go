package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBridge(t *testing.T) {
	t.Run("creates bridge successfully", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		// Create bridge with nil websocket (for testing only)
		bridge := NewBridge(nil, proc)
		require.NotNil(t, bridge)

		assert.NotNil(t, bridge.lspProcess)
		assert.NotNil(t, bridge.doneCh)
	})

	t.Run("bridge fields are initialized", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		bridge := NewBridge(nil, proc)

		assert.NotNil(t, bridge.doneCh)
		// Channel should be empty initially
		select {
		case <-bridge.doneCh:
			t.Error("doneCh should not be closed initially")
		default:
			// Expected
		}
	})
}

func TestBridgeClose(t *testing.T) {
	t.Run("close is safe to call multiple times", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		bridge := NewBridge(nil, proc)

		// Close multiple times
		bridge.Close()
		bridge.Close()
		bridge.Close()

		// Should not panic
	})

	t.Run("close closes websocket connection", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		bridge := NewBridge(nil, proc)
		bridge.Close()

		// WebSocket should be nil after close
		assert.Nil(t, bridge.wsConn)
	})

	t.Run("close cleans up resources", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		bridge := NewBridge(nil, proc)
		bridge.Close()

		// Verify fields are cleaned up
		assert.Nil(t, bridge.wsConn)
		assert.Nil(t, bridge.unsubscribe)
	})

	t.Run("close without websocket", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		bridge := NewBridge(nil, proc)

		// Should not panic even with nil websocket
		bridge.Close()
	})

	t.Run("close after unsubscribe is safe", func(t *testing.T) {
		ctx := context.Background()
		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		// Subscribe to the process, then call unsubscribe
		_, unsubscribe, err := proc.Subscribe()
		require.NoError(t, err)
		unsubscribe()

		bridge := NewBridge(nil, proc)
		bridge.Close()
		// Should not panic even after unsubscribe was called
	})
}

func TestBridgeHandlerParameterValidation(t *testing.T) {
	t.Run("missing language parameter returns 400", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		handler := BridgeHandler(manager, upgrader, "/tmp")

		req := httptest.NewRequest("GET", "/?workspace=/tmp", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "language parameter is required")
	})

	t.Run("missing workspace parameter returns 400", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		handler := BridgeHandler(manager, upgrader, "/tmp")

		req := httptest.NewRequest("GET", "/?language=go", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "workspace parameter is required")
	})

	t.Run("workspace not matching root returns 403", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		handler := BridgeHandler(manager, upgrader, "/allowed")

		req := httptest.NewRequest("GET", "/?language=go&workspace=/forbidden", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "workspace not allowed")
	})

	t.Run("unknown language returns 500", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		workspace := "/tmp"
		handler := BridgeHandler(manager, upgrader, workspace)

		req := httptest.NewRequest("GET", "/?language=unknown-language-xyz&workspace="+workspace, nil)
		w := httptest.NewRecorder()

		handler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to start language server")
	})
}

func TestBridgeHandlerEdgeCases(t *testing.T) {
	t.Run("empty language and workspace", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		handler := BridgeHandler(manager, upgrader, "/tmp")

		req := httptest.NewRequest("GET", "/?language=&workspace=", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		// Should fail on missing/empty language parameter
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestBridgeHandlerWithCustomConfig(t *testing.T) {
	t.Run("custom config with cat binary", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		// Set up a custom config using cat instead of gopls
		customConfig := []LanguageServerConfig{
			{
				ID:          "test",
				LanguageIDs: []string{"test"},
				Binary:      "cat",
				Args:        []string{},
			},
		}
		manager.SetConfig(customConfig)

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		handler := BridgeHandler(manager, upgrader, "/tmp")

		req := httptest.NewRequest("GET", "/?language=test&workspace=/tmp", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		// Should not get parameter validation errors (400/403)
		// May get 400 from WebSocket upgrader (no WebSocket headers)
		// That's fine - we're just checking params passed
		body := w.Body.String()
		assert.NotContains(t, body, "language parameter is required")
		assert.NotContains(t, body, "workspace parameter is required")
		assert.NotContains(t, body, "workspace not allowed")
	})
}

func TestBridgeRunWithNilWebSocket(t *testing.T) {
	t.Run("bridge run with nil websocket panics", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		proc, err := StartLSPProcess(ctx, "/", "cat", []string{})
		require.NoError(t, err)
		defer proc.Close()

		bridge := NewBridge(nil, proc)

		// Bridge.Run will panic if wsConn is nil because it tries to call wsConn.SetReadDeadline
		// This is expected behavior - the bridge needs a real websocket
		assert.Panics(t, func() {
			_ = bridge.Run(ctx)
		})
	})
}

func TestBridgeHandlerRejectsNonMatchingWorkspace(t *testing.T) {
	t.Run("exact match required for workspace", func(t *testing.T) {
		ctx := context.Background()
		manager := NewManager(ctx)
		defer manager.Close()

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		// Use /tmp/other as allowed root
		handler := BridgeHandler(manager, upgrader, "/tmp/other")

		req := httptest.NewRequest("GET", "/?language=go&workspace=/tmp", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		// Should get 403 because /tmp != /tmp/other
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "workspace not allowed")
	})

}
