//go:build !js

package webui

// client_context_ssh.go — the SSH + workspace wiring for client contexts
// (clearing a session key, remembering workspaces, setting the workspace root,
// the withAgentWorkspace chdir helper), split out of client_context.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (ws *ReactWebServer) clearClientSSHContextForSessionKey(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	for clientID, ctx := range ws.clientContexts {
		if ctx == nil || strings.TrimSpace(ctx.SSHSessionKey) != sessionKey {
			continue
		}
		ctx.SSHHostAlias = ""
		ctx.SSHSessionKey = ""
		ctx.SSHLauncherURL = ""
		ctx.SSHHomePath = ""
		ctx.LastSeenAt = time.Now()

		if clientID == defaultWebClientID {
			ws.sshHostAlias = ""
			ws.sshSessionKey = ""
			ws.sshLauncherURL = ""
			ws.sshHomePath = ""
		}
	}
}

// rememberClientWorkspacesLocked snapshots every client context's current
// workspace root into ws.clientWorkspaces. Called while holding ws.mutex —
// from setClientWorkspaceRoot (after a selection changes) and from the idle
// eviction worker (before it deletes contexts). Default-client entries are
// skipped: that client already follows ws.workspaceRoot.
//
// The map is bounded: it only ever holds one entry per client ID that has
// had a context in this process's lifetime. Stale entries for tabs that
// never return are replaced (same ID reuse) or pruned lazily when the
// remembered path no longer exists.
func (ws *ReactWebServer) rememberClientWorkspacesLocked() {
	if ws.clientWorkspaces == nil {
		ws.clientWorkspaces = make(map[string]string)
	}
	for clientID, ctx := range ws.clientContexts {
		if clientID == defaultWebClientID || ctx == nil {
			continue
		}
		if root := strings.TrimSpace(ctx.WorkspaceRoot); root != "" {
			ws.clientWorkspaces[clientID] = root
		}
	}
}

func (ws *ReactWebServer) setClientWorkspaceRoot(clientID, path string) (string, error) {
	workspaceRoot, err := filepathAbsEval(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	info, err := os.Stat(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root %q must be a directory", workspaceRoot)
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	// Resolve daemonRoot the same way to handle symlink differences
	// (macOS /var/folders has symlinks that can cause mismatches).
	resolvedDaemonRoot := ws.daemonRoot
	if evaled, err := filepath.EvalSymlinks(ws.daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}

	if !isWithinWorkspace(workspaceRoot, resolvedDaemonRoot) && workspaceRoot != resolvedDaemonRoot {
		return "", fmt.Errorf("workspace root must stay within daemon root %s", ws.daemonRoot)
	}

	// Defense-in-depth: reject the home directory as a workspace without
	// explicit consent. The API handler (handleAPIWorkspaceSet) is the
	// primary gate and surfaces a structured consent error to the frontend;
	// this prevents any other internal caller from silently setting home as
	// the workspace root. SP-130.
	if isHomeWorkspace(workspaceRoot) && !hasHomeWorkspaceConsent() {
		return "", fmt.Errorf("workspace root must not be the home directory without explicit consent")
	}

	if ws.clientContexts == nil {
		ws.clientContexts = make(map[string]*webClientContext)
	}
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ctx = newWebClientContext(ws.workspaceRoot, ws.sshHostAlias, ws.sshSessionKey, ws.sshLauncherURL, ws.sshHomePath)
		ws.clientContexts[clientID] = ctx
	}

	if ctx.Terminal != nil {
		if err := ctx.Terminal.CloseAllSessions(); err != nil {
			return "", fmt.Errorf("close terminal sessions: %w", err)
		}
	}
	if ctx.FileConsents != nil {
		ctx.FileConsents.clearAll()
	}

	ctx.WorkspaceRoot = workspaceRoot
	ctx.SSHHostAlias = ""
	ctx.SSHSessionKey = ""
	ctx.SSHLauncherURL = ""
	ctx.SSHHomePath = ""
	ctx.Terminal = NewTerminalManager(workspaceRoot)
	ws.startTerminalCleanupIfNeeded(ctx.Terminal)
	// Collect the outgoing agents before clearing the fields below. They are
	// bound to the OLD workspace root, and without an explicit Shutdown their
	// MCP servers and background watchers keep running for the rest of the
	// daemon's life. Released after ws.mutex is dropped.
	releasing := chatSessionAgents(ctx)
	ctx.Agent = nil
	ctx.AgentState = emptyAgentStateSnapshot()
	ctx.CurrentSessionID = ""
	ctx.ActiveQuery = false
	ctx.CurrentQuery = ""
	// Reset chat sessions on workspace change — keep only the default,
	// which starts fresh.
	ctx.ChatSessions = nil
	ctx.DefaultChatID = ""
	ctx.nextChatNumber = 0
	if ctx.FileConsents == nil {
		ctx.FileConsents = newFileConsentManager()
	}
	ctx.ensureDefaultChatSession()
	ctx.LastSeenAt = time.Now()

	if clientID == defaultWebClientID {
		ws.workspaceRoot = workspaceRoot
		ws.sshHostAlias = ""
		ws.sshSessionKey = ""
		ws.sshLauncherURL = ""
		ws.sshHomePath = ""
		ws.terminalManager = ctx.Terminal
		ws.fileConsents = ctx.FileConsents
	} else {
		// Remember non-default selections so an evicted context is recreated
		// in the workspace the user actually chose (see clientWorkspaces).
		if ws.clientWorkspaces == nil {
			ws.clientWorkspaces = make(map[string]string)
		}
		ws.clientWorkspaces[clientID] = workspaceRoot
	}

	// Non-blocking: hands each agent to its own goroutine, so this is safe
	// under the deferred ws.mutex unlock.
	ws.releaseAgents("workspace_switch", releasing...)

	return workspaceRoot, nil
}

func (ws *ReactWebServer) withAgentWorkspace(workspaceRoot string, fn func() error) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return fn()
	}

	ws.workspaceExecMu.Lock()
	defer ws.workspaceExecMu.Unlock()

	originalWD, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current working directory: %w", err)
	}
	if err := os.Chdir(workspaceRoot); err != nil {
		return fmt.Errorf("change working directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	return fn()
}
