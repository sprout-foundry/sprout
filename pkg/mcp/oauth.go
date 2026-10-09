package mcp

// oauth.go — OAuth 2.1 authorization for remote (HTTP) MCP servers, per the
// MCP authorization spec: protected-resource metadata discovery (RFC 9728),
// authorization-server metadata (RFC 8414), dynamic client registration
// (RFC 7591), authorization-code + PKCE (RFC 7636) against a loopback
// callback, and token refresh. Tokens live in the credential backend under
// "mcp-oauth/<server>/<field>" — the config never carries them.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// OAuth token credential-store key prefix. Field is one of: access_token,
// refresh_token, client_id, client_secret, expires_at, auth_servers.
const oauthStorePrefix = "mcp-oauth/"

// oauthCallbackPort is the fixed loopback port the local authorization
// callback listens on. A fixed port keeps the redirect URI stable across
// the authorize request and the callback (required — the AS validates it).
const oauthCallbackPort = 8679

// oauthCallbackPath is the loopback path; the full redirect URI is
// http://127.0.0.1:8679/callback.
const oauthCallbackPath = "/callback"

// oauthHTTPTimeout bounds every individual OAuth HTTP round trip.
const oauthHTTPTimeout = 30 * time.Second

// authorizationServerMetadata is the subset of RFC 8414 metadata sprout uses.
type authorizationServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

// protectedResourceMetadata is the subset of RFC 9728 metadata sprout uses.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// dynamicClientRegistration is the RFC 7591 request/response subset.
type dynamicClientRegistration struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
}

// oauthTokens is the stored token state for one server.
type oauthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"` // unix seconds; 0 = unknown
}

// oauthStoreKey returns the credential-store key for a server's token field.
func oauthStoreKey(server, field string) string {
	return oauthStorePrefix + server + "/" + field
}

// storeOAuthToken persists one field of the server's OAuth state.
func storeOAuthToken(server, field, value string) error {
	return credentials.SetToActiveBackend(oauthStoreKey(server, field), value)
}

// loadOAuthToken reads one field; "" when unset.
func loadOAuthToken(server, field string) string {
	value, _, err := credentials.GetFromActiveBackend(oauthStoreKey(server, field))
	if err != nil {
		return ""
	}
	return value
}

// deleteOAuthState wipes every stored field for the server.
func deleteOAuthState(server string) {
	for _, field := range []string{"access_token", "refresh_token", "client_id", "client_secret", "expires_at", "auth_servers"} {
		_ = credentials.DeleteFromActiveBackend(oauthStoreKey(server, field))
	}
}

// oauthState carries one login attempt's PKCE + state values.
type oauthState struct {
	verifier    string
	state       string
	codeChall   string
	mu          sync.Mutex
	code        string
	transportAS string
	serverName  string
	resource    string
	scopes      []string
	errMsg      string
}

// oauthSessions tracks in-flight logins by state (the callback looks the
// attempt up by the state the AS echoes back).
var oauthSessions = struct {
	mu sync.Mutex
	m  map[string]*oauthState
}{m: make(map[string]*oauthState)}

// callbackOnce guards the one-time loopback listener; oauthServer holds it.
var (
	callbackOnce sync.Once
	oauthServer  *http.Server
)

