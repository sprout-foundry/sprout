//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// handleAPIConfig
// ---------------------------------------------------------------------------

func TestHandleAPIConfig_MethodNotAllowed(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/config", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIConfig(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIConfig_Success(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify expected fields
	if _, ok := resp["port"]; !ok {
		t.Error("expected 'port' in response")
	}
	if _, ok := resp["daemon_root"]; !ok {
		t.Error("expected 'daemon_root' in response")
	}
	if _, ok := resp["workspace_root"]; !ok {
		t.Error("expected 'workspace_root' in response")
	}
	if _, ok := resp["agent"]; !ok {
		t.Error("expected 'agent' in response")
	}
	if _, ok := resp["features"]; !ok {
		t.Error("expected 'features' in response")
	}

	// Verify agent sub-fields
	agentMap, ok := resp["agent"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent field should be a map, got %T", resp["agent"])
	}
	if agentMap["name"] != "sprout" {
		t.Errorf("expected agent name 'sprout', got %v", agentMap["name"])
	}

	// Verify features sub-fields
	featuresMap, ok := resp["features"].(map[string]interface{})
	if !ok {
		t.Fatalf("features field should be a map, got %T", resp["features"])
	}
	if featuresMap["terminal"] != true {
		t.Errorf("expected terminal feature true, got %v", featuresMap["terminal"])
	}
}

// ---------------------------------------------------------------------------
// handleTerminalHistoryGet
// ---------------------------------------------------------------------------

func TestHandleTerminalHistoryGet_NoSessionID(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/history", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["session_id"] != "" {
		t.Errorf("expected empty session_id, got %v", resp["session_id"])
	}
	// JSON numbers decode as float64
	if resp["count"] != float64(0) {
		t.Errorf("expected count 0, got %v", resp["count"])
	}
}

func TestHandleTerminalHistoryGet_HiddenSessionForbidden(t *testing.T) {
	ws, tm := newTestWebServer(t)

	// Create a visible session with history
	s, err := tm.CreateSession("vis-hist", "bash")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	s.mutex.Lock()
	s.History = []string{"ls", "cd foo"}
	s.HistoryIndex = 2
	s.Hidden = true // make it hidden so HasVisibleSession rejects it
	s.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/history?session_id=vis-hist", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryGet(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for hidden session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTerminalHistoryGet_Success(t *testing.T) {
	ws, tm := newTestWebServer(t)

	// Create a visible session with history
	s, err := tm.CreateSession("vis-hist-get", "bash")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	s.mutex.Lock()
	s.History = []string{"ls", "cd foo", "make build"}
	s.HistoryIndex = 3
	s.Hidden = false
	s.mutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/history?session_id=vis-hist-get", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["session_id"] != "vis-hist-get" {
		t.Errorf("expected session_id 'vis-hist-get', got %v", resp["session_id"])
	}
	if resp["count"] != float64(3) {
		t.Errorf("expected count 3, got %v", resp["count"])
	}
	hist, ok := resp["history"].([]interface{})
	if !ok {
		t.Fatalf("history should be a slice, got %T", resp["history"])
	}
	if len(hist) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(hist))
	}
}

func TestHandleTerminalHistoryGet_NonExistentSession(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/history?session_id=nonexistent", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryGet(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 403 or 500 for non-existent session, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleTerminalHistoryPost
// ---------------------------------------------------------------------------

func TestHandleTerminalHistoryPost_InvalidJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader("not json"))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTerminalHistoryPost_EmptyCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"session_id":"s1","command":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty command, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTerminalHistoryPost_WhitespaceOnlyCommand(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"session_id":"s1","command":"   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace-only command, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTerminalHistoryPost_NoSessionID(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"session_id":"","command":"ls"}`
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["stored"] != false {
		t.Errorf("expected stored=false, got %v", resp["stored"])
	}
	if resp["command"] != "ls" {
		t.Errorf("expected command 'ls', got %v", resp["command"])
	}
}

