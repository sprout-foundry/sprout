//go:build !js

package webui

import (
	"context"
	"net/http"
	"runtime/pprof"
	"time"

	lspproxy "github.com/sprout-foundry/sprout/pkg/lsp/proxy"
)

func (ws *ReactWebServer) setupRoutes(ctx context.Context) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", ws.handleIndex)
	// /ssh/ is a reverse proxy registered before /ws and /terminal so prefix match works.
	mux.HandleFunc("/ssh/", ws.handleSSHProxy)

	ws.registerCoreRoutes(mux)
	ws.registerTerminalRoutes(mux, ctx)
	ws.registerQueryRoutes(mux)
	ws.registerCommandRoutes(mux)
	ws.registerDiagnosticsRoutes(mux)
	ws.registerFileRoutes(mux)
	ws.registerDesignRoutes(mux)
	ws.registerStarterRoutes(mux)
	ws.registerPreviewRoutes(mux)
	ws.registerSettingsRoutes(mux)
	ws.registerWorkspaceRoutes(mux)
	ws.registerSyncRoutes(mux)
	ws.registerGitRoutes(mux)
	ws.registerSessionRoutes(mux)
	ws.registerSearchRoutes(mux)
	ws.registerChangesRoutes(mux)
	ws.registerAutomateRoutes(mux)
	ws.registerHumaRoutes(mux)

	return mux
}

// The Huma operations (every huma.Register call) live in huma_routes.go,
// which registerHumaRoutes (huma_api.go) mounts on the same ServeMux. Each
// operation is registered once by the humago adapter as a method+path pattern,
// so the plain mux registrations for those routes were removed. The contract
// test's AST walk reads the Huma paths from huma_routes.go.
func (ws *ReactWebServer) registerCoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws", ws.handleWebSocket)
	mux.HandleFunc("/terminal", ws.handleTerminalWebSocket)
	mux.HandleFunc("/static/", ws.handleStaticFiles)
	mux.HandleFunc("/assets/", ws.handleAssets)
	mux.HandleFunc("/sw.js", ws.handleServiceWorker)
	mux.HandleFunc("/manifest.json", ws.handleManifest)
	mux.HandleFunc("/browserconfig.xml", ws.handleBrowserConfig)
	mux.HandleFunc("/asset-manifest.json", ws.handleAssetManifest)
	mux.HandleFunc("/icon-192.png", ws.handleIcon192)
	mux.HandleFunc("/icon-512.png", ws.handleIcon512)
	mux.HandleFunc("/logo-mark.svg", ws.handleLogoMark)
	mux.HandleFunc("/favicon.ico", ws.handleFavicon)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "ok",
			"port":   ws.port,
			"uptime": time.Since(ws.startTime).String(),
		}
		// Report whether an agent backend is available. In daemon mode
		// (ws.agent == nil) agents are created per-client, so "available"
		// means the config manager can produce one — approximated here by
		// checking whether any client context has an agent.
		if ws.agent != nil {
			resp["agent_available"] = true
		} else {
			resp["agent_available"] = ws.serviceMode
		}
		ws.mutex.RLock()
		resp["active_queries"] = ws.activeQueries
		ws.mutex.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("/api/bootstrap", ws.handleAPIBootstrap)

	// Always-on goroutine dump endpoint. Unlike --debug-pprof (which requires
	// a separate port and is opt-in), this is available on the main webui port
	// so a stuck session can be diagnosed by curling
	//   curl http://localhost:<port>/debug/goroutines
	// without restarting the process. Returns the same stack dump as SIGQUIT
	// but does NOT kill the process.
	mux.HandleFunc("/debug/goroutines", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_ = pprof.Lookup("goroutine").WriteTo(w, 1)
	})
}

func (ws *ReactWebServer) registerQueryRoutes(mux *http.ServeMux) {
	// The /api/query* and /api/completion routes are Huma operations (see
	// registerHumaOperations in huma_routes.go); their plain registrations
	// were removed so each method+path pattern is registered once. The
	// /api/edits/, /api/shell-approvals/, and /api/subagent/ routes are
	// likewise Huma operations (registered as method+subtree patterns); the
	// handlers parse r.URL.Path as before.
	// SP-089-3: password prompt endpoints.
	mux.HandleFunc("/api/password/", ws.handleAPIPasswordRoutes)
	// Foundry proxy endpoints — accept the translated chat format from CloudAdapter
	mux.HandleFunc("/api/proxy/chat", ws.handleAPIProxyChat)
	mux.HandleFunc("/api/proxy/chat/stop", ws.handleAPIProxyChatStop)
	mux.HandleFunc("/api/proxy/chat/status", ws.handleAPIProxyChatStatus)
	mux.HandleFunc("/api/proxy/stats", ws.handleAPIProxyStats)
}

