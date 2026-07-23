//go:build !js

// Package webui provides React web server with embedded assets

package webui

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// corsMiddleware wraps an http.Handler and adds Cross-Origin Resource Sharing
// headers when the request originates from an allowed origin.
//
// This is essential for the Cloudflare Pages + tunnel deployment pattern where
// the webui (e.g. pages.sprout.dev) and API (e.g. api.sprout.dev) live on
// different domains. Without CORS headers, the browser blocks all cross-origin
// requests (fetch, WebSocket upgrades) even when credentials are present.
//
// Allowed origins come from SPROUT_ALLOWED_ORIGINS (comma-separated list).
// Localhost origins are always permitted. When binding to 0.0.0.0 (cloud mode)
// and no allowlist is configured, all origins are accepted — the API is
// explicitly exposed in this mode.
//
// The middleware:
//   - Reflects an allowed Origin back as Access-Control-Allow-Origin
//   - Sets Access-Control-Allow-Credentials: true (required for cookie sharing)
//   - Handles OPTIONS preflight requests with a 204 No Content response
//   - Exposes required headers (Authorization, Content-Type, X-Sprout-Client-ID)
func corsMiddleware(bindAddr string, allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// No Origin header — same-origin request or non-browser client.
				// Pass through without CORS headers.
				next.ServeHTTP(w, r)
				return
			}

			allowedOrigin := resolveAllowedOrigin(origin, bindAddr, allowedOrigins)
			if allowedOrigin == "" {
				// Origin not allowed — still serve the request (don't block it)
				// but don't add CORS headers. The browser will reject the response.
				next.ServeHTTP(w, r)
				return
			}

			// Set CORS response headers
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers",
				"Accept, Authorization, Content-Type, Content-Length, X-Sprout-Client-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
			// Expose headers that the client needs to read on cross-origin responses
			// when credentials: 'include' is set. Set-Cookie is NOT included here —
			// it is always processed by the browser automatically when
			// Access-Control-Allow-Credentials is true. X-Sprout-Client-ID allows
			// the client to verify its identity on each response.
			w.Header().Set("Access-Control-Expose-Headers",
				"X-Sprout-Client-ID, Content-Type")

			// Handle OPTIONS preflight
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveAllowedOrigin checks whether the given Origin is allowed and returns
// the string to reflect in Access-Control-Allow-Origin. Returns "" when the
// origin should not receive CORS headers.
func resolveAllowedOrigin(origin string, bindAddr string, allowedOrigins []string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}

	// A valid Origin must have a scheme and a host.
	// url.Parse() does not reject strings that look like paths (e.g. "foo!!!"),
	// so we must check these explicitly.
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	host := strings.ToLower(parsed.Hostname())

	// Always allow localhost / loopback origins (local development).
	if host == "localhost" {
		return origin
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return origin
	}

	// Check against SPROUT_ALLOWED_ORIGINS allowlist.
	// Origins were pre-normalized at startup; only a simple string comparison.
	if len(allowedOrigins) > 0 {
		normalizedIncoming := normalizeOriginForCompare(parsed)
		for _, allowed := range allowedOrigins {
			if normalizedIncoming == allowed {
				return origin
			}
		}
		// An explicit allowlist is configured and this origin is not on it — deny.
		webuiLogger.Warn("CORS origin denied", slog.String("origin", origin))
		return ""
	}

	// When binding to all interfaces (cloud/service mode) with no allowlist,
	// accept any origin. The API is explicitly exposed in this configuration.
	if bindAddr == "0.0.0.0" || bindAddr == "::" {
		return origin
	}

	// Default (local dev on 127.0.0.1 with no allowlist): deny unknown origins.
	return ""
}
