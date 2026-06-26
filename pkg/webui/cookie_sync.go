//go:build !js

// Package webui provides React web server with embedded assets

package webui

import (
	"net/http"
	"strings"
)

// clientIDResponseHeader is the name of the HTTP response header that echoes
// the server-resolved client ID back to the client on every response.
//
// This is essential for cross-origin deployments (Cloudflare Pages + tunnel)
// where JavaScript on the webui origin cannot read cookies set on the API
// origin (document.cookie is origin-scoped). Without this header, a page
// reload in cross-origin mode loses the client ID because:
//  1. sessionStorage is cleared (tab discard) or empty (fresh page load)
//  2. document.cookie cannot read cookies from a different origin
//  3. A new client ID is generated, losing all server-side state
//
// With this header, the client reads X-Sprout-Client-ID from every API
// response (exposed via Access-Control-Expose-Headers) and writes it to
// sessionStorage, creating a reliable round-trip that survives reloads.
const clientIDResponseHeader = "X-Sprout-Client-ID"

// cookieSyncMiddleware returns middleware that synchronizes the client ID
// between the X-Sprout-Client-ID request header and the sprout_client_id
// response cookie.
//
// Why this is needed:
//
// When the WebUI is served from Cloudflare Pages (e.g. pages.sprout.dev)
// and the API runs behind a Cloudflare Tunnel (e.g. api.sprout.dev), they
// live on different origins. The browser sends the X-Sprout-Client-ID header
// on every request, but custom headers are not persisted across page reloads.
// Without a cookie, every reload generates a new client_id, losing all
// server-side state (workspace, agent session, terminal sessions, etc.).
//
// This middleware solves that by:
//
//  1. Reading the client ID from the header, cookie, or falling back to default
//  2. Writing a Set-Cookie with that client ID on every response
//  3. Echoing the resolved client ID in the X-Sprout-Client-ID response header
//  4. The cookie is configured with SameSite=None; Secure for cross-origin
//     delivery over HTTPS, or SameSite=Lax for local dev (where Secure is
//     not possible on HTTP)
//
// On the next request (including after a page reload), the browser sends
// the cookie automatically (because credentials: 'include' is set on all
// fetch calls). resolveClientID() reads the cookie as a fallback when the
// header is absent, so the session is preserved seamlessly.
//
// Cross-origin cookie + response header strategy:
//
// In a cross-origin deployment, JavaScript on the webui origin (e.g.
// pages.sprout.dev) cannot read cookies set on the API origin
// (e.g. api.sprout.dev). The X-Sprout-Client-ID response header solves
// this: the client reads the header from every response and writes the
// value to sessionStorage. This creates a reliable round-trip that
// survives page reloads without needing to read cross-origin cookies.
//
// Cookie properties:
//   - Path: / (available to all API endpoints)
//   - Max-Age: 30 days (long-lived to survive tab discard/recovery)
//   - HttpOnly: false (accessible to JavaScript for same-origin fallback
//     and for reading on the API origin when the page is served from there)
//   - SameSite: None in cross-origin mode, Lax in local mode
//   - Secure: true in cross-origin mode (required when SameSite=None),
//     false in local mode (localhost uses HTTP)
//
// Security note: the cookie value is a non-secret client identifier (UUID).
// Making it accessible to JavaScript is acceptable because it is not an
// authentication token — the client_id is already stored in sessionStorage
// (accessible to JS), and the server uses it only to locate the caller's
// own session state. The real protection comes from SPROUT_ALLOWED_ORIGINS
// / CheckOrigin on the server side to prevent arbitrary sites from
// establishing connections.
//
// The middleware detects cross-origin mode from the Origin header. When
// Origin is present and differs from the request host, it's cross-origin
// and the cookie uses SameSite=None; Secure. Otherwise it uses
// SameSite=Lax without Secure (for local development).
func cookieSyncMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := r.Header.Get(webClientIDHeader)
			clientID = strings.TrimSpace(clientID)

			// If no header, check the cookie to see if there's an existing
			// session to sync. (The cookie is already read by resolveClientID
			// in the handler chain below, but we need the raw value here for
			// the Set-Cookie response.)
			if clientID == "" {
				cookie, err := r.Cookie(clientIDCookieName)
				if err == nil && cookie.Value != "" {
					clientID = cookie.Value
				}
			}

			if clientID == "" {
				clientID = defaultWebClientID
			}

			// Sanitize before use (prevent path traversal in cookie/headers)
			clientID = sanitizeClientID(clientID)

			// Echo the resolved client ID back in the response header.
			// This is critical for cross-origin deployments where JS cannot
			// read cookies from a different origin. The client reads this
			// header (exposed via Access-Control-Expose-Headers) and writes
			// it to sessionStorage to survive page reloads.
			w.Header().Set(clientIDResponseHeader, clientID)

			// Determine if this is a cross-origin request.
			origin := r.Header.Get("Origin")
			isCrossOrigin := isCrossOriginRequest(origin, r.Host)

			// Determine cookie security settings based on request scheme.
			// SameSite=None requires Secure, but Secure cookies are rejected
			// on HTTP. For local development (HTTP), use SameSite=Lax without
			// Secure regardless of origin. For HTTPS cross-origin, use
			// SameSite=None + Secure.
			isSecure := r.TLS != nil
			sameSite := cookieSameSite(isCrossOrigin, isSecure)

			// Set the cookie on every response so the browser has it for
			// the next request (including after page reload).
			http.SetCookie(w, &http.Cookie{
				Name:     clientIDCookieName,
				Value:    clientID,
				Path:     "/",
				MaxAge:   int(clientIDCookieMaxAge.Seconds()),
				HttpOnly: false, // JS must read this for same-origin tab-discard recovery
				SameSite: sameSite,
				Secure:   sameSite == http.SameSiteNoneMode,
			})

			next.ServeHTTP(w, r)
		})
	}
}

// isCrossOriginRequest returns true when the Origin header indicates a
// different origin from the request Host. Returns false when there is no
// Origin header (same-origin request) or when the origin matches the host.
func isCrossOriginRequest(origin, host string) bool {
	if origin == "" {
		return false
	}
	// Origins are always "scheme://host" (no path), so a simple string
	// comparison of the host component is sufficient.
	originHost := extractHostFromOrigin(origin)
	if originHost == "" {
		return false
	}
	return strings.ToLower(originHost) != strings.ToLower(host)
}

// extractHostFromOrigin extracts the host portion from an Origin header value.
// Origins are always in the form "scheme://host" or "scheme://host:port".
func extractHostFromOrigin(origin string) string {
	// Origin format: "https://example.com" or "https://example.com:443"
	if !strings.Contains(origin, "://") {
		return ""
	}
	parts := strings.SplitN(origin, "://", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// cookieSameSite returns the appropriate SameSite value based on whether
// the request is cross-origin and whether it uses HTTPS. Cross-origin cookies
// on HTTPS use SameSite=None (which requires Secure). Same-origin cookies and
// all HTTP cookies use SameSite=Lax for better privacy.
func cookieSameSite(isCrossOrigin, isSecure bool) http.SameSite {
	if isCrossOrigin && isSecure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}
