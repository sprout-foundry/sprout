//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// authTokenMiddleware returns middleware that requires authentication for write
// endpoints when a non-empty authToken is configured.
//
// When authToken is empty, the middleware is a no-op (passthrough).
// When authToken is set:
//   - Read methods (GET, HEAD, OPTIONS) always pass through
//   - WebSocket upgrade endpoints (/ws, /terminal, /api/lsp/ws) always pass through
//   - Non-API paths (anything not starting with /api/) always pass through
//   - Write methods (POST, PUT, PATCH, DELETE) to /api/* paths require a valid Authorization: Bearer <token> header
//
// On authentication failure, returns 401 Unauthorized with JSON error body and logs the attempt.
func authTokenMiddleware(authToken string) func(http.Handler) http.Handler {
	if authToken == "" {
		// No auth configured - return a no-op middleware
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read methods always pass through
			method := r.Method
			if method == "GET" || method == "HEAD" || method == "OPTIONS" {
				next.ServeHTTP(w, r)
				return
			}

			path := r.URL.Path

			// Only authenticate requests to /api/* paths
			// Non-API paths (SPA, static assets, SSH proxy, health checks) pass through
			if !strings.HasPrefix(path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			// WebSocket upgrade endpoints always pass through even under /api/
			// These have their own security (origin checks) and can't easily send Authorization headers
			if path == "/api/lsp/ws" {
				next.ServeHTTP(w, r)
				return
			}

			// Check Authorization header
			authHeader := r.Header.Get("Authorization")
			expectedBearer := "Bearer " + authToken

			// Use constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expectedBearer)) != 1 {
				webuiLogger.Warn("unauthorized request", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("remote_addr", r.RemoteAddr))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "valid auth token required",
				}); err != nil {
					webuiLogger.Error("authentication error response write failed", slog.Any("err", err))
				}
				return
			}

			// Auth successful - proceed
			next.ServeHTTP(w, r)
		})
	}
}
