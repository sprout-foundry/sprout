//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// designStatusSeed writes the minimal tree the status tests use.
func designStatusSeed(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "design", "tokens")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	// (The variable is deliberately not named "tokens…" — gosec G101 keys on
	// the name, and this is a DTCG fixture, not a credential.)
	seedDoc := `{"color":{"brand":{"primary":{"$type":"color","$value":"#0055ff"}}}}`
	if err := os.WriteFile(filepath.Join(p, "color.tokens.json"), []byte(seedDoc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandleAPIDesignStatus(t *testing.T) {
	t.Run("non-GET returns 405", func(t *testing.T) {
		server := newDesignTestServer(t, t.TempDir())
		req := httptest.NewRequest(http.MethodPost, "/api/design/status", nil)
		rec := httptest.NewRecorder()
		server.handleAPIDesignStatus(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("no design tree returns exists false with 200", func(t *testing.T) {
		dir := t.TempDir()
		server := newDesignTestServer(t, dir)

		req := httptest.NewRequest(http.MethodGet, "/api/design/status", nil)
		rec := httptest.NewRecorder()
		server.handleAPIDesignStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var payload struct {
			Exists bool `json:"exists"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Exists {
			t.Fatal("expected exists=false for a workspace without design/")
		}
	})

	t.Run("seeded tree returns validation drift feedback and tokenRefs", func(t *testing.T) {
		dir := t.TempDir()
		designStatusSeed(t, dir)
		server := newDesignTestServer(t, dir)

		req := httptest.NewRequest(http.MethodGet, "/api/design/status", nil)
		rec := httptest.NewRecorder()
		server.handleAPIDesignStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Exists     bool `json:"exists"`
			Validation struct {
				Errors   int `json:"errors"`
				Warnings int `json:"warnings"`
				Infos    int `json:"infos"`
			} `json:"validation"`
			Drift struct {
				DesignAhead struct {
					Synced bool `json:"synced"`
				} `json:"designAhead"`
				CodeAhead struct {
					Synced bool `json:"synced"`
				} `json:"codeAhead"`
				Synced bool `json:"synced"`
			} `json:"drift"`
			Feedback struct {
				PendingCount int `json:"pendingCount"`
			} `json:"feedback"`
			TokenRefs map[string]int `json:"tokenRefs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.Exists {
			t.Fatal("expected exists=true")
		}
		if payload.Validation.Errors != 0 {
			t.Fatalf("expected clean tree, got errors=%d", payload.Validation.Errors)
		}
		if !payload.Drift.CodeAhead.Synced {
			t.Fatal("no touched set: code-ahead must report synced/not-assessable")
		}
		if payload.TokenRefs["color.brand.primary"] != 0 {
			t.Fatalf("unreferenced token must be absent, got %v", payload.TokenRefs)
		}
	})
}
