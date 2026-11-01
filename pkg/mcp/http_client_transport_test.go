package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Streamable HTTP transport conformance
// ---------------------------------------------------------------------------

// A server that answers initialize + notification + tools/list as SSE —
// the response shape spec-compliant servers may use and that the previous
// client could not parse at all.
func TestMCPHTTPClient_SSEResponseParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json, text/event-stream" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		var reqBody struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		if reqBody.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		result := map[string]interface{}{}
		if reqBody.Method == "tools/list" {
			result["tools"] = []map[string]interface{}{
				{"name": "sse_tool", "description": "from an SSE stream", "inputSchema": map[string]interface{}{"type": "object"}},
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": keepalive\n\n"))
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte("data: " + mustJSON(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody.ID,
			"result":  result,
		}) + "\n\n"))
	}))
	defer server.Close()

	client := NewMCPHTTPClient(MCPServerConfig{Name: "sse", Type: "http", URL: server.URL}, NewTestLogger())
	ctx := context.Background()
	require.NoError(t, client.Start(ctx))

	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "sse_tool", tools[0].Name)
}

// 401 must produce a diagnostic that names the auth paths, not a bare status.
func TestMCPHTTPClient_401NamesAuthCauses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	client := NewMCPHTTPClient(MCPServerConfig{Name: "locked", Type: "http", URL: server.URL}, NewTestLogger())
	ctx := context.Background()
	require.NoError(t, client.Start(ctx))

	err := client.Initialize(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "OAuth")
}

// ---------------------------------------------------------------------------
// Session expiry recovery
// ---------------------------------------------------------------------------

// A server that ends the session it issued (restart, idle reaping) answers
// 404 to a request carrying Mcp-Session-Id. The client must re-initialize
// once and replay the request instead of surfacing the 404.
func TestMCPHTTPClient_SessionExpiryReinitializes(t *testing.T) {
	var (
		mu        sync.Mutex
		sessions  = map[string]bool{} // issued session -> still valid
		issues    int
		initCalls int
		replayed  bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		defer mu.Unlock()

		if reqBody.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		if reqBody.Method == "initialize" {
			initCalls++
			issues++
			sid := fmt.Sprintf("session-%d", issues)
			sessions[sid] = true
			w.Header().Set("Mcp-Session-Id", sid)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(mustJSON(map[string]interface{}{
				"jsonrpc": "2.0", "id": reqBody.ID, "result": map[string]interface{}{},
			})))
			return
		}

		// Every non-initialize request must carry a live session.
		sid := r.Header.Get("Mcp-Session-Id")
		if sid == "" || !sessions[sid] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// The first tools/list lands on a session the server just ended
		// (restart, idle reaping): kill it and answer 404. The replay after
		// re-initialize carries a fresh session and succeeds.
		if reqBody.Method == "tools/list" && !replayed {
			replayed = true
			delete(sessions, sid)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		result := map[string]interface{}{}
		if reqBody.Method == "tools/list" {
			result["tools"] = []map[string]interface{}{
				{"name": "after_reinit", "description": "listed after re-initialize", "inputSchema": map[string]interface{}{"type": "object"}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mustJSON(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqBody.ID, "result": result,
		})))
	}))
	defer server.Close()

	client := NewMCPHTTPClient(MCPServerConfig{Name: "expiring", Type: "http", URL: server.URL}, NewTestLogger())
	ctx := context.Background()
	require.NoError(t, client.Start(ctx))

	tools, err := client.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "after_reinit", tools[0].Name)
	assert.Equal(t, 2, initCalls, "the client should re-initialize exactly once")

	client.mu.RLock()
	assert.Equal(t, "session-2", client.sessionID)
	client.mu.RUnlock()
}

