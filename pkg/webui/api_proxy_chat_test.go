//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

const testProxyChatClientID = "test-proxy-client"

// setupProxyChatTestServer creates a minimal ReactWebServer for proxy chat tests
func setupProxyChatTestServer(t *testing.T) *ReactWebServer {
	t.Helper()

	daemonRoot := t.TempDir()
	workspaceRoot := filepath.Join(daemonRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = workspaceRoot
	ws.terminalManager = NewTerminalManager(daemonRoot)
	ws.fileConsents = newFileConsentManager()

	// Pre-register client context
	clientCtx := ws.getOrCreateClientContextLocked(testProxyChatClientID)
	clientCtx.WorkspaceRoot = workspaceRoot
	clientCtx.Terminal = NewTerminalManager(workspaceRoot)
	clientCtx.FileConsents = newFileConsentManager()

	return ws
}

// TestProxyChat_NoMessages tests request with empty messages array
func TestProxyChat_NoMessages(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	body, _ := json.Marshal(map[string]interface{}{
		"messages": []map[string]string{},
		"stream":   false,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_NoUserMessage tests request with only system messages
func TestProxyChat_NoUserMessage(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	body, _ := json.Marshal(map[string]interface{}{
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful assistant"},
		},
		"stream": false,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_InvalidJSON tests request with invalid JSON
func TestProxyChat_InvalidJSON(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat", bytes.NewReader([]byte("{invalid")))
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_InvalidMethod tests that only POST is allowed
func TestProxyChat_InvalidMethod(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/chat", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChat(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_StopAlreadyCompleted tests stop when no query is active
func TestProxyChat_StopAlreadyCompleted(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id": defaultChatID,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat/stop", bytes.NewReader(body))
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStop(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if completed, ok := resp["already_completed"].(bool); !ok || !completed {
		t.Errorf("expected already_completed=true, got %v", resp["already_completed"])
	}
}

// TestProxyChat_Status tests the status endpoint
func TestProxyChat_Status(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/chat/status?chat_id=default", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should be inactive since we haven't started a query
	if active, ok := resp["active"].(bool); ok && active {
		t.Errorf("expected active=false, got %v", resp["active"])
	}

	if _, ok := resp["chat_id"].(string); !ok {
		t.Errorf("expected chat_id in response")
	}
}

// TestProxyChat_StatusWithActiveQuery tests status with an active query
func TestProxyChat_StatusWithActiveQuery(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	// Create a chat session with an active query
	clientCtx := ws.getOrCreateClientContextLocked(testProxyChatClientID)
	cs := clientCtx.getOrCreateChatSession(defaultChatID)
	cs.setQueryActive(true, "test query")

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/chat/status?chat_id=default", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if active, ok := resp["active"].(bool); !ok || !active {
		t.Errorf("expected active=true, got %v", resp["active"])
	}

	// Clean up
	cs.setQueryActive(false, "")
}

// TestProxyChat_StatusInvalidMethod tests that only GET is allowed for status
func TestProxyChat_StatusInvalidMethod(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat/status", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStatus(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_StopInvalidMethod tests that only POST is allowed for stop
func TestProxyChat_StopInvalidMethod(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/chat/stop", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStop(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_StopInvalidJSON tests stop with invalid JSON
func TestProxyChat_StopInvalidJSON(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat/stop", bytes.NewReader([]byte("{invalid")))
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStop(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_StopWithEmptyBody tests stop with empty body (should use query param)
func TestProxyChat_StopWithEmptyBody(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/chat/stop?chat_id=default", bytes.NewReader([]byte("{}")))
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyChatStop(rec, req)

	// Should return 200 since no query is active
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProxyChat_LastUserMessageExtraction tests the getLastUserMessage helper function
func TestProxyChat_LastUserMessageExtraction(t *testing.T) {
	tests := []struct {
		name     string
		messages []proxyChatMessage
		expected string
	}{
		{
			name: "single user message",
			messages: []proxyChatMessage{
				{Role: "user", Content: "hello"},
			},
			expected: "hello",
		},
		{
			name: "multiple messages with user at end",
			messages: []proxyChatMessage{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "response"},
				{Role: "user", Content: "last"},
			},
			expected: "last",
		},
		{
			name: "multiple user messages",
			messages: []proxyChatMessage{
				{Role: "user", Content: "first"},
				{Role: "user", Content: "last"},
			},
			expected: "last",
		},
		{
			name: "no user messages",
			messages: []proxyChatMessage{
				{Role: "system", Content: "You are helpful"},
				{Role: "assistant", Content: "Hello"},
			},
			expected: "",
		},
		{
			name:     "empty array",
			messages: []proxyChatMessage{},
			expected: "",
		},
		{
			name: "user message with whitespace",
			messages: []proxyChatMessage{
				{Role: "user", Content: "  hello world  "},
			},
			expected: "hello world",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getLastUserMessage(tc.messages)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
