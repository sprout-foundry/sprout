//go:build !js

package webui

// client_workspaces_store.go — the on-disk copy of ws.clientProjectFolders, so a
// tab's project folder selection also survives a daemon restart (an auto-started
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
	clientProjectFoldersFileName = "client_workspaces.json"
	// maxPersistedClientProjectFolders bounds the file: one entry per browser
	// tab identity, and tab identities churn.
	maxPersistedClientProjectFolders = 64
	// clientProjectFolderMaxAge drops selections from tabs that have not
	// returned in a long time; the client cookie itself lives 30 days.
	clientProjectFolderMaxAge = clientIDCookieMaxAge
)

// clientProjectFolderStoreDisabled keeps servers built by NewReactWebServer from
// sharing one on-disk selection file; the package tests set it so a test's
// client IDs cannot leak a remembered project folder into another test's server.
var clientProjectFolderStoreDisabled bool

type persistedClientProjectFolder struct {
	Path      string    `json:"path"`
	UpdatedAt time.Time `json:"updated_at"`
}

type persistedClientProjectFolders struct {
	Version int                                     `json:"version"`
	Clients map[string]persistedClientProjectFolder `json:"clients"`
}

func (ws *ReactWebServer) touchClientProjectFolderLocked(clientID string) {
	if ws.clientProjectFoldersUpdatedAt == nil {
		ws.clientProjectFoldersUpdatedAt = make(map[string]time.Time)
	}
	ws.clientProjectFoldersUpdatedAt[clientID] = time.Now()
}

// initClientProjectFolderStore points the store at the state dir and loads the
// selections a previous daemon process persisted.
func (ws *ReactWebServer) initClientProjectFolderStore() {
	if clientProjectFolderStoreDisabled {
		return
	}
	stateDir, err := envutil.StateDir()
	if err != nil {
		return
	}
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	ws.clientProjectFoldersPath = filepath.Join(stateDir, clientProjectFoldersFileName)
	ws.loadClientProjectFoldersLocked()
}

// loadClientProjectFoldersLocked restores persisted selections, dropping entries
// that are too old, no longer directories, or outside the daemon root (a
// selection could never have been made there through setClientProjectFolder).
func (ws *ReactWebServer) loadClientProjectFoldersLocked() {
	if ws.clientProjectFoldersPath == "" {
		return
	}
	data, err := os.ReadFile(ws.clientProjectFoldersPath)
	if err != nil {
		return
	}
	var stored persistedClientProjectFolders
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}

	resolvedDaemonRoot := ws.daemonRoot
	if evaled, err := filepath.EvalSymlinks(ws.daemonRoot); err == nil {
		resolvedDaemonRoot = evaled
	}
	cutoff := time.Now().Add(-clientProjectFolderMaxAge)
	if ws.clientProjectFolders == nil {
		ws.clientProjectFolders = make(map[string]string)
	}
	if ws.clientProjectFoldersUpdatedAt == nil {
		ws.clientProjectFoldersUpdatedAt = make(map[string]time.Time)
	}
	for clientID, entry := range stored.Clients {
		clientID = sanitizeClientID(strings.TrimSpace(clientID))
		path := strings.TrimSpace(entry.Path)
		if clientID == defaultWebClientID || path == "" || entry.UpdatedAt.Before(cutoff) {
			continue
		}
		if _, exists := ws.clientProjectFolders[clientID]; exists {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		if path != resolvedDaemonRoot && !isWithinProjectFolder(path, resolvedDaemonRoot) {
			continue
		}
		ws.clientProjectFolders[clientID] = path
		ws.clientProjectFoldersUpdatedAt[clientID] = entry.UpdatedAt
	}
	ws.pruneClientProjectFoldersLocked()
}

// pruneClientProjectFoldersLocked enforces the size bound, keeping the most
// recently updated selections.
func (ws *ReactWebServer) pruneClientProjectFoldersLocked() {
	if len(ws.clientProjectFolders) <= maxPersistedClientProjectFolders {
		return
	}
	ids := make([]string, 0, len(ws.clientProjectFolders))
	for id := range ws.clientProjectFolders {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ws.clientProjectFoldersUpdatedAt[ids[i]].After(ws.clientProjectFoldersUpdatedAt[ids[j]])
	})
	for _, id := range ids[maxPersistedClientProjectFolders:] {
		delete(ws.clientProjectFolders, id)
		delete(ws.clientProjectFoldersUpdatedAt, id)
	}
}

// saveClientProjectFoldersLocked writes the selections atomically. Best-effort:
// a failed write only costs the selection surviving a restart.
func (ws *ReactWebServer) saveClientProjectFoldersLocked() {
	if ws.clientProjectFoldersPath == "" {
		return
	}
	ws.pruneClientProjectFoldersLocked()
	stored := persistedClientProjectFolders{Version: 1, Clients: make(map[string]persistedClientProjectFolder, len(ws.clientProjectFolders))}
	for clientID, path := range ws.clientProjectFolders {
		updatedAt := ws.clientProjectFoldersUpdatedAt[clientID]
		if updatedAt.IsZero() {
			updatedAt = time.Now()
		}
		stored.Clients[clientID] = persistedClientProjectFolder{Path: path, UpdatedAt: updatedAt}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(ws.clientProjectFoldersPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		ws.log().Debug("create client workspaces dir failed", slog.Any("err", err))
		return
	}
	tmp, err := os.CreateTemp(dir, clientProjectFoldersFileName+".*.tmp")
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
	if err := os.Rename(tmpName, ws.clientProjectFoldersPath); err != nil {
		_ = os.Remove(tmpName)
	}
}
