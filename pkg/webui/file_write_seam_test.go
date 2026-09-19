//go:build !js

package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// SP-140-7 §7a safe-write seam (opt-in conditional write on POST /api/file)
// ---------------------------------------------------------------------------

// seamWritePOSTs a write body to the handler for rel under root.
func seamWrite(t *testing.T, root, rel string, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	require.NoError(t, err)
	server.workspaceRoot = root
	server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = root

	payload, jsonErr := json.Marshal(body)
	require.NoError(t, jsonErr)
	req := httptest.NewRequest(http.MethodPost, "/api/file?path="+rel, bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	server.handleAPIFile(rec, req)
	return rec
}

func seamRead(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

func TestFileWriteSafeSeam(t *testing.T) {
	root := t.TempDir()
	rel := "design/screens/login.html"
	seed := "<p>v1</p>"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "screens"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(seed), 0o644))

	info, statErr := os.Stat(filepath.Join(root, rel))
	require.NoError(t, statErr)
	sum := sha256.Sum256([]byte(seed))
	seedHash := hex.EncodeToString(sum[:])

	t.Run("unconditional write still works (back-compat)", func(t *testing.T) {
		rec := seamWrite(t, root, rel, map[string]interface{}{"content": "<p>v2</p>"})
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, seamRead(t, root, rel), "v2")
	})

	t.Run("matching baseMtime writes", func(t *testing.T) {
		info2, err := os.Stat(filepath.Join(root, rel))
		require.NoError(t, err)
		rec := seamWrite(t, root, rel, map[string]interface{}{
			"content": "<p>v3</p>", "baseMtime": info2.ModTime().Unix(),
		})
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, seamRead(t, root, rel), "v3")
	})

	t.Run("stale baseMtime returns 409 with current revision and writes nothing", func(t *testing.T) {
		before := seamRead(t, root, rel)
		rec := seamWrite(t, root, rel, map[string]interface{}{
			"content": "<p>lost-update</p>", "baseMtime": info.ModTime().Unix() - 999,
		})
		require.Equal(t, http.StatusConflict, rec.Code)
		var payload struct {
			Error        string `json:"error"`
			CurrentMtime int64  `json:"currentMtime"`
			CurrentHash  string `json:"currentHash"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		require.Equal(t, "revision_conflict", payload.Error)
		require.NotEmpty(t, payload.CurrentHash)
		require.Equal(t, before, seamRead(t, root, rel), "nothing was written")
	})

	t.Run("matching baseHash writes; stale baseHash conflicts", func(t *testing.T) {
		rec := seamWrite(t, root, rel, map[string]interface{}{
			"content": "<p>v4</p>", "baseHash": seedHash,
		})
		// The hash is from v1 but the file is at v3: a genuine conflict.
		require.Equal(t, http.StatusConflict, rec.Code)

		current := seamRead(t, root, rel)
		sumCurrent := sha256.Sum256([]byte(current))
		rec2 := seamWrite(t, root, rel, map[string]interface{}{
			"content": "<p>v5</p>", "baseHash": hex.EncodeToString(sumCurrent[:]),
		})
		require.Equal(t, http.StatusOK, rec2.Code)
		require.Contains(t, seamRead(t, root, rel), "v5")
	})

	t.Run("baseMtime of a deleted file conflicts instead of recreating", func(t *testing.T) {
		require.NoError(t, os.Remove(filepath.Join(root, rel)))
		rec := seamWrite(t, root, rel, map[string]interface{}{
			"content": "<p>zombie</p>", "baseMtime": info.ModTime().Unix(),
		})
		require.Equal(t, http.StatusConflict, rec.Code)
		require.Contains(t, rec.Body.String(), "base_file_missing")
		_, statErr := os.Stat(filepath.Join(root, rel))
		require.True(t, os.IsNotExist(statErr), "nothing was written")
	})
}
