package main

import (
	"strings"
)

// Families group the route inventory. The set and order mirror the contract
// families the later OpenAPI items will document, so the doc reads top to
// bottom in a stable order.
var familyOrder = []string{
	"conversation/query",
	"files",
	"git",
	"settings",
	"terminal",
	"workspace/instances",
	"sync/txn",
	"sessions",
	"search",
	"diagnostics",
	"design",
	"starters",
	"misc/static",
}

// familyRules assigns a family to a path. Rules are evaluated top to bottom;
// the first match wins, so the most specific prefixes come first.
var familyRules = []struct {
	family string
	match  func(path string) bool
}{
	{"conversation/query", func(p string) bool {
		return p == "/api/query" ||
			strings.HasPrefix(p, "/api/query/") ||
			strings.HasPrefix(p, "/api/command/") ||
			p == "/api/completion" ||
			strings.HasPrefix(p, "/api/subagent")
	}},
	{"files", func(p string) bool {
		return p == "/api/files" || strings.HasPrefix(p, "/api/files/") ||
			p == "/api/file" || strings.HasPrefix(p, "/api/file/") ||
			p == "/api/create" || p == "/api/delete" || p == "/api/rename" ||
			p == "/api/browse" || strings.HasPrefix(p, "/api/upload/") ||
			strings.HasPrefix(p, "/api/edits") ||
			strings.HasPrefix(p, "/api/shell-approvals") ||
			strings.HasPrefix(p, "/api/password")
	}},
	{"git", func(p string) bool {
		return strings.HasPrefix(p, "/api/git/")
	}},
	{"settings", func(p string) bool {
		return strings.HasPrefix(p, "/api/settings") ||
			strings.HasPrefix(p, "/api/providers") ||
			strings.HasPrefix(p, "/api/onboarding") ||
			p == "/api/config" || strings.HasPrefix(p, "/api/hotkeys") ||
			strings.HasPrefix(p, "/api/skills") ||
			strings.HasPrefix(p, "/api/local-llm") ||
			p == "/api/password" ||
			strings.HasPrefix(p, "/api/computer-use")
	}},
	{"terminal", func(p string) bool {
		return strings.HasPrefix(p, "/api/terminal/") ||
			p == "/terminal" ||
			strings.HasPrefix(p, "/api/lsp/")
	}},
	{"workspace/instances", func(p string) bool {
		return strings.HasPrefix(p, "/api/workspace") || strings.HasPrefix(p, "/api/instances")
	}},
	{"sync/txn", func(p string) bool {
		return strings.HasPrefix(p, "/api/sync") || strings.HasPrefix(p, "/api/txn")
	}},
	{"sessions", func(p string) bool {
		return strings.HasPrefix(p, "/api/sessions") ||
			strings.HasPrefix(p, "/api/chat-session") ||
			strings.HasPrefix(p, "/api/changes")
	}},
	{"search", func(p string) bool {
		return strings.HasPrefix(p, "/api/search")
	}},
	{"diagnostics", func(p string) bool {
		return p == "/api/diagnostics" || strings.HasPrefix(p, "/api/diagnostics/") ||
			p == "/api/semantic" ||
			p == "/api/stats" || p == "/api/ws-metrics" || p == "/api/support-bundle"
	}},
	{"design", func(p string) bool {
		return strings.HasPrefix(p, "/api/design")
	}},
	{"starters", func(p string) bool {
		return strings.HasPrefix(p, "/api/starters")
	}},
}

// familyForPath returns the family for a route path. API routes that match no
// rule (and non-API routes) fall into misc/static.
func familyForPath(path string) string {
	for _, rule := range familyRules {
		if rule.match(path) {
			return rule.family
		}
	}
	return "misc/static"
}

// isAPIRoute reports whether a path is an API route (the gap note only applies
// to API routes that are not covered by the registry or an intercept).
func isAPIRoute(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// familyOrderIndex is a helper for sorting: the position of a family in the
// fixed order (unknown families sort last).
func familyOrderIndex(family string) int {
	for i, f := range familyOrder {
		if f == family {
			return i
		}
	}
	return len(familyOrder)
}
