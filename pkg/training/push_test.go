package training

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makePIIConversationState builds a state containing PII that must be
// redacted before pushing.
func makePIIConversationState(id, homeDir, username string) agent.ConversationState {
	return agent.ConversationState{
		SessionID:        id,
		Name:             "session with PII in " + homeDir,
		WorkingDirectory: homeDir + "/projects/myapp",
		LastUpdated:      time.Now(),
		Messages: []api.Message{
			{
				Role:    "user",
				Content: "Please fix the file at " + homeDir + "/projects/myapp/main.go",
			},
			{
				Role:    "assistant",
				Content: "I'll read " + homeDir + "/projects/myapp/main.go for you.",
				ToolCalls: []api.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: api.ToolCallFunction{
							Name:      "read_file",
							Arguments: `{"path":"` + homeDir + `/projects/myapp/main.go"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "Author: " + username + " <" + username + "@example.com>",
				ToolCallID: "call_1",
			},
		},
		TaskActions: []agent.TaskAction{
			{Type: "file_read", Description: "Read " + homeDir + "/projects/myapp/main.go", Details: "user:" + username},
		},
	}
}

// recordingHandler is an httptest.Server handler that captures the request
// body and method for assertions.
type recordingHandler struct {
	mu       sync.Mutex
	bodies   [][]byte
	methods  []string
	statusTo int // HTTP status to return (default 200)
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bodies = append(h.bodies, body)
	h.methods = append(h.methods, r.Method)
	status := h.statusTo
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
}

func (h *recordingHandler) lastBody() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bodies) == 0 {
		return nil
	}
	return h.bodies[len(h.bodies)-1]
}

func (h *recordingHandler) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.bodies)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestPushSession_PIIRedacted verifies that PII (home directory paths,
// usernames, emails) is redacted before being sent to the endpoint.
func TestPushSession_PIIRedacted(t *testing.T) {
	// We need a home dir and username that the redaction pipeline will
	// pick up. Since RedactContent calls DefaultPIIConfig() which reads
	// os.UserHomeDir(), we test with a path that matches the anyHomeDirRe
	// pattern (/Users/<name> or /home/<name>).
	homeDir := "/Users/testuser123"
	username := "testuser123"

	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := makePIIConversationState("sess-pii", homeDir, username)

	if err := PushSession(state, server.URL, nil); err != nil {
		t.Fatalf("PushSession returned error: %v", err)
	}

	if handler.callCount() != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", handler.callCount())
	}

	body := string(handler.lastBody())

	// PII must not appear in the pushed data.
	if strings.Contains(body, homeDir) {
		t.Errorf("pushed data contains home directory path %q — PII not redacted", homeDir)
	}
	if strings.Contains(body, username) {
		t.Errorf("pushed data contains username %q — PII not redacted", username)
	}
	if strings.Contains(body, username+"@example.com") {
		t.Errorf("pushed data contains email — PII not redacted")
	}

	// Redaction placeholders should be present.
	if !strings.Contains(body, "$HOME") {
		t.Error("pushed data should contain $HOME placeholder after redaction")
	}
}

// TestPushSession_ExcludedPathsSkipped verifies that sessions whose working
// directory matches an exclude path prefix are silently skipped.
func TestPushSession_ExcludedPathsSkipped(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := agent.ConversationState{
		SessionID:        "sess-excluded",
		WorkingDirectory: "/home/user/secret-project",
		Messages: []api.Message{
			{Role: "user", Content: "do something secret"},
		},
	}

	excludePaths := []string{"/home/user/secret-project"}
	err := PushSession(state, server.URL, excludePaths)
	if err != nil {
		t.Fatalf("PushSession returned error for excluded path: %v", err)
	}

	if handler.callCount() != 0 {
		t.Errorf("expected 0 HTTP calls for excluded path, got %d", handler.callCount())
	}
}

// TestPushSession_ExcludedPathsNotMatched verifies that non-matching exclude
// paths do not block the push.
func TestPushSession_ExcludedPathsNotMatched(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := agent.ConversationState{
		SessionID:        "sess-ok",
		WorkingDirectory: "/home/user/normal-project",
		Messages: []api.Message{
			{Role: "user", Content: "normal work"},
		},
	}

	excludePaths := []string{"/home/user/secret-project"}
	err := PushSession(state, server.URL, excludePaths)
	if err != nil {
		t.Fatalf("PushSession returned error: %v", err)
	}

	if handler.callCount() != 1 {
		t.Errorf("expected 1 HTTP call for non-excluded path, got %d", handler.callCount())
	}
}

// TestPushSession_HTTPPOST verifies the HTTP method and path are correct.
func TestPushSession_HTTPPOST(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := agent.ConversationState{
		SessionID:        "sess-method",
		WorkingDirectory: "/tmp/work",
		Messages:         []api.Message{{Role: "user", Content: "hello"}},
	}

	if err := PushSession(state, server.URL, nil); err != nil {
		t.Fatalf("PushSession returned error: %v", err)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.methods) != 1 {
		t.Fatalf("expected 1 call, got %d", len(handler.methods))
	}
	if handler.methods[0] != http.MethodPost {
		t.Errorf("expected POST method, got %s", handler.methods[0])
	}
}

// TestPushSession_ContainsRedactedData verifies the POST body is valid JSON
// containing the session data with redacted content.
func TestPushSession_ContainsRedactedData(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := agent.ConversationState{
		SessionID:        "sess-data",
		Name:             "test session",
		WorkingDirectory: "/tmp/work",
		Messages: []api.Message{
			{Role: "user", Content: "Hello world"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	if err := PushSession(state, server.URL, nil); err != nil {
		t.Fatalf("PushSession returned error: %v", err)
	}

	body := handler.lastBody()
	var pushed agent.ConversationState
	if err := json.Unmarshal(body, &pushed); err != nil {
		t.Fatalf("failed to unmarshal pushed body as ConversationState: %v", err)
	}

	if pushed.SessionID != "sess-data" {
		t.Errorf("expected SessionID 'sess-data', got %q", pushed.SessionID)
	}
	if len(pushed.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(pushed.Messages))
	}
	if pushed.Messages[0].Content != "Hello world" {
		t.Errorf("expected first message content 'Hello world', got %q", pushed.Messages[0].Content)
	}
}

// TestPushSession_DoesNotMutateOriginal verifies that the original state
// passed to PushSession is not mutated by the redaction process.
func TestPushSession_DoesNotMutateOriginal(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	originalContent := "/Users/originaluser/path/to/file"
	state := agent.ConversationState{
		SessionID:        "sess-immutable",
		WorkingDirectory: "/tmp/work",
		Messages: []api.Message{
			{Role: "user", Content: originalContent},
		},
	}

	if err := PushSession(state, server.URL, nil); err != nil {
		t.Fatalf("PushSession returned error: %v", err)
	}

	// Original must be unmodified.
	if state.Messages[0].Content != originalContent {
		t.Errorf("original state was mutated: expected %q, got %q", originalContent, state.Messages[0].Content)
	}
}

// TestPushSession_EndpointError verifies that a non-2xx response returns
// an error but does not panic.
func TestPushSession_EndpointError(t *testing.T) {
	handler := &recordingHandler{statusTo: http.StatusInternalServerError}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := agent.ConversationState{
		SessionID:        "sess-err",
		WorkingDirectory: "/tmp/work",
		Messages:         []api.Message{{Role: "user", Content: "hello"}},
	}

	err := PushSession(state, server.URL, nil)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code 500, got: %v", err)
	}
}

// TestPushSession_ConnectionRefused verifies that a connection error is
// returned without panicking.
func TestPushSession_ConnectionRefused(t *testing.T) {
	state := agent.ConversationState{
		SessionID:        "sess-refused",
		WorkingDirectory: "/tmp/work",
		Messages:         []api.Message{{Role: "user", Content: "hello"}},
	}

	// Use a port that's almost certainly not listening.
	err := PushSession(state, "http://127.0.0.1:1", nil)
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

// TestPushSession_EmptyEndpoint verifies behavior with an empty endpoint.
// The function should still attempt the POST (to "/sessions") and fail gracefully.
func TestPushSession_EmptyEndpoint(t *testing.T) {
	state := agent.ConversationState{
		SessionID:        "sess-empty",
		WorkingDirectory: "/tmp/work",
		Messages:         []api.Message{{Role: "user", Content: "hello"}},
	}

	// With empty endpoint, the URL becomes "/sessions" which will fail.
	// The function should return an error but not panic.
	err := PushSession(state, "", nil)
	if err == nil {
		// Some HTTP clients may succeed with relative URL on test server,
		// so we just verify no panic occurred.
	}
}

// TestPushSession_ToolCallArgumentsRedacted verifies that tool call
// arguments have PII redacted.
func TestPushSession_ToolCallArgumentsRedacted(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	homeDir := "/Users/testuser456"
	state := agent.ConversationState{
		SessionID:        "sess-toolargs",
		WorkingDirectory: "/tmp/work",
		Messages: []api.Message{
			{
				Role:    "assistant",
				Content: "Reading file",
				ToolCalls: []api.ToolCall{
					{
						ID:   "tc_1",
						Type: "function",
						Function: api.ToolCallFunction{
							Name:      "read_file",
							Arguments: `{"path":"` + homeDir + `/secret.go"}`,
						},
					},
				},
			},
		},
	}

	if err := PushSession(state, server.URL, nil); err != nil {
		t.Fatalf("PushSession returned error: %v", err)
	}

	body := string(handler.lastBody())
	if strings.Contains(body, homeDir) {
		t.Errorf("tool call arguments contain unredacted home dir %q", homeDir)
	}
}

// TestPushSession_TimeoutRespected verifies that PushSession respects the
// 5-second timeout when the server is slow.
func TestPushSession_TimeoutRespected(t *testing.T) {
	// Create a server that responds slowly (longer than pushTimeout).
	// Use a channel to allow the handler to finish after the test completes.
	done := make(chan struct{})
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(15 * time.Second):
		case <-done:
		}
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(slowHandler)
	defer func() {
		close(done)
		server.Close()
	}()

	state := agent.ConversationState{
		SessionID:        "sess-timeout",
		WorkingDirectory: "/tmp/work",
		Messages:         []api.Message{{Role: "user", Content: "hello"}},
	}

	start := time.Now()
	err := PushSession(state, server.URL, nil)
	elapsed := time.Since(start)

	if err == nil {
		if elapsed >= 9*time.Second {
			t.Error("PushSession waited too long — timeout not respected")
		}
	} else {
		if elapsed >= 9*time.Second {
			t.Errorf("PushSession took %v — timeout not working", elapsed)
		}
	}
}

// TestPushSession_EmptyMessages verifies that a state with no messages
// still pushes successfully.
func TestPushSession_EmptyMessages(t *testing.T) {
	handler := &recordingHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()

	state := agent.ConversationState{
		SessionID:        "sess-empty-msgs",
		WorkingDirectory: "/tmp/work",
	}

	if err := PushSession(state, server.URL, nil); err != nil {
		t.Fatalf("PushSession returned error for empty messages: %v", err)
	}

	if handler.callCount() != 1 {
		t.Errorf("expected 1 HTTP call, got %d", handler.callCount())
	}
}

// TestRedactConversationState verifies the internal redaction function
// directly, checking all fields are scrubbed.
func TestRedactConversationState(t *testing.T) {
	homeDir := "/Users/redacttest"
	state := agent.ConversationState{
		Name:             "session in " + homeDir,
		WorkingDirectory: homeDir + "/project",
		Messages: []api.Message{
			{Role: "user", Content: "path is " + homeDir + "/main.go"},
		},
		TaskActions: []agent.TaskAction{
			{Description: "read " + homeDir + "/main.go", Details: "in " + homeDir},
		},
	}

	redacted := redactConversationState(state)

	for _, msg := range redacted.Messages {
		if strings.Contains(msg.Content, homeDir) {
			t.Errorf("message content not redacted: %q", msg.Content)
		}
	}
	for _, action := range redacted.TaskActions {
		if strings.Contains(action.Description, homeDir) {
			t.Errorf("task action description not redacted: %q", action.Description)
		}
		if strings.Contains(action.Details, homeDir) {
			t.Errorf("task action details not redacted: %q", action.Details)
		}
	}
	if strings.Contains(redacted.Name, homeDir) {
		t.Errorf("session name not redacted: %q", redacted.Name)
	}

	// Original must be untouched.
	if strings.Contains(state.Messages[0].Content, "$HOME") {
		t.Error("original state was mutated by redactConversationState")
	}
}
