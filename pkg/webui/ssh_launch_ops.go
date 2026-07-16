//go:build !js

// Package webui: SSH tunnel, health check, and diagnostics helpers (split from ssh_launch.go).

package webui

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

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
		lastErr = fmt.Errorf("timed out waiting for remote sprout backend")
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

// extractSentinelResult finds the content after a sentinel marker line in
// SSH command output. RC file sourcing (fortune, neofetch, MOTD) can emit
// stray stdout that corrupts line-based parsing — the sentinel ensures we
// only parse the intentional result block.
func extractSentinelResult(output, marker string) (string, error) {
	idx := strings.Index(output, marker)
	if idx < 0 {
		return "", fmt.Errorf("sentinel marker %q not found in output", marker)
	}
	// Return everything after the marker line (skip the marker itself + newline)
	rest := output[idx+len(marker):]
	// Skip the newline after the marker
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimPrefix(rest, "\r\n")
	return rest, nil
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
		fmt.Sprintf(`LOG_FILE="$HOME/.cache/sprout-webui/logs/%s.log"`, sanitizeRemoteLogName(hostAlias)),
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
