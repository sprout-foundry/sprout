//go:build !js

// Package webui: SSH command helpers and launch-status management (split from ssh_launch.go).

package webui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SSH command helpers
// ---------------------------------------------------------------------------

// normalizeRemoteWorkspacePath converts tilde-prefixed paths to their $HOME
// equivalent so that "~/project", "$HOME/project", and "${HOME}/project" all
// collapse to the same canonical form used as the session key.
func normalizeRemoteWorkspacePath(path string) string {
	switch {
	case path == "~":
		return "$HOME"
	case strings.HasPrefix(path, "~/"):
		return "$HOME/" + strings.TrimPrefix(path, "~/")
	case strings.HasPrefix(path, "${HOME}"):
		return "$HOME" + strings.TrimPrefix(path, "${HOME}")
	default:
		return path
	}
}

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
	remoteShell := "bash"
	if !shellExists("bash") {
		remoteShell = "sh"
	}
	baseArgs = append(baseArgs, hostAlias, fmt.Sprintf("%s -lc %s", remoteShell, shellEscapeSSH(script)))
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
	remoteShell := "bash"
	if !shellExists("bash") {
		remoteShell = "sh"
	}
	baseArgs = append(baseArgs, hostAlias, fmt.Sprintf("%s -lc %s", remoteShell, shellEscapeSSH(script)))
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