// StartOAuthLogin runs the full browser authorization flow for one server:
// discover the authorization server, register a client (DCR), open the
// browser at the authorize URL, wait on the loopback callback, and exchange
// the code. Returns a human-readable status line.
func StartOAuthLogin(ctx context.Context, serverName string, config *MCPServerConfig) (string, error) {
	if config == nil || config.Type != "http" || strings.TrimSpace(config.URL) == "" {
		return "", fmt.Errorf("OAuth login applies to HTTP MCP servers only")
	}

	resourceURL := normalizeMCPResourceURL(config.URL)

	// 1. Discover the authorization server (RFC 9728) — fall back to the
	// resource origin when metadata is absent (some ASs serve OAuth directly
	// from the MCP host).
	authServer, scopes, err := discoverAuthorizationServer(ctx, config.URL)
	if err != nil {
		authServer = resourceURL // origin fallback: issuer at the resource root
	}
	asMeta, err := fetchAuthorizationServerMetadata(ctx, authServer)
	if err != nil {
		return "", fmt.Errorf("authorization server metadata for %s: %w", authServer, err)
	}
	if asMeta.AuthorizationEndpoint == "" || asMeta.TokenEndpoint == "" {
		return "", fmt.Errorf("authorization server %s metadata lacks authorization_endpoint/token_endpoint", authServer)
	}

	// Prefer S256; only fall back to plain when the server offers nothing else.
	challMethod := "S256"
	for _, m := range asMeta.CodeChallengeMethodsSupported {
		if m == "S256" {
			challMethod = "S256"
			break
		}
		if m == "plain" && challMethod != "S256" {
			challMethod = "plain"
		}
	}

	// 2. Register a client (DCR). A stored client_id from a previous login
	// is reused; servers without registration_endpoint require a static
	// client configured via set-credential under <server>_CLIENT_ID.
	clientID := loadOAuthToken(serverName, "client_id")
	clientSecret := loadOAuthToken(serverName, "client_secret")
	if clientID == "" {
		if asMeta.RegistrationEndpoint == "" {
			return "", fmt.Errorf("server %s does not offer dynamic client registration and no stored client_id exists — store one with mcp_refresh set-credential (server %q, env_var %q)", serverName, serverName, "CLIENT_ID")
		}
		reg, err := registerClient(ctx, asMeta.RegistrationEndpoint, resourceURL)
		if err != nil {
			return "", fmt.Errorf("dynamic client registration: %w", err)
		}
		clientID = reg.ClientID
		clientSecret = reg.ClientSecret
		if err := storeOAuthToken(serverName, "client_id", clientID); err != nil {
			return "", err
		}
		if clientSecret != "" {
			if err := storeOAuthToken(serverName, "client_secret", clientSecret); err != nil {
				return "", err
			}
		}
	}

	// 3. PKCE + state, then the loopback callback.
	verifier, err := randomString(48)
	if err != nil {
		return "", err
	}
	state, err := randomString(24)
	if err != nil {
		return "", err
	}
	codeChall := pkceChallenge(verifier, challMethod)

	sess := &oauthState{
		verifier:    verifier,
		state:       state,
		codeChall:   codeChall,
		transportAS: authServer,
		serverName:  serverName,
		resource:    resourceURL,
		scopes:      firstNonEmpty(scopes, asMeta.ScopesSupported),
	}
	oauthSessions.mu.Lock()
	oauthSessions.m[state] = sess
	oauthSessions.mu.Unlock()
	defer func() {
		oauthSessions.mu.Lock()
		delete(oauthSessions.m, state)
		oauthSessions.mu.Unlock()
	}()

	authURL, err := buildAuthorizeURL(asMeta.AuthorizationEndpoint, clientID, resourceURL, challMethod, sess)
	if err != nil {
		return "", err
	}

	if err := serveOAuthCallback(ctx); err != nil {
		return "", fmt.Errorf("OAuth callback listener: %w", err)
	}

	openURLInBrowser(authURL)

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("OAuth login for %s canceled", serverName)
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("OAuth login for %s timed out after 5m waiting for the browser callback", serverName)
	case <-sess.done():
	}

	if sess.errMsg != "" {
		return "", fmt.Errorf("authorization failed: %s", sess.errMsg)
	}
	if sess.code == "" {
		return "", fmt.Errorf("authorization callback carried no code")
	}

	// 4. Exchange the code for tokens.
	tokens, err := exchangeCodeForTokens(ctx, asMeta.TokenEndpoint, clientID, clientSecret, sess)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if err := saveTokens(serverName, tokens); err != nil {
		return "", err
	}
	if err := storeOAuthToken(serverName, "auth_servers", authServer); err != nil {
		return "", err
	}

	return fmt.Sprintf("OAuth login for %s complete (authorization server %s, client %s). Use mcp_refresh refresh to reconnect the server with the new token.", serverName, authServer, clientID), nil
}

// OAuthStatus reports whether the server holds usable OAuth tokens.
func OAuthStatus(serverName string) (loggedIn bool, detail string) {
	if loadOAuthToken(serverName, "access_token") == "" {
		return false, "not logged in"
	}
	detail = "logged in"
	if authServer := loadOAuthToken(serverName, "auth_servers"); authServer != "" {
		detail += " (authorization server " + authServer + ")"
	}
	if refresh := loadOAuthToken(serverName, "refresh_token"); refresh == "" {
		detail += ", no refresh token"
	}
	return true, detail
}