// A 404 on a request that carried no session is a wrong endpoint, not an
// expired session: it must not trigger a re-initialize loop.
func TestMCPHTTPClient_404WithoutSessionIsNotRetried(t *testing.T) {
	var initCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			Method string `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		if reqBody.Method == "initialize" {
			initCalls++
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewMCPHTTPClient(MCPServerConfig{Name: "wrong-path", Type: "http", URL: server.URL}, NewTestLogger())
	ctx := context.Background()
	require.NoError(t, client.Start(ctx))

	err := client.Initialize(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Equal(t, 1, initCalls, "a 404 on initialize must not be retried")
}

// ---------------------------------------------------------------------------
// SSE resumption
// ---------------------------------------------------------------------------

// The id of the last consumed SSE event is recorded so a broken stream can be
// resumed, but it must not be attached to unrelated requests: Last-Event-ID
// names a position in one logical stream.
func TestMCPHTTPClient_SSEEventIDRecordedNotLeaked(t *testing.T) {
	var (
		mu       sync.Mutex
		lastSeen []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		lastSeen = append(lastSeen, r.Header.Get("Last-Event-ID"))
		mu.Unlock()

		if reqBody.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: evt-42\n"))
		_, _ = w.Write([]byte("data: " + mustJSON(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqBody.ID,
			"result": map[string]interface{}{"tools": []interface{}{}},
		}) + "\n\n"))
	}))
	defer server.Close()

	client := NewMCPHTTPClient(MCPServerConfig{Name: "resumable", Type: "http", URL: server.URL}, NewTestLogger())
	ctx := context.Background()
	require.NoError(t, client.Start(ctx))

	_, err := client.ListTools(ctx)
	require.NoError(t, err)

	client.mu.RLock()
	assert.Equal(t, "evt-42", client.lastEventID, "the consumed event id is recorded for resumption")
	client.mu.RUnlock()

	// A second, unrelated request must not carry the previous stream's id.
	_, err = client.ListTools(ctx)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, lastSeen)
	assert.Empty(t, lastSeen[len(lastSeen)-1],
		"Last-Event-ID must not leak onto an unrelated request")
} // A tools/call must NOT be replayed after a session expiry: the server may
// already have executed it, and a replay would perform the side effect twice.
func TestMCPHTTPClient_SessionExpiryDoesNotReplayToolCall(t *testing.T) {
	var (
		mu       sync.Mutex
		issues   int
		sessions = map[string]bool{}
		calls    int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		defer mu.Unlock()

		if reqBody.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if reqBody.Method == "initialize" {
			issues++
			sid := fmt.Sprintf("session-%d", issues)
			sessions[sid] = true
			w.Header().Set("Mcp-Session-Id", sid)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(mustJSON(map[string]interface{}{
				"jsonrpc": "2.0", "id": reqBody.ID, "result": map[string]interface{}{},
			})))
			return
		}

		if reqBody.Method == "tools/call" {
			calls++
			delete(sessions, r.Header.Get("Mcp-Session-Id"))
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mustJSON(map[string]interface{}{
			"jsonrpc": "2.0", "id": reqBody.ID, "result": map[string]interface{}{},
		})))
	}))
	defer server.Close()

	client := NewMCPHTTPClient(MCPServerConfig{Name: "payments", Type: "http", URL: server.URL}, NewTestLogger())
	ctx := context.Background()
	require.NoError(t, client.Start(ctx))
	require.NoError(t, client.Initialize(ctx))

	_, err := client.CallTool(ctx, MCPToolCallRequest{Name: "create_charge", Arguments: map[string]interface{}{}})
	require.Error(t, err)
	assert.ErrorIs(t, err, errMCPSessionExpired)
	assert.Contains(t, err.Error(), "not safe to retry")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, calls, "a tool call must not be replayed")
	assert.Equal(t, 1, issues, "a non-idempotent method must not trigger a re-initialize")
}

// A zero configured timeout must not become net/http's "no timeout" — a hung
// server would then wedge the call forever.
func TestNewMCPHTTPClient_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	client := NewMCPHTTPClient(MCPServerConfig{Name: "svc", Type: "http", URL: "https://example.com/mcp"}, NewTestLogger())
	assert.Equal(t, DefaultServerTimeout, client.httpClient.Timeout)
}

// ---------------------------------------------------------------------------
// Header resolution
// ---------------------------------------------------------------------------

func TestBuildRequestHeaders_ExplicitHeadersWin(t *testing.T) {
	config := &MCPServerConfig{
		Name: "svc",
		Type: "http",
		URL:  "https://example.com/mcp",
		Headers: map[string]string{
			"Authorization": "Bearer explicit",
			"X-Custom":      "always",
		},
		Credentials: map[string]string{
			"AUTHORIZATION": SecretRef("svc", "AUTHORIZATION"),
		},
		Env: map[string]string{},
	}
	require.NoError(t, credentialsSetForTest("mcp/svc/AUTHORIZATION", "Bearer from-credential"))

	headers, err := buildRequestHeaders("svc", config)
	require.NoError(t, err)
	assert.Equal(t, "Bearer explicit", headers["Authorization"])
	assert.Equal(t, "always", headers["X-Custom"])
}

func TestBuildRequestHeaders_TokenShapedCredentialBecomesBearer(t *testing.T) {
	config := &MCPServerConfig{
		Name: "svc",
		Type: "http",
		Credentials: map[string]string{
			"FIGMA_TOKEN": SecretRef("svc", "FIGMA_TOKEN"),
		},
		Env: map[string]string{},
	}
	require.NoError(t, credentialsSetForTest("mcp/svc/FIGMA_TOKEN", "fig-pat-123"))

	headers, err := buildRequestHeaders("svc", config)
	require.NoError(t, err)
	assert.Equal(t, "Bearer fig-pat-123", headers["Authorization"])
}

func TestBuildRequestHeaders_HyphenatedCredentialBecomesHeader(t *testing.T) {
	config := &MCPServerConfig{
		Name: "svc",
		Type: "http",
		Credentials: map[string]string{
			"X-Figma-Token": SecretRef("svc", "X-Figma-Token"),
		},
		Env: map[string]string{},
	}
	require.NoError(t, credentialsSetForTest("mcp/svc/X-Figma-Token", "fig-header-token"))

	headers, err := buildRequestHeaders("svc", config)
	require.NoError(t, err)
	assert.Equal(t, "fig-header-token", headers["X-Figma-Token"])
}

func TestBuildRequestHeaders_HeaderPlaceholderResolves(t *testing.T) {
	config := &MCPServerConfig{
		Name: "svc",
		Type: "http",
		Headers: map[string]string{
			"X-Figma-Token": SecretRef("svc", "X-Figma-Token"),
		},
		Env: map[string]string{},
	}
	require.NoError(t, credentialsSetForTest("mcp/svc/X-Figma-Token", "resolved-value"))

	headers, err := buildRequestHeaders("svc", config)
	require.NoError(t, err)
	assert.Equal(t, "resolved-value", headers["X-Figma-Token"])
}

// ---------------------------------------------------------------------------
// SSE frame parsing (unit)
// ---------------------------------------------------------------------------

func TestParseSSEResponse_SelectsMatchingID(t *testing.T) {
	body := "event: message\n" +
		`data: {"jsonrpc":"2.0","method":"push","params":{}}` + "\n" +
		"\n" +
		`data: {"jsonrpc":"2.0","id":7,"result":{"ok":true}}` + "\n\n"

	msg, err := parseSSEResponse([]byte(body), 7)
	require.NoError(t, err)
	assert.NotNil(t, msg.Result)
}

func TestParseSSEResponse_NoMatch(t *testing.T) {
	body := `data: {"jsonrpc":"2.0","id":99,"result":{}}` + "\n\n"
	_, err := parseSSEResponse([]byte(body), 7)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// OAuth unit pieces
// ---------------------------------------------------------------------------

func TestPKCEChallenge_S256(t *testing.T) {
	verifier := "test-verifier-with-known-length-and-content"
	chall := pkceChallenge(verifier, "S256")
	assert.Len(t, chall, 43) // base64url(SHA-256) is 43 chars
	assert.Equal(t, chall, pkceChallenge(verifier, "S256"))
	assert.Equal(t, verifier, pkceChallenge(verifier, "plain"))
}

func TestNormalizeMCPResourceURL(t *testing.T) {
	assert.Equal(t, "https://mcp.figma.com/mcp", normalizeMCPResourceURL("https://mcp.figma.com/mcp?x=1#frag"))
	assert.Equal(t, "http://127.0.0.1:3845/mcp", normalizeMCPResourceURL("http://127.0.0.1:3845/mcp"))
}

func TestPathSuffixHelpers(t *testing.T) {
	assert.Equal(t, "/mcp", pathSuffix("/mcp"))
	assert.Equal(t, "", pathSuffix(""))
	assert.Equal(t, "", pathSuffix("/"))
	assert.Equal(t, "/a/b", pathSuffixOfIssuer("https://auth.example.com/a/b"))
	assert.Equal(t, "", pathSuffixOfIssuer("https://auth.example.com"))
}

func TestTokenShapedEnvVarDetection(t *testing.T) {
	assert.True(t, looksLikeTokenEnvVar("FIGMA_TOKEN"))
	assert.True(t, looksLikeTokenEnvVar("OPENAI_API_KEY"))
	assert.True(t, looksLikeTokenEnvVar("API_KEY"))
	assert.True(t, looksLikeTokenEnvVar("ACCESS_TOKEN"))
	assert.False(t, looksLikeTokenEnvVar("HOME"))
	assert.False(t, looksLikeTokenEnvVar("DATABASE_URL"))
}

func TestHeaderNameNormalization(t *testing.T) {
	assert.Equal(t, "X-Api-Key", normalizeHeaderName("X_API_KEY"))
	assert.Equal(t, "X-Figma-Token", normalizeHeaderName("X-Figma-Token"))
	assert.Equal(t, "X-Auth-Token", normalizeHeaderName("x-auth_token"))
}

func TestOAuthStatus_NotLoggedIn(t *testing.T) {
	loggedIn, detail := OAuthStatus("no-such-server-oauth")
	assert.False(t, loggedIn)
	assert.Contains(t, detail, "not logged in")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// credentialsSetForTest writes to the active credential backend (the same
// path production resolution reads).
func credentialsSetForTest(key, value string) error {
	return credentials.SetToActiveBackend(key, value)
}
