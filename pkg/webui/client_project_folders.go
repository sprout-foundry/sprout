//go:build !js

package webui

// client_workspaces.go — per-client project folder bookkeeping: the project root a
// client selected, the server-level mirror for the default client, and the
// remembered selection that outlives an evicted context. Its on-disk copy
// lives in client_workspaces_store.go.

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// projectRootOr is the project folder the client selected, or fallback for a
// context that has none recorded.
func (cc *webClientContext) projectRootOr(fallback string) string {
	if root := strings.TrimSpace(cc.ProjectRoot); root != "" {
		return root
	}
	return fallback
}

// setClientActiveRootLocked points a client at root (its project or one of
// its worktrees) without resetting chats. Only the default client mirrors the
// change into ws.projectFolder: that root is what new contexts and the CLI
// fall back to, so another tab's chat or worktree switch must not move it.
// Callers must hold ws.mutex.
func (ws *ReactWebServer) setClientActiveRootLocked(clientID string, ctx *webClientContext, root string) (previous string, changed bool) {
	previous = ctx.ProjectFolder
	if root == "" || root == previous {
		return previous, false
	}
	ctx.ProjectFolder = root
	if clientID == defaultWebClientID {
		ws.projectFolder = root
	} else {
		ws.rememberClientProjectFolderLocked(clientID, ctx)
	}
	return previous, true
}

// publishProjectFolderChanged tells one client's tabs that their project folder
// moved so the UI re-reads it instead of showing (and acting on) a stale path.
func (ws *ReactWebServer) publishProjectFolderChanged(clientID, previous, current, source string) {
	data := map[string]interface{}{
		"daemon_root":             ws.GetDaemonRoot(),
		"project_folder":          current,
		"previous_project_folder": previous,
	}
	if source != "" {
		data["source"] = source
	}
	ws.publishClientEvent(clientID, events.EventTypeProjectFolderChanged, data)
}

// rememberClientProjectFolderLocked records the client's selected project so a
// recreated context (after idle eviction or a daemon restart) starts there.
// The project root is remembered rather than a chat's worktree: chats do not
// survive the context, and the worktree they were bound to may be gone.
func (ws *ReactWebServer) rememberClientProjectFolderLocked(clientID string, ctx *webClientContext) {
	if clientID == defaultWebClientID || ctx == nil {
		return
	}
	root := ctx.projectRootOr(strings.TrimSpace(ctx.ProjectFolder))
	if root == "" {
		return
	}
	if ws.clientProjectFolders == nil {
		ws.clientProjectFolders = make(map[string]string)
	}
	if ws.clientProjectFolders[clientID] == root {
		return
	}
	ws.clientProjectFolders[clientID] = root
	ws.touchClientProjectFolderLocked(clientID)
	ws.saveClientProjectFoldersLocked()
}

// rememberClientProjectFoldersLocked snapshots every non-default client
// context's selection into ws.clientProjectFolders. Called while holding
// ws.mutex — from the idle eviction worker before it deletes contexts.
// Default-client entries are skipped: that client follows ws.projectFolder.
func (ws *ReactWebServer) rememberClientProjectFoldersLocked() {
	for clientID, ctx := range ws.clientContexts {
		ws.rememberClientProjectFolderLocked(clientID, ctx)
	}
}

func (ws *ReactWebServer) forgetClientProjectFolderLocked(clientID string) {
	if _, ok := ws.clientProjectFolders[clientID]; !ok {
		return
	}
	delete(ws.clientProjectFolders, clientID)
	delete(ws.clientProjectFoldersUpdatedAt, clientID)
	ws.saveClientProjectFoldersLocked()
}
