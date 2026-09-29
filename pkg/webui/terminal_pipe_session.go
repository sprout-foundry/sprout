//go:build !js

package webui

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"

	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// createFallbackUnixSession creates a terminal session without a real PTY.
// This is the fallback path when pty.StartWithSize fails (e.g. on Alpine
// Linux, minimal containers, or systems without /dev/pts mounted).
func (tm *TerminalManager) createFallbackUnixSession(sessionID, shellOverride string) (*TerminalSession, error) {
	shell, shellArgs, err := tm.resolveShell(shellOverride)
	if err != nil {
		return nil, fmt.Errorf("resolve shell: %w", err)
	}
	return tm.startPipeSession(sessionID, shell, shellArgs)
}

// createWindowsSession creates a pipe-based session on Windows, where
// creack/pty has no ConPTY support. A POSIX shell (Git Bash) is preferred
// over cmd.exe because the agent-exec layer writes POSIX command lines
// (`/bin/sh -c '...' && echo sentinel:$?`) into hidden sessions.
func (tm *TerminalManager) createWindowsSession(sessionID, shellOverride string) (*TerminalSession, error) {
	shell, shellArgs, err := tm.resolveWindowsShell(shellOverride)
	if err != nil {
		return nil, fmt.Errorf("resolve shell: %w", err)
	}
	return tm.startPipeSession(sessionID, shell, shellArgs)
}

func (tm *TerminalManager) resolveWindowsShell(shellOverride string) (string, []string, error) {
	if override := strings.TrimSpace(shellOverride); override != "" {
		// The UI sends a listed shell's name; resolve it through the list so
		// "bash" means Git Bash rather than the WSL launcher on PATH.
		for _, s := range tm.availableWindowsShells() {
			if strings.EqualFold(s.Name, override) {
				return s.Path, resolveShellArgs(s.Path), nil
			}
		}
		if !shellExists(override) {
			return "", nil, fmt.Errorf("requested shell %q not found in PATH", override)
		}
		return override, resolveShellArgs(override), nil
	}
	if testShell := strings.TrimSpace(os.Getenv("SPROUT_TEST_SHELL")); testShell != "" && shellExists(testShell) {
		return testShell, nil, nil
	}
	if sh := shellexec.Path(); sh != "" {
		return sh, resolveShellArgs(sh), nil
	}
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec, nil, nil
	}
	return "cmd.exe", nil, nil
}

// startPipeSession runs shell with plain pipes instead of a PTY. The stdin
// write end is stored in session.Pty so WriteRawInput and ExecuteCommandAndWait
// work unchanged; stdout and stderr share one pipe drained by runPipeReader.
//
// Limitations (session.NoPTY == true): resize is a no-op, line editing and
// cursor control are unavailable, and Ctrl+C (\x03) is delivered as a plain
// byte rather than a signal.
func (tm *TerminalManager) startPipeSession(sessionID, shell string, shellArgs []string) (*TerminalSession, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}
	defaultSize := &pty.Winsize{Rows: 24, Cols: 80}
	cmd.Env = buildTerminalEnv(shell, defaultSize)

	// os.Pipe rather than cmd.StdinPipe: the latter returns an unexported
	// wrapper, and session.Pty must be an *os.File.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pipe session stdin pipe: %w", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		cancel()
		stdinR.Close()
		stdinW.Close()
		return nil, fmt.Errorf("pipe session stdout pipe: %w", err)
	}
	cmd.Stdin = stdinR
	cmd.Stdout = outW
	cmd.Stderr = outW

	startErr := cmd.Start()
	stdinR.Close()
	outW.Close()
	if startErr != nil {
		cancel()
		stdinW.Close()
		outR.Close()
		return nil, fmt.Errorf("pipe session start %s: %w", shell, startErr)
	}

	session := &TerminalSession{
		ID:        sessionID,
		Command:   cmd,
		Pty:       stdinW,
		Cancel:    cancel,
		Active:    true,
		LastUsed:  time.Now(),
		StartedAt: time.Now(),
		Size:      defaultSize,
		ring:      newSessRing(),
		NoPTY:     true,
	}

	go tm.runPipeReader(session, outR)

	return session, nil
}

func (tm *TerminalManager) runPipeReader(session *TerminalSession, stdout *os.File) {
	markDead := func() {
		session.mutex.Lock()
		session.Active = false
		session.closeBackgroundDoneLocked()
		session.mutex.Unlock()
		session.closeAllSubs()
	}
	defer stdout.Close()
	defer func() {
		if r := recover(); r != nil {
			webuiLogger.Error("pipe terminal reader panicked", slog.String("session_id", session.ID), slog.Any("panic", r))
			markDead()
		}
	}()
	buf := make([]byte, 32768)
	prevCR := false
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			var chunk []byte
			chunk, prevCR = translateNewlines(buf[:n], prevCR)
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
		if readErr != nil {
			webuiLogger.Info("pipe terminal stdout closed", slog.String("session_id", session.ID), slog.Any("err", readErr))
			markDead()
			return
		}
	}
}

// translateNewlines does a PTY's ONLCR output mapping: xterm.js moves down
// but not back to column 0 on a bare LF, so pipe output would staircase.
// prevCR carries a chunk-final CR so a CRLF split across reads is not
// doubled. Returns a fresh slice (buf is reused by the reader).
func translateNewlines(buf []byte, prevCR bool) ([]byte, bool) {
	out := make([]byte, 0, len(buf)+bytes.Count(buf, []byte{'\n'}))
	for _, b := range buf {
		if b == '\n' && !prevCR {
			out = append(out, '\r')
		}
		out = append(out, b)
		prevCR = b == '\r'
	}
	return out, prevCR
}
