//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/llmproxy"
)

// Tier 2b: LLM proxy routing through the sprout-foundry platform.
//
// The host page calls SproutWasm.setPlatformEndpoint(url) at boot. Once
// set, every LLM call out of the Go-WASM agent path goes through
// `{url}/api/proxy/llm/{provider}/*` instead of directly hitting
// api.openai.com / api.anthropic.com / etc. The platform attaches the
// authenticated user's encrypted API key on its side, so no keys ever
// touch the browser.
//
// The mechanism is an http.RoundTripper installed onto
// http.DefaultTransport at init time — see pkg/llmproxy/transport.go.
// Any http.Client that doesn't override .Transport (which is what
// pkg/agent_api's provider clients do) inherits the rewriting.

func init() {
	// Install the rewriting transport eagerly so it's in place before
	// the host page has had a chance to call setPlatformEndpoint. The
	// transport is a no-op when no endpoint is configured.
	llmproxy.Install()
}

func proxyJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"setPlatformEndpoint": js.FuncOf(setPlatformEndpointFunc),
		"getPlatformEndpoint": js.FuncOf(getPlatformEndpointFunc),
		"setCorsProxy":        js.FuncOf(setCorsProxyFunc),
		"getCorsProxy":        js.FuncOf(getCorsProxyFunc),
		"getProxyDiagnostics": js.FuncOf(getProxyDiagnosticsFunc),
	}
}

// setPlatformEndpointFunc records the sprout-foundry platform base URL
// (e.g. "https://platform.sprout-foundry.com"). LLM API calls are
// rewritten to route through it. Pass "" to disable rewriting and
// restore direct provider calls.
//
// Signature: setPlatformEndpoint(url: string): {ok: true, url: string}
func setPlatformEndpointFunc(_ js.Value, args []js.Value) interface{} {
	url := argString(args, 0, "")
	llmproxy.SetPlatformEndpoint(url)
	return map[string]interface{}{"ok": true, "url": url}
}

// getPlatformEndpointFunc returns the currently-configured platform base
// URL, or "" when unset. Diagnostic helper for host pages that want to
// confirm the boot sequence wired the endpoint correctly.
func getPlatformEndpointFunc(_ js.Value, _ []js.Value) interface{} {
	return llmproxy.GetPlatformEndpoint()
}

// setCorsProxyFunc sets a CORS proxy URL. When configured, ALL HTTP/HTTPS
// requests are routed through {proxyUrl}/{url-encoded original URL} before
// any other rewriting is attempted.
//
// Signature: setCorsProxy(proxyUrl: string): {ok: true, proxy: string}
func setCorsProxyFunc(_ js.Value, args []js.Value) interface{} {
	url := argString(args, 0, "")
	llmproxy.SetCorsProxy(url)
	return map[string]interface{}{"ok": true, "proxy": url}
}

// getCorsProxyFunc returns the currently-configured CORS proxy URL,
// or "" when unset.
func getCorsProxyFunc(_ js.Value, _ []js.Value) interface{} {
	return llmproxy.GetCorsProxy()
}

// getProxyDiagnosticsFunc returns diagnostic info about the current proxy
// configuration. Useful for host pages to verify the boot sequence
// configured routing correctly.
//
// Signature: getProxyDiagnostics(): {platformEndpoint, corsProxy, isActive}
func getProxyDiagnosticsFunc(_ js.Value, _ []js.Value) interface{} {
	return map[string]interface{}{
		"platformEndpoint": llmproxy.GetPlatformEndpoint(),
		"corsProxy":        llmproxy.GetCorsProxy(),
		"isActive":         llmproxy.GetPlatformEndpoint() != "" || llmproxy.GetCorsProxy() != "",
	}
}
