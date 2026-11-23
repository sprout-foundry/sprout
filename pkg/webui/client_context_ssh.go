//go:build !js

package webui

// client_context_ssh.go — the SSH + project folder wiring for client contexts
// (clearing a session key, remembering project folders, setting the project folder,
// the withAgentProjectFolder chdir helper), split out of client_context.go.

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

func (ws *ReactWebServer) setClientProjectFolder(clientID, path string) (string, error) {
	projectFolder, err := filepathAbsEval(path)
	if err != nil {
		return "", fmt.Errorf("resolve project folder: %w", err)
	}

	info, err := os.Stat(projectFolder)
	if err != nil {
		return "", fmt.Errorf("stat project folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project folder %q must be a directory", projectFolder)
	}

	ws.mutex.Lock()
	defer ws.mutex.Unlock()

	// Resolve daemonRoot the same way to handle symlink differences
	// (macOS /var/folders has symlinks that can cause mismatches).
	resolvedDaemonRoot := ws.daemonRoot
	if evaled, err := filepath.EvalSymlinks(ws.daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}

	if !isWithinProjectFolder(projectFolder, resolvedDaemonRoot) && projectFolder != resolvedDaemonRoot {
		return "", fmt.Errorf("project folder must stay within daemon root %s", ws.daemonRoot)
	}

	// Defense-in-depth: reject the home directory as a project folder without
	// explicit consent. The API handler (handleAPIProjectFolderSet) is the
	// primary gate and surfaces a structured consent error to the frontend;
	// this prevents any other internal caller from silently setting home as
	// the project folder. SP-130.
	if isHomeProjectFolder(projectFolder) && !hasHomeProjectFolderConsent() {
		return "", fmt.Errorf("project folder must not be the home directory without explicit consent")
	}

	if ws.clientContexts == nil {
		ws.clientContexts = make(map[string]*webClientContext)
	}
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ctx = newWebClientContext(ws.projectFolder, ws.sshHostAlias, ws.sshSessionKey, ws.sshLauncherURL, ws.sshHomePath)
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

	ctx.ProjectFolder = projectFolder
	ctx.ProjectRoot = projectFolder
	ctx.SSHHostAlias = ""
	ctx.SSHSessionKey = ""
	ctx.SSHLauncherURL = ""
	ctx.SSHHomePath = ""
	ctx.Terminal = NewTerminalManager(projectFolder)
	ws.startTerminalCleanupIfNeeded(ctx.Terminal)
	// Collect the outgoing agents before clearing the fields below. They are
	// bound to the OLD project folder, and without an explicit Shutdown their
	// MCP servers and background watchers keep running for the rest of the
	// daemon's life. Released after ws.mutex is dropped.
	releasing := chatSessionAgents(ctx)
	ctx.Agent = nil
	ctx.AgentState = emptyAgentStateSnapshot()
	ctx.CurrentSessionID = ""
	ctx.ActiveQuery = false
	ctx.CurrentQuery = ""
	// Reset chat sessions on project folder change — keep only the default,
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
		ws.projectFolder = projectFolder
		ws.sshHostAlias = ""
		ws.sshSessionKey = ""
		ws.sshLauncherURL = ""
		ws.sshHomePath = ""
		ws.terminalManager = ctx.Terminal
		ws.fileConsents = ctx.FileConsents
	} else {
		// Remember non-default selections so an evicted context is recreated
		// in the project folder the user actually chose (see clientProjectFolders).
		ws.rememberClientProjectFolderLocked(clientID, ctx)
	}

	// Non-blocking: hands each agent to its own goroutine, so this is safe
	// under the deferred ws.mutex unlock.
	ws.releaseAgents("project_folder_switch", releasing...)

	return projectFolder, nil
}

func (ws *ReactWebServer) withAgentProjectFolder(projectFolder string, fn func() error) error {
	projectFolder = strings.TrimSpace(projectFolder)
	if projectFolder == "" {
		return fn()
	}

	ws.projectFolderExecMu.Lock()
	defer ws.projectFolderExecMu.Unlock()

	originalWD, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current working directory: %w", err)
	}
	if err := os.Chdir(projectFolder); err != nil {
		return fmt.Errorf("change working directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	return fn()
}
