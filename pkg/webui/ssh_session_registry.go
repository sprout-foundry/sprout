package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SSH session persistence
// ---------------------------------------------------------------------------

func sshSessionRegistryPath() string {
	return filepath.Join(getLeditConfigDir(), "ssh_sessions.json")
}

func readPersistedSSHSession(sessionKey string) (*persistedSSHWorkspaceSession, error) {
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return nil, fmt.Errorf("read session registry: %w", err)
	}
	session, ok := registry[sessionKey]
	if !ok {
		return nil, nil
	}
	copy := session
	return &copy, nil
}

func readPersistedSSHSessionRegistry() (map[string]persistedSSHWorkspaceSession, error) {
	data, err := os.ReadFile(sshSessionRegistryPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]persistedSSHWorkspaceSession{}, nil
		}
		return nil, fmt.Errorf("read SSH session registry file: %w", err)
	}
	if len(data) == 0 {
		return map[string]persistedSSHWorkspaceSession{}, nil
	}

	var registry map[string]persistedSSHWorkspaceSession
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parse SSH session registry: %w", err)
	}
	if registry == nil {
		registry = map[string]persistedSSHWorkspaceSession{}
	}
	return registry, nil
}

func writePersistedSSHSessionRegistry(registry map[string]persistedSSHWorkspaceSession) error {
	if err := os.MkdirAll(getLeditConfigDir(), 0755); err != nil {
		return fmt.Errorf("create session registry directory: %w", err)
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session registry: %w", err)
	}
	tmpPath := sshSessionRegistryPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write session registry temp file: %w", err)
	}
	return os.Rename(tmpPath, sshSessionRegistryPath())
}

func persistSSHSession(session *sshWorkspaceSession) error {
	if session == nil {
		return nil
	}
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return fmt.Errorf("read session registry for persist: %w", err)
	}
	registry[session.Key] = persistedSSHWorkspaceSession{
		Key:                 session.Key,
		HostAlias:           session.HostAlias,
		RemoteWorkspacePath: session.RemoteWorkspacePath,
		RemotePort:          session.RemotePort,
		RemotePID:           session.RemotePID,
		StartedAt:           session.StartedAt,
	}
	return writePersistedSSHSessionRegistry(registry)
}

func removePersistedSSHSession(sessionKey string) error {
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return fmt.Errorf("read session registry for removal: %w", err)
	}
	delete(registry, sessionKey)
	return writePersistedSSHSessionRegistry(registry)
}

func (ws *ReactWebServer) restorePersistedSSHSession(sessionKey string) (*sshLaunchResult, error) {
	persisted, err := readPersistedSSHSession(sessionKey)
	if err != nil || persisted == nil {
		return nil, fmt.Errorf("read persisted SSH session: %w", err)
	}

	if err := ensureSSHProgramsAvailable(); err != nil {
		return nil, fmt.Errorf("ensure SSH programs available: %w", err)
	}

	localPort, err := findFreeLocalPort()
	if err != nil {
		return nil, fmt.Errorf("allocate local port: %w", err)
	}

	// SSH remote daemons always listen on the fixed daemon port.
	// Ignore the persisted remote port — it may be stale from a pre-fix
	// session that used a random port.
	remotePort := DaemonPort
	if persisted.RemotePort != DaemonPort {
		_ = removePersistedSSHSession(sessionKey)
	}

	tunnelCmd, err := startSSHTunnel(persisted.HostAlias, localPort, remotePort, nil)
	if err != nil {
		_ = removePersistedSSHSession(sessionKey)
		return nil, fmt.Errorf("start SSH tunnel for restored session: %w", err)
	}

	if err := waitForWebHealth(localPort, sshRestoreHealthTimeout); err != nil {
		_ = killProcess(tunnelCmd)
		_ = removePersistedSSHSession(sessionKey)
		return nil, fmt.Errorf("health check for restored session: %w", err)
	}

	session := &sshWorkspaceSession{
		Key:                 persisted.Key,
		HostAlias:           persisted.HostAlias,
		RemoteWorkspacePath: persisted.RemoteWorkspacePath,
		LocalPort:           localPort,
		RemotePort:          DaemonPort,
		RemotePID:           persisted.RemotePID,
		URL:                 fmt.Sprintf("http://127.0.0.1:%d", localPort),
		TunnelCmd:           tunnelCmd,
		StartedAt:           persisted.StartedAt,
		ReusedDaemon:        true,
	}

	ws.sshSessionsMu.Lock()
	ws.sshSessions[sessionKey] = session
	// Re-persist with the corrected remote port so future restores work cleanly.
	_ = persistSSHSession(session)
	ws.sshSessionsMu.Unlock()
	go ws.watchSSHSession(sessionKey, session, tunnelCmd)

	return &sshLaunchResult{
		URL:       session.URL,
		LocalPort: session.LocalPort,
		ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
	}, nil
}

