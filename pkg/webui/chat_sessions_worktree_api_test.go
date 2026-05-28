//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// Renamed to avoid conflict with coverage_test.go
func TestSanitizePathComponentWorktree(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"feature-branch", "feature-branch"},
		{"feature/branch", "feature_branch"},
		{"feature\\branch", "feature_branch"},
		{"feature:branch", "feature_branch"},
		{"123.456", "123.456"},
		{"a/b/c/d", "a_b_c_d"},
		{"", ""},
		{"hello world", "hello_world"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizePathComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizePathComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleAPIChatSessionWorktreeGetMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session/test/worktree", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeGet(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeGetInvalidRoute(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-session//worktree", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeGet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty chatID, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeGetSuccess(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("default")
	req := httptest.NewRequest(http.MethodGet, "/api/chat-session/default/worktree", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIChatSessionWorktreeSetMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-session/test/worktree", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSet(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeSetInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session/default/worktree", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeSetInvalidRoute(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session//worktree", strings.NewReader(`{"worktree_path":""}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid route, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeSetSuccess(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("default")
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session/default/worktree", strings.NewReader(`{"worktree_path":""}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIChatSessionWorktreeSwitchMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-session/test/worktree/switch", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSwitch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeSwitchMissingPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session/default/worktree/switch", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing worktree_path, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeSwitchInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session/default/worktree/switch", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeSwitchInvalidRoute(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-session//worktree/switch", strings.NewReader(`{"worktree_path":"/foo"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeSwitch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeDispatcherMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodDelete, "/api/chat-session/test/worktree", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktree(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeDispatcherInvalidRoute(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-session/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktree(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeListMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/worktree-mappings", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeList(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionWorktreeListSuccess(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.getOrCreateClientContext("default")
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/worktree-mappings", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionWorktreeList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIChatSessionCreateInWorktreeMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/chat-sessions/create-in-worktree", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionCreateInWorktree(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionCreateInWorktreeMissingBranch(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-in-worktree", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionCreateInWorktree(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing branch, got %d", rec.Code)
	}
}

func TestHandleAPIChatSessionCreateInWorktreeInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat-sessions/create-in-worktree", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIChatSessionCreateInWorktree(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
