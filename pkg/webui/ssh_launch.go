package webui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SSH command helpers
// ---------------------------------------------------------------------------

func runSSHLoggedCommand(logger *sshLaunchLogger, step, summary string, cmd *exec.Cmd) ([]byte, error) {
	logger.Logf("%s: running %s", step, summary)
	out, err := cmd.CombinedOutput()
	output := trimSSHOutput(out)
	if output != "" {
		logger.Logf("%s output:\n%s", step, output)
	}
	if err != nil {
		logger.Logf("%s error: %v", step, err)
		return out, newSSHLaunchFailure(step, "SSH workspace setup failed", output, logger)
	}
	logger.Logf("%s completed", step)
	return out, nil
}

func newSSHCommand(hostAlias, script string, extraArgs ...string) *exec.Cmd {
	baseArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ConnectionAttempts=1",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=2",
	}
	baseArgs = append(baseArgs, extraArgs...)
	baseArgs = append(baseArgs, hostAlias, fmt.Sprintf("sh -lc %s", shellEscapeSSH(script)))
	return exec.Command("ssh", baseArgs...)
}

func newSSHCommandContext(ctx context.Context, hostAlias, script string, extraArgs ...string) *exec.Cmd {
	baseArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ConnectionAttempts=1",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=2",
	}
	baseArgs = append(baseArgs, extraArgs...)
	baseArgs = append(baseArgs, hostAlias, fmt.Sprintf("sh -lc %s", shellEscapeSSH(script)))
	return exec.CommandContext(ctx, "ssh", baseArgs...)
}

// ---------------------------------------------------------------------------
// SSH launch status
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) setSSHLaunchStatus(sessionKey, step, status string, inProgress bool, lastErr string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	ws.sshLaunchStatusMu.Lock()
	defer ws.sshLaunchStatusMu.Unlock()
	ws.sshLaunchStatuses[sessionKey] = &sshLaunchStatus{
		Key:        sessionKey,
		Step:       strings.TrimSpace(step),
		Status:     strings.TrimSpace(status),
		InProgress: inProgress,
		LastError:  strings.TrimSpace(lastErr),
		UpdatedAt:  time.Now(),
	}
}

func (ws *ReactWebServer) getSSHLaunchStatus(sessionKey string) *sshLaunchStatus {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	ws.sshLaunchStatusMu.RLock()
	defer ws.sshLaunchStatusMu.RUnlock()
	status := ws.sshLaunchStatuses[sessionKey]
	if status == nil {
		return nil
	}
	copy := *status
	return &copy
}

