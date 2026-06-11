//go:build !js

package webui

import (
	"net/http"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// securityHeadersMiddleware wraps an http.Handler and adds security headers to all responses.
//
// Framing posture is controlled by SPROUT_FRAME_ANCESTORS:
//
//   - Unset (default): X-Frame-Options: DENY + CSP frame-ancestors 'none'.
//     The web UI cannot be iframed by anything, anywhere — appropriate
//     for the typical localhost / single-user daemon model.
//
//   - Set to a space- or comma-separated list of CSP source expressions
//     (origins like `https://admin.acme.com`, or wildcards / keywords like
//     `'self'`): X-Frame-Options is OMITTED (the header can't express an
//     allowlist), and frame-ancestors is set to the configured list.
//     Modern browsers honor frame-ancestors over X-Frame-Options when
//     both are present anyway, so dropping X-Frame-Options is safe.
//
// This is the single env var standing between "the binary works today"
// and "I can drop it into an iframe on my admin portal." Kept narrow —
// only frame-ancestors is configurable — so callers can't accidentally
// loosen the rest of the CSP.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	frameAncestors := normalizeFrameAncestors(envutil.GetEnvSimple("FRAME_ANCESTORS"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if frameAncestors == "" {
			// Default DENY-everything posture.
			w.Header().Set("X-Frame-Options", "DENY")
		}
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+ // unsafe-eval required for WASM; unsafe-inline for React/Vite bundles
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; "+
				"font-src 'self' data:; "+
				"connect-src 'self' ws: wss:; "+
				"worker-src 'self' blob:; "+
				"child-src 'self' blob:; "+
				"frame-ancestors "+frameAncestorsClause(frameAncestors)+"; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self';")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy",
			"camera=(), microphone=(), geolocation=(), payment=(), usb=(), magnetometer=(), gyroscope=(), accelerometer=()")

		next.ServeHTTP(w, r)
	})
}

// normalizeFrameAncestors collapses whitespace and commas into single
// spaces and trims, so callers can write either `https://a, https://b`
// or `https://a https://b` and both produce the same CSP clause.
func normalizeFrameAncestors(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	return strings.Join(parts, " ")
}

// frameAncestorsClause builds the frame-ancestors CSP directive value.
// Defaults to `'none'` when the env var is unset.
func frameAncestorsClause(normalized string) string {
	if normalized == "" {
		return "'none'"
	}
	return normalized
}
