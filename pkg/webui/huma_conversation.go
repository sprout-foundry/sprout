//go:build !js

// huma_conversation.go holds the Huma operation handlers for the conversation
// family (the chat-sessions, sessions, subagent, edits, and shell-approvals
// routes) that are registered in registerHumaOperations (huma_routes.go).
//
// Each handler is a thin wrapper over the existing plain handler: it hands the
// live request and ResponseWriter (captured via humaRequestInput) to the plain
// handler, which parses the request and writes the response exactly as it
// always did. The Huma output body is a no-op callback, so Huma writes nothing
// of its own — the wire bytes are exactly what the plain handler produced,
// which is what keeps the migration behavior-preserving. The HTTP method gate
// is enforced by the Huma operation; a wrong method reaches the SPA catch-all
// (the same behavior the migrated query/completion family relies on).
package webui

import (
	"context"
)

// chatSessionsHumaHandler is the Huma handler for GET /api/chat-sessions.
func (ws *ReactWebServer) chatSessionsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessions(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsCreateHumaHandler is the Huma handler for POST /api/chat-sessions/create.
func (ws *ReactWebServer) chatSessionsCreateHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsCreate(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsCreateInWorktreeHumaHandler is the Huma handler for POST /api/chat-sessions/create-in-worktree.
func (ws *ReactWebServer) chatSessionsCreateInWorktreeHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionCreateInWorktree(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsDeleteHumaHandler is the Huma handler for POST /api/chat-sessions/delete.
func (ws *ReactWebServer) chatSessionsDeleteHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsDelete(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsDeleteAllHumaHandler is the Huma handler for POST /api/chat-sessions/delete-all.
func (ws *ReactWebServer) chatSessionsDeleteAllHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsDeleteAll(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsRenameHumaHandler is the Huma handler for POST /api/chat-sessions/rename.
func (ws *ReactWebServer) chatSessionsRenameHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsRename(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsPinHumaHandler is the Huma handler for POST /api/chat-sessions/pin.
func (ws *ReactWebServer) chatSessionsPinHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsPin(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsUnpinHumaHandler is the Huma handler for POST /api/chat-sessions/unpin.
func (ws *ReactWebServer) chatSessionsUnpinHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsUnpin(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsSwitchHumaHandler is the Huma handler for POST /api/chat-sessions/switch.
func (ws *ReactWebServer) chatSessionsSwitchHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsSwitch(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionMessagesHumaHandler is the Huma handler for GET /api/chat-sessions/messages.
func (ws *ReactWebServer) chatSessionMessagesHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionMessages(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionsCompactHumaHandler is the Huma handler for POST /api/chat-sessions/compact.
func (ws *ReactWebServer) chatSessionsCompactHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionsCompact(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionClearHistoryHumaHandler is the Huma handler for POST /api/chat-sessions/history.
func (ws *ReactWebServer) chatSessionClearHistoryHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionClearHistory(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionForkHumaHandler is the Huma handler for POST /api/chat-sessions/fork.
func (ws *ReactWebServer) chatSessionForkHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionFork(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionBreakpointsHumaHandler is the Huma handler for POST /api/chat-sessions/breakpoints.
func (ws *ReactWebServer) chatSessionBreakpointsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionBreakpoints(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionWorktreeListHumaHandler is the Huma handler for GET /api/chat-sessions/worktree-mappings.
func (ws *ReactWebServer) chatSessionWorktreeListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionWorktreeList(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sessionsListHumaHandler is the Huma handler for GET /api/sessions.
func (ws *ReactWebServer) sessionsListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISessions(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sessionRestoreHumaHandler is the Huma handler for POST /api/sessions/restore.
func (ws *ReactWebServer) sessionRestoreHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIRestoreSession(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sessionsSearchHumaHandler is the Huma handler for GET /api/sessions/search.
func (ws *ReactWebServer) sessionsSearchHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISessionsSearch(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// sessionExportHumaHandler is the Huma handler for GET /api/sessions/{id}/export.
// The plain handler reads the id via r.PathValue("id") (populated by the
// ServeMux {id} pattern when routed through the mux) with a fallback for direct
// test calls, so the wrapped call is byte-identical.
func (ws *ReactWebServer) sessionExportHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISessionExport(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// subagentCancelHumaHandler is the Huma handler for POST /api/subagent/
// (documented shape /api/subagent/{id}/cancel). The plain handler parses the id
// from r.URL.Path, which is unchanged by the subtree routing.
func (ws *ReactWebServer) subagentCancelHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISubagentCancel(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// editStatusHumaHandler is the Huma handler for GET /api/edits/ (documented
// shape /api/edits/{id}). The plain dispatcher routes to the status branch when
// the path has no /decision suffix.
func (ws *ReactWebServer) editStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIEdits(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// editDecisionHumaHandler is the Huma handler for POST /api/edits/ (documented
// shape /api/edits/{id}/decision). The plain dispatcher routes to the decision
// branch when the path ends in /decision.
func (ws *ReactWebServer) editDecisionHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIEdits(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// shellApprovalDecisionHumaHandler is the Huma handler for POST
// /api/shell-approvals/ (documented shape /api/shell-approvals/{id}/decision).
func (ws *ReactWebServer) shellApprovalDecisionHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIShellApprovals(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// chatSessionWorktreeHumaHandler is the Huma handler for GET and POST
// /api/chat-session/ (the worktree surface, documented shapes
// /api/chat-session/{chatId}/worktree and .../worktree/switch). The plain
// dispatcher parses the {chatId}/worktree[/switch] sub-segments from r.URL.Path
// and routes to the get/set/switch branch; each enforces its own method gate.
func (ws *ReactWebServer) chatSessionWorktreeHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIChatSessionWorktree(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
