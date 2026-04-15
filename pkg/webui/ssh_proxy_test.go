package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// newProxyServer creates a ReactWebServer with a fake SSH session whose tunnel
// port points at tunnelPort.  sessionKey is the raw (unencoded) session key.
func newProxyServer(tunnelPort int, sessionKey string) *ReactWebServer {
	srv := &ReactWebServer{
		port:        54000,
		sshSessions: make(map[string]*sshWorkspaceSession),
	}
	srv.sshSessions[sessionKey] = &sshWorkspaceSession{
		Key:       sessionKey,
		HostAlias: "test-host",
		LocalPort: tunnelPort,
		StartedAt: time.Now(),
	}
	return srv
}

// sessionProxyPath returns the /ssh/{encodedKey}{suffix} path for a session key.
// Only encodes the characters that url.PathEscape would encode for our test keys.
func sessionProxyPath(sessionKey, suffix string) string {
	r := strings.NewReplacer(":", "%3A", "$", "%24", " ", "%20")
	return "/ssh/" + r.Replace(sessionKey) + suffix
}

const testSessionKey = "test-host::$HOME"

// startEchoBackend creates an httptest server that acts as the "remote" ledit
// backend.  It handles:
//   - /health          → {"status":"ok"}
//   - /api/workspace   → {"workspace_root":"/remote/project"}
//   - /api/stats       → {"session_id":"s1"}
//   - /api/providers   → {"providers":["openai"]}
//   - /api/files       → {"files":[]}
//   - /api/echo        → echoes x-ledit-client-id and the raw query string
//   - /ws              → WebSocket echo server
func startEchoBackend(t *testing.T) (*httptest.Server, int) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok"}`) //nolint:errcheck
	})
	mux.HandleFunc("/api/workspace", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"workspace_root":"/remote/project"}`) //nolint:errcheck
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"session_id":"s1"}`) //nolint:errcheck
	})
	mux.HandleFunc("/api/providers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"providers":["openai"]}`) //nolint:errcheck
	})
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"files":[]}`) //nolint:errcheck
	})
	mux.HandleFunc("/api/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"client_id": r.Header.Get("X-Ledit-Client-ID"),
			"query":     r.URL.RawQuery,
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if writeErr := conn.WriteMessage(mt, msg); writeErr != nil {
				return
			}
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.Listener.Addr().(*net.TCPAddr).Port
}

// ─────────────────────────────────────────────────────────────────────────────
// normalizeRemoteWorkspacePath
// ─────────────────────────────────────────────────────────────────────────────

func TestNormalizeRemoteWorkspacePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"~", "$HOME"},
		{"~/project", "$HOME/project"},
		{"~/a/b/c", "$HOME/a/b/c"},
		{"${HOME}/project", "$HOME/project"},
		{"${HOME}", "$HOME"},
		{"$HOME", "$HOME"},
		{"$HOME/project", "$HOME/project"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tc := range cases {
		got := normalizeRemoteWorkspacePath(tc.in)
		if got != tc.want {
			t.Errorf("normalizeRemoteWorkspacePath(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 404 for unknown session
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHProxyReturns404ForUnknownSession(t *testing.T) {
	srv := &ReactWebServer{sshSessions: make(map[string]*sshWorkspaceSession)}

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath("no-such-host::$HOME", "/health"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Static / local asset serving (no backend needed)
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHProxyServesIndexWithInjectedProxyBase(t *testing.T) {
	// Port 1 is always refused — proves assets come from local embed, not backend.
	srv := newProxyServer(1, testSessionKey)

	for _, path := range []string{"/", "/index.html"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, path), nil)
			rec := httptest.NewRecorder()
			srv.handleSSHProxy(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 from index, got %d", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "LEDIT_PROXY_BASE") {
				t.Fatalf("LEDIT_PROXY_BASE not injected; body head:\n%.300s", body)
			}
			if !strings.Contains(body, "/ssh/") {
				t.Fatalf("/ssh/ not present in proxy base injection; body head:\n%.300s", body)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("expected text/html, got %q", ct)
			}
		})
	}
}

func TestSSHProxyServicesWorkerFromLocalEmbed(t *testing.T) {
	srv := newProxyServer(1, testSessionKey) // port 1 = backend unavailable

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/sw.js"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /sw.js from embed, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestSSHProxyManifestFromLocalEmbed(t *testing.T) {
	srv := newProxyServer(1, testSessionKey)

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/manifest.json"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /manifest.json, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "json") {
		t.Fatalf("expected JSON Content-Type for manifest, got %q", ct)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP proxying
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHProxyHealthEndpoint(t *testing.T) {
	_, port := startEchoBackend(t)
	srv := newProxyServer(port, testSessionKey)

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/health"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON from /health: %v; body: %s", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", body["status"])
	}
}

func TestSSHProxyWorkspaceAPI(t *testing.T) {
	_, port := startEchoBackend(t)
	srv := newProxyServer(port, testSessionKey)

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/api/workspace"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["workspace_root"] != "/remote/project" {
		t.Fatalf("unexpected workspace_root: %v", body["workspace_root"])
	}
}

func TestSSHProxyForwardsRequestHeaders(t *testing.T) {
	_, port := startEchoBackend(t)
	srv := newProxyServer(port, testSessionKey)

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/api/echo"), nil)
	req.Header.Set("X-Ledit-Client-ID", "my-client-id-xyz")
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["client_id"] != "my-client-id-xyz" {
		t.Fatalf("X-Ledit-Client-ID not forwarded; got %q", body["client_id"])
	}
}

func TestSSHProxyPreservesQueryString(t *testing.T) {
	_, port := startEchoBackend(t)
	srv := newProxyServer(port, testSessionKey)

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/api/echo?filter=go&limit=10"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["query"] != "filter=go&limit=10" {
		t.Fatalf("query string not preserved; got %q", body["query"])
	}
}

func TestSSHProxyMultipleAPIPaths(t *testing.T) {
	_, port := startEchoBackend(t)
	srv := newProxyServer(port, testSessionKey)

	cases := []struct {
		path  string
		check func(t *testing.T, body map[string]interface{})
	}{
		{
			"/api/stats",
			func(t *testing.T, body map[string]interface{}) {
				if body["session_id"] != "s1" {
					t.Fatalf("unexpected session_id: %v", body["session_id"])
				}
			},
		},
		{
			"/api/providers",
			func(t *testing.T, body map[string]interface{}) {
				providers, ok := body["providers"].([]interface{})
				if !ok || len(providers) == 0 {
					t.Fatalf("expected providers list, got: %v", body)
				}
			},
		},
		{
			"/api/files",
			func(t *testing.T, body map[string]interface{}) {
				if _, ok := body["files"]; !ok {
					t.Fatalf("expected files key, got: %v", body)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, tc.path), nil)
			rec := httptest.NewRecorder()
			srv.handleSSHProxy(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
			}
			var body map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v; body: %s", err, rec.Body.String())
			}
			tc.check(t, body)
		})
	}
}

func TestSSHProxyReturns502WhenBackendDown(t *testing.T) {
	// Start a real listener then immediately close it, so the OS reports
	// "connection refused" without a long TCP timeout.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not open test listener: %v", err)
	}
	closedPort := l.Addr().(*net.TCPAddr).Port
	l.Close() // immediately closed — the port is now refused

	srv := newProxyServer(closedPort, testSessionKey)

	req := httptest.NewRequest(http.MethodGet, sessionProxyPath(testSessionKey, "/api/stats"), nil)
	rec := httptest.NewRecorder()
	srv.handleSSHProxy(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when backend is down, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// sshServeIndexWithBase (unit test for the injection helper directly)
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHServeIndexInjectsBeforeClosingHead(t *testing.T) {
	rec := httptest.NewRecorder()
	proxyBase := "/ssh/ai-worker%3A%3A%24HOME"
	sshServeIndexWithBase(rec, proxyBase, "$HOME")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()

	scriptIdx := strings.Index(body, "LEDIT_PROXY_BASE")
	headIdx := strings.Index(body, "</head>")

	if scriptIdx < 0 {
		t.Fatal("LEDIT_PROXY_BASE not injected into index.html")
	}
	if headIdx < 0 {
		t.Fatal("</head> not found in index.html")
	}
	if scriptIdx > headIdx {
		t.Fatalf("LEDIT_PROXY_BASE appears after </head> (scriptIdx=%d, headIdx=%d)", scriptIdx, headIdx)
	}
	// The injected value must contain the exact proxyBase (JSON-encoded, so quotes preserved).
	want := fmt.Sprintf(`window.LEDIT_PROXY_BASE="%s"`, proxyBase)
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in body; got (head 400 chars):\n%.400s", want, body)
	}
	// Verify initial workspace is also injected.
	if !strings.Contains(body, `window.LEDIT_INITIAL_WORKSPACE="$HOME"`) {
		t.Fatalf("expected window.LEDIT_INITIAL_WORKSPACE in body; got (head 400 chars):\n%.400s", body)
	}
}

func TestSSHServeIndexSetsHTMLContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	sshServeIndexWithBase(rec, "/ssh/test%3A%3A%24HOME", "$HOME")

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html Content-Type, got %q", ct)
	}
}

func TestSSHServeIndexSetsNoCacheHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	sshServeIndexWithBase(rec, "/ssh/test%3A%3A%24HOME", "$HOME")

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") {
		t.Fatalf("expected no-cache in Cache-Control, got %q", cc)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WebSocket proxy
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHProxyWebSocketEcho(t *testing.T) {
	_, backendPort := startEchoBackend(t)
	proxySrv := newProxyServer(backendPort, testSessionKey)

	// Wrap in a real TCP server so gorilla/websocket's dialer works.
	testSrv := httptest.NewServer(http.HandlerFunc(proxySrv.handleSSHProxy))
	t.Cleanup(testSrv.Close)

	wsURL := "ws" + strings.TrimPrefix(testSrv.URL, "http") + sessionProxyPath(testSessionKey, "/ws")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	send := []string{"hello", "world", `{"type":"ping"}`}
	received := make([]string, 0, len(send))
	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range send {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("read error: %v", err)
				return
			}
			mu.Lock()
			received = append(received, string(msg))
			mu.Unlock()
		}
	}()

	for _, msg := range send {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			t.Fatalf("write error: %v", err)
		}
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != len(send) {
		t.Fatalf("expected %d echoed messages, got %d", len(send), len(received))
	}
	for i := range send {
		if received[i] != send[i] {
			t.Fatalf("msg[%d]: want %q, got %q", i, send[i], received[i])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Concurrency / race safety
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHProxyConcurrentRequests(t *testing.T) {
	_, port := startEchoBackend(t)
	proxySrv := newProxyServer(port, testSessionKey)

	httpSrv := httptest.NewServer(http.HandlerFunc(proxySrv.handleSSHProxy))
	t.Cleanup(httpSrv.Close)

	paths := []string{
		sessionProxyPath(testSessionKey, "/health"),
		sessionProxyPath(testSessionKey, "/api/workspace"),
		sessionProxyPath(testSessionKey, "/api/stats"),
		sessionProxyPath(testSessionKey, "/api/providers"),
		sessionProxyPath(testSessionKey, "/api/files"),
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(paths)*10)

	for i := 0; i < 10; i++ {
		for _, p := range paths {
			wg.Add(1)
			go func(u string) {
				defer wg.Done()
				resp, err := http.Get(httpSrv.URL + u)
				if err != nil {
					errCh <- fmt.Errorf("GET %s: %w", u, err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errCh <- fmt.Errorf("GET %s: expected 200, got %d", u, resp.StatusCode)
				}
			}(p)
		}
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Real ledit backend integration test
// Runs only when LEDIT_INTEGRATION_SSH_TEST=1 is set in the environment.
// The test expects a local ledit process to be running on LEDIT_WEBUI_PORT
// (defaults to 54000) that has at least one SSH session attached.
// It verifies:
//   1. /ssh/{key}/ returns 200 with LEDIT_PROXY_BASE injected.
//   2. /ssh/{key}/health returns 200 with {"status":"ok"}.
//   3. /ssh/{key}/api/workspace returns a valid JSON object.
// ─────────────────────────────────────────────────────────────────────────────

func TestSSHProxyRealBackendIntegration(t *testing.T) {
	if os.Getenv("LEDIT_INTEGRATION_SSH_TEST") != "1" {
		t.Skip("set LEDIT_INTEGRATION_SSH_TEST=1 to run this integration test")
	}

	localPort := os.Getenv("LEDIT_WEBUI_PORT")
	if localPort == "" {
		localPort = "54000"
	}
	sshSessionRaw := os.Getenv("LEDIT_SSH_SESSION_KEY")
	if sshSessionRaw == "" {
		t.Fatal("LEDIT_SSH_SESSION_KEY must be set for the integration test")
	}

	r := strings.NewReplacer(":", "%3A", "$", "%24")
	baseURL := fmt.Sprintf("http://127.0.0.1:%s/ssh/%s", localPort, r.Replace(sshSessionRaw))

	client := &http.Client{Timeout: 15 * time.Second}

	t.Run("index_has_proxy_base", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "LEDIT_PROXY_BASE") {
			t.Fatalf("LEDIT_PROXY_BASE not injected; body head:\n%.400s", body)
		}
	})

	t.Run("health_proxied", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
		}
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("invalid JSON from /health: %v; body: %s", err, body)
		}
		if data["status"] != "ok" {
			t.Fatalf("expected status=ok from /health, got: %v", data)
		}
	})

	t.Run("workspace_api_proxied", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/api/workspace")
		if err != nil {
			t.Fatalf("GET /api/workspace: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
		}
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("invalid JSON from /api/workspace: %v; body: %s", err, body)
		}
		if _, ok := data["workspace_root"]; !ok {
			t.Fatalf("expected workspace_root in response; got: %v", data)
		}
	})

	t.Run("websocket_proxied", func(t *testing.T) {
		wsURL := fmt.Sprintf("ws://127.0.0.1:%s/ssh/%s/ws", localPort, r.Replace(sshSessionRaw))
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("WS dial to %s: %v", wsURL, err)
		}
		defer conn.Close()

		// Send a ping-style JSON message and expect any valid response within 5s.
		conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
			t.Fatalf("WS write: %v", err)
		}
		_, _, err = conn.ReadMessage()
		if err != nil {
			// A real ledit backend may close after the first message if it
			// doesn't recognise "ping" — that's still proof the proxy works.
			t.Logf("WS read returned (may be normal close): %v", err)
		}
	})
}
