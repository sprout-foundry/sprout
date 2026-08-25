//go:build !js

// Package webui: SSH workspace launch orchestration (split from ssh_launch.go).

package webui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (ws *ReactWebServer) launchSSHWorkspace(req sshLaunchRequestDTO) (result *sshLaunchResult, err error) {
	hostAlias := strings.TrimSpace(req.HostAlias)
	if hostAlias == "" {
		return nil, fmt.Errorf("SSH host alias is required")
	}

	remoteWorkspacePath := strings.TrimSpace(req.RemoteWorkspacePath)
	if remoteWorkspacePath == "" {
		remoteWorkspacePath = "$HOME"
	}
	// Normalize tilde shortcuts so ~/project and $HOME/project resolve to the
	// same session key and don't fork separate in-flight state.
	remoteWorkspacePath = normalizeRemoteWorkspacePath(remoteWorkspacePath)

	logger, loggerErr := newSSHLaunchLogger(hostAlias, remoteWorkspacePath)
	if loggerErr != nil {
		return nil, fmt.Errorf("failed to create SSH launch logger for %s: %w", hostAlias, loggerErr)
	}
	defer logger.Close()

	sessionKey := hostAlias + "::" + remoteWorkspacePath
	ws.setSSHLaunchStatus(sessionKey, "connecting", fmt.Sprintf("Connecting to %s...", hostAlias), true, "")
	defer func() {
		if err != nil {
			details := ""
			logPath := ""
			step := "failed"
			var launchErr *sshLaunchError
			if errors.As(err, &launchErr) {
				details = launchErr.Details
				logPath = launchErr.LogPath
				step = launchErr.Step
			}
			ws.sshLaunchStatusMu.Lock()
			ws.sshLaunchStatuses[sessionKey] = &sshLaunchStatus{
				Key:        sessionKey,
				Step:       step,
				Status:     "SSH workspace launch failed",
				InProgress: false,
				LastError:  err.Error(),
				Details:    details,
				LogPath:    logPath,
				UpdatedAt:  time.Now(),
			}
			ws.sshLaunchStatusMu.Unlock()
			return
		}
		proxyBase := ""
		proxyURL := ""
		localPort := 0
		if result != nil {
			proxyBase = result.ProxyBase
			proxyURL = result.ProxyBase + "/"
			localPort = result.LocalPort
		}
		ws.sshLaunchStatusMu.Lock()
		ws.sshLaunchStatuses[sessionKey] = &sshLaunchStatus{
			Key:        sessionKey,
			Step:       "ready",
			Status:     fmt.Sprintf("SSH workspace ready: %s", hostAlias),
			InProgress: false,
			UpdatedAt:  time.Now(),
			ProxyBase:  proxyBase,
			ProxyURL:   proxyURL,
			LocalPort:  localPort,
		}
		ws.sshLaunchStatusMu.Unlock()
	}()

	// Deduplicate: if a launch for this key is already in progress, wait for it.
	ws.sshInFlightMu.Lock()
	if waitCh, exists := ws.sshInFlight[sessionKey]; exists {
		ws.sshInFlightMu.Unlock()
		<-waitCh
		ws.sshSessionsMu.Lock()
		existing := ws.sshSessions[sessionKey]
		ws.sshSessionsMu.Unlock()
		if existing != nil {
			return &sshLaunchResult{
				URL:       existing.URL,
				LocalPort: existing.LocalPort,
				ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
			}, nil
		}
		return nil, fmt.Errorf("concurrent SSH launch for %s did not succeed", sessionKey)
	}
	doneCh := make(chan struct{})
	ws.sshInFlight[sessionKey] = doneCh
	ws.sshInFlightMu.Unlock()
	defer func() {
		ws.sshInFlightMu.Lock()
		delete(ws.sshInFlight, sessionKey)
		ws.sshInFlightMu.Unlock()
		close(doneCh)
	}()

	// Bound the entire launch sequence so a stalled SSH connection or slow
	// profile script on the remote host cannot block indefinitely.
	launchCtx, launchCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer launchCancel()

	ws.sshSessionsMu.Lock()
	if existing := ws.sshSessions[sessionKey]; existing != nil {
		ws.setSSHLaunchStatus(sessionKey, "reusing-session", fmt.Sprintf("Reusing existing session for %s...", hostAlias), true, "")
		if err := waitForWebHealth(existing.LocalPort, 2*time.Second); err == nil {
			result = &sshLaunchResult{
				URL:       existing.URL,
				LocalPort: existing.LocalPort,
				ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
			}
			ws.sshSessionsMu.Unlock()
			return result, nil
		}
		ws.setSSHLaunchStatus(sessionKey, "discarding-stale-session", fmt.Sprintf("Discarding stale session for %s...", hostAlias), true, "")
		ws.stopSSHSessionLocked(sessionKey)
	}
	ws.sshSessionsMu.Unlock()

	if restored, err := ws.restorePersistedSSHSession(sessionKey); err == nil && restored != nil {
		ws.setSSHLaunchStatus(sessionKey, "restored-session", fmt.Sprintf("Restored existing session for %s", hostAlias), false, "")
		logger.Logf("restored existing SSH session %q", sessionKey)
		return restored, nil
	}

	ws.setSSHLaunchStatus(sessionKey, "checking-local-tools", "Checking local SSH tools...", true, "")
	if err := ensureSSHProgramsAvailable(); err != nil {
		logger.Logf("ssh program availability check failed: %v", err)
		return nil, fmt.Errorf("check SSH program availability: %w", err)
	}
	logger.Logf("ssh and scp detected locally")

	ws.setSSHLaunchStatus(sessionKey, "inspecting-remote", fmt.Sprintf("Inspecting remote host %s...", hostAlias), true, "")
	remoteInfo, err := inspectRemoteSSHHost(launchCtx, hostAlias, logger)
	if err != nil {
		return nil, fmt.Errorf("inspect remote SSH host: %w", err)
	}
	logger.Logf("remote host detected platform=%s arch=%s", remoteInfo.Platform, remoteInfo.Arch)

	ws.setSSHLaunchStatus(sessionKey, "preparing-local-backend", "Preparing local backend binary...", true, "")
	localBinary, err := prepareLocalSSHBinary(remoteInfo.Platform, remoteInfo.Arch, logger)
	if err != nil {
		return nil, fmt.Errorf("prepare local SSH binary: %w", err)
	}
	logger.Logf("local SSH backend binary ready: %s", localBinary)

	ws.setSSHLaunchStatus(sessionKey, "installing-remote-backend", fmt.Sprintf("Installing backend on %s...", hostAlias), true, "")
	remoteBinary, binaryWasUploaded, err := ensureRemoteSSHBinary(launchCtx, hostAlias, localBinary, remoteInfo, logger)
	if err != nil {
		return nil, fmt.Errorf("install remote SSH binary: %w", err)
	}
	logger.Logf("remote SSH backend installed at %s (uploaded=%v)", remoteBinary, binaryWasUploaded)

	// When a new binary was freshly uploaded (different fingerprint from any
	// previously-cached remote binary), force-restart the daemon so it picks
	// up the new binary. This happens inside startRemoteSSHBackend under a
	// cross-session lock to avoid races when concurrent SSH launches target
	// the same host.
	if binaryWasUploaded {
		logger.Logf("new backend binary uploaded; will force-restart existing daemon during backend start")
	}

	ws.setSSHLaunchStatus(sessionKey, "allocating-local-port", "Allocating local tunnel port...", true, "")
	localPort, err := findFreeLocalPort()
	if err != nil {
		logger.Logf("failed to allocate local tunnel port: %v", err)
		return nil, fmt.Errorf("allocate local tunnel port: %w", err)
	}
	logger.Logf("allocated local tunnel port %d", localPort)

	launcherURL := fmt.Sprintf("http://127.0.0.1:%d", ws.port)
	ws.setSSHLaunchStatus(sessionKey, "starting-remote-backend", fmt.Sprintf("Starting remote backend on %s...", hostAlias), true, "")
	remotePort, remotePID, reusedDaemon, err := startRemoteSSHBackend(launchCtx, hostAlias, sessionKey, launcherURL, remoteWorkspacePath, remoteBinary, binaryWasUploaded, logger)
	if err != nil {
		return nil, fmt.Errorf("start remote SSH backend: %w", err)
	}
	if reusedDaemon {
		logger.Logf("reusing existing remote daemon port=%d pid=%d", remotePort, remotePID)
	} else {
		logger.Logf("remote SSH backend started port=%d pid=%d", remotePort, remotePID)
	}

	ws.setSSHLaunchStatus(sessionKey, "starting-tunnel", fmt.Sprintf("Starting tunnel for %s...", hostAlias), true, "")
	tunnelCmd, err := startSSHTunnel(hostAlias, localPort, remotePort, logger)
	if err != nil {
		return nil, fmt.Errorf("start SSH tunnel: %w", err)
	}
	logger.Logf("ssh tunnel started local_port=%d remote_port=%d", localPort, remotePort)

	ws.setSSHLaunchStatus(sessionKey, "health-check", "Waiting for SSH workspace health check...", true, "")
	if err := waitForWebHealth(localPort, sshLaunchHealthTimeout); err != nil {
		logger.Logf("health check failed: %v", err)
		if isRetryableSSHHealthError(err) {
			logger.Logf("health check failed with retryable error; restarting tunnel once")
			_ = killProcess(tunnelCmd)

			retryLocalPort, portErr := findFreeLocalPort()
			if portErr != nil {
				logger.Logf("retry health: failed to allocate retry local port: %v", portErr)
			} else {
				logger.Logf("retry health: allocated retry local tunnel port %d", retryLocalPort)
				retryTunnelCmd, retryTunnelErr := startSSHTunnel(hostAlias, retryLocalPort, remotePort, logger)
				if retryTunnelErr != nil {
					logger.Logf("retry health: failed to start retry tunnel: %v", retryTunnelErr)
				} else {
					if retryErr := waitForWebHealth(retryLocalPort, 12*time.Second); retryErr == nil {
						logger.Logf("health check passed on retry local port %d", retryLocalPort)
						localPort = retryLocalPort
						tunnelCmd = retryTunnelCmd
					} else {
						logger.Logf("retry health check failed: %v", retryErr)
						_ = killProcess(retryTunnelCmd)
					}
				}
			}
		}

		if pingErr := waitForWebHealth(localPort, 1500*time.Millisecond); pingErr != nil {
			details := collectSSHHealthFailureDetails(hostAlias, remotePort, remotePID, err, logger)
			_ = killProcess(tunnelCmd)
			// Don't kill a reused daemon — it was running before us.
			if !reusedDaemon {
				_ = stopRemoteSSHBackend(hostAlias, remotePID)
			}
			return nil, newSSHLaunchFailure("health-check", "failed to connect to SSH workspace", details, logger)
		}
	}
	logger.Logf("health check passed for local port %d", localPort)

	session := &sshWorkspaceSession{
		Key:                 sessionKey,
		HostAlias:           hostAlias,
		RemoteWorkspacePath: remoteWorkspacePath,
		LocalPort:           localPort,
		RemotePort:          remotePort,
		RemotePID:           remotePID,
		URL:                 fmt.Sprintf("http://127.0.0.1:%d", localPort),
		TunnelCmd:           tunnelCmd,
		StartedAt:           time.Now(),
		ReusedDaemon:        reusedDaemon,
	}

	ws.sshSessionsMu.Lock()
	ws.sshSessions[sessionKey] = session
	// Persist to disk while holding the lock to avoid race conditions with concurrent
	// session launches that could overwrite each other's registry entries.
	_ = persistSSHSession(session)
	ws.sshSessionsMu.Unlock()

	go ws.watchSSHSession(sessionKey, session, tunnelCmd)

	result = &sshLaunchResult{
		URL:       session.URL,
		LocalPort: session.LocalPort,
		ProxyBase: "/ssh/" + url.PathEscape(sessionKey),
	}
	return result, nil
}
