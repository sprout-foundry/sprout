//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------- handleAPISyncOp ----------

func TestHandleAPISyncOp_MethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/sync/op", nil)
			rec := httptest.NewRecorder()
			ws.handleAPISyncOp(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

func TestHandleAPISyncOp_InvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/op", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPISyncOp_EmptyPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/op",
		strings.NewReader(`{"op_type":"write","path":"","content":"hello"}`))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty path, got %d", rec.Code)
	}
}

func TestHandleAPISyncOp_WriteOp(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Create the default client context so getWorkspaceRootForRequest works.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	// Now send the write op.
	body := `{"op_type":"write","path":"hello.txt","content":"world","browser_seq":1,"timestamp":1000}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/op", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result agent.SyncOpResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	// Verify file was created on disk.
	content, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("file not created on disk: %v", err)
	}
	if string(content) != "world" {
		t.Fatalf("file content = %q, want %q", string(content), "world")
	}
}

func TestHandleAPISyncOp_DeleteOp(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "todelete.txt")
	if err := os.WriteFile(testFile, []byte("delete me"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Create the default client context.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	body := `{"op_type":"delete","path":"todelete.txt","browser_seq":1,"timestamp":1000}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/op", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result agent.SyncOpResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	// Verify file was deleted.
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatalf("file still exists after delete op")
	}
}

func TestHandleAPISyncOp_RenameOp(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("rename me"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Create the default client context.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	body := `{"op_type":"rename","path":"old.txt","new_path":"new.txt","browser_seq":1,"timestamp":1000}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/op", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result agent.SyncOpResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	// Verify file was renamed.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old file still exists after rename")
	}
	newFile := filepath.Join(dir, "new.txt")
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("new file not created: %v", err)
	}
	if string(content) != "rename me" {
		t.Fatalf("new file content = %q, want %q", string(content), "rename me")
	}
}

func TestHandleAPISyncOp_ConflictDetection(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(testFile, []byte("container content"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Pre-set metadata: container has unsynced writes.
	ws.agent.SetFileMetadata("conflict.txt", agent.WorkspaceFileMetadata{
		ContainerSeq:      5,
		LastSyncedContainer: 3,
	})

	// Create the default client context.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	body := `{"op_type":"write","path":"conflict.txt","content":"new content","browser_seq":10,"timestamp":1000}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/op", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for conflict, got %d: %s", rec.Code, rec.Body.String())
	}

	var result agent.SyncOpResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if result.Accepted {
		t.Fatal("expected accepted=false for conflict")
	}
	if result.ConflictPath == "" {
		t.Fatal("expected conflict_path to be set")
	}
	if !strings.Contains(result.Error, "container has unsynced writes") {
		t.Errorf("expected conflict error message, got %q", result.Error)
	}
}

func TestHandleAPISyncOp_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Create the default client context.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	body := `{"op_type":"write","path":"../../../etc/passwd","content":"hack","browser_seq":1,"timestamp":1000}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/op", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncOp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", rec.Code, rec.Body.String())
	}

	var result agent.SyncOpResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if result.Accepted {
		t.Fatal("expected accepted=false for path traversal")
	}
}

// ---------- handleAPISyncBatch ----------

func TestHandleAPISyncBatch_MethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/sync/batch", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISyncBatch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPISyncBatch_InvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/batch", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	ws.handleAPISyncBatch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPISyncBatch_EmptyOps(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/batch",
		strings.NewReader(`{"ops":[]}`))
	rec := httptest.NewRecorder()
	ws.handleAPISyncBatch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ops, got %d", rec.Code)
	}
}

func TestHandleAPISyncBatch_MultipleOps(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Create the default client context.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	body := `{"ops":[
		{"op_type":"write","path":"a.txt","content":"one","browser_seq":1,"timestamp":1000},
		{"op_type":"write","path":"b.txt","content":"two","browser_seq":2,"timestamp":1001},
		{"op_type":"write","path":"c.txt","content":"three","browser_seq":3,"timestamp":1002}
	]}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/batch", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncBatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Results []agent.SyncOpResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Results))
	}
	for i, r := range resp.Results {
		if !r.Accepted {
			t.Errorf("result %d not accepted: %s", i, r.Error)
		}
	}

	// Verify all files were created.
	for name, content := range map[string]string{"a.txt": "one", "b.txt": "two", "c.txt": "three"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("file %s not created: %v", name, err)
		}
		if string(data) != content {
			t.Errorf("file %s content = %q, want %q", name, string(data), content)
		}
	}
}

func TestHandleAPISyncBatch_StopsOnConflict(t *testing.T) {
	dir := t.TempDir()
	conflictFile := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(conflictFile, []byte("container content"), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.workspaceRoot = dir

	// Set conflict on the second file.
	ws.agent.SetFileMetadata("conflict.txt", agent.WorkspaceFileMetadata{
		ContainerSeq:      5,
		LastSyncedContainer: 3,
	})

	// Create the default client context.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	ws.getClientContextForRequest(req)

	body := `{"ops":[
		{"op_type":"write","path":"ok.txt","content":"fine","browser_seq":1,"timestamp":1000},
		{"op_type":"write","path":"conflict.txt","content":"new","browser_seq":2,"timestamp":1001},
		{"op_type":"write","path":"skipped.txt","content":"nope","browser_seq":3,"timestamp":1002}
	]}`
	req = httptest.NewRequest(http.MethodPost, "/api/sync/batch", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISyncBatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Results []agent.SyncOpResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Results))
	}

	// First op succeeded.
	if !resp.Results[0].Accepted {
		t.Errorf("first op should be accepted: %s", resp.Results[0].Error)
	}
	// Second op conflicted.
	if resp.Results[1].Accepted {
		t.Error("second op should not be accepted (conflict)")
	}
	// Third op was skipped.
	if resp.Results[2].Accepted {
		t.Error("third op should not be accepted (skipped)")
	}
	if !strings.Contains(resp.Results[2].Error, "skipped") {
		t.Errorf("third op error should mention skipped, got %q", resp.Results[2].Error)
	}
}

// ---------- handleAPISyncStatus ----------

func TestHandleAPISyncStatus_MethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISyncStatus(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPISyncStatus_EmptyState(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// No agent set → nil from GetSyncStatus.
	req := httptest.NewRequest(http.MethodGet, "/api/sync/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISyncStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	// files should be null when agent is nil.
	if resp["files"] != nil {
		t.Errorf("expected null files, got %v", resp["files"])
	}
}

func TestHandleAPISyncStatus_WithMetadata(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	ws.agent = &agent.Agent{}
	ws.agent.SetFileMetadata("x.txt", agent.WorkspaceFileMetadata{
		BrowserSeq: 5,
		ContainerSeq: 3,
	})
	ws.agent.SetFileMetadata("y.txt", agent.WorkspaceFileMetadata{
		BrowserSeq: 2,
		ContainerSeq: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sync/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISyncStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	files := resp["files"].(map[string]interface{})
	if len(files) != 2 {
		t.Fatalf("expected 2 files in response, got %d", len(files))
	}
	if _, ok := files["x.txt"]; !ok {
		t.Error("expected x.txt in response")
	}
	if _, ok := files["y.txt"]; !ok {
		t.Error("expected y.txt in response")
	}
}
