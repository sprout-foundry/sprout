//go:build !js

package webui

// SP-155 §155a (TODO 155.4) preview API tests: the dev-server state and
// action endpoints the preview pane polls, exercised the way the
// neighboring api_*_test.go files do their handlers — a real (unserved)
// server, requests through httptest, responses unmarshalled from the
// recorded body.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/preview"
)

// newPreviewTestServer builds the minimal server shape the other pkg/webui
// handler tests use, rooted at a fresh temp project directory (the preview
// manager is keyed by the workspace root), and stops the preview managers
// (and any dev server they own) on cleanup.
func newPreviewTestServer(t *testing.T) (*ReactWebServer, string) {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	server.workspaceRoot = root
	t.Cleanup(server.stopPreviewManagers)
	return server, root
}

// writePreviewManifest writes a starter manifest into root declaring a dev
// command and port (each omitted when empty/zero).
func writePreviewManifest(t *testing.T, root, dev string, port int) {
	t.Helper()
	var manifest string
	switch {
	case dev == "" && port == 0:
		manifest = `{"starter":{"id":"fixture","version":"0.0.1"}}`
	case dev == "":
		manifest = fmt.Sprintf(`{"starter":{"id":"fixture","version":"0.0.1"},"dev_port":%d}`, port)
	default:
		manifest = fmt.Sprintf(`{"starter":{"id":"fixture","version":"0.0.1"},"dev":%q,"dev_port":%d}`, dev, port)
	}
	dir := filepath.Join(root, ".sprout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "starter.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeInvalidPreviewManifest writes a corrupt starter manifest into root.
func writeInvalidPreviewManifest(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".sprout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "starter.json"), []byte(`{invalid json`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// localServerOnPort serves HTTP on the given port (a real local listener,
// no shell involved) and closes it when the test ends. It stands in for an
// already-running dev server (the detect path).
func localServerOnPort(t *testing.T, port int) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("preview app"))
	}))
	s.Listener = ln
	s.Start()
	t.Cleanup(s.Close)
	return s
}

// freePreviewPort returns a currently-free port on 127.0.0.1.
func freePreviewPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// previewShAvailable skips the tests whose dev command is a shell builtin
// when no sh exists on this platform.
func previewShAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform")
	}
}

// previewStateJSON mirrors the wire shape of the /api/preview responses
// (the manager's State), unmarshalled against a separate local type so a
// field rename on the manager side fails these tests.
type previewStateJSON struct {
	Status   string `json:"status"`
	URL      string `json:"url,omitempty"`
	Error    string `json:"error,omitempty"`
	Detected bool   `json:"detected,omitempty"`
}

// previewStatus invokes the status handler and unmarshals its body.
func previewStatus(t *testing.T, ws *ReactWebServer) previewStateJSON {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/preview/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIPreviewStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from status, got %d: %s", rec.Code, rec.Body.String())
	}
	var body previewStateJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("status response is not JSON: %s (%v)", rec.Body.String(), err)
	}
	return body
}

// previewInvoke calls a preview action handler (start/restart/stop) with a
// POST and returns the recorder.
func previewInvoke(t *testing.T, ws *ReactWebServer, handler func(http.ResponseWriter, *http.Request), path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// previewAction calls a preview action handler and unmarshals its JSON body
// (expected code 200 unless wantCode says otherwise).
func previewAction(t *testing.T, ws *ReactWebServer, handler func(http.ResponseWriter, *http.Request), path string, wantCode int) previewStateJSON {
	t.Helper()
	rec := previewInvoke(t, ws, handler, path)
	if rec.Code != wantCode {
		t.Fatalf("expected %d, got %d: %s", wantCode, rec.Code, rec.Body.String())
	}
	var body previewStateJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %s (%v)", rec.Body.String(), err)
	}
	return body
}

