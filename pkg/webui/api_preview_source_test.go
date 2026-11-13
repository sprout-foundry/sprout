//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPreviewSourceServer builds a server with a started preview origin and a
// default client context rooted at root, so getWorkspaceRootForRequest resolves
// the same root the origin serves from.
func newPreviewSourceServer(t *testing.T, root string) *ReactWebServer {
	t.Helper()
	ws := &ReactWebServer{
		previewOrigin:  newPreviewOrigin(),
		clientContexts: make(map[string]*webClientContext),
	}
	if err := ws.previewOrigin.start(root); err != nil {
		t.Fatalf("starting preview origin: %v", err)
	}
	t.Cleanup(ws.previewOrigin.stop)

	resolved := root
	if evaled, err := filepath.EvalSymlinks(root); err == nil {
		resolved = evaled
	}
	ws.clientContexts[defaultWebClientID] = &webClientContext{
		WorkspaceRoot: resolved,
		FileConsents:  newFileConsentManager(),
	}
	return ws
}

// callPreviewSource drives handleAPIPreviewSource directly.
func callPreviewSource(t *testing.T, ws *ReactWebServer, path string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/preview/source?path="+path, nil)
	rec := httptest.NewRecorder()
	ws.handleAPIPreviewSource(rec, req)

	var body map[string]interface{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding response %q: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, body
}

func TestAPIPreviewSourceMintsURL(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<p>x</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := newPreviewSourceServer(t, root)

	code, body := callPreviewSource(t, ws, "page.html")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %v)", code, body)
	}
	if body["available"] != true {
		t.Fatalf("available = %v, want true", body["available"])
	}
	url, _ := body["url"].(string)
	if !strings.Contains(url, previewPathPref) || !strings.HasSuffix(url, "/page.html") {
		t.Errorf("url = %q, want a preview-origin URL ending in /page.html", url)
	}

	// The minted URL must actually serve the file.
	resp, served := getPreview(t, url)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET minted url: status = %d, want 200", resp.StatusCode)
	}
	if served != "<p>x</p>" {
		t.Errorf("served %q, want the file content", served)
	}
}

func TestAPIPreviewSourceUnavailableWithoutOrigin(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := newPreviewSourceServer(t, root)
	ws.previewOrigin.stop()

	code, body := callPreviewSource(t, ws, "page.html")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["available"] != false {
		t.Errorf("available = %v, want false when the origin is not running", body["available"])
	}
	if body["url"] != nil {
		t.Errorf("url = %v, want none when unavailable", body["url"])
	}
}

func TestAPIPreviewSourceRequiresPath(t *testing.T) {
	ws := newPreviewSourceServer(t, t.TempDir())
	code, _ := callPreviewSource(t, ws, "")
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 without a path", code)
	}
}

func TestAPIPreviewSourceRejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-secret.html")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	ws := newPreviewSourceServer(t, root)

	code, body := callPreviewSource(t, ws, outside)
	if code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a path outside the workspace (body %v)", code, body)
	}
	if body["url"] != nil {
		t.Errorf("url = %v, want none for a rejected path", body["url"])
	}
}

func TestAPIPreviewSourceRejectsMissingFile(t *testing.T) {
	ws := newPreviewSourceServer(t, t.TempDir())
	code, _ := callPreviewSource(t, ws, "nope.html")
	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a missing file", code)
	}
}