// ---------------------------------------------------------------------------
// SSH workspace launch
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) launchSSHWorkspace(req sshLaunchRequestDTO) (result *sshLaunchResult, err error) {
	hostAlias := strings.TrimSpace(req.HostAlias)
	if hostAlias == "" {
		return nil, fmt.Errorf("SSH host alias is required")
	}

	remoteWorkspacePath := strings.TrimSpace(req.RemoteWorkspacePath)
	if remoteWorkspacePath == "" {
		remoteWorkspacePath = "$HOME"
	}

	logger, loggerErr := newSSHLaunchLogger(hostAlias, remoteWorkspacePath)
	if loggerErr != nil {
		return nil, fmt.Errorf("failed to create SSH launch logger for %s: %w", hostAlias, loggerErr)
	}
	defer logger.Close()

	sessionKey := hostAlias + "::" + remoteWorkspacePath
	ws.setSSHLaunchStatus(sessionKey, "connecting", fmt.Sprintf("Connecting to %s...", hostAlias), true, "")
	defer func() {
		if err != nil {
			ws.setSSHLaunchStatus(sessionKey, "failed", "SSH workspace launch failed", false, err.Error())
			return
		}
		ws.setSSHLaunchStatus(sessionKey, "ready", fmt.Sprintf("SSH workspace ready: %s", hostAlias), false, "")
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
	remoteBinary, err := ensureRemoteSSHBinary(launchCtx, hostAlias, localBinary, remoteInfo, logger)
	if err != nil {
		return nil, fmt.Errorf("install remote SSH binary: %w", err)
	}
	logger.Logf("remote SSH backend installed at %s", remoteBinary)

	ws.setSSHLaunchStatus(sessionKey, "allocating-local-port", "Allocating local tunnel port...", true, "")
	localPort, err := findFreeLocalPort()
	if err != nil {
		logger.Logf("failed to allocate local tunnel port: %v", err)
		return nil, fmt.Errorf("allocate local tunnel port: %w", err)
	}
	logger.Logf("allocated local tunnel port %d", localPort)

	launcherURL := fmt.Sprintf("http://127.0.0.1:%d", ws.port)
	ws.setSSHLaunchStatus(sessionKey, "starting-remote-backend", fmt.Sprintf("Starting remote backend on %s...", hostAlias), true, "")
	remotePort, remotePID, reusedDaemon, err := startRemoteSSHBackend(launchCtx, hostAlias, sessionKey, launcherURL, remoteWorkspacePath, remoteBinary, logger)
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

// ---------------------------------------------------------------------------
// Remote backend start/stop
// ---------------------------------------------------------------------------

func startRemoteSSHBackend(ctx context.Context, hostAlias, sessionKey, launcherURL, remoteWorkspacePath, remoteBinary string, logger *sshLaunchLogger) (remotePort int, remotePID int, reused bool, err error) {
	workspaceRaw := strings.TrimSpace(remoteWorkspacePath)
	if workspaceRaw == "" {
		workspaceRaw = "$HOME"
	}

	// SSH remote daemons use a fixed port so that multiple SSH sessions
	// to the same host can detect and reuse an existing daemon instead of
	// launching duplicates.

	script := strings.Join([]string{
		"set -e",
		`DAEMON_PORT=` + fmt.Sprintf("%d", DaemonPort),
		"",
		"# check_existing_daemon: health-probe port DAEMON_PORT and return PID if healthy.",
		`check_existing_daemon() {`,
		`  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then`,
		`    return 1`,
		`  fi`,
		`  local resp=""`,
		`  if command -v curl >/dev/null 2>&1; then`,
		`    resp=$(curl -sf -m 3 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || return 1`,
		`  else`,
		`    resp=$(wget -qO- -T 3 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || return 1`,
		`  fi`,
		`  # Look for a JSON response with a "port" field to confirm ledit.`,
		`  case "$resp" in`,
		`    *'"status":"ok"'*) ;;`,
		`    *) return 1 ;;`,
		`  esac`,
		`  # Find the PID listening on the daemon port.`,
		`  local pid=""`,
		`  if command -v lsof >/dev/null 2>&1; then`,
		`    pid=$(lsof -ti tcp:$DAEMON_PORT -sTCP:LISTEN 2>/dev/null | head -1)`,
		`  elif command -v ss >/dev/null 2>&1; then`,
		`    pid=$(ss -tlnpH "sport = :$DAEMON_PORT" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1)`,
		`  elif command -v netstat >/dev/null 2>&1; then`,
		`    pid=$(netstat -tlnp 2>/dev/null | grep ":$DAEMON_PORT " | sed -n 's/.*LISTEN[[:space:]]*\([0-9]*\)\/.*/\1/p' | head -1)`,
		`  elif command -v fuser >/dev/null 2>&1; then`,
		`    pid=$(fuser "$DAEMON_PORT/tcp" 2>/dev/null | head -1)`,
		`  fi`,
		`  [ -n "$pid" ] && [ "$pid" -gt 0 ] 2>/dev/null && echo "$pid"`,
		`  return 0`,
		`}`,
		"",
		"# Try to reuse an existing daemon on this host.",
		`EXISTING_PID=$(check_existing_daemon || true)`,
		`if [ -n "$EXISTING_PID" ]; then`,
		`  printf "%s\\n%s\\nreused\\n" "$DAEMON_PORT" "$EXISTING_PID"`,
		`  exit 0`,
		`fi`,
		"",
		"# No existing daemon — start one.",
		`mkdir -p "$HOME/.cache/ledit-webui/logs"`,
		fmt.Sprintf("WORKSPACE_RAW=%s", shellEscapeSSH(workspaceRaw)),
		`WORKSPACE_PATH="$WORKSPACE_RAW"`,
		`case "$WORKSPACE_PATH" in`,
		`  '$HOME'|'${HOME}') WORKSPACE_PATH="$HOME" ;;`,
		`  '$HOME/'*) WORKSPACE_PATH="$HOME/${WORKSPACE_PATH#\$HOME/}" ;;`,
		`  '${HOME}/'*) WORKSPACE_PATH="$HOME/${WORKSPACE_PATH#\${HOME}/}" ;;`,
		`  '~') WORKSPACE_PATH="$HOME" ;;`,
		`  '~/'*) WORKSPACE_PATH="$HOME/${WORKSPACE_PATH#~/}" ;;`,
		`esac`,
		// Validate the workspace path is accessible without cd-ing into it.
		// The daemon runs host-level from $HOME to support multi-workspace.
		`test -d "$WORKSPACE_PATH" || { echo "remote workspace path is not accessible: $WORKSPACE_RAW" >&2; exit 1; }`,
		fmt.Sprintf(`LOG_FILE="$HOME/.cache/ledit-webui/logs/%s.log"`, sanitizeRemoteLogName(hostAlias)),
		// Launch the daemon from $HOME (not the workspace directory) using
		// the user's main config.  No --isolated-config — this is a host-level
		// daemon that serves multiple workspaces via per-client context.
		// Explicitly cd to $HOME to ensure daemonRoot is the user's home,
		// regardless of what the SSH login shell's profile does.
		fmt.Sprintf(
			`cd "$HOME" 2>/dev/null || cd /tmp; `+
				`nohup env BROWSER=none LEDIT_SSH_HOST_ALIAS=%s LEDIT_SSH_SESSION_KEY=%s LEDIT_SSH_LAUNCHER_URL=%s LEDIT_SSH_HOME="$HOME" %s agent --daemon --web-port "$DAEMON_PORT" >"$LOG_FILE" 2>&1 < /dev/null &`,
			shellEscapeSSH(hostAlias),
			shellEscapeSSH(sessionKey),
			shellEscapeSSH(launcherURL),
			shellEscapeSSH(remoteBinary),
		),
		"REMOTE_PID=$!",
		`# Poll the daemon's health endpoint until it responds or the process dies.`,
		`if command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1; then`,
		`  START_WAIT=0`,
		`  MAX_WAIT=15`,
		`  while [ $START_WAIT -lt $MAX_WAIT ]; do`,
		`    if ! kill -0 "$REMOTE_PID" 2>/dev/null; then`,
		`      echo "ERROR: ledit daemon exited prematurely on port $DAEMON_PORT" >&2`,
		`      echo "Last 50 lines from daemon log:" >&2`,
		`      tail -n 50 "$LOG_FILE" 2>&1 | while IFS= read -r line; do echo "  $line" >&2; done`,
		`      exit 1`,
		`    fi`,
		`    HEALTH=""`,
		`    if command -v curl >/dev/null 2>&1; then`,
		`      HEALTH=$(curl -sf -m 1 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || true`,
		`    else`,
		`      HEALTH=$(wget -qO- -T 1 "http://127.0.0.1:$DAEMON_PORT/health" 2>/dev/null) || true`,
		`    fi`,
		`    case "$HEALTH" in`,
		`      *'"status":"ok"'*) break ;;`,
		`    esac`,
		`    sleep 1`,
		`    START_WAIT=$((START_WAIT + 1))`,
		`  done`,
		`else`,
		`  sleep 1`,
		`  if ! kill -0 "$REMOTE_PID" 2>/dev/null; then`,
		`    echo "ERROR: ledit daemon failed to start on port $DAEMON_PORT — another daemon may already be running on this host" >&2`,
		`    exit 1`,
		`  fi`,
		`fi`,
		`printf "%s\n%s\nnew\n" "$DAEMON_PORT" "$REMOTE_PID"`,
	}, "\n")

	cmd := newSSHCommandContext(ctx, hostAlias, script)
	out, err := runSSHLoggedCommand(logger, "start-remote-backend", fmt.Sprintf("ssh %s start remote backend", hostAlias), cmd)
	if err != nil {
		return 0, 0, false, fmt.Errorf("start remote backend for %s: %w", hostAlias, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0, false, fmt.Errorf("failed to determine remote backend port for %s", hostAlias)
	}
	remotePort, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || remotePort <= 0 {
		return 0, 0, false, fmt.Errorf("invalid remote web port for %s", hostAlias)
	}
	remotePID, _ = strconv.Atoi(strings.TrimSpace(lines[1]))
	wasReused := len(lines) >= 3 && strings.TrimSpace(lines[2]) == "reused"
	return remotePort, remotePID, wasReused, nil
}

func stopRemoteSSHBackend(hostAlias string, remotePID int) error {
	if strings.TrimSpace(hostAlias) == "" || remotePID <= 0 {
		return nil
	}

	cmd := newSSHCommand(hostAlias, fmt.Sprintf("kill %d >/dev/null 2>&1 || true", remotePID))
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("failed to kill SSH process: %s: %w", msg, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// SSH tunnel & health check
// ---------------------------------------------------------------------------

func startSSHTunnel(hostAlias string, localPort, remotePort int, logger *sshLaunchLogger) (*exec.Cmd, error) {
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ConnectionAttempts=1",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ExitOnForwardFailure=yes",
		"-N",
		"-L", fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort),
		hostAlias,
	)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		logger.Logf("start-tunnel error: %v %s", err, strings.TrimSpace(stderr.String()))
		return nil, newSSHLaunchFailure("start-tunnel", "failed to start SSH tunnel", strings.TrimSpace(stderr.String()), logger)
	}
	logger.Logf("start-tunnel launched pid=%d", cmd.Process.Pid)

	return cmd, nil
}

func waitForWebHealth(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 800 * time.Millisecond}
	var lastErr error
	const pollInterval = 250 * time.Millisecond

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health endpoint returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(pollInterval)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("timed out waiting for remote ledit backend")
	}
	return fmt.Errorf("failed to connect to SSH workspace: %w", lastErr)
}

func isRetryableSSHHealthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "eof")
}

// ---------------------------------------------------------------------------
// diagnostics helpers
// ---------------------------------------------------------------------------

func sanitizeRemoteLogName(hostAlias string) string {
	var b strings.Builder
	for _, r := range hostAlias {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "ssh"
	}
	return b.String()
}

func collectSSHHealthFailureDetails(hostAlias string, remotePort, remotePID int, probeErr error, logger *sshLaunchLogger) string {
	sections := make([]string, 0, 3)
	if probeErr != nil {
		sections = append(sections, fmt.Sprintf("Local health probe failed: %v", probeErr))
	}

	remoteDetails := inspectRemoteSSHBackendFailure(hostAlias, remotePort, remotePID, logger)
	if remoteDetails != "" {
		sections = append(sections, remoteDetails)
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func inspectRemoteSSHBackendFailure(hostAlias string, remotePort, remotePID int, logger *sshLaunchLogger) string {
	hostAlias = strings.TrimSpace(hostAlias)
	if hostAlias == "" {
		return ""
	}

	script := strings.Join([]string{
		"set +e",
		fmt.Sprintf("REMOTE_PORT=%d", remotePort),
		fmt.Sprintf("REMOTE_PID=%d", remotePID),
		fmt.Sprintf(`LOG_FILE="$HOME/.cache/ledit-webui/logs/%s.log"`, sanitizeRemoteLogName(hostAlias)),
		`echo "Remote backend port: $REMOTE_PORT"`,
		`if [ "$REMOTE_PID" -gt 0 ] && kill -0 "$REMOTE_PID" >/dev/null 2>&1; then`,
		`  echo "Remote backend PID: $REMOTE_PID (running)"`,
		"else",
		`  echo "Remote backend PID: $REMOTE_PID (not running)"`,
		"fi",
		`if [ "$REMOTE_PORT" -gt 0 ]; then`,
		`  if command -v ss >/dev/null 2>&1; then`,
		`    echo "--- remote listener (ss) ---"`,
		`    ss -ltn 2>/dev/null | grep -E "[:.]$REMOTE_PORT[[:space:]]" || true`,
		`  elif command -v netstat >/dev/null 2>&1; then`,
		`    echo "--- remote listener (netstat) ---"`,
		`    netstat -ltn 2>/dev/null | grep -E "[:.]$REMOTE_PORT[[:space:]]" || true`,
		`  fi`,
		`  if command -v curl >/dev/null 2>&1; then`,
		`    echo "--- remote health probe ---"`,
		`    curl -sS -i --max-time 2 "http://127.0.0.1:$REMOTE_PORT/health" || true`,
		`  fi`,
		`fi`,
		`echo "Remote log: $LOG_FILE"`,
		`if [ -f "$LOG_FILE" ]; then`,
		`  echo "--- remote log tail ---"`,
		`  tail -n 80 "$LOG_FILE" 2>/dev/null || cat "$LOG_FILE" 2>/dev/null || true`,
		"else",
		`  echo "Remote log file not found"`,
		"fi",
	}, "\n")

	cmd := newSSHCommand(hostAlias, script)
	out, err := cmd.CombinedOutput()
	output := trimSSHOutput(out)
	if err != nil {
		if output != "" {
			logger.Logf("inspect-remote-failure output:\n%s", output)
		}
		logger.Logf("inspect-remote-failure error: %v", err)
		if output == "" {
			output = err.Error()
		}
		return fmt.Sprintf("Remote diagnostics failed: %s", output)
	}
	if output != "" {
		logger.Logf("inspect-remote-failure output:\n%s", output)
	}
	return output
}