// OAuthLogout wipes the server's stored OAuth state.
func OAuthLogout(serverName string) {
	deleteOAuthState(serverName)
}

// EnsureFreshAccessToken returns a valid access token for the server,
// refreshing it when expired. Errors when no login exists.
func EnsureFreshAccessToken(ctx context.Context, serverName string) (string, error) {
	token := loadOAuthToken(serverName, "access_token")
	if token == "" {
		return "", fmt.Errorf("server %s has no OAuth login — run mcp_refresh login first", serverName)
	}
	expiresAt := parseInt64(loadOAuthToken(serverName, "expires_at"))
	if expiresAt == 0 || time.Now().Unix() < expiresAt-60 {
		return token, nil
	}

	refresh := loadOAuthToken(serverName, "refresh_token")
	if refresh == "" {
		return "", fmt.Errorf("OAuth token for %s expired and no refresh token is stored — run mcp_refresh login again", serverName)
	}

	authServer := loadOAuthToken(serverName, "auth_servers")
	if authServer == "" {
		return "", fmt.Errorf("OAuth state for %s is missing its authorization server — run mcp_refresh login again", serverName)
	}
	asMeta, err := fetchAuthorizationServerMetadata(ctx, authServer)
	if err != nil {
		return "", fmt.Errorf("authorization server metadata for refresh: %w", err)
	}

	clientID := loadOAuthToken(serverName, "client_id")
	clientSecret := loadOAuthToken(serverName, "client_secret")

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	tokens, err := postTokenRequest(ctx, asMeta.TokenEndpoint, form)
	if err != nil {
		return "", err
	}
	// RFC 6749 §6: the AS may omit refresh_token on refresh — keep the old one.
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refresh
	}
	if tokens.ExpiresAt == 0 {
		tokens.ExpiresAt = expiresAt
	}
	if err := saveTokens(serverName, tokens); err != nil {
		return "", err
	}
	return tokens.AccessToken, nil
}

// ---------------------------------------------------------------------------
// Flow internals
// ---------------------------------------------------------------------------

