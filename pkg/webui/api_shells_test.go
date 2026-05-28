//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPITerminalShells(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("GET returns shells array", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/terminal/shells", nil)
		rec := httptest.NewRecorder()

		server.handleAPITerminalShells(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if _, ok := resp["shells"]; !ok {
			t.Error("expected shells field in response")
		}
	})

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/terminal/shells", nil)
		rec := httptest.NewRecorder()

		server.handleAPITerminalShells(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})
}
