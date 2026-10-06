package runner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func fakeDaemon(t *testing.T) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte(r.Method + " " + r.URL.Path + " auth=" + r.Header.Get("Authorization") + " body=" + string(body))) //nolint:gosec // G705: test echo server
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

func hostCall(t *testing.T, h *HostServer, path, bearer, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	h.Handler().ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

func TestHostServerProxiesOnlyWithTheWorkspaceSecret(t *testing.T) {
	h := NewHostServer()
	h.Bind("ws-1", fakeDaemon(t), "s3cret")

	code, body := hostCall(t, h, "/daemon/ws-1/api/txn/run", "s3cret", `{"command":"make"}`)
	if code != http.StatusOK {
		t.Fatalf("authorized call: got %d %s", code, body)
	}
	if !strings.Contains(body, "POST /api/txn/run") || !strings.Contains(body, `body={"command":"make"}`) {
		t.Errorf("daemon got the wrong request: %s", body)
	}
	if !strings.Contains(body, "auth=Bearer s3cret") {
		t.Errorf("the daemon checks the same secret, so it must be forwarded: %s", body)
	}

	for name, bearer := range map[string]string{"none": "", "wrong": "other", "prefix": "s3cre"} {
		if code, _ := hostCall(t, h, "/daemon/ws-1/api/txn/run", bearer, ""); code != http.StatusForbidden {
			t.Errorf("%s bearer: got %d, want 403", name, code)
		}
	}
	if code, _ := hostCall(t, h, "/daemon/ws-2/api/txn/run", "s3cret", ""); code != http.StatusNotFound {
		t.Errorf("unknown workspace: got %d, want 404", code)
	}
	if code, _ := hostCall(t, h, "/daemon/ws-1", "s3cret", ""); code != http.StatusNotFound {
		t.Errorf("no daemon path: got %d, want 404", code)
	}

	h.Unbind("ws-1")
	if code, _ := hostCall(t, h, "/daemon/ws-1/api/txn/run", "s3cret", ""); code != http.StatusNotFound {
		t.Errorf("after unbind: got %d, want 404", code)
	}
}

func TestHostServerRejectsBindingsWithoutASecret(t *testing.T) {
	h := NewHostServer()
	h.Bind("ws-1", fakeDaemon(t), "")
	if code, _ := hostCall(t, h, "/daemon/ws-1/api/txn/run", "anything", ""); code != http.StatusForbidden {
		t.Errorf("a binding with no secret must reject every call, got %d", code)
	}
}

func TestHostServerUnreachableDaemonIs502(t *testing.T) {
	h := NewHostServer()
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	h.Bind("ws-1", port, "s3cret")
	if code, _ := hostCall(t, h, "/daemon/ws-1/api/txn/status", "s3cret", ""); code != http.StatusBadGateway {
		t.Errorf("got %d, want 502", code)
	}
}
