//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestBuildSessionList(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("empty list returns empty slice", func(t *testing.T) {
		result := server.buildSessionList(nil)
		if result == nil {
			t.Fatal("expected non-nil slice for empty input")
		}
		if len(result) != 0 {
			t.Errorf("expected 0 entries, got %d", len(result))
		}
	})

	t.Run("empty slice returns empty slice", func(t *testing.T) {
		result := server.buildSessionList([]agent.SessionInfo{})
		if len(result) != 0 {
			t.Errorf("expected 0 entries, got %d", len(result))
		}
	})
}

func TestHandleAPISessions(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("GET returns sessions array", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		rec := httptest.NewRecorder()

		server.handleAPISessions(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if _, ok := resp["sessions"]; !ok {
			t.Error("expected sessions field in response")
		}
		if _, ok := resp["current_session_id"]; !ok {
			t.Error("expected current_session_id field in response")
		}
	})

	t.Run("GET with scope=all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions?scope=all", nil)
		rec := httptest.NewRecorder()

		server.handleAPISessions(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		if _, ok := resp["sessions"]; !ok {
			t.Error("expected sessions field in response")
		}
	})

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
		rec := httptest.NewRecorder()

		server.handleAPISessions(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rec.Code)
		}
	})
}
