package mcp

import (
	"log"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// Workspace-scoped credential fallback.
//
// Credential resolution for MCP servers goes through credentials.GetFromActiveBackend,
// which resolves the store from the process environment (SPROUT_CONFIG or the
// global config dir). A workspace-scoped `sprout login` stores the platform
// token in the workspace's own store, and a daemon that did not start inside
// that scope cannot see it — so the Authorization header built from a
// {{credential:...}} placeholder would silently resolve to nothing and every
// request would go out unauthenticated. The agent registers the workspace it
// serves here; resolution then falls back to that scope when the process's
// own store has no value for the key.
//
// Process-scoped, like the active backend itself: when one process serves
// several workspaces the most recently registered one wins, and the fallback
// is only consulted for keys the process's own store does not hold.
var (
	workspaceCredDirMu sync.Mutex
	workspaceCredDir   string
)

// SetWorkspaceCredentialDir registers the config directory (the workspace's
// .sprout) whose credential store is consulted as a fallback when the
// process's active backend holds no value for a referenced key. An empty
// argument clears the fallback.
func SetWorkspaceCredentialDir(dir string) {
	workspaceCredDirMu.Lock()
	defer workspaceCredDirMu.Unlock()
	workspaceCredDir = dir
}

// GetWorkspaceCredentialDir returns the currently registered fallback
// directory, or "" when none was set.
func GetWorkspaceCredentialDir() string {
	workspaceCredDirMu.Lock()
	defer workspaceCredDirMu.Unlock()
	return workspaceCredDir
}

// getMCPHeaderCredential looks up a credential key, first in the process's
// active backend and then in the registered workspace store. It returns the
// value and where it came from ("stored" or "workspace"); both are empty
// when neither scope holds the key.
func getMCPHeaderCredential(key string) (string, string) {
	value, source, err := credentials.GetFromActiveBackend(key)
	if err == nil && value != "" {
		return value, source
	}
	dir := GetWorkspaceCredentialDir()
	if dir == "" {
		return "", ""
	}
	store, loadErr := credentials.LoadStoreFromDir(dir)
	if loadErr != nil {
		log.Printf("[mcp] workspace credential store %s unreadable: %v", dir, loadErr)
		return "", ""
	}
	if v, ok := store[key]; ok && v != "" {
		return v, "workspace"
	}
	return "", ""
}