// registerCommandRoutes mounts SP-114 Phase 2 endpoints. /api/command/execute
// is the dedicated command surface (separate from /api/query/steer which is
// for mid-turn steering of an active LLM query). Commands here must be
// SteerCapable — destructive commands stay CLI-only. /api/command/complete
// is the command-bar argument/name completion endpoint (mirrors the
// terminal's cmd/slash_completer.go over HTTP).
func (ws *ReactWebServer) registerCommandRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/command/execute", ws.handleAPICommandExecute)
	mux.HandleFunc("/api/command/complete", ws.handleAPICommandComplete)
}

func (ws *ReactWebServer) registerDiagnosticsRoutes(mux *http.ServeMux) {
	// /api/stats is now a Huma operation (see registerHumaRoutes); the plain
	// registration was removed so the method+path pattern is registered once.
	mux.HandleFunc("/api/providers", ws.handleAPIProviders)
	mux.HandleFunc("/api/providers/models", ws.handleGetModels)
	mux.HandleFunc("/api/diagnostics", ws.handleAPIDiagnostics)
	mux.HandleFunc("/api/semantic", ws.handleAPISemantic)
	mux.HandleFunc("/api/support-bundle", ws.handleAPISupportBundle)
	mux.HandleFunc("/api/ws-metrics", ws.handleAPIWSMetrics)
}

func (ws *ReactWebServer) registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/files", ws.handleAPIFiles)
	mux.HandleFunc("/api/files/prettier-config", ws.handleAPIGetPrettierConfig)
	mux.HandleFunc("/api/create", ws.handleAPICreateFile)
	mux.HandleFunc("/api/delete", ws.handleAPIDeleteItem)
	mux.HandleFunc("/api/rename", ws.handleAPIRenameItem)
	mux.HandleFunc("/api/open-in-file-browser", ws.handleAPIOpenInFileBrowser)
	mux.HandleFunc("/api/browse", ws.handleAPIBrowse)
	mux.HandleFunc("/api/file", ws.handleAPIFile)
	mux.HandleFunc("/api/file/consent", ws.handleAPIFileConsent)
	mux.HandleFunc("/api/file/check-modified", ws.handleAPIFileCheckModified)
}

// registerDesignRoutes mounts SP-140-6 §6b's read-only design endpoint.
// GET /api/design/status aggregates the tree's validation/drift/feedback
// signals for the webui health strip (§6c) — the same pkg/design scanners
// the agent tools read, so both surfaces share one truth.
func (ws *ReactWebServer) registerDesignRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/design/status", ws.handleAPIDesignStatus)
}

// registerStarterRoutes mounts the SP-153 §153b starter surface (TODO
// 153.6): the embedded starter catalogue and the instantiate endpoint
// the web UI's new-project flow (153.7) calls to populate a fresh
// project directory.
func (ws *ReactWebServer) registerStarterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/starters", ws.handleAPIStartersList)
	mux.HandleFunc("/api/starters/instantiate", ws.handleAPIStartersInstantiate)
}

