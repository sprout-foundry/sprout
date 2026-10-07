//go:build !js

package webui

// Starter API tests: the embedded catalogue
// endpoint and the instantiate endpoint, exercised the same way the
// neighboring api_*_test.go files do their handlers — a real (unserved)
// server, requests through httptest, responses unmarshalled from the
// recorded body.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// newStartersTestServer builds the same minimal server shape the other
// pkg/webui handler tests use (no auth, no agent, no serving). The
// starter handlers are stateless — no client context, no workspace root —
// so no decoration is needed.
func newStartersTestServer(t *testing.T) *ReactWebServer {
	t.Helper()
	const (
		testBind  = "127.0.0.1"
		testToken = ""
	)
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, testBind, "", testToken)
	if err != nil {
		t.Fatal(err)
	}
	// Scope the instantiate endpoint's containment bound (fix.8) to a
	// controlled daemon root, so tests exercise the 403 path with
	// predictable edges instead of the process's real $HOME.
	server.daemonRoot = t.TempDir()
	return server
}

// starterEntryJSON mirrors the wire shape of one GET /api/starters entry;
// unmarshalling against a separate local type so a field rename on the
// handler side fails these tests.
type starterEntryJSON struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Files       int    `json:"files"`
	HasManifest bool   `json:"has_manifest"`
}

type starterListJSON struct {
	Starters []starterEntryJSON `json:"starters"`
}

// starterInstantiateBody renders the POST body for the instantiate
// endpoint (the fixture starter by default); name is "" when unset.
func starterInstantiateBody(starter, path, name string) string {
	return fmt.Sprintf(`{"starter":%q,"path":%q,"name":%q}`, starter, path, name)
}

// jsonError is the writeJSONErr shape: {"error": ..., "code": ...}.
type jsonError struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func jsonErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body jsonError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not {error, code} JSON: %s (%v)", rec.Body.String(), err)
	}
	if body.Code != want {
		t.Fatalf("expected code %q, got %q (message: %s)", want, body.Code, body.Error)
	}
}

func TestHandleAPIStartersList(t *testing.T) {
	t.Run("non-GET returns 405", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/starters", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIStartersList(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("lists the user-facing starters, withholding test-only ones", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/api/starters", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIStartersList(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected application/json, got %q", got)
		}
		var body starterListJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response must carry a starters array: %s (%v)", rec.Body.String(), err)
		}
		// The test-only fixture is withheld from the chooser (fix.9): it is
		// still addressable by id (Instantiate) and in the full catalogue
		// (starters.List), but must never reach the user-facing list. There
		// are no product starters yet, so the list is expected to be empty —
		// the assertion is "fixture absent", not "at least one present".
		for i := range body.Starters {
			if body.Starters[i].ID == "fixture" {
				t.Fatalf("test-only fixture must be withheld from the user-facing list: %s", rec.Body.String())
			}
		}
	})
}

