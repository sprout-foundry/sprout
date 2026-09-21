//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// SP-140-7 §7f review parity: a write through the webui (POST /api/file)
// publishes the same file_changed event an agent write publishes, so user
// edits appear in the per-turn change strip beside agent edits. This is the
// "like code" promise — both writers' edits are equally visible — pinned with
// a test so it survives refactors.
func TestFileWritePublishesFileChangedEvent(t *testing.T) {
	root := t.TempDir()
	rel := "design/screens/login.html"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "screens"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("<p>v1</p>"), 0o644))

	server := newDesignTestServer(t, root)

	// Subscribe to the bus before the write; Unsubscribe by name at cleanup.
	const subscriber = "file-write-parity-test"
	eventCh := server.eventBus.Subscribe(subscriber)
	t.Cleanup(func() { server.eventBus.Unsubscribe(subscriber) })
	payload, jsonErr := json.Marshal(map[string]string{"content": "<p>user edit</p>"})
	require.NoError(t, jsonErr)
	req := httptest.NewRequest(http.MethodPost, "/api/file?path="+rel, bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	server.handleAPIFile(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// The file_changed event must arrive, naming the written path with a
	// write action — the same shape the per-turn strip consumes for agent
	// writes (useWebEventHandler's file_changed branch).
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-eventCh:
			require.True(t, ok, "event bus closed before file_changed arrived")
			if event.Type != events.EventTypeFileChanged {
				continue
			}
			data, mapErr := json.Marshal(event.Data)
			require.NoError(t, mapErr)
			var fields map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &fields))
			// The publisher spells it file_path; the webui consumer accepts
			// both spellings (useWebSocketEventHandler's file_changed branch).
			filePath, _ := fields["file_path"].(string)
			if filePath == "" {
				filePath, _ = fields["path"].(string)
			}
			require.Contains(t, filePath, "design/screens/login.html")
			require.Equal(t, "write", fields["action"])
			return
		case <-deadline:
			t.Fatal("no file_changed event published by the webui write")
		}
	}
}
