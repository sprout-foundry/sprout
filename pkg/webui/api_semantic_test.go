package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
	lspsemantic "github.com/alantheprice/ledit/pkg/lsp/semantic"
)

type captureSemanticAdapter struct {
	lastInput lspsemantic.ToolInput
}

func (a *captureSemanticAdapter) Run(input lspsemantic.ToolInput) (lspsemantic.ToolResult, error) {
	a.lastInput = input
	time.Sleep(2 * time.Millisecond)
	return lspsemantic.ToolResult{
		Capabilities: lspsemantic.Capabilities{Diagnostics: true, Definition: true},
		Diagnostics: []lspsemantic.ToolDiagnostic{{
			From:     1,
			To:       2,
			Severity: "error",
			Message:  "boom",
			Source:   "test",
		}},
	}, nil
}

func TestHandleAPISemanticIncludesDurationAndNormalizesTrigger(t *testing.T) {
	previousRegistry := semanticAdapterRegistry
	t.Cleanup(func() {
		semanticAdapterRegistry = previousRegistry
	})

	registry := lspsemantic.NewRegistry()
	adapter := &captureSemanticAdapter{}
	registry.RegisterSingleton(adapter, "typescript")
	semanticAdapterRegistry = registry

	workspaceRoot := t.TempDir()
	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	ws.workspaceRoot = workspaceRoot
	ws.daemonRoot = workspaceRoot
	ws.fileConsents = newFileConsentManager()

	body, err := json.Marshal(map[string]any{
		"path":        "src/example.ts",
		"content":     "const value = 1;",
		"language_id": "TypeScript",
		"method":      "diagnostics",
		"trigger":     " SAVE ",
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/semantic", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPISemantic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp semanticResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if adapter.lastInput.Trigger != "save" {
		t.Fatalf("expected normalized trigger 'save', got %q", adapter.lastInput.Trigger)
	}
	if adapter.lastInput.FilePath != filepath.Join(workspaceRoot, "src/example.ts") {
		t.Fatalf("expected canonical file path inside workspace, got %q", adapter.lastInput.FilePath)
	}
	if resp.DurationMs <= 0 {
		t.Fatalf("expected duration_ms > 0, got %d", resp.DurationMs)
	}
	if len(resp.Diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(resp.Diagnostics))
	}
}