// waitForPreviewStatus polls the status handler until the given status
// shows up (or the deadline elapses), returning the final state.
func waitForPreviewStatus(t *testing.T, ws *ReactWebServer, want preview.Status) previewStateJSON {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var body previewStateJSON
	for {
		body = previewStatus(t, ws)
		if preview.Status(body.Status) == want {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for preview status %q (last: %+v)", want, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestHandleAPIPreviewStatus(t *testing.T) {
	t.Run("non-GET returns 405", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/preview/status", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIPreviewStatus(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("no manifest reports stopped with the reason", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		body := previewStatus(t, ws)
		if body.Status != string(preview.StatusStopped) {
			t.Fatalf("expected stopped, got %+v", body)
		}
		if !strings.Contains(body.Error, "no starter manifest") {
			t.Fatalf("expected the no-manifest reason, got %q", body.Error)
		}
	})

	t.Run("manifest with a dev declaration but nothing running reports stopped", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		writePreviewManifest(t, root, "npm run dev", port)

		body := previewStatus(t, ws)
		if body.Status != string(preview.StatusStopped) {
			t.Fatalf("expected stopped, got %+v", body)
		}
		if body.Error != "" {
			t.Fatalf("a stopped-but-available dev server has no error, got %q", body.Error)
		}
		if body.URL != "" || body.Detected {
			t.Fatalf("nothing is running: %+v", body)
		}
	})
}

func TestHandleAPIPreviewStart(t *testing.T) {
	t.Run("non-POST returns 405", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/api/preview/start", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIPreviewStart(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("no manifest is refused with 404", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		rec := previewInvoke(t, ws, ws.handleAPIPreviewStart, "/api/preview/start")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "no_dev_command")
	})

	t.Run("invalid manifest is refused with 400", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		writeInvalidPreviewManifest(t, root)
		rec := previewInvoke(t, ws, ws.handleAPIPreviewStart, "/api/preview/start")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "invalid_manifest")
	})

	t.Run("manifest without a dev command is refused with 404", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		writePreviewManifest(t, root, "", 0)
		rec := previewInvoke(t, ws, ws.handleAPIPreviewStart, "/api/preview/start")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "no_dev_command")
	})

	t.Run("dev command without a port is refused with 404", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		writePreviewManifest(t, root, "npm run dev", 0)
		rec := previewInvoke(t, ws, ws.handleAPIPreviewStart, "/api/preview/start")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "no_dev_command")
	})

	t.Run("detects an already-running server", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		localServerOnPort(t, port)
		writePreviewManifest(t, root, "npm run dev", port)

		body := previewAction(t, ws, ws.handleAPIPreviewStart, "/api/preview/start", http.StatusOK)
		if body.Status != string(preview.StatusRunning) {
			t.Fatalf("expected running (detected), got %+v", body)
		}
		if !body.Detected {
			t.Error("expected detected=true for an external server")
		}
		if body.URL != fmt.Sprintf("http://localhost:%d", port) {
			t.Errorf("expected the localhost URL, got %q", body.URL)
		}

		// A second start while running is an idempotent no-op.
		body = previewAction(t, ws, ws.handleAPIPreviewStart, "/api/preview/start", http.StatusOK)
		if body.Status != string(preview.StatusRunning) || !body.Detected {
			t.Fatalf("a second start must report the running detected state, got %+v", body)
		}
	})

	t.Run("a fast-failing dev command settles failed", func(t *testing.T) {
		previewShAvailable(t)
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		writePreviewManifest(t, root, "exit 1", port)

		body := previewAction(t, ws, ws.handleAPIPreviewStart, "/api/preview/start", http.StatusOK)
		if body.Status != string(preview.StatusStarting) {
			t.Fatalf("expected starting, got %+v", body)
		}

		body = waitForPreviewStatus(t, ws, preview.StatusFailed)
		if body.Error == "" {
			t.Error("a failed start must carry a reason")
		}
	})
}

