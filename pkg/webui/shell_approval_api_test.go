//go:build !js

// Package webui — integration tests for shell approval API (SP-093-3).
package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleAPIShellApprovalDecision_NoAgent verifies that the handler returns
// 503 when there's no agent instance (resolveShellApprovalAgent returns nil).
// The frontend's handleShellApprovalSubmit treats non-2xx as an error and
// keeps the dialog open so the user can retry.
func TestHandleAPIShellApprovalDecision_NoAgent(t *testing.T) {
	var reqID = "test-no-agent-" + t.Name()

	// Register in the broker.
	ch := agent.TestShellApprovalRegister(reqID)
	defer agent.TestShellApprovalCleanup(reqID)
	require.NotNil(t, ch)

	payload := map[string]any{
		"request_id": reqID,
		"decisions":  map[string]bool{"part-0": true, "part-1": false},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	// Zero-value ReactWebServer — resolveShellApprovalAgent returns nil.
	ws := &ReactWebServer{}
	ws.handleAPIShellApprovalDecision(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "expected 503, got %d: %s", rec.Code, rec.Body.String())

	// The decision was NOT delivered (no agent), so the channel should be empty.
	select {
	case <-ch:
		t.Fatal("channel should be empty — no agent means no delivery")
	default:
		// Good — channel is empty as expected.
	}
}

// TestHandleAPIShellApprovalDecision_WithAgent verifies the full delivery path:
// the handler calls RespondToShellApproval on the agent, which delivers to
// the broker channel, unblocking the goroutine in RequestShellApproval.
func TestHandleAPIShellApprovalDecision_WithAgent(t *testing.T) {
	var reqID = "test-with-agent-" + t.Name()

	// Register in the broker (simulates RequestShellApproval registering).
	ch := agent.TestShellApprovalRegister(reqID)
	defer agent.TestShellApprovalCleanup(reqID)
	require.NotNil(t, ch)

	payload := map[string]any{
		"request_id": reqID,
		"decisions":  map[string]bool{"part-0": true, "part-1": false, "part-2": true},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ws := &ReactWebServer{}
	ag, err := agent.NewAgentWithModel("test:test")
	require.NoError(t, err)
	defer ag.Shutdown()
	ws.agent = ag

	ws.handleAPIShellApprovalDecision(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var respBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	assert.Equal(t, true, respBody["ok"])
	assert.Equal(t, reqID, respBody["request_id"])

	// Verify the channel received the decisions.
	select {
	case decisions := <-ch:
		assert.True(t, decisions["part-0"], "part-0 should be approved")
		assert.False(t, decisions["part-1"], "part-1 should be rejected")
		assert.True(t, decisions["part-2"], "part-2 should be approved")
	default:
		t.Fatal("decisions not received on channel — agent delivery failed")
	}
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

// TestShellApprovalDecision_DoubleRespond verifies that a second POST
// for the same request ID returns 200 (the frontend doesn't retry) but
// the channel doesn't receive a second value.
func TestShellApprovalDecision_DoubleRespond(t *testing.T) {
	var reqID = "test-double-respond-" + t.Name()

	ch := agent.TestShellApprovalRegister(reqID)
	defer agent.TestShellApprovalCleanup(reqID)
	require.NotNil(t, ch)

	payload := map[string]any{
		"request_id": reqID,
		"decisions":  map[string]bool{"part-0": true},
	}
	body, _ := json.Marshal(payload)

	ws := &ReactWebServer{}
	ag, err := agent.NewAgentWithModel("test:test")
	require.NoError(t, err)
	defer ag.Shutdown()
	ws.agent = ag

	// First POST — should deliver.
	req1 := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	ws.handleAPIShellApprovalDecision(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Receive the first delivery.
	select {
	case decisions := <-ch:
		assert.True(t, decisions["part-0"])
	default:
		t.Fatal("first delivery not received")
	}

	// Second POST with same ID — RespondToShellApproval returns false
	// (entry already resolved), so the handler returns 410 Gone.
	body2, _ := json.Marshal(map[string]any{
		"request_id": reqID,
		"decisions":  map[string]bool{"part-0": false},
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/shell-approvals/"+reqID+"/decision", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	ws.handleAPIShellApprovalDecision(rec2, req2)
	assert.Equal(t, http.StatusGone, rec2.Code, "expected 410 for already-responded request")

	// Channel should NOT have a second value.
	select {
	case <-ch:
		t.Fatal("channel should not have a second delivery")
	default:
		// Good — only one delivery.
	}
}
