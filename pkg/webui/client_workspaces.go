//go:build !js

package webui

// client_workspaces.go — per-client workspace bookkeeping: the project root a
// client selected, the server-level mirror for the default client, and the
// remembered selection that outlives an evicted context.

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// projectRootOr is the workspace the client selected, or fallback for a
// context that has none recorded.
func (cc *webClientContext) projectRootOr(fallback string) string {
	if root := strings.TrimSpace(cc.ProjectRoot); root != "" {
		return root
	}
	return fallback
}

// setClientActiveRootLocked points a client at root (its project or one of
// its worktrees) without resetting chats. Only the default client mirrors the
// change into ws.workspaceRoot: that root is what new contexts and the CLI
// fall back to, so another tab's chat or worktree switch must not move it.
// Callers must hold ws.mutex.
func (ws *ReactWebServer) setClientActiveRootLocked(clientID string, ctx *webClientContext, root string) (previous string, changed bool) {
	previous = ctx.WorkspaceRoot
	if root == "" || root == previous {
		return previous, false
	}
	ctx.WorkspaceRoot = root
	if clientID == defaultWebClientID {
		ws.workspaceRoot = root
	} else {
		ws.rememberClientWorkspaceLocked(clientID, ctx)
	}
	return previous, true
}

// publishWorkspaceChanged tells one client's tabs that their workspace root
// moved so the UI re-reads it instead of showing (and acting on) a stale path.
func (ws *ReactWebServer) publishWorkspaceChanged(clientID, previous, current, source string) {
	data := map[string]interface{}{
		"daemon_root":             ws.GetDaemonRoot(),
		"workspace_root":          current,
		"previous_workspace_root": previous,
	}
	if source != "" {
		data["source"] = source
	}
	ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, data)
}

// rememberClientWorkspaceLocked records the client's selected project so a
// recreated context (after idle eviction or a daemon restart) starts there.
// The project root is remembered rather than a chat's worktree: chats do not
// survive the context, and the worktree they were bound to may be gone.
func (ws *ReactWebServer) rememberClientWorkspaceLocked(clientID string, ctx *webClientContext) {
	if clientID == defaultWebClientID || ctx == nil {
		return
	}
	root := ctx.projectRootOr(strings.TrimSpace(ctx.WorkspaceRoot))
	if root == "" {
		return
	}
	if ws.clientWorkspaces == nil {
		ws.clientWorkspaces = make(map[string]string)
	}
	if ws.clientWorkspaces[clientID] == root {
		return
	}
	ws.clientWorkspaces[clientID] = root
}

// rememberClientWorkspacesLocked snapshots every non-default client
// context's selection into ws.clientWorkspaces. Called while holding
// ws.mutex — from the idle eviction worker before it deletes contexts.
// Default-client entries are skipped: that client follows ws.workspaceRoot.
func (ws *ReactWebServer) rememberClientWorkspacesLocked() {
	for clientID, ctx := range ws.clientContexts {
		ws.rememberClientWorkspaceLocked(clientID, ctx)
	}
}

func (ws *ReactWebServer) forgetClientWorkspaceLocked(clientID string) {
	if _, ok := ws.clientWorkspaces[clientID]; !ok {
		return
	}
	delete(ws.clientWorkspaces, clientID)
}
