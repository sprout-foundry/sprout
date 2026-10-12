//go:build !js

package webui

// client_workspaces_store.go — the on-disk copy of ws.clientWorkspaces, so a
// tab's workspace selection also survives a daemon restart (an auto-started
// daemon reaps itself when idle and is respawned on demand).

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

const (
	clientWorkspacesFileName = "client_workspaces.json"
	// maxPersistedClientWorkspaces bounds the file: one entry per browser
	// tab identity, and tab identities churn.
	maxPersistedClientWorkspaces = 64
	// clientWorkspaceMaxAge drops selections from tabs that have not
	// returned in a long time; the client cookie itself lives 30 days.
	clientWorkspaceMaxAge = clientIDCookieMaxAge
)

// clientWorkspaceStoreDisabled keeps servers built by NewReactWebServer from
// sharing one on-disk selection file; the package tests set it so a test's
// client IDs cannot leak a remembered workspace into another test's server.
var clientWorkspaceStoreDisabled bool

type persistedClientWorkspace struct {
	Path      string    `json:"path"`
	UpdatedAt time.Time `json:"updated_at"`
}

type persistedClientWorkspaces struct {
	Version int                                 `json:"version"`
	Clients map[string]persistedClientWorkspace `json:"clients"`
}

func (ws *ReactWebServer) touchClientWorkspaceLocked(clientID string) {
	if ws.clientWorkspacesUpdatedAt == nil {
		ws.clientWorkspacesUpdatedAt = make(map[string]time.Time)
	}
	ws.clientWorkspacesUpdatedAt[clientID] = time.Now()
}

// initClientWorkspaceStore points the store at the state dir and loads the
// selections a previous daemon process persisted.
func (ws *ReactWebServer) initClientWorkspaceStore() {
	if clientWorkspaceStoreDisabled {
		return
	}
	stateDir, err := envutil.StateDir()
	if err != nil {
		return
	}
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ws.clientWorkspacesPath = filepath.Join(stateDir, clientWorkspacesFileName)
	ws.loadClientWorkspacesLocked()
}

// loadClientWorkspacesLocked restores persisted selections, dropping entries
// that are too old, no longer directories, or outside the daemon root (a
// selection could never have been made there through setClientWorkspaceRoot).
func (ws *ReactWebServer) loadClientWorkspacesLocked() {
	if ws.clientWorkspacesPath == "" {
		return
	}
	data, err := os.ReadFile(ws.clientWorkspacesPath)
	if err != nil {
		return
	}
	var stored persistedClientWorkspaces
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}

	resolvedDaemonRoot := ws.daemonRoot
	if evaled, err := filepath.EvalSymlinks(ws.daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}
	cutoff := time.Now().Add(-clientWorkspaceMaxAge)
	if ws.clientWorkspaces == nil {
		ws.clientWorkspaces = make(map[string]string)
	}
	if ws.clientWorkspacesUpdatedAt == nil {
		ws.clientWorkspacesUpdatedAt = make(map[string]time.Time)
	}
	for clientID, entry := range stored.Clients {
		clientID = sanitizeClientID(strings.TrimSpace(clientID))
		path := strings.TrimSpace(entry.Path)
		if clientID == defaultWebClientID || path == "" || entry.UpdatedAt.Before(cutoff) {
			continue
		}
		if _, exists := ws.clientWorkspaces[clientID]; exists {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		if path != resolvedDaemonRoot && !isWithinWorkspace(path, resolvedDaemonRoot) {
			continue
		}
		ws.clientWorkspaces[clientID] = path
		ws.clientWorkspacesUpdatedAt[clientID] = entry.UpdatedAt
	}
	ws.pruneClientWorkspacesLocked()
}

// pruneClientWorkspacesLocked enforces the size bound, keeping the most
// recently updated selections.
func (ws *ReactWebServer) pruneClientWorkspacesLocked() {
	if len(ws.clientWorkspaces) <= maxPersistedClientWorkspaces {
		return
	}
	ids := make([]string, 0, len(ws.clientWorkspaces))
	for id := range ws.clientWorkspaces {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ws.clientWorkspacesUpdatedAt[ids[i]].After(ws.clientWorkspacesUpdatedAt[ids[j]])
	})
	for _, id := range ids[maxPersistedClientWorkspaces:] {
		delete(ws.clientWorkspaces, id)
		delete(ws.clientWorkspacesUpdatedAt, id)
	}
}

// saveClientWorkspacesLocked writes the selections atomically. Best-effort:
// a failed write only costs the selection surviving a restart.
func (ws *ReactWebServer) saveClientWorkspacesLocked() {
	if ws.clientWorkspacesPath == "" {
		return
	}
	ws.pruneClientWorkspacesLocked()
	stored := persistedClientWorkspaces{Version: 1, Clients: make(map[string]persistedClientWorkspace, len(ws.clientWorkspaces))}
	for clientID, path := range ws.clientWorkspaces {
		updatedAt := ws.clientWorkspacesUpdatedAt[clientID]
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}
		stored.Clients[clientID] = persistedClientWorkspace{Path: path, UpdatedAt: updatedAt}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(ws.clientWorkspacesPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		ws.log().Debug("create client workspaces dir failed", slog.Any("err", err))
		return
	}
	tmp, err := os.CreateTemp(dir, clientWorkspacesFileName+".*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, ws.clientWorkspacesPath); err != nil {
		_ = os.Remove(tmpName)
	}
}