func TestHandleTerminalHistoryPost_HiddenSessionForbidden(t *testing.T) {
	ws, tm := newTestWebServer(t)

	s, err := tm.CreateSession("hidden-post", "bash")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	s.mutex.Lock()
	s.Hidden = true
	s.mutex.Unlock()

	body := `{"session_id":"hidden-post","command":"ls"}`
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for hidden session, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTerminalHistoryPost_Success(t *testing.T) {
	ws, tm := newTestWebServer(t)

	s, err := tm.CreateSession("post-success", "bash")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	s.mutex.Lock()
	s.History = []string{"ls"}
	s.HistoryIndex = 1
	s.Hidden = false
	s.mutex.Unlock()

	body := `{"session_id":"post-success","command":"go build"}`
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["stored"] != true {
		t.Errorf("expected stored=true, got %v", resp["stored"])
	}
	if resp["command"] != "go build" {
		t.Errorf("expected command 'go build', got %v", resp["command"])
	}
	if resp["session_id"] != "post-success" {
		t.Errorf("expected session_id 'post-success', got %v", resp["session_id"])
	}
}

func TestHandleTerminalHistoryPost_TrimmedCommand(t *testing.T) {
	ws, tm := newTestWebServer(t)

	s, err := tm.CreateSession("post-trim", "bash")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	s.mutex.Lock()
	s.Hidden = false
	s.mutex.Unlock()

	// Send command with leading/trailing whitespace — handler trims it
	body := `{"session_id":"post-trim","command":"  echo hello  "}`
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/history", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleTerminalHistoryPost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["command"] != "echo hello" {
		t.Errorf("expected trimmed command 'echo hello', got %q", resp["command"])
	}
}

// ---------------------------------------------------------------------------
// tryParseMultipartFile
// ---------------------------------------------------------------------------

func TestTryParseMultipartFile_NotMultipart_Extra(t *testing.T) {
	data, ok := tryParseMultipartFile([]byte("hello"), "text/plain")
	if ok {
		t.Error("expected false for non-multipart content type")
	}
	if data != nil {
		t.Error("expected nil data for non-multipart content type")
	}
}

func TestTryParseMultipartFile_EmptyContentType(t *testing.T) {
	data, ok := tryParseMultipartFile([]byte("hello"), "")
	if ok {
		t.Error("expected false for empty content type")
	}
	if data != nil {
		t.Error("expected nil data for empty content type")
	}
}

func TestTryParseMultipartFile_InvalidMultipart(t *testing.T) {
	// Content type says multipart but body is not valid multipart
	data, ok := tryParseMultipartFile([]byte("not multipart data"), "multipart/form-data; boundary=----WebKitFormBoundary")
	if ok {
		t.Error("expected false for invalid multipart body")
	}
	if data != nil {
		t.Error("expected nil data for invalid multipart body")
	}
}

func TestTryParseMultipartFile_MissingImageField(t *testing.T) {
	// Valid multipart but no "image" field
	body := buildMultipartBodyExtra("other_field", []byte("value"))
	data, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")
	if ok {
		t.Error("expected false when 'image' field is missing")
	}
	if data != nil {
		t.Error("expected nil data when 'image' field is missing")
	}
}

func TestTryParseMultipartFile_Success(t *testing.T) {
	// Valid multipart with "image" field containing PNG data (magic bytes)
	pngData := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a} // PNG magic
	body := buildMultipartBodyExtra("image", pngData)
	data, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")
	if !ok {
		t.Fatal("expected true for valid multipart with image field")
	}
	if len(data) != len(pngData) {
		t.Errorf("expected %d bytes, got %d", len(pngData), len(data))
	}
	// Verify the data matches
	for i := range pngData {
		if data[i] != pngData[i] {
			t.Errorf("byte mismatch at index %d: got 0x%02x, want 0x%02x", i, data[i], pngData[i])
			break
		}
	}
}

// buildMultipartBody creates a minimal multipart/form-data body with the given field name and data.
func buildMultipartBodyExtra(fieldName string, data []byte) []byte {
	boundary := "----WebKitFormBoundary7MA4YWxkTrZu0gW"
	buf := []byte("--" + boundary + "\r\n")
	buf = append(buf, "Content-Disposition: form-data; name=\""+fieldName+"\"; filename=\"test.bin\"\r\n"...)
	buf = append(buf, "Content-Type: application/octet-stream\r\n"...)
	buf = append(buf, "\r\n"...)
	buf = append(buf, data...)
	buf = append(buf, "\r\n"...)
	buf = append(buf, "--"+boundary+"--\r\n"...)
	return buf
}
