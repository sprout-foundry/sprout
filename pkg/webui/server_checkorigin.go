//go:build !js

// Package webui provides React web server with embedded assets

package webui

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// newCheckOriginFunc returns a CheckOrigin function for the WebSocket upgrader.
// The allowedOrigins are pre-normalized at startup for fast per-request comparison.
func newCheckOriginFunc(bindAddr string, allowedOrigins []string) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		// Allow localhost connections (IPv4 and IPv6).
		// When binding to 0.0.0.0 (cloud/service mode),
		// accept any origin since the service is explicitly
		// exposed. The SPROUT_ALLOWED_ORIGINS env var
		// provides finer-grained control for specific origins.
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Allow same-origin and direct connections
		}

		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}

		// Check against SPROUT_ALLOWED_ORIGINS allowlist.
		// Origins were pre-normalized at startup; only a simple
		// string comparison is needed per request.
		if len(allowedOrigins) > 0 {
			normalizedIncoming := normalizeOriginForCompare(parsed)
			for _, allowed := range allowedOrigins {
				if normalizedIncoming == allowed {
					return true
				}
			}
		}

		// When binding to all interfaces, accept any origin.
		if bindAddr == "0.0.0.0" || bindAddr == "::" {
			return true
		}
		return false
	}
}
