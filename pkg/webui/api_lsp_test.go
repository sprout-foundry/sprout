//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sprout-foundry/sprout/pkg/events"
	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

func TestHandleLSPStatus(t *testing.T) {
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	require.NoError(t, err)
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

	t.Run("GET returns all 14 servers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

		servers, ok := resp["servers"].([]interface{})
		require.True(t, ok, "servers should be an array")
		assert.Equal(t, 14, len(servers), "should return all 14 language server configs")
	})

	t.Run("each server has installHint field", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))

		var servers []map[string]interface{}
		require.NoError(t, json.Unmarshal(raw["servers"], &servers))

		for _, srv := range servers {
			serverMap := srv
			// Every server must have the installHint field in the JSON response
			// Even if empty, it should be present (but omitempty means empty strings are omitted)
			// So we check that either installHint exists or the value is empty
			_, hasInstallHint := serverMap["installHint"]
			_, hasID := serverMap["id"]
			assert.True(t, hasID, "each server should have an id field")

			if hasInstallHint {
				hint, ok := serverMap["installHint"].(string)
				assert.True(t, ok, "installHint should be a string")
				assert.NotEmpty(t, hint, "installHint should not be empty when present")
			}
		}
	})

	t.Run("gopls shows available=true in CI", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))

		var servers []map[string]interface{}
		require.NoError(t, json.Unmarshal(raw["servers"], &servers))

		// Find the Go server
		var goServer *map[string]interface{}
		for i := range servers {
			if srv, ok := servers[i]["id"].(string); ok && srv == "go" {
				goServer = &servers[i]
				break
			}
		}
		require.NotNil(t, goServer, "should have a Go server in the response")

		// In CI, gopls is installed, so it should be available
		available, ok := (*goServer)["available"].(bool)
		require.True(t, ok, "available should be a boolean")
		assert.True(t, available, "gopls should be available in CI environment")
	})

	t.Run("server IDs match expected set", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))

		var servers []map[string]interface{}
		require.NoError(t, json.Unmarshal(raw["servers"], &servers))

		expectedIDs := map[string]bool{
			"go":         false,
			"typescript": false,
			"python":     false,
			"rust":       false,
			"c-cpp":      false,
			"csharp":     false,
			"java":       false,
			"ruby":       false,
			"php":        false,
			"swift":      false,
			"kotlin":     false,
			"dart":       false,
			"lua":        false,
			"shell":      false,
		}

		for _, srv := range servers {
			id, ok := srv["id"].(string)
			require.True(t, ok, "server should have string id")
			_, exists := expectedIDs[id]
			assert.True(t, exists, "unexpected server ID: %s", id)
			expectedIDs[id] = true
		}

		for id, found := range expectedIDs {
			assert.True(t, found, "expected server ID %q not found in response", id)
		}
	})

	t.Run("each server has correct fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/lsp/status", nil)
		rec := httptest.NewRecorder()

		server.handleLSPStatus(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&raw))

		var servers []map[string]interface{}
		require.NoError(t, json.Unmarshal(raw["servers"], &servers))

		for _, srv := range servers {
			// Required fields
			_, hasID := srv["id"]
			_, hasLanguages := srv["languages"]
			_, hasBinary := srv["binary"]
			_, hasAvailable := srv["available"]

			assert.True(t, hasID, "server should have 'id' field")
			assert.True(t, hasLanguages, "server should have 'languages' field")
			assert.True(t, hasBinary, "server should have 'binary' field")
			assert.True(t, hasAvailable, "server should have 'available' field")
		}
	})
}
