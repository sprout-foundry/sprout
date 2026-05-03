package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

func TestHandleLSPStatus(t *testing.T) {
	server := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
	server.lspManager = lspproxy.NewManager(context.Background())

	t.Run("GET returns servers array", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if _, ok := resp["servers"]; !ok {
			t.Error("expected servers field in response")
		}
		if _, ok := resp["active"]; !ok {
			t.Error("expected active field in response")
		}
		if _, ok := resp["workspace"]; !ok {
			t.Error("expected workspace field in response")
		}
	})

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})
}
