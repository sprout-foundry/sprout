package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RFC 8707: the client repeats the resource indicator on both grant types so
// an AS that binds tokens to a resource can check the audience on every
// exchange, not just at the authorize step. Regression tests for the token
// endpoint rejecting the code with invalid_grant ("the code is not for this
// resource") when the parameter went missing.

func isolateOAuthStore(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	credentials.ResetStorageBackend()
	if err := credentials.SetStorageMode("file"); err != nil {
		t.Fatalf("SetStorageMode: %v", err)
	}
	t.Cleanup(credentials.ResetStorageBackend)
}

// oauthResourceTestServer is a minimal authorization server: RFC 8414
// metadata at the root well-known path, DCR, and a token endpoint that
// records the last form it received.
type oauthResourceTestServer struct {
	*httptest.Server
	mu        sync.Mutex
	tokenForm url.Values
}

func newOAuthResourceTestServer(t *testing.T) *oauthResourceTestServer {
	t.Helper()
	ts := &oauthResourceTestServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := "http://" + r.Host
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			writeOAuthJSON(w, map[string]interface{}{
				"issuer":                                origin,
				"authorization_endpoint":                origin + "/oauth/authorize",
				"token_endpoint":                        origin + "/oauth/token",
				"registration_endpoint":                 origin + "/oauth/register",
				"code_challenge_methods_supported":      []string{"S256"},
				"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
				"token_endpoint_auth_methods_supported": []string{"none"},
				"scopes_supported":                      []string{"tasks:read"},
			})
		case "/oauth/register":
			writeOAuthJSON(w, map[string]interface{}{"client_id": "reg-client-1"})
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			ts.mu.Lock()
			ts.tokenForm = r.PostForm
			ts.mu.Unlock()
			writeOAuthJSON(w, map[string]interface{}{
				"access_token":  "at-new",
				"refresh_token": "rt-new",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *oauthResourceTestServer) lastTokenForm() url.Values {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.tokenForm
}

func writeOAuthJSON(w http.ResponseWriter, body map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// An expired access token with a stored resource must send the resource on
// the refresh grant and save the rotated token set.
func TestEnsureFreshAccessToken_RefreshCarriesResource(t *testing.T) {
	isolateOAuthStore(t)
	ts := newOAuthResourceTestServer(t)
	resource := ts.URL + "/mcp"

	require.NoError(t, storeOAuthToken("res-test", "access_token", "at-expired"))
	require.NoError(t, storeOAuthToken("res-test", "refresh_token", "rt-old"))
	require.NoError(t, storeOAuthToken("res-test", "client_id", "reg-client-1"))
	require.NoError(t, storeOAuthToken("res-test", "expires_at", "1")) // long past
	require.NoError(t, storeOAuthToken("res-test", "auth_servers", ts.URL))
	require.NoError(t, storeOAuthToken("res-test", "resource", resource))
	t.Cleanup(func() { deleteOAuthState("res-test") })

	token, err := EnsureFreshAccessToken(context.Background(), "res-test", "")
	require.NoError(t, err)
	assert.Equal(t, "at-new", token)
	assert.Equal(t, "rt-new", loadOAuthToken("res-test", "refresh_token"), "rotated refresh token is stored")

	form := ts.lastTokenForm()
	require.NotNil(t, form, "token endpoint was not called")
	assert.Equal(t, "refresh_token", form.Get("grant_type"))
	assert.Equal(t, "rt-old", form.Get("refresh_token"))
	assert.Equal(t, resource, form.Get("resource"), "refresh must repeat the resource indicator")
}

// A login with no stored resource (server without RFC 8707) must omit the
// parameter rather than send an empty value.
func TestEnsureFreshAccessToken_NoResourceOmitted(t *testing.T) {
	isolateOAuthStore(t)
	ts := newOAuthResourceTestServer(t)

	require.NoError(t, storeOAuthToken("res-plain", "access_token", "at-expired"))
	require.NoError(t, storeOAuthToken("res-plain", "refresh_token", "rt-old"))
	require.NoError(t, storeOAuthToken("res-plain", "client_id", "reg-client-1"))
	require.NoError(t, storeOAuthToken("res-plain", "expires_at", "1"))
	require.NoError(t, storeOAuthToken("res-plain", "auth_servers", ts.URL))
	t.Cleanup(func() { deleteOAuthState("res-plain") })

	_, err := EnsureFreshAccessToken(context.Background(), "res-plain", "")
	require.NoError(t, err)

	form := ts.lastTokenForm()
	require.NotNil(t, form)
	assert.Empty(t, form.Get("resource"))
}

// The code exchange must bind to the session's resource — the AS validates
// it against what the authorize step stored.
func TestExchangeCodeForTokens_SendsResource(t *testing.T) {
	isolateOAuthStore(t)
	ts := newOAuthResourceTestServer(t)

	sess := &oauthState{
		verifier: "test-verifier",
		state:    "test-state",
		resource: ts.URL + "/mcp",
	}
	tokens, err := exchangeCodeForTokens(context.Background(), ts.URL+"/oauth/token", "reg-client-1", "", sess)
	require.NoError(t, err)
	assert.Equal(t, "at-new", tokens.AccessToken)

	form := ts.lastTokenForm()
	require.NotNil(t, form)
	assert.Equal(t, "authorization_code", form.Get("grant_type"))
	assert.Equal(t, ts.URL+"/mcp", form.Get("resource"), "code exchange must repeat the authorize-time resource")
	assert.Equal(t, "test-verifier", form.Get("code_verifier"))
}

// A login made before the resource was stored (no resource field) derives
// it from the server's configured URL, so refreshes against a
// resource-binding AS keep working without a re-login.
func TestEnsureFreshAccessToken_ResourceDerivedFromServerURL(t *testing.T) {
	isolateOAuthStore(t)
	ts := newOAuthResourceTestServer(t)
	resource := ts.URL + "/mcp"

	require.NoError(t, storeOAuthToken("res-legacy", "access_token", "at-expired"))
	require.NoError(t, storeOAuthToken("res-legacy", "refresh_token", "rt-old"))
	require.NoError(t, storeOAuthToken("res-legacy", "client_id", "reg-client-1"))
	require.NoError(t, storeOAuthToken("res-legacy", "expires_at", "1"))
	require.NoError(t, storeOAuthToken("res-legacy", "auth_servers", ts.URL))
	t.Cleanup(func() { deleteOAuthState("res-legacy") })

	_, err := EnsureFreshAccessToken(context.Background(), "res-legacy", resource)
	require.NoError(t, err)

	form := ts.lastTokenForm()
	require.NotNil(t, form)
	assert.Equal(t, resource, form.Get("resource"), "legacy login derives the resource from the configured URL")
}

// A token with more than 60s of life is returned as-is, not refreshed; one
// inside the 60s skew window counts as expired.
func TestEnsureFreshAccessToken_SkewWindow(t *testing.T) {
	isolateOAuthStore(t)
	ts := newOAuthResourceTestServer(t)

	future := strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10)
	require.NoError(t, storeOAuthToken("res-skew", "access_token", "at-valid"))
	require.NoError(t, storeOAuthToken("res-skew", "expires_at", future))
	t.Cleanup(func() { deleteOAuthState("res-skew") })

	token, err := EnsureFreshAccessToken(context.Background(), "res-skew", ts.URL+"/mcp")
	require.NoError(t, err)
	assert.Equal(t, "at-valid", token)
	assert.Nil(t, ts.lastTokenForm(), "a token with life left must not be refreshed")
}
