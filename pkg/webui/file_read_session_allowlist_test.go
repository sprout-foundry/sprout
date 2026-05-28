//go:build !js

package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestHandleFileRead_SessionAllowlistBypassesTokenCheck verifies the
// filesystem-perms unification: when a path is on the active agent's
// session-allowed folder list, the WebUI file API serves the file
// WITHOUT requiring a consent token. This is what makes browser file
// opens consistent with agent read_file calls — one approval covers
// both surfaces.
func TestHandleFileRead_SessionAllowlistBypassesTokenCheck(t *testing.T) {
	workspaceRoot := t.TempDir()
	externalDir := t.TempDir() // outside workspaceRoot
	externalFile := filepath.Join(externalDir, "audit.log")
	if err := os.WriteFile(externalFile, []byte("hello from outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	ctx.WorkspaceRoot = workspaceRoot

	// Without the allowlist, the request must be rejected with the
	// external_path_consent_required code (baseline).
	req := httptest.NewRequest(http.MethodGet, "/api/file?path="+externalFile, nil)
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec := httptest.NewRecorder()
	ws.handleAPIFile(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("baseline (no consent, no allowlist): expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Now seed the active chat agent's allowlist with the file's
	// parent directory. The same request should now succeed.
	chat := ctx.getOrCreateChatSession("default")
	chat.Agent.AddSessionAllowedFolder(externalDir)

	req = httptest.NewRequest(http.MethodGet, "/api/file?path="+externalFile, nil)
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPIFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("after allowlisting parent dir: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "hello from outside" {
		t.Errorf("file content mismatch: got %q", got)
	}

	// And a different external file outside the allowlisted folder
	// MUST still be rejected — allowlist scope is per-folder, not
	// session-wide.
	otherDir := t.TempDir()
	otherFile := filepath.Join(otherDir, "secret.txt")
	if err := os.WriteFile(otherFile, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/file?path="+otherFile, nil)
	req.Header.Set("X-Sprout-Client-ID", clientID)
	rec = httptest.NewRecorder()
	ws.handleAPIFile(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-allowlisted external file: expected 403, got %d body=%s (allowlist scope is leaking!)", rec.Code, rec.Body.String())
	}
}

// TestHandleFileRead_AllowlistAndTokenAreEitherOr confirms the
// allowlist check doesn't BREAK the legacy 2-minute token flow. A
// token-issued request must still work for paths NOT on the allowlist.
func TestHandleFileRead_AllowlistAndTokenAreEitherOr(t *testing.T) {
	workspaceRoot := t.TempDir()
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "data.txt")
	if err := os.WriteFile(externalFile, []byte("token-allowed"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	clientID := "test-client"
	ctx := ws.getOrCreateClientContext(clientID)
	ctx.WorkspaceRoot = workspaceRoot

	// Issue a consent token directly via the per-client manager.
	token, _, err := ctx.FileConsents.issue(externalFile, "read", 2*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/file?path="+externalFile, nil)
	req.Header.Set("X-Sprout-Client-ID", clientID)
	req.Header.Set(consentTokenHeader, token)
	rec := httptest.NewRecorder()
	ws.handleAPIFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token-issued request expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "token-allowed" {
		t.Errorf("file content mismatch: got %q", got)
	}
}

// Make sure the t.TempDir paths actually qualify as outside-workspace
// (TempDir on some platforms can be a symlinked /tmp -> /private/tmp).
// If this guard fires, the test was no-op rather than catching the
// case we care about.
func TestExternalPathFixturesAreOutsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	ext := t.TempDir()
	if isWithinWorkspace(filepath.Clean(ext), filepath.Clean(ws)) {
		t.Fatalf("test precondition broken: %q reported as inside workspace %q", ext, ws)
	}
	_ = fmt.Sprintf("noop: %s %s", ws, ext) // keep fmt import if other tests get trimmed
}