func (ws *ReactWebServer) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/onboarding/status", ws.handleAPIOnboardingStatus)
	mux.HandleFunc("/api/onboarding/complete", ws.handleAPIOnboardingComplete)
	mux.HandleFunc("/api/onboarding/skip", ws.handleAPIOnboardingSkip)
	// /api/config is now a Huma operation (see registerHumaRoutes); the plain
	// registration was removed so the method+path pattern is registered once.
	mux.HandleFunc("/api/settings", ws.handleAPISettings)
	mux.HandleFunc("/api/settings/mcp", ws.handleAPISettingsMCP)
	mux.HandleFunc("/api/settings/mcp/servers/", ws.handleAPISettingsMCPServers)
	mux.HandleFunc("/api/settings/providers", ws.handleAPISettingsProviders)
	mux.HandleFunc("/api/settings/providers/", ws.handleAPISettingsProviders)
	mux.HandleFunc("/api/settings/credentials", ws.handleAPISettingsCredentials)
	mux.HandleFunc("/api/settings/credentials/", ws.handleAPISettingsCredentials)
	mux.HandleFunc("/api/settings/skills", ws.handleAPISettingsSkills)
	mux.HandleFunc("/api/skills", ws.handleAPIListSkills)
	mux.HandleFunc("/api/skills/", ws.handleAPISkillsRoutes)
	mux.HandleFunc("/api/settings/subagent-types", ws.handleAPISettingsSubagentTypes)
	mux.HandleFunc("/api/settings/subagent-types/", ws.handleAPISettingsSubagentTypes)
	mux.HandleFunc("/api/hotkeys", ws.handleAPIHotkeys)
	mux.HandleFunc("/api/hotkeys/validate", ws.handleAPIHotkeysValidate)
	mux.HandleFunc("/api/hotkeys/preset", ws.handleAPIHotkeysPreset)
	mux.HandleFunc("/api/computer-use/test", ws.handleAPIComputerUseTest)
	mux.HandleFunc("/api/local-llm/status", ws.handleLocalLLMStatus)
	mux.HandleFunc("/api/local-llm/start", ws.handleLocalLLMStart)
	mux.HandleFunc("/api/local-llm/models", ws.handleLocalLLMModels)
	mux.HandleFunc("/api/local-llm/download", ws.handleLocalLLMDownload)
	mux.HandleFunc("/api/local-llm/download/cancel", ws.handleLocalLLMDownloadCancel)
}

func (ws *ReactWebServer) registerWorkspaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/workspace", ws.handleAPIWorkspace)
	mux.HandleFunc("/api/workspace/browse", ws.handleAPIWorkspaceBrowse)
	mux.HandleFunc("/api/workspace/symbols", ws.handleAPIWorkspaceSymbols)
	mux.HandleFunc("/api/workspace/projects", ws.handleAPIWorkspaceProjects)
	// SP-046: workspace sync handlers
	mux.HandleFunc("/api/workspace/sync", ws.handleAPIWorkspaceSync)
	mux.HandleFunc("/api/workspace/takeover", ws.handleAPIWorkspaceTakeover)
	mux.HandleFunc("/api/instances", ws.handleAPIInstances)
	mux.HandleFunc("/api/instances/select", ws.handleAPIInstanceSelect)
	mux.HandleFunc("/api/instances/ssh-hosts", ws.handleAPISSHHosts)
	mux.HandleFunc("/api/instances/ssh-open", ws.handleAPISSHOpen)
	mux.HandleFunc("/api/instances/ssh-launch-status", ws.handleAPISSHLaunchStatus)
	mux.HandleFunc("/api/instances/ssh-browse", ws.handleAPISSHBrowse)
	mux.HandleFunc("/api/instances/ssh-sessions", ws.handleAPISSHSessions)
	mux.HandleFunc("/api/instances/ssh-close", ws.handleAPISSHSessionDelete)
}

func (ws *ReactWebServer) registerSyncRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/sync/op", ws.handleAPISyncOp)
	mux.HandleFunc("/api/sync/batch", ws.handleAPISyncBatch)
	mux.HandleFunc("/api/sync/status", ws.handleAPISyncStatus)
}

