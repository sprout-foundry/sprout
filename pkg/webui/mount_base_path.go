//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// Sub-path mounting.
//
// The UI is built with absolute references ("/assets/main-<hash>.js", API
// roots "/api/...", the "/ws" socket), which assume it owns the origin root.
// A host that mounts the daemon under a prefix of its own origin
// ("https://host/agent/") needs those references to carry the prefix, or the
// browser resolves them against the host's root and fetches the host's own
// bundles.
//
// SetBasePath makes the server do that rewriting: the served index.html gets
// the prefix on its asset references and on the API/WebSocket roots the
// client derives at runtime, and requests arriving with the prefix are
// stripped before routing. The reverse proxy in front of the daemon may
// forward either form — with the prefix (no strip) or without it (strip) —
// because both are routed identically.

// SetBasePath sets the mount prefix the UI is served under. Accepts
// "/agent", "agent", "/agent/" or "/" (all equivalent to "/agent" or "" for
// the root). Intended to be called before Start.
func (ws *ReactWebServer) SetBasePath(basePath string) {
	ws.basePath = normalizeBasePath(basePath)
}

// normalizeBasePath reduces a user-supplied mount path to "/prefix" form,
// with no trailing slash and "" for the origin root.
func normalizeBasePath(basePath string) string {
	trimmed := strings.Trim(strings.TrimSpace(basePath), "/")
	if trimmed == "" {
		return ""
	}
	return "/" + trimmed
}

// stripBasePath removes the mount prefix from a request path, reporting
// whether the path carried it. A request for the bare prefix ("/agent") maps
// to "/".
func stripBasePath(basePath, requestPath string) (string, bool) {
	if basePath == "" {
		return requestPath, false
	}
	if requestPath == basePath {
		return "/", true
	}
	if strings.HasPrefix(requestPath, basePath+"/") {
		return strings.TrimPrefix(requestPath, basePath), true
	}
	return requestPath, false
}

// basePathMiddleware routes requests that arrive with the mount prefix as if
// they had been sent without it. Requests without the prefix pass through
// unchanged: a proxy that strips the prefix itself produces those, and the
// daemon's own health checks address the root.
func (ws *ReactWebServer) basePathMiddleware(next http.Handler) http.Handler {
	if ws.basePath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stripped, ok := stripBasePath(ws.basePath, r.URL.Path); ok {
			r = r.Clone(r.Context())
			r.URL.Path = stripped
			r.URL.RawPath = ""
		}
		next.ServeHTTP(w, r)
	})
}

// rewriteIndexForBasePath prefixes the absolute references in the Vite
// index.html with the mount path and publishes the prefix to the client as
// window.SPROUT_PROXY_BASE — the same value the SSH proxy injects, which the
// client already applies to its API and WebSocket roots.
//
// Only references that start at the origin root are rewritten: "/assets/…",
// "/static/…", "/logo-mark.svg", "/manifest.json", "/sw.js" and friends, plus
// the "/api/" and "/ws" roots inside the inline bootstrap script. Protocol-
// relative and already-relative references are left alone.
func rewriteIndexForBasePath(index []byte, basePath string) []byte {
	if basePath == "" {
		return index
	}
	// Attribute references first: they cover every root-level file the
	// document names. The quoted-path pass then catches anything the
	// attribute form missed (inline JSON, preload maps) without
	// double-prefixing what the first pass already rewrote.
	out := index
	for _, attr := range []string{"href", "src", "content"} {
		marker := []byte(` ` + attr + `="/`)
		out = bytes.ReplaceAll(out, marker, []byte(` `+attr+`="`+basePath+`/`))
	}
	for _, root := range []string{"/assets/", "/static/", "/wasm/", "/editor.html", "/terminal.html"} {
		out = bytes.ReplaceAll(out, []byte(`"`+root), []byte(`"`+basePath+root))
	}
	// The client derives its API/WS roots from window.SPROUT_PROXY_BASE when
	// set; publish it as the first thing in <head>, before the module script
	// (which Vite emits in the head) runs.
	baseJSON, _ := json.Marshal(basePath)
	script := []byte("<script>window.SPROUT_PROXY_BASE=" + string(baseJSON) + ";</script>")
	if idx := bytes.Index(out, []byte("<head>")); idx >= 0 {
		at := idx + len("<head>")
		merged := make([]byte, 0, len(out)+len(script))
		merged = append(merged, out[:at]...)
		merged = append(merged, script...)
		merged = append(merged, out[at:]...)
		out = merged
	} else {
		out = append(script, out...)
	}
	return out
}
