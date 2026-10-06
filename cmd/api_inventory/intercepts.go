package main

import (
	"regexp"
	"strings"
)

// The "served by" components. In the local product every route is served by
// the daemon; in a hosted/cloud deployment the same route is served by
// whichever of the in-browser WASM agent, browser-local storage, or the host
// backend the web UI routing sends it to.
const (
	compDaemon = "daemon"
	compWASM   = "in-browser WASM agent" // #nosec G101 -- not a credential; a component label for the Served-by column
	compLocal  = "browser-local"
	compHost   = "host"
)

// componentOrder is the fixed display order for the Served-by column.
var componentOrder = []string{compDaemon, compWASM, compLocal, compHost}

// categoryComponent maps a registry category to its served-by component.
func categoryComponent(category string) string {
	switch category {
	case "wasm-local":
		return compWASM
	case "foundry-backend":
		return compHost
	case "synthetic", "no-op", "browser-git":
		return compLocal
	default:
		return ""
	}
}

// intercept is one dynamic/special-case intercept from cloudAdapter.fetch,
// encoded explicitly so the generator mirrors the web UI's routing. Each
// entry names the cloudAdapter block it mirrors (see the Comment field) and
// the component that serves a matching path in cloud mode.
type intercept struct {
	Name      string
	Component string
	Match     func(path string) bool
	Comment   string
}

var editDecisionRe = regexp.MustCompile(`^/api/edits/[^/]+/decision$`)
var shellApprovalDecisionRe = regexp.MustCompile(`^/api/shell-approvals/[^/]+/decision$`)

// browserLocalSessionPaths are the paths the browser-local (localStorage)
// session stores serve in cloud mode: cloudSessionHandlers (sessions) and
// cloudChatSessions (chat-sessions). They are enumerated here rather than
// matched by prefix because the adapter intercepts the whole /api/sessions*
// and /api/chat-sessions* trees but only serves these specific operations
// locally; the rest fall through to the synthetic registry or the host proxy.
var browserLocalSessionPaths = map[string]bool{
	"/api/sessions":                 true,
	"/api/sessions/restore":         true,
	"/api/chat-sessions":            true,
	"/api/chat-sessions/create":     true,
	"/api/chat-sessions/switch":     true,
	"/api/chat-sessions/messages":   true,
	"/api/chat-sessions/rename":     true,
	"/api/chat-sessions/delete":     true,
	"/api/chat-sessions/delete-all": true,
}

// intercepts is the explicit table of cloudAdapter.dynamic intercepts. The
// order is the order the blocks appear in cloudAdapter.fetch.
var intercepts = []intercept{
	{
		Name:      "chat endpoint map",
		Component: compHost,
		Match:     func(p string) bool { return p == "/api/query/status" },
		Comment:   "Mirrors CHAT_ENDPOINT_MAP (cloudProxyRoutes.ts) and the 'Chat endpoint translation' block: /api/query/status is proxied to /proxy/chat/status. query/stop/steer are wasm-local in the registry, so only status is proxied here.",
	},
	{
		Name:      "in-browser git",
		Component: compLocal,
		Match:     func(p string) bool { return strings.HasPrefix(p, "/api/git/") },
		Comment:   "Mirrors the 'Git operations' block: any /api/git/* path is handled in-browser via isomorphic-git (handleBrowserGitRequest). Covers git routes absent from the registry.",
	},
	{
		Name:      "stats proxy",
		Component: compHost,
		Match:     func(p string) bool { return p == "/api/stats" },
		Comment:   "Mirrors the 'Stats endpoint translation' block: /api/stats is rewritten to /api/proxy/stats on the host backend.",
	},
	{
		Name:      "settings core proxy",
		Component: compHost,
		Match: func(p string) bool {
			return p == "/api/settings" ||
				strings.HasPrefix(p, "/api/settings/credentials") ||
				strings.HasPrefix(p, "/api/settings/providers")
		},
		Comment: "Mirrors the 'Settings endpoint translation' block: core settings (user prefs, credentials, providers) are proxied to the host; subagent-types/MCP/skills/hotkeys are intercepted as synthetic.",
	},
	{
		Name:      "browser-local session stores",
		Component: compLocal,
		Match:     func(p string) bool { return browserLocalSessionPaths[p] },
		Comment:   "Mirrors the 'Cloud session persistence' and 'Chat sessions' blocks: localStorage-backed session/chat-session stores (cloudSessionHandlers, cloudChatSessions) serve these specific operations. cloudAdapter routes only /api/sessions/{GET, /restore POST, /delete POST, {id} DELETE} and the seven /api/chat-sessions* operations listed in browserLocalSessionPaths; unhandled sub-paths (e.g. /api/sessions/{id}/export, /api/chat-sessions/fork) fall through to the synthetic registry or the host proxy. This is a path-level simplification of the per-method routing.",
	},
	{
		Name:      "edit decision",
		Component: compWASM,
		Match:     func(p string) bool { return editDecisionRe.MatchString(p) || strings.HasPrefix(p, "/api/edits/") },
		Comment:   "Mirrors the 'Dynamic edit-decision interception' block: the registry is static-only, so /api/edits/{id}/decision is intercepted by regex and served by the WASM shell. cloudAdapter fires it only on POST + a /decision suffix (GET status is not intercepted and falls through to the host proxy); this path-level matcher is a deliberate simplification for the Served-by column.",
	},
	{
		Name:      "shell-approval decision",
		Component: compWASM,
		Match: func(p string) bool {
			return shellApprovalDecisionRe.MatchString(p) || strings.HasPrefix(p, "/api/shell-approvals/")
		},
		Comment: "Mirrors the 'Dynamic shell-approval-decision interception' block: /api/shell-approvals/{id}/decision is intercepted by regex and served by the WASM shell. cloudAdapter fires it only on POST + a /decision suffix (other methods fall through to the host proxy); this path-level matcher is a deliberate simplification for the Served-by column.",
	}}

// servedByForRoute computes the Served-by components for a route: always the
// daemon, plus any component contributed by a covering registry entry or a
// matching intercept. Returned in the fixed componentOrder.
func servedByForRoute(path string, registry []RegistryEntry) []string {
	components := map[string]bool{compDaemon: true}
	for _, e := range registry {
		covers := (e.IsPrefix && strings.HasPrefix(path, e.Path)) || (!e.IsPrefix && path == e.Path)
		if covers {
			if c := categoryComponent(e.Category); c != "" {
				components[c] = true
			}
		}
	}
	for _, ic := range intercepts {
		if ic.Match(path) {
			components[ic.Component] = true
		}
	}
	var out []string
	for _, c := range componentOrder {
		if components[c] {
			out = append(out, c)
		}
	}
	return out
}

// isCovered reports whether any registry entry or intercept covers the path.
// A route that is not covered is daemon-only; for /api/* routes that is a
// routing gap worth noting in the doc.
func isCovered(path string, registry []RegistryEntry) bool {
	for _, e := range registry {
		if (e.IsPrefix && strings.HasPrefix(path, e.Path)) || (!e.IsPrefix && path == e.Path) {
			return true
		}
	}
	for _, ic := range intercepts {
		if ic.Match(path) {
			return true
		}
	}
	return false
}