func TestHandleAPIPreviewRestart(t *testing.T) {
	t.Run("non-POST returns 405", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/api/preview/restart", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIPreviewRestart(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("no manifest is refused with 404", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		rec := previewInvoke(t, ws, ws.handleAPIPreviewRestart, "/api/preview/restart")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "no_dev_command")
	})

	t.Run("restart of an undetected running server is a no-op report", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		localServerOnPort(t, port)
		writePreviewManifest(t, root, "npm run dev", port)

		// Detect first.
		body := previewAction(t, ws, ws.handleAPIPreviewStart, "/api/preview/start", http.StatusOK)
		if body.Status != string(preview.StatusRunning) || !body.Detected {
			t.Fatalf("expected running detected, got %+v", body)
		}

		// Restart leaves the external server alone and reports running.
		body = previewAction(t, ws, ws.handleAPIPreviewRestart, "/api/preview/restart", http.StatusOK)
		if body.Status != string(preview.StatusRunning) || !body.Detected {
			t.Fatalf("a restart of an external server must report running detected, got %+v", body)
		}
	})

	t.Run("restart of an absent server settles from the start cycle", func(t *testing.T) {
		previewShAvailable(t)
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		writePreviewManifest(t, root, "exit 1", port)

		body := previewAction(t, ws, ws.handleAPIPreviewRestart, "/api/preview/restart", http.StatusOK)
		if body.Status != string(preview.StatusStarting) {
			t.Fatalf("expected starting, got %+v", body)
		}

		body = waitForPreviewStatus(t, ws, preview.StatusFailed)
		if body.Error == "" {
			t.Error("a failed (re)start must carry a reason")
		}
	})
}

func TestHandleAPIPreviewStop(t *testing.T) {
	t.Run("non-POST returns 405", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/api/preview/stop", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIPreviewStop(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("stop on a fresh manager reports stopped", func(t *testing.T) {
		ws, _ := newPreviewTestServer(t)
		body := previewAction(t, ws, ws.handleAPIPreviewStop, "/api/preview/stop", http.StatusOK)
		if body.Status != string(preview.StatusStopped) {
			t.Fatalf("expected stopped, got %+v", body)
		}
	})

	t.Run("stop leaves a detected external server alone", func(t *testing.T) {
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		localServerOnPort(t, port)
		writePreviewManifest(t, root, "npm run dev", port)

		previewAction(t, ws, ws.handleAPIPreviewStart, "/api/preview/start", http.StatusOK)

		body := previewAction(t, ws, ws.handleAPIPreviewStop, "/api/preview/stop", http.StatusOK)
		if body.Status != string(preview.StatusRunning) {
			t.Fatalf("a detected external server is not stopped by Stop, got %+v", body)
		}
		if !body.Detected {
			t.Error("expected detected=true")
		}
	})

	t.Run("stop after a failed start reports stopped", func(t *testing.T) {
		previewShAvailable(t)
		ws, root := newPreviewTestServer(t)
		port := freePreviewPort(t)
		writePreviewManifest(t, root, "exit 1", port)

		previewAction(t, ws, ws.handleAPIPreviewStart, "/api/preview/start", http.StatusOK)
		waitForPreviewStatus(t, ws, preview.StatusFailed)

		body := previewAction(t, ws, ws.handleAPIPreviewStop, "/api/preview/stop", http.StatusOK)
		if body.Status != string(preview.StatusStopped) {
			t.Fatalf("expected stopped after stop, got %+v", body)
		}
	})
}

// TestPreviewRoutesRegistered verifies the endpoints exist on the real mux
// (a missing registration is the classic silent preview breakage).
func TestPreviewRoutesRegistered(t *testing.T) {
	ws, _ := newPreviewTestServer(t)
	mux := ws.setupRoutes(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/api/preview/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/preview/status expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/preview/start", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /api/preview/start without a manifest expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/preview/restart", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /api/preview/restart without a manifest expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/preview/stop", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/preview/stop expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
