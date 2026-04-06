package webui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

// CloseSession closes a terminal session and cleans up associated resources.
// For tmux-backed sessions, the tmux server session is also killed.
func (tm *TerminalManager) CloseSession(sessionID string) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	session, exists := tm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Stop the monitor goroutine
	session.mutex.Lock()
	if session.monitorDone != nil {
		select {
		case <-session.monitorDone:
		default:
			close(session.monitorDone)
		}
	}

	// Cancel the context (kills the attach process, not the tmux session)
	session.Cancel()

	// Close PTY
	if session.Pty != nil {
		session.Pty.Close()
	}

	// Wait for command to finish
	if session.Command != nil && session.Command.Process != nil {
		session.Command.Process.Wait()
	}

	session.Active = false
	tmuxBacked := session.TmuxBacked
	session.mutex.Unlock()

	// Kill the tmux session if this was tmux-backed
	if tmuxBacked {
		tmuxName := tm.TmuxSessionName(sessionID)
		killCmd := exec.Command("tmux", "kill-session", "-t", tmuxName)
		if err := killCmd.Run(); err != nil {
			// The tmux session might have been killed externally already.
			// This is not an error condition — just log it.
			fmt.Printf("TerminalManager: failed to kill tmux session %s (may already be dead): %v\n", tmuxName, err)
		}
	}

	delete(tm.sessions, sessionID)

	return nil
}

// DetachFromSession detaches the WebSocket from a session without destroying it.
// For tmux-backed sessions, the tmux session stays alive for future reattach.
// For raw PTY sessions, this is equivalent to CloseSession (they can't survive disconnect).
func (tm *TerminalManager) DetachFromSession(sessionID string) error {
	tm.mutex.RLock()
	session, exists := tm.sessions[sessionID]
	tm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.RLock()
	tmuxBacked := session.TmuxBacked
	session.mutex.RUnlock()

	if tmuxBacked {
		// For tmux-backed sessions: stop the monitor goroutine, cancel the attach
		// process, and close the PTY — but keep the tmux server session alive.
		session.mutex.Lock()
		if session.monitorDone != nil {
			select {
			case <-session.monitorDone:
			default:
				close(session.monitorDone)
			}
		}
		if session.Cancel != nil {
			session.Cancel()
		}
		if session.Pty != nil {
			session.Pty.Close()
		}
		if session.Command != nil && session.Command.Process != nil {
			_, _ = session.Command.Process.Wait()
		}
		session.Active = false
		session.mutex.Unlock()

		fmt.Printf("TerminalManager: detached from tmux-backed session %s (tmux session preserved for reattach)\n", sessionID)
		return nil
	}

	// For raw PTY sessions: close completely since they can't survive disconnect
	return tm.CloseSession(sessionID)
}

// CloseAllSessions closes all known terminal sessions and returns the first error encountered.
func (tm *TerminalManager) CloseAllSessions() error {
	sessionIDs := tm.ListSessions()
	var firstErr error
	for _, sessionID := range sessionIDs {
		if err := tm.CloseSession(sessionID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close session %s during CloseAllSessions: %w", sessionID, err)
		}
	}
	return firstErr
}

