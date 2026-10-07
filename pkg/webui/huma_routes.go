//go:build !js

// huma_routes.go holds registerHumaOperations, the single function that
// registers every Huma operation on the API. The huma.Register calls live
// here (not in routes.go) so the contract test's AST walk of the Huma route
// files discovers the Huma paths alongside the plain mux patterns, and so the
// registration body stays under the source line budget. Both the live server
// (via registerHumaRoutes in huma_api.go) and the contract doc generator (via
// HumaOpenAPIDoc) call this one function, so the registered set cannot drift.
package webui

import (
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

func registerHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "get-stats",
		Method:      http.MethodGet,
		Path:        "/api/stats",
		Summary:     "Server statistics",
		Description: "Reports server and per-client statistics, including provider, model, and token usage.",
		Tags:        []string{"diagnostics"},
	}, ws.humaGetStats)

	huma.Register(api, huma.Operation{
		OperationID: "get-config",
		Method:      http.MethodGet,
		Path:        "/api/config",
		Summary:     "Current server configuration",
		Description: "Reports the server's port, daemon and workspace roots, agent metadata, and enabled features.",
		Tags:        []string{"settings"},
	}, ws.humaGetConfig)

	// ---- conversation/query family ----------------
	// Each of these operations reuses the same builder the plain handler
	// drives, so the registered set (live server + contract doc generator)
	// cannot drift. The plain mux registrations for these routes are removed
	// (the method+path pattern is registered once by the humago adapter).

	huma.Register(api, huma.Operation{
		OperationID: "queryStart",
		Method:      http.MethodPost,
		Path:        "/api/query",
		Summary:     "Start a query in a chat session.",
		Description: "Submits a query to the agent for the resolved chat (body `chat_id` or the client's active chat) and starts the query lifecycle asynchronously. The response is written by the shared runner, so this operation reports a 202 Accepted with the submitted query text.",
		Tags:        []string{"conversation/query"},
	}, ws.queryHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "querySteer",
		Method:      http.MethodPost,
		Path:        "/api/query/steer",
		Summary:     "Steer an active query.",
		Description: "Injects user input into the currently running query loop, or executes a safe slash command mid-turn. Requires an active query.",
		Tags:        []string{"conversation/query"},
	}, ws.steerHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "querySteerRetract",
		Method:      http.MethodPost,
		Path:        "/api/query/steer/retract",
		Summary:     "Retract the latest staged steer.",
		Description: "Pulls back the newest staged-but-unpicked steer message so the user can edit it. Returns 200 with success=false when nothing is pending.",
		Tags:        []string{"conversation/query"},
	}, ws.steerRetractHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "queryStop",
		Method:      http.MethodPost,
		Path:        "/api/query/stop",
		Summary:     "Stop the active query.",
		Description: "Interrupts the currently running query loop and cancels its subagents. Returns 200 already_completed when there is nothing to stop.",
		Tags:        []string{"conversation/query"},
	}, ws.queryStopHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "queryStatus",
		Method:      http.MethodGet,
		Path:        "/api/query/status",
		Summary:     "Active-query status for a chat.",
		Description: "Reports whether a query is currently active for the resolved chat. A polling fallback for when the WebSocket drops and reconnects.",
		Tags:        []string{"conversation/query"},
	}, ws.queryStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "queryRewind",
		Method:      http.MethodPost,
		Path:        "/api/query/rewind",
		Summary:     "Rewind the conversation to a prior turn.",
		Description: "Truncates the conversation history back to a prior turn, optionally reverting file changes made during the discarded turns. Requires `to_turn`.",
		Tags:        []string{"conversation/query"},
	}, ws.rewindHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "completionGenerate",
		Method:      http.MethodPost,
		Path:        "/api/completion",
		Summary:     "Generate a code completion.",
		Description: "Generates a code completion for the given prefix/suffix using the resolved completion provider and model. Requires `prefix`.",
		Tags:        []string{"conversation/query"},
	}, ws.completionHumaHandler)

	// ---- conversation/chat-sessions + sessions family ----------------
	// Each of these operations drives the existing plain handler through the
	// live ResponseWriter (see huma_conversation.go), so the response bytes are
	// unchanged and the documented request/response schemas in the seed carry
	// through the merge. The subtree routes (/api/subagent/, /api/edits/,
	// /api/shell-approvals/) are registered as method+subtree patterns (the
	// humago adapter passes the operation path straight to ServeMux), which the
	// handlers parse by reading r.URL.Path as they already do.
	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsList",
		Method:      http.MethodGet,
		Path:        "/api/chat-sessions",
		Summary:     "List the client's chat sessions.",
		Description: "Lists the requesting client's chat sessions with metadata, including the active chat.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsCreate",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/create",
		Summary:     "Create a chat session.",
		Description: "Creates a new chat session for the client. In shared mode this is rejected beyond the single shared chat.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsCreateHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsCreateInWorktree",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/create-in-worktree",
		Summary:     "Create a chat session in a new git worktree.",
		Description: "Creates a git worktree off the current branch and a chat session bound to it.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsCreateInWorktreeHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsDelete",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/delete",
		Summary:     "Delete a chat session.",
		Description: "Deletes a chat session. The default session cannot be deleted.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsDeleteHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsDeleteAll",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/delete-all",
		Summary:     "Delete all non-default chat sessions.",
		Description: "Deletes every chat session except the default one.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsDeleteAllHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsRename",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/rename",
		Summary:     "Rename a chat session.",
		Description: "Renames a chat session and emits a session_changed event for the client.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsRenameHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsPin",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/pin",
		Summary:     "Pin a chat session.",
		Description: "Pins a chat session so it sorts to the top of the session list.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsPinHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsUnpin",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/unpin",
		Summary:     "Unpin a chat session.",
		Description: "Removes the pin from a chat session.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsUnpinHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsSwitch",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/switch",
		Summary:     "Switch the active chat session.",
		Description: "Sets the client's active chat session and emits a session_changed event.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsSwitchHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionMessages",
		Method:      http.MethodGet,
		Path:        "/api/chat-sessions/messages",
		Summary:     "Fetch a chat session's messages.",
		Description: "Returns the display messages for a chat session.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionMessagesHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionsCompact",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/compact",
		Summary:     "Compact a chat session's state.",
		Description: "Compacts a chat session's conversation state to reclaim context.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionsCompactHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionClearHistory",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/history",
		Summary:     "Clear a chat session's history.",
		Description: "Clears a chat session's message history.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionClearHistoryHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionFork",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/fork",
		Summary:     "Fork a chat session at a breakpoint.",
		Description: "Forks a chat session's history at a prior turn into a new session.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionForkHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionBreakpoints",
		Method:      http.MethodPost,
		Path:        "/api/chat-sessions/breakpoints",
		Summary:     "List a chat session's forkable breakpoints.",
		Description: "Lists the turns a chat session can be forked at.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionBreakpointsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionWorktreeList",
		Method:      http.MethodGet,
		Path:        "/api/chat-sessions/worktree-mappings",
		Summary:     "List chat sessions with worktree mappings.",
		Description: "Lists the chat sessions bound to git worktrees.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionWorktreeListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sessionsList",
		Method:      http.MethodGet,
		Path:        "/api/sessions",
		Summary:     "List saved sessions.",
		Description: "Lists saved sessions with message count and token metadata, scoped by the `scope` query parameter.",
		Tags:        []string{"conversation/query"},
	}, ws.sessionsListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sessionRestore",
		Method:      http.MethodPost,
		Path:        "/api/sessions/restore",
		Summary:     "Restore a saved session.",
		Description: "Restores a saved session into the client's active (or a specified) chat and publishes the recovered messages.",
		Tags:        []string{"conversation/query"},
	}, ws.sessionRestoreHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sessionsSearch",
		Method:      http.MethodGet,
		Path:        "/api/sessions/search",
		Summary:     "Search across saved sessions.",
		Description: "Searches saved sessions' transcripts for a query string.",
		Tags:        []string{"conversation/query"},
	}, ws.sessionsSearchHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "sessionExport",
		Method:      http.MethodGet,
		Path:        "/api/sessions/{id}/export",
		Summary:     "Export a saved session to a downloadable file.",
		Description: "Streams a saved session to a downloadable file (json or markdown) with a Content-Disposition header.",
		Tags:        []string{"conversation/query"},
	}, ws.sessionExportHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "subagentCancel",
		Method:      http.MethodPost,
		Path:        "/api/subagent/",
		Summary:     "Cancel a running subagent.",
		Description: "Cancels one running subagent in the active chat at the /api/subagent/{id}/cancel shape. Idempotent: a non-running ID returns a 200 already_completed payload.",
		Tags:        []string{"conversation/query"},
	}, ws.subagentCancelHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "editStatus",
		Method:      http.MethodGet,
		Path:        "/api/edits/",
		Summary:     "Get a pending edit proposal's status.",
		Description: "Returns the state of a pending edit proposal at the /api/edits/{id} shape.",
		Tags:        []string{"conversation/query"},
	}, ws.editStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "editDecision",
		Method:      http.MethodPost,
		Path:        "/api/edits/",
		Summary:     "Submit a per-hunk edit decision.",
		Description: "Submits the user's per-hunk accept/reject choices for a pending edit at the /api/edits/{id}/decision shape, unblocking the agent's edit-approval broker.",
		Tags:        []string{"conversation/query"},
	}, ws.editDecisionHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "shellApprovalDecision",
		Method:      http.MethodPost,
		Path:        "/api/shell-approvals/",
		Summary:     "Submit a per-part shell approval decision.",
		Description: "Submits per-part accept/reject choices for a pending shell approval at the /api/shell-approvals/{id}/decision shape, unblocking the agent's shell-approval broker.",
		Tags:        []string{"conversation/query"},
	}, ws.shellApprovalDecisionHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionWorktreeGet",
		Method:      http.MethodGet,
		Path:        "/api/chat-session/",
		Summary:     "Get a chat session's worktree path.",
		Description: "Returns the worktree path bound to the chat session at the /api/chat-session/{chatId}/worktree shape (the worktree-mapping GET form).",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionWorktreeHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "chatSessionWorktreeSet",
		Method:      http.MethodPost,
		Path:        "/api/chat-session/",
		Summary:     "Set or switch a chat session's worktree.",
		Description: "Sets the worktree path for the chat session at the /api/chat-session/{chatId}/worktree shape, or switches the active workspace to it at the /api/chat-session/{chatId}/worktree/switch shape. Only the switch and set branches accept POST.",
		Tags:        []string{"conversation/query"},
	}, ws.chatSessionWorktreeHumaHandler)

	// ---- files family ----------------
	registerFilesHumaOperations(api, ws)

	// ---- git family ----------------
	registerGitHumaOperations(api, ws)

	// ---- settings/configuration family ----------------
	registerSettingsHumaOperations(api, ws)
	registerSettingsMiscHumaOperations(api, ws)

	// ---- workspace/instances family ----------------
	registerWorkspaceHumaOperations(api, ws)

	// ---- terminal family ----------------
	registerTerminalHumaOperations(api, ws)

	// ---- sync/txn family ----------------
	registerSyncTxnHumaOperations(api, ws)

	// ---- command family ----------------
	registerCommandHumaOperations(api, ws)

	// ---- proxy family ----------------
	registerProxyHumaOperations(api, ws)

	// ---- cross-family misc (support-bundle, ws-metrics, open-in-file-browser,
	// computer-use/test, browse) ----------------
	registerMiscHumaOperations(api, ws)

	// ---- design + starters families ----------------
	registerDesignStartersHumaOperations(api, ws)
}
