// Package llmproxy provides an http.RoundTripper that rewrites direct
// calls to well-known LLM provider endpoints (api.openai.com,
// api.anthropic.com, etc.) so they instead route through the sprout-foundry
// platform's /api/proxy/llm/{provider}/* path.
//
// This exists because the WASM build can't ship API keys client-side —
// the platform holds per-user encrypted keys in its own datastore and
// attaches them server-side on each proxied request. Native sprout users
// who want to dogfood the platform routing can install the transport too;
// it's a no-op when no endpoint is configured.
//
// See roadmap/SP-045-wasm-feature-parity.md §"Tier 2b" for the broader
// design and roadmap/SP-046-workspace-sync-model.md for the related sync
// transport.
package llmproxy

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// rewriteTransport wraps an http.RoundTripper and rewrites outgoing
// requests targeting known LLM provider hosts to go through the
// configured platform proxy URL.
//
// Safe for concurrent use. The platform endpoint can be swapped at any
// time via SetPlatformEndpoint — requests in flight at the moment of the
// swap see whichever value was current when they entered RoundTrip.
type rewriteTransport struct {
	base http.RoundTripper

	// platformBase holds the configured platform base URL (e.g.
	// "https://platform.sprout-foundry.com"). Stored as an atomic.Value
	// to allow lock-free reads on the request path. A zero/empty value
	// means routing is disabled and requests pass through unchanged.
	platformBase atomic.Value // string

	// corsProxy holds a generic CORS proxy URL prefix (e.g.
	// "https://cors-proxy.example.com"). When set, ALL HTTP/HTTPS
	// requests are rewritten to {corsProxy}/{url-encoded original URL}
	// before any other rewriting is attempted. This allows users to
	// route through an arbitrary CORS proxy to bypass browser CORS
	// restrictions. A zero/empty value means this feature is disabled.
	corsProxy atomic.Value // string
}

var defaultTransport = &rewriteTransport{base: http.DefaultTransport}

// Install replaces http.DefaultTransport with the llmproxy RoundTripper.
// Once installed, every http.Client that doesn't override .Transport
// picks up the URL rewriting (which is exactly what `pkg/agent_api`
// providers do — see models.go where http.Client{Timeout: ...} is
// constructed with no Transport).
//
// Idempotent. Safe to call multiple times; only the first replaces
// the default transport, subsequent calls are no-ops.
func Install() {
	// Capture the current DefaultTransport as our base ONCE. Otherwise
	// repeated installs would build up nested wrappers, each calling the
	// previous, growing the call chain on every JS bootstrap.
	if _, ok := http.DefaultTransport.(*rewriteTransport); ok {
		return
	}
	defaultTransport.base = http.DefaultTransport
	http.DefaultTransport = defaultTransport
}

// SetPlatformEndpoint configures the base URL of the sprout-foundry
// platform. Calls to known LLM providers will be rewritten to
// `{base}/api/proxy/llm/{provider}{path}`. Pass "" to disable rewriting.
//
// Calling before Install() is harmless — the value is stored and applied
// once the transport gets installed.
func SetPlatformEndpoint(base string) {
	base = strings.TrimRight(base, "/")
	if base != "" {
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
	}
	defaultTransport.platformBase.Store(base)
}

// GetPlatformEndpoint returns the currently-configured platform base URL
// (or "" when unset). Exposed mostly for diagnostic + test paths.
func GetPlatformEndpoint() string {
	v := defaultTransport.platformBase.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// SetCorsProxy configures a generic CORS proxy URL prefix. When set, ALL
// HTTP/HTTPS requests are rewritten to {corsProxy}/{url-encoded original URL}
// before any other rewriting is attempted. This allows users to route
// through an arbitrary CORS proxy to bypass browser CORS restrictions.
// Pass "" to disable CORS proxy rewriting.
//
// Precedence: corsProxy > platformEndpoint > no rewriting.
func SetCorsProxy(proxyURL string) {
	proxyURL = strings.TrimRight(proxyURL, "/")
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
	}
	defaultTransport.corsProxy.Store(proxyURL)
}

// GetCorsProxy returns the currently-configured CORS proxy URL (or ""
// when unset). Exposed for diagnostic + test paths.
func GetCorsProxy() string {
	v := defaultTransport.corsProxy.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// RoundTrip implements http.RoundTripper. Rewrites the URL when:
//  1. corsProxy is set (highest precedence) — routes ALL HTTP/HTTPS
//     requests through the CORS proxy with URL-encoded original URL
//  2. platformEndpoint is set AND the host matches a known provider —
//     routes through the sprout platform proxy
//
// Otherwise delegates straight to the base transport.
func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check corsProxy FIRST (highest precedence).
	if proxy, _ := t.corsProxy.Load().(string); proxy != "" {
		scheme := strings.ToLower(req.URL.Scheme)
		if scheme == "http" || scheme == "https" {
			encoded := url.QueryEscape(req.URL.String())
			rewritten, err := url.Parse(proxy + "/" + encoded)
			if err != nil {
				// Misconfigured proxy URL — pass through.
				return t.base.RoundTrip(req)
			}
			newReq := req.Clone(req.Context())
			newReq.URL = rewritten
			newReq.Host = rewritten.Host
			newReq.Header.Set("Host", rewritten.Host)
			return t.base.RoundTrip(newReq)
		}
		// Non-HTTP(S) URL: pass through unchanged.
		return t.base.RoundTrip(req)
	}

	base, _ := t.platformBase.Load().(string)
	if base == "" {
		return t.base.RoundTrip(req)
	}

	provider, suffix, ok := matchProvider(req.URL)
	if !ok {
		return t.base.RoundTrip(req)
	}

	rewritten, err := url.Parse(base + "/api/proxy/llm/" + provider + suffix)
	if err != nil {
		// Misconfigured platform URL — pass through and let the original
		// request happen. This avoids breaking the user with a confusing
		// "invalid proxy URL" error mid-request.
		return t.base.RoundTrip(req)
	}

	// Clone so we don't mutate the caller's *http.Request — the contract
	// for RoundTripper.RoundTrip is explicit about that.
	newReq := req.Clone(req.Context())
	newReq.URL = rewritten
	newReq.Host = rewritten.Host
	// The Host header must follow .Host for HTTP/1.x conformance.
	newReq.Header.Set("Host", rewritten.Host)
	return t.base.RoundTrip(newReq)
}