// done exposes the session's completion signal.
func (s *oauthState) done() <-chan struct{} {
	d := make(chan struct{})
	go func() {
		for {
			s.mu.Lock()
			finished := s.code != "" || s.errMsg != ""
			s.mu.Unlock()
			if finished {
				close(d)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return d
}

// discoverAuthorizationServer fetches RFC 9728 protected-resource metadata
// from the MCP URL's origin and returns the first authorization server plus
// the resource's supported scopes.
func discoverAuthorizationServer(ctx context.Context, mcpURL string) (authServer string, scopes []string, err error) {
	u, err := url.Parse(mcpURL)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" {
		return "", nil, fmt.Errorf("invalid MCP URL %q", mcpURL)
	}
	origin := u.Scheme + "://" + u.Host
	base := origin + "/.well-known/oauth-protected-resource"
	candidates := []string{
		base + pathSuffix(u.Path), // path-scoped first (RFC 9728 §3.1)
		base,
	}
	client := &http.Client{Timeout: oauthHTTPTimeout}
	for _, metaURL := range candidates {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
		if reqErr != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, reqErr := client.Do(req)
		if reqErr != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil {
			continue
		}
		var meta protectedResourceMetadata
		if unmarshalErr := json.Unmarshal(body, &meta); unmarshalErr != nil || len(meta.AuthorizationServers) == 0 {
			continue
		}
		return strings.TrimRight(meta.AuthorizationServers[0], "/"), meta.ScopesSupported, nil
	}
	return "", nil, fmt.Errorf("no protected-resource metadata at %s", origin)
}

// pathSuffix returns "/mcp" for "https://host/mcp", "" for root paths — the
// path-scoped well-known location insert.
func pathSuffix(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p
}

// fetchAuthorizationServerMetadata fetches RFC 8414 metadata, trying the
// path-scoped then root well-known locations.
func fetchAuthorizationServerMetadata(ctx context.Context, authServer string) (*authorizationServerMetadata, error) {
	base := strings.TrimRight(authServer, "/")
	candidates := []string{
		base + "/.well-known/oauth-authorization-server",
		// RFC 8414 §3.1 path insert, split at the host component:
		base + "/.well-known/oauth-authorization-server" + pathSuffixOfIssuer(base),
		// OAuth 2.0 (pre-8414) location some ASs still serve:
		base + "/.well-known/openid-configuration",
	}
	client := &http.Client{Timeout: oauthHTTPTimeout}
	var lastErr error
	for _, metaURL := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil {
			lastErr = fmt.Errorf("status %d from %s", resp.StatusCode, metaURL)
			continue
		}
		var meta authorizationServerMetadata
		if err := json.Unmarshal(body, &meta); err != nil {
			lastErr = err
			continue
		}
		return &meta, nil
	}
	return nil, fmt.Errorf("no authorization-server metadata under %s: %v", base, lastErr)
}

// pathSuffixOfIssuer inserts the issuer's path component for the RFC 8414
// path-scoped well-known location (issuer https://host/a/b → /a/b).
func pathSuffixOfIssuer(issuer string) string {
	_, rest, ok := strings.Cut(issuer, "://")
	if !ok {
		return ""
	}
	if _, path, ok := strings.Cut(rest, "/"); ok {
		return "/" + strings.TrimRight(path, "/")
	}
	return ""
}

// registerClient performs RFC 7591 dynamic client registration.
func registerClient(ctx context.Context, registrationEndpoint, resourceURL string) (*dynamicClientRegistration, error) {
	payload := dynamicClientRegistration{
		RedirectURIs:            []string{oauthRedirectURI()},
		TokenEndpointAuthMethod: "none", // public client (PKCE); secret kept when the AS issues one anyway
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Sprout",
	}
	body, err := json.Marshal(payload) //nolint:gosec // G117: DCR requests carry the client secret the AS just issued (RFC 7591 §2)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: oauthHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("registration endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}
	var reg dynamicClientRegistration
	if err := json.Unmarshal(respBody, &reg); err != nil {
		return nil, err
	}
	if reg.ClientID == "" {
		return nil, fmt.Errorf("registration response carried no client_id")
	}
	return &reg, nil
}

// buildAuthorizeURL assembles the authorization redirect.
func buildAuthorizeURL(authorizationEndpoint, clientID, resource, challMethod string, sess *oauthState) (string, error) {
	u, err := url.Parse(authorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid authorization_endpoint %q", authorizationEndpoint)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", oauthRedirectURI())
	q.Set("state", sess.state)
	q.Set("code_challenge", sess.codeChall)
	q.Set("code_challenge_method", challMethod)
	if len(sess.scopes) > 0 {
		q.Set("scope", strings.Join(sess.scopes, " "))
	}
	// RFC 8707 resource indicator — binds the issued token to the MCP server.
	q.Set("resource", resource)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// serveOAuthCallback starts the loopback callback server once (idempotent
// for the process lifetime) and blocks until the flow completes.
func serveOAuthCallback(ctx context.Context) error {
	callbackOnce.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc(oauthCallbackPath, handleOAuthCallback)
		oauthServer = &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", oauthCallbackPort), Handler: mux, ReadHeaderTimeout: 10 * time.Second} //nolint:gosec // G112: header timeout set; loopback-only listener
		go func() {
			_ = oauthServer.ListenAndServe() // closed with the process; callback bind failure surfaces via the session error
		}()
	})
	// Fail fast when the port is occupied (another app, or a sprout callback
	// already bound in a different process — the fixed port is per-machine).
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", oauthCallbackPort), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		// Something is listening — ours (fine) or a squatter (their problem
		// shows up as a wrong redirect). Confirm it is ours by probing.
		resp, probeErr := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s?probe=1", oauthCallbackPort, oauthCallbackPath))
		if probeErr == nil {
			_ = resp.Body.Close()
			return nil
		}
		return fmt.Errorf("port %d is in use by another process; free it and retry", oauthCallbackPort)
	}
	return nil
}

