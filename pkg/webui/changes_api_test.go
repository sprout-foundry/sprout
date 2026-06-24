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

	"github.com/sprout-foundry/sprout/pkg/events"
)

// writeEditorConfig writes a config.json with LastUsedProvider="editor"
// into the given config directory, causing isProviderAvailable() to
// return false so no real agent is created during tests.
func writeEditorConfig(t *testing.T, cfgDir string) {
	t.Helper()
	cfg := map[string]string{"last_used_provider": "editor"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// newNoAgentTestServer creates a ReactWebServer with no agent wired to
// the default client, simulating daemon mode at page-load time (before
// the first chat query creates an agent).
func newNoAgentTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	daemonRoot := t.TempDir()

	// Isolate config so isProviderAvailable() returns false (editor mode).
	cfgDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", cfgDir)
	t.Setenv("LEDIT_CONFIG", cfgDir)
	writeEditorConfig(t, cfgDir)

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot
	return ws
}

// TestChangesAPI_NoAgent_SessionReturns200 verifies that the session
// manifest endpoint returns 200 (not 503) when no agent exists.
func TestChangesAPI_NoAgent_SessionReturns200(t *testing.T) {
	ws := newNoAgentTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/changes/session", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChangesSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["files"]; !ok {
		t.Errorf("expected 'files' key in response, got: %v", resp)
	}
}

// TestChangesAPI_NoAgent_SummaryReturns200 verifies the summary
// endpoint returns an empty disabled response instead of 503.
func TestChangesAPI_NoAgent_SummaryReturns200(t *testing.T) {
	ws := newNoAgentTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/changes/summary", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChangesSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["enabled"] != false {
		t.Errorf("expected enabled=false, got: %v", resp["enabled"])
	}
}

// TestChangesAPI_NoAgent_TimelineReturns200 verifies the timeline
// endpoint returns persisted data instead of 503.
func TestChangesAPI_NoAgent_TimelineReturns200(t *testing.T) {
	ws := newNoAgentTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/changes/timeline", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChangesTimeline(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestChangesAPI_NoAgent_DiffReturnsNotFound verifies the diff
// endpoint returns a not-found envelope instead of 503.
func TestChangesAPI_NoAgent_DiffReturnsNotFound(t *testing.T) {
	ws := newNoAgentTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/changes/diff?path=foo.go", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChangesDiff(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["found"] != false {
		t.Errorf("expected found=false, got: %v", resp["found"])
	}
}

// TestChangesAPI_NoAgent_DiffMissingPath verifies the diff endpoint
// still rejects requests missing the path param.
func TestChangesAPI_NoAgent_DiffMissingPath(t *testing.T) {
	ws := newNoAgentTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/changes/diff", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChangesDiff(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestChangesAPI_NoAgent_RevertReturns409 verifies the revert endpoint
// returns 409 Conflict (not 503) when no agent exists — there's nothing
// to revert.
func TestChangesAPI_NoAgent_RevertReturns409(t *testing.T) {
	ws := newNoAgentTestServer(t)

	body := `{"scope":"all"}`
	req := httptest.NewRequest(http.MethodPost, "/api/changes/revert", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIChangesRevert(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}
