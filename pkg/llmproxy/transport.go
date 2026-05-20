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
	defaultTransport.platformBase.Store(strings.TrimRight(base, "/"))
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

// RoundTrip implements http.RoundTripper. Rewrites the URL when the
// target host matches a known provider AND the platform endpoint is
// configured; otherwise delegates straight to the base transport.
func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
