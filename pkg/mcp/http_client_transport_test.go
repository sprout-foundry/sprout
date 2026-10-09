package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