func (ws *ReactWebServer) listSSHSessions() ([]sshSessionEntryDTO, error) {
	registry, err := readPersistedSSHSessionRegistry()
	if err != nil {
		return nil, fmt.Errorf("list SSH sessions: %w", err)
	}

	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()

	sessions := make([]sshSessionEntryDTO, 0, len(registry))
	for key, persisted := range registry {
		entry := sshSessionEntryDTO{
			Key:                 key,
			HostAlias:           persisted.HostAlias,
			RemoteWorkspacePath: persisted.RemoteWorkspacePath,
			RemotePort:          persisted.RemotePort,
			RemotePID:           persisted.RemotePID,
			StartedAt:           persisted.StartedAt,
			Active:              false,
		}
		if active := ws.sshSessions[key]; active != nil {
			entry.Active = true
			entry.LocalPort = active.LocalPort
			entry.URL = active.URL
		}
		sessions = append(sessions, entry)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Active != sessions[j].Active {
			return sessions[i].Active && !sessions[j].Active
		}
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (ws *ReactWebServer) closeSSHSession(sessionKey string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return fmt.Errorf("ssh session key is required")
	}

	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	ws.stopSSHSessionLocked(sessionKey)
	return nil
}

func (ws *ReactWebServer) stopSSHSessionLocked(key string) {
	session := ws.sshSessions[key]
	if session == nil {
		_ = removePersistedSSHSession(key)
		ws.clearClientSSHContextForSessionKey(key)
		return
	}
	_ = killProcess(session.TunnelCmd)
	// Never kill a remote daemon that was running before we connected —
	// other SSH sessions or the user may still depend on it.
	if !session.ReusedDaemon {
		_ = stopRemoteSSHBackend(session.HostAlias, session.RemotePID)
	}
	_ = removePersistedSSHSession(key)
	delete(ws.sshSessions, key)
	ws.clearClientSSHContextForSessionKey(key)
}

func (ws *ReactWebServer) watchSSHSession(key string, session *sshWorkspaceSession, cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	_ = cmd.Wait()

	// Attempt to reconnect the tunnel before giving up.
	const maxRetries = 3
	backoff := 2 * time.Second
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Check whether the session has been replaced or explicitly closed.
		ws.sshSessionsMu.Lock()
		current := ws.sshSessions[key]
		ws.sshSessionsMu.Unlock()
		if current == nil || current != session {
			return
		}

		time.Sleep(backoff)
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		newLocalPort, portErr := findFreeLocalPort()
		if portErr != nil {
			continue
		}
		newTunnel, tunnelErr := startSSHTunnel(session.HostAlias, newLocalPort, session.RemotePort, nil)
		if tunnelErr != nil {
			continue
		}
		if healthErr := waitForWebHealth(newLocalPort, sshRestoreHealthTimeout); healthErr != nil {
			_ = killProcess(newTunnel)
			continue
		}

		// Reconnect succeeded — swap in the new tunnel under the lock so the
		// proxy always reads a consistent LocalPort value.
		ws.sshSessionsMu.Lock()
		current = ws.sshSessions[key]
		if current != nil && current == session {
			oldTunnel := session.TunnelCmd
			session.LocalPort = newLocalPort
			session.TunnelCmd = newTunnel
			session.URL = fmt.Sprintf("http://127.0.0.1:%d", newLocalPort)
			ws.sshSessionsMu.Unlock()
			_ = killProcess(oldTunnel)
			go ws.watchSSHSession(key, session, newTunnel)
			return
		}
		ws.sshSessionsMu.Unlock()
		_ = killProcess(newTunnel)
		return
	}

	// All reconnection attempts failed; clean up.
	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	current := ws.sshSessions[key]
	if current != nil && current == session {
		// Never kill a remote daemon that was running before we connected.
		if !session.ReusedDaemon {
			_ = stopRemoteSSHBackend(session.HostAlias, session.RemotePID)
		}
		_ = removePersistedSSHSession(key)
		delete(ws.sshSessions, key)
	}
}

func (ws *ReactWebServer) shutdownSSHSessions() {
	ws.sshSessionsMu.Lock()
	defer ws.sshSessionsMu.Unlock()
	for key := range ws.sshSessions {
		ws.stopSSHSessionLocked(key)
	}
}
