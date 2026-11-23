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

func (ws *ReactWebServer) getProjectFolderForRequest(r *http.Request) string {
	root := ws.getClientContextForRequest(r).ProjectFolder
	// Resolve symlinks so that canonicalizePath comparisons are consistent
	// (macOS /var → /private/var). The daemonRoot/workspaceRoot are resolved
	// at server construction, but per-client context roots may not be.
	if evaled, err := filepath.EvalSymlinks(root); err == nil {
		return evaled
	}
	return root
}

// getLayeredConfigManager creates a config manager using the layered approach
// (global → project folder → session) for the given client ID.
// This is used as a fallback when no live agent's config manager is available.
func (ws *ReactWebServer) getLayeredConfigManager(clientID string) (*configuration.Manager, error) {
	configBase, err := configuration.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config directory: %w", err)
	}

	// Resolve project folder for this client
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	ws.mutex.RUnlock()
	var projectFolder string
	if ctx != nil {
		projectFolder = ctx.ProjectFolder
	}

	var projectFolderDir string
	if projectFolder != "" {
		projectFolderDir = configuration.ProjectFolderConfigDir(projectFolder)
	}

	return configuration.NewManagerWithLayers(configBase, projectFolderDir)
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

// getProjectFolderForClient returns the per-client project folder for the given client ID.
// Falls back to the server-level project folder if the client context doesn't exist.
func (ws *ReactWebServer) getProjectFolderForClient(clientID string) string {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		root := ctx.ProjectFolder
		if evaled, err := filepath.EvalSymlinks(root); err == nil {
			return evaled
		}
		return root
	}
	return ws.projectFolder
}

// getAutomateDir returns the project-folder-local automate/ directory for the
// current client's project folder. Falls back to the CWD when no project folder
// is set (e.g., standalone daemon or pre-chat initialization).
func (ws *ReactWebServer) getAutomateDir(r *http.Request) string {
	projectFolder := ws.getProjectFolderForRequest(r)
	if projectFolder == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		projectFolder = wd
	}
	return filepath.Join(projectFolder, "automate")
}
