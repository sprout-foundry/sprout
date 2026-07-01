//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
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
	ws.registerDiagnosticsRoutes(mux)
	ws.registerFileRoutes(mux)
	ws.registerSettingsRoutes(mux)
	ws.registerWorkspaceRoutes(mux)
	ws.registerSyncRoutes(mux)
	ws.registerGitRoutes(mux)
	ws.registerSessionRoutes(mux)
	ws.registerSearchRoutes(mux)
	ws.registerChangesRoutes(mux)
	ws.registerAutomateRoutes(mux)

	return mux
}

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"port":   ws.port,
			"uptime": time.Since(ws.startTime).String(),
		})
	})
	mux.HandleFunc("/api/bootstrap", ws.handleAPIBootstrap)
}

func (ws *ReactWebServer) registerQueryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/query", ws.handleAPIQuery)
	mux.HandleFunc("/api/query/steer", ws.handleAPIQuerySteer)
	mux.HandleFunc("/api/query/stop", ws.handleAPIQueryStop)
	mux.HandleFunc("/api/query/status", ws.handleAPIQueryStatus)
	// SP-071-3: rewind the conversation to a prior turn.
	mux.HandleFunc("/api/query/rewind", ws.handleAPIQueryRewind)
	// SP-072-4: per-hunk edit approval endpoints.
	mux.HandleFunc("/api/edits/", ws.handleAPIEdits)
	// SP-093-3: per-part shell approval decision endpoint.
	mux.HandleFunc("/api/shell-approvals/", ws.handleAPIShellApprovals)
	// SP-089-3: password prompt endpoints.
	mux.HandleFunc("/api/password/", ws.handleAPIPasswordRoutes)
	// SP-059: per-subagent cancel; path is /api/subagent/{id}/cancel.
	mux.HandleFunc("/api/subagent/", ws.handleAPISubagentCancel)
	// Foundry proxy endpoints — accept the translated chat format from CloudAdapter
	mux.HandleFunc("/api/proxy/chat", ws.handleAPIProxyChat)
	mux.HandleFunc("/api/proxy/chat/stop", ws.handleAPIProxyChatStop)
	mux.HandleFunc("/api/proxy/chat/status", ws.handleAPIProxyChatStatus)
	mux.HandleFunc("/api/proxy/stats", ws.handleAPIProxyStats)
}

func (ws *ReactWebServer) registerDiagnosticsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/stats", ws.handleAPIStats)
	mux.HandleFunc("/api/embedding-index", ws.handleAPIEmbeddingIndex)
	mux.HandleFunc("/api/costs/summary", ws.handleCostsSummary)
	mux.HandleFunc("/api/costs/history", ws.handleCostsHistory)
	mux.HandleFunc("/api/costs/detail", ws.handleCostsDetail)
	mux.HandleFunc("/api/providers", ws.handleAPIProviders)
	mux.HandleFunc("/api/providers/models", ws.handleGetModels)
	mux.HandleFunc("/api/diagnostics", ws.handleAPIDiagnostics)
	mux.HandleFunc("/api/semantic", ws.handleAPISemantic)
	mux.HandleFunc("/api/recall", ws.handleAPIRecall)
	mux.HandleFunc("/api/support-bundle", ws.handleAPISupportBundle)
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

func (ws *ReactWebServer) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/onboarding/status", ws.handleAPIOnboardingStatus)
	mux.HandleFunc("/api/onboarding/complete", ws.handleAPIOnboardingComplete)
	mux.HandleFunc("/api/onboarding/skip", ws.handleAPIOnboardingSkip)
	mux.HandleFunc("/api/config", ws.handleAPIConfig)
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
	mux.HandleFunc("/api/sessions", ws.handleAPISessions)
	mux.HandleFunc("/api/sessions/restore", ws.handleAPIRestoreSession)
	mux.HandleFunc("/api/sessions/search", ws.handleAPISessionsSearch)
	mux.HandleFunc("/api/sessions/{id}/export", ws.handleAPISessionExport)
	// Revision history + rollback now flow through /api/changes/* (the
	// ChangeTracker session buffer) and the LLM rollback_changes tool.
	// The old /api/history/* endpoints were removed with RevisionListPanel.
	mux.HandleFunc("/api/chat-sessions", ws.handleAPIChatSessions)
	mux.HandleFunc("/api/chat-sessions/create", ws.handleAPIChatSessionsCreate)
	mux.HandleFunc("/api/chat-sessions/create-in-worktree", ws.handleAPIChatSessionCreateInWorktree)
	mux.HandleFunc("/api/chat-sessions/delete", ws.handleAPIChatSessionsDelete)
	mux.HandleFunc("/api/chat-sessions/delete-all", ws.handleAPIChatSessionsDeleteAll)
	mux.HandleFunc("/api/chat-sessions/rename", ws.handleAPIChatSessionsRename)
	mux.HandleFunc("/api/chat-sessions/pin", ws.handleAPIChatSessionsPin)
	mux.HandleFunc("/api/chat-sessions/unpin", ws.handleAPIChatSessionsUnpin)
	mux.HandleFunc("/api/chat-sessions/switch", ws.handleAPIChatSessionsSwitch)
	mux.HandleFunc("/api/chat-sessions/compact", ws.handleAPIChatSessionsCompact)
	mux.HandleFunc("/api/chat-sessions/history", ws.handleAPIChatSessionClearHistory)
	mux.HandleFunc("/api/chat-sessions/worktree-mappings", ws.handleAPIChatSessionWorktreeList)
	mux.HandleFunc("/api/chat-session/", ws.handleAPIChatSessionWorktree)
}

func (ws *ReactWebServer) registerSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/search", ws.handleAPIQuerySearch)
	mux.HandleFunc("/api/search/semantic/status", ws.handleAPISemanticStatus)
	mux.HandleFunc("/api/search/semantic/build", ws.handleAPISemanticBuild)
	mux.HandleFunc("/api/search/semantic/preview", ws.handleAPISemanticPreview)
	mux.HandleFunc("/api/search/semantic/preview-context", ws.handleAPISemanticPreviewContext)
	mux.HandleFunc("/api/search/semantic", ws.handleAPISemanticSearch)
	mux.HandleFunc("/api/search/replace", ws.handleAPIQuerySearchReplace)
	mux.HandleFunc("/api/upload/image", ws.handleUploadImage)
}