// handleOAuthCallback completes the session the AS redirected to.
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errParam := q.Get("error"); errParam != "" {
		finishOAuthSession(q.Get("state"), "", errParam+": "+q.Get("error_description"))
		http.Error(w, "Sprout MCP login failed: "+errParam, http.StatusBadRequest)
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		http.Error(w, "Sprout MCP login: callback missing code/state", http.StatusBadRequest)
		return
	}
	oauthSessions.mu.Lock()
	sess := oauthSessions.m[state]
	oauthSessions.mu.Unlock()
	if sess == nil {
		http.Error(w, "Sprout MCP login: unknown or expired login attempt (state mismatch)", http.StatusBadRequest)
		return
	}
	finishOAuthSession(state, code, "")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<html><body><h3>Sprout MCP login complete.</h3>You can close this tab and return to Sprout.</body></html>"))
}

// finishOAuthSession records the callback outcome.
func finishOAuthSession(state, code, errMsg string) {
	oauthSessions.mu.Lock()
	sess := oauthSessions.m[state]
	oauthSessions.mu.Unlock()
	if sess == nil {
		return
	}
	sess.mu.Lock()
	sess.code = code
	sess.errMsg = errMsg
	sess.mu.Unlock()
}

// exchangeCodeForTokens swaps the authorization code for tokens at the
// token endpoint.
func exchangeCodeForTokens(ctx context.Context, tokenEndpoint, clientID, clientSecret string, sess *oauthState) (*oauthTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {sess.code},
		"redirect_uri":  {oauthRedirectURI()},
		"client_id":     {clientID},
		"code_verifier": {sess.verifier},
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	return postTokenRequest(ctx, tokenEndpoint, form)
}

// postTokenRequest POSTs a form to the token endpoint and parses the JSON
// token response (both the grant and refresh shapes).
func postTokenRequest(ctx context.Context, tokenEndpoint string, form url.Values) (*oauthTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: oauthHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	var wire struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("token response not JSON: %w", err)
	}
	if wire.AccessToken == "" {
		return nil, fmt.Errorf("token response carried no access_token")
	}
	tokens := &oauthTokens{
		AccessToken:  wire.AccessToken,
		RefreshToken: wire.RefreshToken,
		TokenType:    wire.TokenType,
	}
	if wire.ExpiresIn > 0 {
		tokens.ExpiresAt = time.Now().Unix() + wire.ExpiresIn
	}
	return tokens, nil
}

// saveTokens persists the token set (expiry stored as unix seconds).
func saveTokens(serverName string, tokens *oauthTokens) error {
	if err := storeOAuthToken(serverName, "access_token", tokens.AccessToken); err != nil {
		return err
	}
	if err := storeOAuthToken(serverName, "refresh_token", tokens.RefreshToken); err != nil {
		return err
	}
	return storeOAuthToken(serverName, "expires_at", fmt.Sprintf("%d", tokens.ExpiresAt))
}

// normalizeMCPResourceURL renders the resource indicator for an MCP URL:
// scheme://host[:port]/path with the trailing slash kept as-is and no query.
func normalizeMCPResourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.RawQuery = ""
	return u.String()
}

// openURLInBrowser opens an http(s) URL in the user's browser, best effort —
// the login flow prints the URL too, so a failure here costs nothing. Inlined
// (rather than pkg/runner.OpenURL) because runner imports this package.
func openURLInBrowser(raw string) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return
	}
	target := u.String()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target) //nolint:gosec // G204: validated http(s) URL
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target) //nolint:gosec // G204: validated http(s) URL
	default:
		cmd = exec.Command("xdg-open", target) //nolint:gosec // G204: validated http(s) URL
	}
	_ = cmd.Start()
}

// oauthRedirectURI is the stable loopback redirect.
func oauthRedirectURI() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", oauthCallbackPort, oauthCallbackPath)
}

// randomString returns a URL-safe random string of ~n characters.
func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// pkceChallenge derives the code_challenge from the verifier.
func pkceChallenge(verifier, method string) string {
	if method == "plain" {
		return verifier
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// parseInt64 parses s or returns 0.
func parseInt64(s string) int64 {
	var v int64
	_, _ = fmt.Sscanf(strings.TrimSpace(s), "%d", &v)
	return v
}

// firstNonEmpty returns the first non-empty slice.
func firstNonEmpty(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}
