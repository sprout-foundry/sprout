package apiconformance

import "net/http"

// Probe describes one operation the suite checks against a live base URL: the
// request to send (method, path, and any query) and the expectation (a 200 and
// a body shaped by the operation's 200 schema). A probe is deliberately
// conservative: it must be safe to run against a fresh, disposable workspace
// with no active chat and no running agent.
type Probe struct {
	// OperationID is the operation's operationId; it is what a result is
	// reported by and how a suite maps a probe back to its spec Operation.
	OperationID string

	// Method is the HTTP verb to send.
	Method string

	// Path is the concrete request path. Path parameters (if any) are
	// substituted with a safe concrete value (or the path is left empty of the
	// parameter where that is the safe form). A probe is only added when the
	// resulting request is safe to fire at a fresh workspace.
	Path string

	// Query carries the request query string values. It is empty for the
	// majority of probes; a GET that needs a query parameter to return a 200
	// (e.g. search) supplies it here.
	Query map[string]string

	// ExpectedStatus is the status the probe expects. It is 200 for every probe;
	// the field exists so the table reads as data and a future probe family can
	// expect a different 2xx without a struct change.
	ExpectedStatus int

	// Family is the probe's primary family tag, the group a per-family report
	// aggregates under.
	Family string
}

// DefaultProbes returns the curated set of safe, read-only GET probes for a
// fresh, disposable workspace. Each entry returns a usable 200 response with no
// active chat, running agent, or prior state required. The set deliberately
// excludes:
//   - GETs that need a session, chat, or edit id to exist
//     (editStatus, chatSessionWorktreeGet, the chat-sessions/{id} family,
//     sessionExport) — a fresh workspace has none, and inventing an id only
//     probes 404 paths;
//   - GETs that depend on an external or optional subsystem being present
//     (sshHosts, sshSessions, sshLaunchStatus, localLLMStatus,
//     localLLMModels) — they 400/500 when the subsystem is absent, which is a
//     property of the host, not the contract;
//   - GETs whose happy path issues a live external call (providersModels: it
//     400s without a `provider` query parameter and then contacts the provider
//     to discover its model list) — a conformance probe must not reach the
//     network;
//   - GETs that require a query parameter the table will not fabricate
//     (searchQuery is the one exception: it needs ?query and is included with
//     a literal query so a fresh workspace returns a valid, possibly-empty
//     result set).
//
// The default set is the high-signal baseline. A later suite consumer overrides
// or extends it through Suite.WithProbes; every probe here maps to a GET
// operation documented in openapi.yaml, and its Family is that operation's
// primary tag.
func DefaultProbes() []Probe {
	const get = http.MethodGet
	const ok = 200

	// make builds one probe from its parts. Family is the primary tag.
	makeProbe := func(opID, family, path string, query map[string]string) Probe {
		return Probe{OperationID: opID, Method: get, Path: path, Query: query, ExpectedStatus: ok, Family: family}
	}

	return []Probe{
		// settings
		makeProbe("get-config", "settings", "/api/config", nil),
		makeProbe("hotkeysGet", "settings", "/api/hotkeys", nil),
		makeProbe("onboardingStatus", "settings", "/api/onboarding/status", nil),
		makeProbe("providersList", "settings", "/api/providers", nil),
		makeProbe("settingsGet", "settings", "/api/settings", nil),
		makeProbe("settingsSkillsGet", "settings", "/api/settings/skills", nil),
		makeProbe("settingsSubagentTypes", "settings", "/api/settings/subagent-types", nil),
		makeProbe("settingsMcpGet", "settings", "/api/settings/mcp", nil),

		// diagnostics
		makeProbe("get-stats", "diagnostics", "/api/stats", nil),

		// workspace/instances
		makeProbe("workspaceGet", "workspace/instances", "/api/workspace", nil),
		makeProbe("workspaceProjects", "workspace/instances", "/api/workspace/projects", nil),
		makeProbe("workspaceBrowse", "workspace/instances", "/api/workspace/browse", nil),
		makeProbe("instancesList", "workspace/instances", "/api/instances", nil),

		// files
		makeProbe("filesList", "files", "/api/files", nil),
		makeProbe("prettierConfig", "files", "/api/files/prettier-config", nil),
		makeProbe("browseDir", "files", "/api/browse", nil),

		// git
		makeProbe("gitStatus", "git", "/api/git/status", nil),
		makeProbe("gitBranches", "git", "/api/git/branches", nil),
		makeProbe("gitLog", "git", "/api/git/log", nil),
		makeProbe("gitDiff", "git", "/api/git/diff", nil),
		makeProbe("gitWorktrees", "git", "/api/git/worktrees", nil),

		// design
		makeProbe("designStatus", "design", "/api/design/status", nil),

		// terminal
		makeProbe("terminalSessions", "terminal", "/api/terminal/sessions", nil),
		makeProbe("terminalShells", "terminal", "/api/terminal/shells", nil),
		makeProbe("terminalHistoryGet", "terminal", "/api/terminal/history", nil),
		makeProbe("agentSessionsList", "terminal", "/api/terminal/agent-sessions", nil),
		makeProbe("lspStatus", "terminal", "/api/lsp/status", nil),

		// conversation/query
		makeProbe("sessionsList", "conversation/query", "/api/sessions", nil),
		makeProbe("chatSessionsList", "conversation/query", "/api/chat-sessions", nil),
		makeProbe("chatSessionWorktreeList", "conversation/query", "/api/chat-sessions/worktree-mappings", nil),
		makeProbe("queryStatus", "conversation/query", "/api/query/status", nil),
		makeProbe("proxyChatStatus", "conversation/query", "/api/proxy/chat/status", nil),
		makeProbe("proxyStats", "conversation/query", "/api/proxy/stats", nil),

		// sync/txn
		makeProbe("syncStatus", "sync/txn", "/api/sync", nil),
		makeProbe("syncAgentStatus", "sync/txn", "/api/sync/status", nil),
		makeProbe("txnStatus", "sync/txn", "/api/txn/status", nil),

		// search (needs a query parameter to return a 200 on a fresh workspace)
		makeProbe("searchQuery", "search", "/api/search", map[string]string{"query": "x"}),

		// starters
		makeProbe("startersList", "starters", "/api/starters", nil),

		// misc/static
		makeProbe("wsMetrics", "misc/static", "/api/ws-metrics", nil),
	}
}
