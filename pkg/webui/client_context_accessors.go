//go:build !js

package webui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func (ws *ReactWebServer) getWorkspaceRootForRequest(r *http.Request) string {
	root := ws.getClientContextForRequest(r).WorkspaceRoot
	// Resolve symlinks so that canonicalizePath comparisons are consistent
	// (macOS /var → /private/var). The daemonRoot/workspaceRoot are resolved
	// at server construction, but per-client context roots may not be.
	if evaled, err := filepath.EvalSymlinks(root); err == nil {
		return evaled
	}
	return root
}

// getLayeredConfigManager creates a config manager using the layered approach
// (global → workspace → session) for the given client ID.
// This is used as a fallback when no live agent's config manager is available.
func (ws *ReactWebServer) getLayeredConfigManager(clientID string) (*configuration.Manager, error) {
	configBase, err := configuration.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config directory: %w", err)
	}

	// Resolve workspace root for this client
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	var workspaceRoot string
	if ctx != nil {
		workspaceRoot = ctx.WorkspaceRoot
	}

	var workspaceDir string
	if workspaceRoot != "" {
		workspaceDir = filepath.Join(workspaceRoot, configuration.ConfigDirName)
	}

	return configuration.NewManagerWithLayers(configBase, workspaceDir)
}

func (ws *ReactWebServer) getTerminalManagerForRequest(r *http.Request) *TerminalManager {
	return ws.getClientContextForRequest(r).Terminal
}

func (ws *ReactWebServer) getFileConsentManagerForRequest(r *http.Request) *fileConsentManager {
	return ws.getClientContextForRequest(r).FileConsents
}

// getActiveAgentForRequest resolves the agent backing the request's
// active chat session. Returns nil when there's no live agent (e.g.,
// the browser is making a file-API call before any chat session has
// been initialized).
//
// The file-API handlers use this to consult the agent's session
// folder allowlist — paths the user previously approved via the
// approval dialog auto-pass without needing the 2-minute token flow.
func (ws *ReactWebServer) getActiveAgentForRequest(r *http.Request) *agent.Agent {
	clientID := ws.resolveClientID(r)
	_, chatID := ws.getActiveChatContext(clientID)
	if chatID == "" {
		return nil
	}
	a, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		return nil
	}
	return a
}

func (ws *ReactWebServer) getCurrentSessionIDForRequest(r *http.Request) string {
	return ws.getClientContextForRequest(r).CurrentSessionID
}

// getWorkspaceRootForClient returns the per-client workspace root for the given client ID.
// Falls back to the server-level workspace root if the client context doesn't exist.
func (ws *ReactWebServer) getWorkspaceRootForClient(clientID string) string {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		root := ctx.WorkspaceRoot
		if evaled, err := filepath.EvalSymlinks(root); err == nil {
			return evaled
		}
		return root
	}
	return ws.workspaceRoot
}

// getAutomateDir returns the workspace-local automate/ directory for the
// current client's workspace. Falls back to the CWD when no workspace root
// is set (e.g., standalone daemon or pre-chat initialization).
func (ws *ReactWebServer) getAutomateDir(r *http.Request) string {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	if workspaceRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		workspaceRoot = wd
	}
	return filepath.Join(workspaceRoot, "automate")
}