func TestHandleAPIStartersInstantiate(t *testing.T) {
	t.Run("non-POST returns 405", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/api/starters/instantiate", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("instantiates into a fresh directory", func(t *testing.T) {
		ws := newStartersTestServer(t)
		dest := filepath.Join(ws.daemonRoot, "newproj")
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", dest, "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Root     string `json:"root"`
			Starter  string `json:"starter"`
			Files    int    `json:"files"`
			Manifest struct {
				Starter struct {
					ID      string `json:"id"`
					Version string `json:"version"`
				} `json:"starter"`
				Build string `json:"build"`
			} `json:"manifest"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("instantiate response is not JSON: %s (%v)", rec.Body.String(), err)
		}
		if resp.Root == "" {
			t.Error("expected a non-empty root in the response")
		}
		if resp.Starter != "fixture" {
			t.Errorf("expected starter=fixture, got %q", resp.Starter)
		}
		if resp.Files != 3 {
			t.Errorf("expected 3 project-content files, got %d", resp.Files)
		}
		if resp.Manifest.Starter.ID != "fixture" || resp.Manifest.Starter.Version != "0.1.0" {
			t.Errorf("expected the fixture manifest in the response, got %+v", resp.Manifest.Starter)
		}
		if resp.Manifest.Build != "npm run build" {
			t.Errorf("expected the fixture build command, got %q", resp.Manifest.Build)
		}

		// The tree and the manifest were actually written to the target.
		for _, rel := range []string{"README.md", "index.html", filepath.Join("src", "main.js")} {
			if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
				t.Errorf("expected %s in the instantiated project: %v", rel, err)
			}
		}
		m, err := starterstore.LoadStarterManifest(dest)
		if err != nil {
			t.Fatalf("the written manifest must load through pkg/starterstore: %v", err)
		}
		if m.Starter.ID != "fixture" || m.Starter.Version != "0.1.0" {
			t.Errorf("manifest round-trip mismatch: %+v", m.Starter)
		}
	})

	t.Run("unknown starter returns 404", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("no-such-starter", filepath.Join(ws.daemonRoot, "x"), "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "unknown_starter")
	})

	t.Run("path-like starter id is rejected", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("../../evil", filepath.Join(ws.daemonRoot, "x"), "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "invalid_starter_id")
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader("not json"))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		jsonErrorCode(t, rec, "invalid_json")
	})

	t.Run("missing starter returns 400", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(fmt.Sprintf(`{"path":%q}`, filepath.Join(ws.daemonRoot, "x"))))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		jsonErrorCode(t, rec, "starter_required")
	})

	t.Run("missing path returns 400", func(t *testing.T) {
		ws := newStartersTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(`{"starter":"fixture"}`))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		jsonErrorCode(t, rec, "path_required")
	})

	t.Run("relative path is rejected", func(t *testing.T) {
		for _, rel := range []string{"fixture", "../evil"} {
			ws := newStartersTestServer(t)
			req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", rel, "")))
			rec := httptest.NewRecorder()
			ws.handleAPIStartersInstantiate(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("relative path %q: expected 400, got %d: %s", rel, rec.Code, rec.Body.String())
			}
			jsonErrorCode(t, rec, "path_must_be_absolute")
		}
	})

	t.Run("target outside daemon root returns 403", func(t *testing.T) {
		ws := newStartersTestServer(t)
		// A fresh directory that is NOT under ws.daemonRoot: the
		// containment check must refuse it before anything is written.
		outside := t.TempDir()
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", outside, "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for a target outside the daemon root, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "target_outside_daemon_root")
		// Nothing was written to the out-of-bounds target.
		if entries, err := os.ReadDir(outside); err == nil && len(entries) > 0 {
			t.Errorf("expected no writes to the out-of-bounds target, found %d entries", len(entries))
		}
	})

	t.Run("daemon root itself is an allowed target", func(t *testing.T) {
		ws := newStartersTestServer(t)
		// The daemon root itself (or a child of it) is in-bounds.
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", filepath.Join(ws.daemonRoot, "child"), "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for a target under the daemon root, got %d: %s", rec.Code, rec.Body.String())
		}
		if _, err := os.Stat(filepath.Join(ws.daemonRoot, "child", "README.md")); err != nil {
			t.Errorf("expected the tree under the daemon root: %v", err)
		}
	})

	t.Run("absolute traversal is canonicalized, not refused", func(t *testing.T) {
		ws := newStartersTestServer(t)
		base := ws.daemonRoot
		// Clean collapses the ".." segment; the write lands at base/proj.
		target := filepath.Join(base, "x", "..", "proj")
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", target, "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		clean := filepath.Join(base, "proj")
		if _, err := os.Stat(filepath.Join(clean, "README.md")); err != nil {
			t.Errorf("expected the tree at the canonicalized %s: %v", clean, err)
		}
		if _, err := os.Stat(filepath.Join(base, "x")); err == nil {
			t.Error("the unresolved traversal component must not have been created")
		}
	})

	t.Run("non-empty destination is refused and left untouched", func(t *testing.T) {
		ws := newStartersTestServer(t)
		dest := ws.daemonRoot
		existing := filepath.Join(dest, "keep.txt")
		if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", dest, "")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		jsonErrorCode(t, rec, "destination_not_empty")
		if _, err := os.Stat(existing); err != nil {
			t.Errorf("the existing content must survive the refusal: %v", err)
		}
	})

	t.Run("project name is accepted but never used for paths", func(t *testing.T) {
		ws := newStartersTestServer(t)
		dest := filepath.Join(ws.daemonRoot, "named")
		req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", dest, "my app")))
		rec := httptest.NewRecorder()
		ws.handleAPIStartersInstantiate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
			t.Errorf("the tree must land at the given path, not at a name-derived one: %v", err)
		}
	})

	t.Run("path-like project name is rejected", func(t *testing.T) {
		for _, name := range []string{"../../evil", "a/b", ".."} {
			ws := newStartersTestServer(t)
			req := httptest.NewRequest(http.MethodPost, "/api/starters/instantiate", strings.NewReader(starterInstantiateBody("fixture", filepath.Join(ws.daemonRoot, "n"), name)))
			rec := httptest.NewRecorder()
			ws.handleAPIStartersInstantiate(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("name %q: expected 400, got %d: %s", name, rec.Code, rec.Body.String())
			}
			jsonErrorCode(t, rec, "invalid_name")
		}
	})
}
