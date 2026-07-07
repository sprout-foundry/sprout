//go:build !js

package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func workingDirScopeHash(workingDir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(workingDir))))
	return hex.EncodeToString(sum[:8])
}

func writeTestSession(stateDir, sessionID, workingDir string, cs agent.ConversationState) error {
	scopeHash := workingDirScopeHash(workingDir)
	scopeDir := filepath.Join(stateDir, "scoped", scopeHash)
	if err := os.MkdirAll(scopeDir, 0o700); err != nil {
		return fmt.Errorf("create scope dir: %w", err)
	}
	path := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	data, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func makeTestSession(sessionID string) agent.ConversationState {
	return agent.ConversationState{
		SessionID:        sessionID,
		Name:             "Test Export Session",
		WorkingDirectory: "/tmp/export-test",
		TotalCost:        0.42,
		PromptTokens:     5000,
		CompletionTokens: 7000,
		LastUpdated:      time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		Messages: []api.Message{
			{Role: "user", Content: "Hello, this is a test message with my API key " + fakeAPIKey},
			{Role: "assistant", Content: "I can help you with that!"},
		},
	}
}

func makeTestSessionWithToolCalls(sessionID string) agent.ConversationState {
	return agent.ConversationState{
		SessionID:        sessionID,
		Name:             "Test Export with Tools",
		WorkingDirectory: "/tmp/export-test",
		TotalCost:        0.15,
		PromptTokens:     2000,
		CompletionTokens: 3000,
		LastUpdated:      time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		Messages: []api.Message{
			{Role: "user", Content: "Run this command for me"},
			{
				Role:      "assistant",
				Content:   "Sure, let me run it.",
				ToolCalls: []api.ToolCall{{Function: api.ToolCallFunction{Name: "shell_command", Arguments: `{"command":"ls"}`}}},
			},
			{Role: "tool", Content: "file1\nfile2"},
			{Role: "assistant", Content: "Done!"},
		},
	}
}

// fakeAPIKey matches gitleaks' openai-api-key rule shape
// (sk- + 20 alphanumeric + T3BlbkFJ + 20 alphanumeric). Assembled at init
// from split substrings so the literal source on disk is not itself a
// contiguous secret-shaped string that secret scanners would flag, while the
// runtime value still has enough Shannon entropy (≈4.99) for gitleaks to
// detect it during redaction tests. NOT a live credential.
func init() {
	fakeAPIKey = "sk-" + "Zm9vYmFyMTIzNDU2Nzg5" + "T3BlbkFJ" + "QWxwaGFiZXRhMTIzNDU2"
}

var fakeAPIKey string

func setupExportTest(t *testing.T) (*ReactWebServer, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)

	// webui's TestMain installs a package-wide state-dir hook via
	// agent.SetTestStateDirHook, so t.Setenv("HOME", root) alone
	// won't redirect GetStateDir to our per-test temp root. We use
	// SetGetStateDirForTest to install our own override, then capture
	// the PREVIOUS hook and explicitly re-install it on cleanup.
	//
	// Note: SetGetStateDirFunc returns the previous function rather
	// than a restore closure, so calling `restore()` directly would
	// simply invoke defaultGetStateDir() and discard the result. We
	// re-install the captured previous function via a second
	// SetGetStateDirFunc call to actually undo the override.
	sessionsDir := filepath.Join(root, ".sprout", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	previousGetStateDir := agent.SetGetStateDirForTest(sessionsDir)
	t.Cleanup(func() { agent.SetGetStateDirFunc(previousGetStateDir) })

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	return ws, root
}

// ---------------------------------------------------------------------------
// Test 1: TestHandleAPISessionExport_Markdown
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_Markdown(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-md")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-md", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-md/export?format=markdown&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/markdown; charset=utf-8" {
		t.Errorf("expected Content-Type 'text/markdown; charset=utf-8', got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "test-md") {
		t.Errorf("expected body to contain session id, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestHandleAPISessionExport_HTML
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_HTML(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-html")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-html", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-html/export?format=html&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type 'text/html; charset=utf-8', got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<html") {
		t.Errorf("expected body to contain '<html>', got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestHandleAPISessionExport_JSON
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_JSON(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-json")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-json", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-json/export?format=json&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type 'application/json; charset=utf-8', got %q", ct)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if _, ok := resp["session_id"]; !ok {
		t.Error("expected 'session_id' key in JSON response")
	}
	if _, ok := resp["messages"]; !ok {
		t.Error("expected 'messages' key in JSON response")
	}
}

// ---------------------------------------------------------------------------
// Test 4: TestHandleAPISessionExport_InvalidFormat
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_InvalidFormat(t *testing.T) {
	ws, _ := setupExportTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test/export?format=docx", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errStr, ok := resp["error"].(string); !ok || !strings.Contains(errStr, "invalid format") {
		t.Errorf("expected error about invalid format, got %v", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// Test 5: TestHandleAPISessionExport_NotFound
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_NotFound(t *testing.T) {
	ws, root := setupExportTest(t)
	// Intentionally don't create any session
	_ = root

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent/export", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test 6: TestHandleAPISessionExport_NoCost_Default
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_NoCost_Default(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-cost-default")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-cost-default", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	// Default: include_cost=true (or omit the param entirely)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-cost-default/export?format=markdown&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "$") {
		// Cost footers contain $ — check for cost-related content
		if !strings.Contains(body, "Cost") && !strings.Contains(body, "cost") && !strings.Contains(body, "0.42") {
			t.Errorf("expected cost info in output by default, got: %s", body)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 7: TestHandleAPISessionExport_NoCostFlag
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_NoCostFlag(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-nocost")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-nocost", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-nocost/export?format=markdown&include_cost=false&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// With include_cost=false, per-turn footers should be omitted.
	if strings.Contains(body, "Cost:") {
		t.Errorf("expected no 'Cost:' footer when include_cost=false, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Test 8: TestHandleAPISessionExport_IncludeToolCalls
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_IncludeToolCalls(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSessionWithToolCalls("test-tools")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-tools", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-tools/export?format=markdown&include_tool_calls=true&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "shell_command") {
		t.Errorf("expected tool name 'shell_command' in output, got: %s", body)
	}
	if !strings.Contains(body, "<details>") {
		t.Errorf("expected <details> block for tool call, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Test 9: TestHandleAPISessionExport_Disposition
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_Disposition(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-disposition")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-disposition", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	// Test markdown
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-disposition/export?format=markdown&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	cd := rec.Header().Get("Content-Disposition")
	if cd == "" {
		t.Error("expected Content-Disposition header to be set")
	}
	if !strings.Contains(cd, "attachment") {
		t.Errorf("expected attachment in Content-Disposition, got %q", cd)
	}
	if !strings.Contains(cd, "test-disposition.md") {
		t.Errorf("expected 'test-disposition.md' in Content-Disposition, got %q", cd)
	}

	// Test json
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/test-disposition/export?format=json&cwd=/tmp/export-test", nil)
	rec = httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)
	cd = rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "test-disposition.json") {
		t.Errorf("expected 'test-disposition.json' in Content-Disposition, got %q", cd)
	}
}

// ---------------------------------------------------------------------------
// Test 10: TestHandleAPISessionExport_RedactionDefault
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_RedactionDefault(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-redact")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-redact", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	// Default: no_secret_redaction=false → RedactSecrets=true
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-redact/export?format=markdown&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, fakeAPIKey) {
		t.Errorf("expected secret to be redacted by default, but found raw secret in: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Test 11: TestHandleAPISessionExport_NoRedactionFlag
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_NoRedactionFlag(t *testing.T) {
	ws, root := setupExportTest(t)

	cs := makeTestSession("test-noredact")
	stateDir := filepath.Join(root, ".sprout", "sessions")
	if err := writeTestSession(stateDir, "test-noredact", "/tmp/export-test", cs); err != nil {
		t.Fatalf("writeTestSession: %v", err)
	}

	// no_secret_redaction=true → RedactSecrets=false
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/test-noredact/export?format=markdown&no_secret_redaction=true&cwd=/tmp/export-test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, fakeAPIKey) {
		t.Errorf("expected raw secret when no_secret_redaction=true, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Test 12: TestHandleAPISessionExport_MethodNotAllowed
// ---------------------------------------------------------------------------

func TestHandleAPISessionExport_MethodNotAllowed(t *testing.T) {
	ws, _ := setupExportTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/test/export", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionExport(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}
