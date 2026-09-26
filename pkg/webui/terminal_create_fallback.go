//go:build !js

package webui

// terminal_create_fallback.go — the webui terminal-session fallback
// (non-PTY) creation path: createWindowsSession and the shell-arg resolver
// (resolveShellArgs). Selection is runtime.GOOS-based (no Windows-only
// syscalls), so this file carries the //go:build !js tag rather than a
// GOOS filename suffix. Split out of terminal_create.go.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

// createWindowsSession creates a fallback session for Windows (non-PTY).
func (tm *TerminalManager) createWindowsSession(sessionID string) (*TerminalSession, error) {
	// Windows implementation - simplified fallback without PTY.
	// Full PTY on Windows requires conpty which is more complex.
	cmd := exec.Command("cmd")
	ctx, cancel := context.WithCancel(context.Background())
	cmd = exec.CommandContext(ctx, cmd.Path)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

	defaultSize := &pty.Winsize{Rows: 24, Cols: 80}

	// Run cmd.exe under the same sanitized env as the Unix shells so the
	// user-interactive terminal on Windows does not inherit sprout's
	// log-suppression NO_COLOR / FORCE_COLOR (same leak fix as the Unix
	// paths — see buildTerminalEnv).
	cmd.Env = buildTerminalEnv("cmd", defaultSize)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	session := &TerminalSession{
		ID:        sessionID,
		Command:   cmd,
		Cancel:    cancel,
		Active:    true,
		LastUsed:  time.Now(),
		StartedAt: time.Now(),
		Size:      &pty.Winsize{Rows: 24, Cols: 80},
		ring:      newSessRing(),
	}

	// Store stdin in the Pty field for WriteRawInput compatibility.
	if ptyFile, ok := stdin.(*os.File); ok {
		session.Pty = ptyFile
	}

	// Start a reader goroutine for the stdout pipe.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				webuiLogger.Error("Windows terminal reader panicked", slog.String("session_id", sessionID), slog.Any("panic", r))
				session.mutex.Lock()
				session.Active = false
				session.closeBackgroundDoneLocked()
				session.mutex.Unlock()
				session.closeAllSubs()
			}
		}()
		buf := make([]byte, 32768)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				session.mutex.Lock()
				session.LastUsed = time.Now()
				session.mutex.Unlock()
				session.broadcast(chunk)
				if completed, code, isBg := session.checkBackgroundSentinel(chunk); completed {
					tm.notifySessionUpdate(map[string]interface{}{
						"session_id": session.ID,
						"chat_id":    session.ChatID,
						"event":      "completed",
						"exit_code":  code,
						"is_bg":      isBg,
					})
				}
			}
			if err != nil {
				session.mutex.Lock()
				session.Active = false
				session.closeBackgroundDoneLocked()
				session.mutex.Unlock()
				session.closeAllSubs()
				return
			}
		}
	}()

	return session, nil
}

// resolveShellArgs returns the extra arguments to pass when launching a shell
// in login/interactive mode so that rc files are sourced correctly.
func resolveShellArgs(shell string) []string {
	base := shell
	if idx := strings.LastIndex(shell, "/"); idx >= 0 {
		base = shell[idx+1:]
	}
	switch base {
	case "bash", "zsh":
		return []string{"--login"}
	default:
		return nil
	}
}