// ReattachSession reattaches to an existing tmux-backed session.
// It returns the scrollback buffer content for immediate display on the client.
// If the tmux session was killed externally, the stale entry is cleaned up and an error is returned.
func (tm *TerminalManager) ReattachSession(sessionID string, maxScrollback int) (string, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	session, exists := tm.sessions[sessionID]
	if !exists {
		return "", fmt.Errorf("session %s does not exist", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.TmuxBacked {
		return "", fmt.Errorf("session %s is not tmux-backed and cannot be reattached", sessionID)
	}

	tmuxName := tm.TmuxSessionName(sessionID)

	// Check if tmux session still exists
	hasSession := exec.Command("tmux", "has-session", "-t", tmuxName)
	if err := hasSession.Run(); err != nil {
		// Tmux session was killed externally — clean up stale entry
		fmt.Printf("TerminalManager: tmux session %s no longer exists, cleaning up\n", tmuxName)
		session.Active = false
		delete(tm.sessions, sessionID)
		return "", fmt.Errorf("tmux session %s no longer exists (killed externally)", tmuxName)
	}

	// Stop the old monitor goroutine
	if session.monitorDone != nil {
		select {
		case <-session.monitorDone:
			// Already stopped
		default:
			close(session.monitorDone)
		}
	}

	// Cancel old attach command and close old PTY
	if session.Cancel != nil {
		session.Cancel()
	}
	if session.Pty != nil {
		session.Pty.Close()
	}
	// Wait for old command to finish (non-blocking)
	if session.Command != nil && session.Command.Process != nil {
		_, _ = session.Command.Process.Wait()
	}

	// Capture scrollback with escape sequences for proper rendering
	scrollback, err := tm.captureScrollback(tmuxName, maxScrollback)
	if err != nil {
		fmt.Printf("TerminalManager: warning: failed to capture scrollback for %s: %v\n", tmuxName, err)
		// Non-fatal: continue with reattach even if scrollback capture fails
	}

	// Create new PTY by attaching to the tmux session
	ctx, cancel := context.WithCancel(context.Background())

	attachCmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", tmuxName)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		attachCmd.Dir = tm.workspaceRoot
	}
	attachCmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	ptyFile, err := pty.StartWithSize(attachCmd, session.Size)
	if err != nil {
		cancel()
		// Tmux session might have died between has-session check and attach
		session.Active = false
		delete(tm.sessions, sessionID)
		killCmd := exec.Command("tmux", "kill-session", "-t", tmuxName)
		_ = killCmd.Run()
		return "", fmt.Errorf("failed to reattach to tmux session: %w", err)
	}

	// Replace session fields with new PTY
	session.Command = attachCmd
	session.Pty = ptyFile
	session.Output = ptyFile
	session.Cancel = cancel
	session.Active = true
	session.LastUsed = time.Now()
	session.OutputCh = make(chan []byte, 10000)
	session.monitorDone = make(chan struct{})

	// Start a new monitor goroutine
	go tm.monitorSession(session)

	return scrollback, nil
}

// captureScrollback captures the scrollback buffer from a tmux session.
func (tm *TerminalManager) captureScrollback(tmuxName string, maxLines int) (string, error) {
	if maxLines <= 0 {
		maxLines = 2000
	}
	captureCmd := exec.Command("tmux", "capture-pane",
		"-t", tmuxName,
		"-p",
		"-S", fmt.Sprintf("-%d", maxLines),
		"-e", // include escape sequences
	)
	var stdout, stderr bytes.Buffer
	captureCmd.Stdout = &stdout
	captureCmd.Stderr = &stderr
	if err := captureCmd.Run(); err != nil {
		return "", fmt.Errorf("tmux capture-pane failed: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// monitorSession monitors a terminal session and handles output.
func (tm *TerminalManager) monitorSession(session *TerminalSession) {
	// Capture channel references at function entry so this goroutine
	// always closes the channels it was started with — even if ReattachSession
	// replaces them with new ones while we are still running.
	outputCh := session.OutputCh
	doneCh := session.monitorDone

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Terminal session %s monitor panic: %v\n", session.ID, r)
		}
		// Close the output channel this goroutine was started with.
		if outputCh != nil {
			close(outputCh)
		}
	}()

	// Signal that this goroutine has stopped.
	defer func() {
		if doneCh != nil {
			select {
			case <-doneCh:
				// Already closed (by Detach/Close/Reattach)
			default:
				close(doneCh)
			}
		}
	}()

	// Start goroutine to read from PTY (single stream for both stdout and stderr)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 1024)
		for {
			session.mutex.RLock()
			if !session.Active {
				session.mutex.RUnlock()
				break
			}
			outputReader := session.Output
			session.mutex.RUnlock()

			if outputReader == nil {
				break
			}

			// Read from PTY (handles both stdout and stderr)
			n, err := outputReader.Read(buf)
			if err != nil {
				if err != io.EOF {
					fmt.Printf("Terminal session %s PTY read error: %v\n", session.ID, err)
				}
				break
			}

			if n > 0 {
				session.LastUsed = time.Now()
				output := make([]byte, n)
				copy(output, buf[:n])
				fmt.Printf("Terminal %s: Read %d bytes from PTY: %q\n", session.ID, n, string(output))

				// Send to output channel (non-blocking)
				select {
				case outputCh <- output:
					// Output sent successfully
				case <-doneCh:
					// Monitor was asked to stop
					return
				default:
					// Channel is full, skip this output
					fmt.Printf("Terminal %s output channel full, dropping data\n", session.ID)
				}
			}
		}
	}()

	// Wait for either the command to finish, the read goroutine to stop, or monitor to be cancelled
	select {
	case <-readDone:
		// Read goroutine finished
	case <-doneCh:
		// Monitor was asked to stop (detach/reattach/close).
		// Wait for the reader goroutine to fully stop before we close OutputCh,
		// otherwise it may panic with "send on closed channel".
		<-readDone
		return
	}

	// Mark session as inactive only if the command exited on its own
	// (not if we were cancelled externally by detach/reattach)
	session.mutex.Lock()
	session.Active = false
	session.mutex.Unlock()

	fmt.Printf("Terminal session %s ended\n", session.ID)
}

// CleanupInactiveSessions removes sessions that have been inactive for too long.
func (tm *TerminalManager) CleanupInactiveSessions(timeout time.Duration) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	now := time.Now()
	for sessionID, session := range tm.sessions {
		session.mutex.RLock()
		inactive := now.Sub(session.LastUsed) > timeout
		session.mutex.RUnlock()

		if inactive {
			fmt.Printf("Cleaning up inactive terminal session: %s\n", sessionID)
			go tm.CloseSession(sessionID)
		}
	}
}

// StartCleanupWorker starts a background worker to clean up inactive sessions.
func (tm *TerminalManager) StartCleanupWorker(ctx context.Context, interval time.Duration, timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tm.CleanupInactiveSessions(timeout)
			}
		}
	}()
}
