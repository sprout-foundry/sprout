package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/runner/sandbox"
)

// TestHostLauncherEndToEnd runs a real workspace daemon on this machine and
// drives the txn protocol through the host server, the way the platform
// does. It needs a built sprout binary:
//
//	SPROUT_RUNNER_E2E_BIN=/path/to/sprout go test -run EndToEnd ./pkg/runner/
func TestHostLauncherEndToEnd(t *testing.T) {
	bin := os.Getenv("SPROUT_RUNNER_E2E_BIN")
	if bin == "" {
		t.Skip("set SPROUT_RUNNER_E2E_BIN to a built sprout binary")
	}
	for _, tc := range []struct {
		name      string
		sandboxed bool
	}{{"bare-metal", false}, {"native", true}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sandboxed && !sandbox.Detect().Available {
				t.Skip("no OS sandbox here")
			}
			runHostE2E(t, bin, tc.sandboxed)
		})
	}
}

func runHostE2E(t *testing.T, bin string, sandboxed bool) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	allowFileRepos = true
	t.Cleanup(func() { allowFileRepos = false })
	origin := filepath.Join(t.TempDir(), "origin")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", origin},
		{"-C", origin, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: test fixture repo
			t.Fatalf("git %v: %s", args, out)
		}
	}

	l := &HostLauncher{Sandboxed: sandboxed, SproutBin: bin}
	task := WorkspaceTask{WorkspaceID: "e2e-ws", Action: "start", RepoURL: "file://" + origin, TxnSecret: "e2e-secret"}
	ctx := context.Background()
	ws, err := l.Start(ctx, task)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = l.Destroy(ctx, task.WorkspaceID, ws) })

	host := NewHostServer()
	host.Bind(ws.ID, ws.Port, task.TxnSecret)
	srv := httptest.NewServer(host.Handler())
	t.Cleanup(srv.Close)

	run := func(bearer, command string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/daemon/e2e-ws/api/txn/run",
			strings.NewReader(`{"command":`+mustJSON(command)+`,"timeout_seconds":30}`))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}

	code, body := run("e2e-secret", "pwd && echo hello > made.txt && git log --oneline | wc -l")
	if code != http.StatusOK {
		t.Fatalf("txn run: %d %v", code, body)
	}
	if out, _ := body["stdout"].(string); !strings.Contains(out, filepath.Join("e2e-ws", "repo")) {
		t.Errorf("command must run in the workspace clone; stdout %q", out)
	}
	if code, _ := run("wrong", "true"); code != http.StatusForbidden {
		t.Errorf("wrong bearer through the host server: %d", code)
	}

	// The daemon enforces the same secret itself.
	direct, err := http.Post("http://127.0.0.1:"+strconv.Itoa(ws.Port)+"/api/txn/run", "application/json", strings.NewReader(`{"command":"true"}`))
	if err != nil {
		t.Fatalf("calling the daemon directly: %v", err)
	}
	if direct != nil {
		_ = direct.Body.Close()
		if direct.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated call straight to the daemon: %d, want 401", direct.StatusCode)
		}
	}

	if sandboxed {
		home, _ := os.UserHomeDir()
		outside := filepath.Join(home, ".sprout-runner-e2e-escape")
		_, body := run("e2e-secret", "touch "+outside+"; echo exit=$?")
		if _, err := os.Stat(outside); err == nil {
			_ = os.Remove(outside)
			t.Errorf("native mode let a command write outside the workspace: %v", body)
		}
	}
}

// TestHostLauncherLocalDirectoryEndToEnd runs a workspace daemon against a
// plain (non-git) local directory and proves the txn protocol works in place,
// the way a bare-metal runner serves the user's real checkout. Same gates as
// the clone-based test above.
func TestHostLauncherLocalDirectoryEndToEnd(t *testing.T) {
	bin := os.Getenv("SPROUT_RUNNER_E2E_BIN")
	if bin == "" {
		t.Skip("set SPROUT_RUNNER_E2E_BIN to a built sprout binary")
	}
	for _, tc := range []struct {
		name      string
		sandboxed bool
	}{{"bare-metal", false}, {"native", true}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sandboxed && !sandbox.Detect().Available {
				t.Skip("no OS sandbox here")
			}
			runLocalDirE2E(t, bin, tc.sandboxed)
		})
	}
}

func runLocalDirE2E(t *testing.T, bin string, sandboxed bool) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())

	local := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "hello.txt"), []byte("local workspace\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	l := &HostLauncher{SproutBin: bin, Sandboxed: sandboxed}
	task := WorkspaceTask{WorkspaceID: "e2e-local", Action: "start", WorkspaceDir: local, TxnSecret: "e2e-secret"}
	ctx := context.Background()
	ws, err := l.Start(ctx, task)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = l.Destroy(ctx, task.WorkspaceID, ws) })

	host := NewHostServer()
	host.Bind(ws.ID, ws.Port, task.TxnSecret)
	srv := httptest.NewServer(host.Handler())
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/daemon/e2e-local/api/txn/run",
		strings.NewReader(`{"command":"pwd && cat hello.txt && echo ran > made.txt","timeout_seconds":30}`))
	req.Header.Set("Authorization", "Bearer e2e-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("txn run in a local directory: %d %v", resp.StatusCode, body)
	}
	out, _ := body["stdout"].(string)
	if !strings.Contains(out, "myproject") || !strings.Contains(out, "local workspace") {
		t.Errorf("command must run in the user's directory; stdout %q", out)
	}
	if _, err := os.Stat(filepath.Join(local, "made.txt")); err != nil {
		t.Errorf("the command's writes must land in the real directory: %v", err)
	}

	if sandboxed {
		home, _ := os.UserHomeDir()
		outside := filepath.Join(home, ".sprout-runner-e2e-escape")
		escape := func() bool {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/daemon/e2e-local/api/txn/run",
				strings.NewReader(`{"command":"touch `+outside+`","timeout_seconds":15}`))
			req.Header.Set("Authorization", "Bearer e2e-secret")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			_, statErr := os.Stat(outside)
			return statErr == nil
		}
		if escape() {
			_ = os.Remove(outside)
			t.Error("native mode let a command write outside the local workspace and its metadata dir")
		}
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
