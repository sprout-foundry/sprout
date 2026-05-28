//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPIConfirm(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("POST with invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/git/commit", bytes.NewReader([]byte("not json")))
		rec := httptest.NewRecorder()

		server.handleAPIConfirm(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("POST without request_id returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"response": true,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git/commit", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIConfirm(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("POST with unknown request_id returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"request_id": "nonexistent-id",
			"response":   true,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git/commit", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		server.handleAPIConfirm(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rec.Code)
		}
	})

	t.Run("non-POST returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/git/commit", nil)
		rec := httptest.NewRecorder()

		server.handleAPIConfirm(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})
}