func (ws *ReactWebServer) registerGitRoutes(mux *http.ServeMux) {
	// ETH-2 transactional escalation: the container-side execution surface
	// the platform drives a push/run/pull transaction against. Exact-match
	// patterns. Method semantics are load-bearing: GET/HEAD on /api/txn/status
	// is read-only (unauthenticated through the auth middleware); the three
	// POSTs mutate or execute and sit behind the Bearer boundary — see
	// api_txn.go.
	mux.HandleFunc("/api/txn/push", ws.handleAPITxnPush)
	mux.HandleFunc("/api/txn/run", ws.handleAPITxnRun)
	mux.HandleFunc("/api/txn/status", ws.handleAPITxnStatus)
	mux.HandleFunc("/api/txn/pull", ws.handleAPITxnPull)

	// ETH-1 sync-on-resume: workspace git reconciliation report (same JSON
	// as `sprout sync`). Exact-match pattern — it does not collide with the
	// /api/sync/op|batch|status agent file-sync routes registered in
	// registerSyncRoutes. Method semantics are load-bearing: GET/HEAD is
	// status-only (unauthenticated through the auth middleware), POST is
	// the only method that may pull — see handleAPISync.
	mux.HandleFunc("/api/sync", ws.handleAPISync)
	mux.HandleFunc("/api/git/status", ws.handleAPIGitStatus)
	mux.HandleFunc("/api/git/stage", ws.handleAPIGitStage)
	mux.HandleFunc("/api/git/unstage", ws.handleAPIGitUnstage)
	mux.HandleFunc("/api/git/discard", ws.handleAPIGitDiscard)
	mux.HandleFunc("/api/git/commit", ws.handleAPIGitCommit)
	mux.HandleFunc("/api/git/commit-message", ws.handleAPIGitCommitMessage)
	mux.HandleFunc("/api/git/confirm", ws.handleAPIConfirm)
	mux.HandleFunc("/api/git/deep-review", ws.handleAPIGitDeepReview)
	mux.HandleFunc("/api/git/deep-review/fix", ws.handleAPIGitDeepReviewFix)
	mux.HandleFunc("/api/git/deep-review/fix/start", ws.handleAPIGitDeepReviewFixStart)
	mux.HandleFunc("/api/git/deep-review/fix/status", ws.handleAPIGitDeepReviewFixStatus)
	mux.HandleFunc("/api/git/stage-all", ws.handleAPIGitStageAll)
	mux.HandleFunc("/api/git/unstage-all", ws.handleAPIGitUnstageAll)
	mux.HandleFunc("/api/git/diff", ws.handleAPIGitDiff)
	mux.HandleFunc("/api/git/branches", ws.handleAPIGitBranches)
	mux.HandleFunc("/api/git/worktrees", ws.handleAPIGitWorktrees)
	mux.HandleFunc("/api/git/worktree/create", ws.handleAPIGitWorktreeCreate)
	mux.HandleFunc("/api/git/worktree/remove", ws.handleAPIGitWorktreeRemove)
	mux.HandleFunc("/api/git/worktree/checkout", ws.handleAPIGitWorktreeCheckout)
	mux.HandleFunc("/api/git/checkout", ws.handleAPIGitCheckout)
	mux.HandleFunc("/api/git/revert", ws.handleAPIGitRevert)
	mux.HandleFunc("/api/git/pull-request", ws.handleAPIGitPullRequest)
	mux.HandleFunc("/api/git/branch/create", ws.handleAPIGitCreateBranch)
	mux.HandleFunc("/api/git/pull", ws.handleAPIGitPull)
	mux.HandleFunc("/api/git/push", ws.handleAPIGitPush)
	mux.HandleFunc("/api/git/log", ws.handleAPIGitLog)
	mux.HandleFunc("/api/git/commit/show", ws.handleAPIGitCommitShow)
	mux.HandleFunc("/api/git/commit/show/file", ws.handleAPIGitCommitFileDiff)
}

func (ws *ReactWebServer) registerTerminalRoutes(mux *http.ServeMux, ctx context.Context) {
	ws.lspManager = lspproxy.NewManager(ctx)
	mux.HandleFunc("/api/lsp/ws", lspproxy.BridgeHandler(ws.lspManager, ws.upgrader, ws.workspaceRoot))
	mux.HandleFunc("/api/lsp/status", ws.handleLSPStatus)
	mux.HandleFunc("/api/terminal/history", ws.handleTerminalHistory)
	mux.HandleFunc("/api/terminal/sessions", ws.handleAPITerminalSessions)
	mux.HandleFunc("/api/terminal/shells", ws.handleAPITerminalShells)
	mux.HandleFunc("/api/terminal/agent-sessions", ws.handleAPIAgentSessions)
	mux.HandleFunc("/api/terminal/agent-sessions/", ws.handleAPIAgentSessionActions)
}

func (ws *ReactWebServer) registerSessionRoutes(mux *http.ServeMux) {
	// The /api/sessions* and /api/chat-sessions* routes are Huma operations
	// (see registerHumaOperations in huma_routes.go); their plain registrations
	// were removed so each method+path pattern is registered once. The
	// singular /api/chat-session/ worktree route is a separate surface and
	// stays a plain handler.
	mux.HandleFunc("/api/chat-session/", ws.handleAPIChatSessionWorktree)
}

func (ws *ReactWebServer) registerSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/search", ws.handleAPIQuerySearch)
	mux.HandleFunc("/api/search/replace", ws.handleAPIQuerySearchReplace)
	mux.HandleFunc("/api/upload/image", ws.handleUploadImage)
}
