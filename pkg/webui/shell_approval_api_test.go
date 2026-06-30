//go:build !js

// Package webui — integration tests for shell approval API (SP-093-3).
package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAPIShellApprovalDecision_ValidPayload(t *testing.T) {
	var reqID = "test-valid-" + t.Name()
	parts := []agent.ShellPart{
		{ID: "part-0", Text: "echo hi", Kind: "unknown", Semantic: ""},
		{ID: "part-1", Text: "rm -rf foo", Kind: "rm", Semantic: "delete"},
	}

	// Register the pending approval and grab the channel
	ch := RegisterShellApproval(reqID, "echo hi && rm -rf foo", parts)
	require.NotNil(t, ch, "RegisterShellApproval should return a channel for a new ID")

	// Build the decision payload
	payload := map[string]any{
		"request_id": reqID,
		"decisions":  map[string]bool{"part-0": true, "part-1": false},
	}
	body, _ := json.Marshal(payload)

	// Create the HTTP request
	req := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	// Call the handler via a zero-value ReactWebServer (no real server needed)
	ws := &ReactWebServer{}
	ws.handleAPIShellApprovalDecision(rec, req)

	// The goroutine should receive the decisions within 1 second
	var wg sync.WaitGroup
	wg.Add(1)
	var received map[string]bool
	go func() {
		defer wg.Done()
		select {
		case got := <-ch:
			received = got
		case <-time.After(time.Second):
			t.Error("timed out waiting for decisions on channel")
		}
	}()

	// Give the handler time to send
	time.Sleep(50 * time.Millisecond)
	wg.Wait()

	// Assert the response
	assert.Equal(t, http.StatusOK, rec.Code, "expected 200 OK, got %d: %s", rec.Code, rec.Body.String())

	var respBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	assert.Equal(t, true, respBody["ok"])
	assert.Equal(t, reqID, respBody["request_id"])

	// Assert the channel received the correct decisions
	assert.Equal(t, map[string]bool{"part-0": true, "part-1": false}, received)
}

func TestHandleAPIShellApprovalDecision_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/bad-json/decision", bytes.NewReader([]byte("not json at all{{{")))
	rec := httptest.NewRecorder()

	ws := &ReactWebServer{}
	ws.handleAPIShellApprovalDecision(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "expected 400 for invalid JSON")
	assert.Contains(t, rec.Body.String(), "Invalid JSON")
}

func TestHandleAPIShellApprovalDecision_MissingDecisions(t *testing.T) {
	var reqID = "test-missing-decisions-" + t.Name()

	// Payload with request_id but no decisions field
	payload := map[string]any{
		"request_id": reqID,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ws := &ReactWebServer{}
	ws.handleAPIShellApprovalDecision(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "expected 400 for missing decisions")
	assert.Contains(t, rec.Body.String(), "decisions map required")
}

func TestHandleAPIShellApprovalDecision_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/shell-approvals/some-id/decision", nil)
	rec := httptest.NewRecorder()

	ws := &ReactWebServer{}
	ws.handleAPIShellApprovalDecision(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "expected 405 for non-POST")
}

func TestExtractShellApprovalIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/shell-approvals/abc-123/decision", "abc-123"},
		{"/api/shell-approvals/my-request-id/decision", "my-request-id"},
		{"/api/shell-approvals/abc-123/", "abc-123"},
		{"/api/shell-approvals/abc-123", "abc-123"},
		{"/api/shell-approvals//decision", ""},
		{"/api/shell-approvals/", ""},
		{"/api/shell-approvals", ""},
		{"/api/other/abc-123/decision", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractShellApprovalIDFromPath(tt.path)
			assert.Equal(t, tt.want, got, "extractShellApprovalIDFromPath(%q)", tt.path)
		})
	}
}

func TestRegisterShellApproval_Deduplication(t *testing.T) {
	var reqID = "test-dedup-" + t.Name()
	parts := []agent.ShellPart{{ID: "p0", Text: "echo", Kind: "unknown", Semantic: ""}}

	ch1 := RegisterShellApproval(reqID, "echo", parts)
	require.NotNil(t, ch1, "first registration should return a channel")

	ch2 := RegisterShellApproval(reqID, "echo", parts)
	assert.Nil(t, ch2, "second registration with same ID should return nil (dedup)")

	// Cleanup: deliver a decision so the registry entry is removed
	payload := map[string]any{"request_id": reqID, "decisions": map[string]bool{"p0": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ws := &ReactWebServer{}
	ws.handleAPIShellApprovalDecision(rec, req)
	_ = rec // response not important for this test
}
