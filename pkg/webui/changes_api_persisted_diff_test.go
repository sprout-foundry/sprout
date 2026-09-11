//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/history"
)

// With no live agent, the diff endpoint must still serve a diff for a
// path recorded in the persisted history store — that's what keeps the
// timeline tab's view-diff action working across sessions.
func TestChangesAPI_NoAgent_DiffServesPersistedStore(t *testing.T) {
	ws := newNoAgentTestServer(t)

	// Record a change directly in the history store.
	target := filepath.Join(t.TempDir(), "old-session.go")
	if err := history.RecordChangeWithDetails(
		"rev-webui-diff-test", target,
		"before content\n", "after content\n",
		"edit via EditFile", "note",
		"instructions", "response", "test-model",
	); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/changes/diff?path="+target, nil)
	rec := httptest.NewRecorder()
	ws.handleAPIChangesDiff(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Found bool   `json:"found"`
		Diff  string `json:"diff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, rec.Body.String())
	}
	if !resp.Found {
		t.Fatalf("expected found=true from persisted store, got: %s", rec.Body.String())
	}
	if !strings.Contains(resp.Diff, "before content") || !strings.Contains(resp.Diff, "after content") {
		t.Errorf("diff missing stored before/after content: %q", resp.Diff)
	}
}
