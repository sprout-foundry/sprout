package webui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// tmuxSessionPrefix is prepended to session IDs when creating tmux session names.
const tmuxSessionPrefix = "ledit_"

// shellExists checks if a shell binary exists in PATH.
func shellExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// TerminalSession represents a terminal session
type TerminalSession struct {
	ID           string
	Command      *exec.Cmd
	Pty          *os.File  // PTY file handle
	Output       io.Reader // PTY handles both input and output
	Cancel       context.CancelFunc
	Active       bool
	mutex        sync.RWMutex
	LastUsed     time.Time
	History      []string
	HistoryIndex int
	OutputCh     chan []byte
	Size         *pty.Winsize // Terminal size for resizing
	TmuxBacked   bool         // Whether this session uses tmux for persistence
	monitorDone  chan struct{} // Close to signal the monitor goroutine to stop
}

// TerminalManager manages terminal sessions
type TerminalManager struct {
	sessions      map[string]*TerminalSession
	mutex         sync.RWMutex
	workspaceRoot string
	tmuxAvailable bool
	tmuxCheckOnce sync.Once
}

// NewTerminalManager creates a new terminal manager
func NewTerminalManager(workspaceRoot string) *TerminalManager {
	tm := &TerminalManager{
		sessions:      make(map[string]*TerminalSession),
		workspaceRoot: workspaceRoot,
	}
	// Check tmux availability once (Unix only)
	tm.tmuxCheckOnce.Do(func() {
		if runtime.GOOS != "windows" {
			tm.tmuxAvailable = shellExists("tmux")
			if tm.tmuxAvailable {
				fmt.Printf("TerminalManager: tmux detected, terminal persistence enabled\n")
			} else {
				fmt.Printf("TerminalManager: tmux not found, terminal persistence disabled (install tmux for persistence)\n")
			}
		}
	})
	return tm
}

// TmuxSessionName returns the tmux session name for a given session ID.
func (tm *TerminalManager) TmuxSessionName(sessionID string) string {
	return tmuxSessionPrefix + sessionID
}

// IsTmuxAvailable returns whether tmux was found at startup.
func (tm *TerminalManager) IsTmuxAvailable() bool {
	return tm.tmuxAvailable
}

// HasSession checks if a session exists (for reattach).
func (tm *TerminalManager) HasSession(sessionID string) bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	_, exists := tm.sessions[sessionID]
	return exists
}

// CreateSession creates a new terminal session with PTY support.
// When tmux is available on Unix, the session is backed by a tmux server
// so it can survive WebSocket disconnections and be reattached.
func (tm *TerminalManager) CreateSession(sessionID string) (*TerminalSession, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// Check if session already exists
	if _, exists := tm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	// Determine shell based on OS with better fallback logic
	switch runtime.GOOS {
	case "windows":
		// On Windows, fallback to basic exec approach
		return tm.createWindowsSession(sessionID)
	default:
		// Unix-like systems - try tmux first, then raw PTY
		if tm.tmuxAvailable {
			return tm.createTmuxSession(sessionID)
		}
		return tm.createUnixSession(sessionID)
	}
}

// createTmuxSession creates a tmux-backed terminal session for persistence.
func (tm *TerminalManager) createTmuxSession(sessionID string) (*TerminalSession, error) {
	tmuxName := tm.TmuxSessionName(sessionID)

	// Determine the shell to use
	shell, shellArgs, err := tm.resolveShell()
	if err != nil {
		return nil, err
	}

	// Default terminal size
	defaultSize := &pty.Winsize{
		Rows: 24,
		Cols: 80,
	}

	// Create the detached tmux session with the user's shell
	createArgs := []string{
		"new-session",
		"-d",
		"-s", tmuxName,
		"-x", fmt.Sprintf("%d", defaultSize.Cols),
		"-y", fmt.Sprintf("%d", defaultSize.Rows),
		shell,
	}
	createArgs = append(createArgs, shellArgs...)
	createCmd := exec.Command("tmux", createArgs...)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		createCmd.Dir = tm.workspaceRoot
	}
	// Inherit useful env vars for the shell inside tmux
	createCmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=1",
		"SHELL="+shell,
		"LEDIT_WEB_TERMINAL=1",
	)

	if output, err := createCmd.CombinedOutput(); err != nil {
		fmt.Printf("TerminalManager: tmux new-session failed for %s: %v, output: %s\n", tmuxName, err, string(output))
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Set environment variable inside tmux session for child processes
	setEnvCmd := exec.Command("tmux", "set-environment", "-t", tmuxName, "LEDIT_WEB_TERMINAL", "1")
	_ = setEnvCmd.Run() // Best effort

	// Disable tmux mouse mode so xterm.js handles its own scroll and selection.
	mouseOffCmd := exec.Command("tmux", "set-option", "-t", tmuxName, "mouse", "off")
	_ = mouseOffCmd.Run() // Best effort

	// Now attach to the tmux session to get a PTY we can read/write
	ctx, cancel := context.WithCancel(context.Background())

	attachCmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", tmuxName)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		attachCmd.Dir = tm.workspaceRoot
	}
	attachCmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	ptyFile, err := pty.StartWithSize(attachCmd, defaultSize)
	if err != nil {
		cancel()
		// Clean up the tmux session
		killCmd := exec.Command("tmux", "kill-session", "-t", tmuxName)
		_ = killCmd.Run()
		return nil, fmt.Errorf("failed to attach to tmux session: %w", err)
	}

	// Create session
	session := &TerminalSession{
		ID:          sessionID,
		Command:     attachCmd,
		Pty:         ptyFile,
		Output:      ptyFile,
		Cancel:      cancel,
		Active:      true,
		LastUsed:    time.Now(),
		OutputCh:    make(chan []byte, 10000),
		Size:        defaultSize,
		TmuxBacked:  true,
		monitorDone: make(chan struct{}),
	}

	tm.sessions[sessionID] = session

	// Start monitoring the session
	go tm.monitorSession(session)

	return session, nil
}

// createUnixSession creates a raw PTY terminal session (fallback when tmux is unavailable).
func (tm *TerminalManager) createUnixSession(sessionID string) (*TerminalSession, error) {
	shell, shellArgs, err := tm.resolveShell()
	if err != nil {
		return nil, err
	}

	// Create context for the command
	ctx, cancel := context.WithCancel(context.Background())

	// Setup command with interactive shell
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

	// Set environment variables for better terminal experience
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=1",
		"SHELL="+shell,
		"LEDIT_WEB_TERMINAL=1",
	)

	// Set default terminal size
	defaultSize := &pty.Winsize{
		Rows: 24,
		Cols: 80,
	}

	// Start the command with PTY
	ptyFile, err := pty.StartWithSize(cmd, defaultSize)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	// Create session
	session := &TerminalSession{
		ID:          sessionID,
		Command:     cmd,
		Pty:         ptyFile,
		Output:      ptyFile, // PTY handles both input and output
		Cancel:      cancel,
		Active:      true,
		LastUsed:    time.Now(),
		OutputCh:    make(chan []byte, 10000),
		Size:        defaultSize,
		TmuxBacked:  false,
		monitorDone: make(chan struct{}),
	}

	tm.sessions[sessionID] = session

	// Start monitoring the session
	go tm.monitorSession(session)

	return session, nil
}

// resolveShell determines which shell to use on Unix systems.
func (tm *TerminalManager) resolveShell() (shell string, shellArgs []string, err error) {
	userShell := os.Getenv("SHELL")
	switch {
	case userShell != "":
		return userShell, []string{"--login"}, nil
	case shellExists("bash"):
		return "bash", []string{"--login"}, nil
	case shellExists("zsh"):
		return "zsh", []string{"--login"}, nil
	case shellExists("sh"):
		return "sh", []string{"-l"}, nil
	default:
		return "", nil, fmt.Errorf("no suitable shell found")
	}
}

// createWindowsSession creates a fallback session for Windows (non-PTY)
func (tm *TerminalManager) createWindowsSession(sessionID string) (*TerminalSession, error) {
	// Windows implementation - this is a simplified version
	// Full PTY on Windows requires conpty which is more complex
	cmd := exec.Command("cmd")
	ctx, cancel := context.WithCancel(context.Background())
	cmd = exec.CommandContext(ctx, cmd.Path)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

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

	// Create a basic session (Windows fallback)
	session := &TerminalSession{
		ID:          sessionID,
		Command:     cmd,
		Output:      stdout, // Limited functionality on Windows
		Cancel:      cancel,
		Active:      true,
		LastUsed:    time.Now(),
		OutputCh:    make(chan []byte, 10000),
		TmuxBacked:  false,
		monitorDone: make(chan struct{}),
	}

	// For Windows, we'll store stdin separately in the PTY field for compatibility
	if ptyFile, ok := stdin.(*os.File); ok {
		session.Pty = ptyFile
	}

	return session, nil
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

// GetSession retrieves a terminal session
func (tm *TerminalManager) GetSession(sessionID string) (*TerminalSession, bool) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	session, exists := tm.sessions[sessionID]
	return session, exists
}

// ExecuteCommand executes a command in the specified session
func (tm *TerminalManager) ExecuteCommand(sessionID, command string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}

	// Add to history (without the trailing enter key) for normal commands only.
	cleanCommand := strings.TrimRight(command, "\r\n")
	controlOnly := isControlOnlyCommand(cleanCommand)
	if !controlOnly {
		// Trim whitespace and skip empty commands.
		trimmedCommand := strings.TrimSpace(cleanCommand)
		if trimmedCommand != "" {
			// Avoid consecutive duplicates.
			if len(session.History) == 0 || session.History[len(session.History)-1] != trimmedCommand {
				session.History = append(session.History, trimmedCommand)
				if len(session.History) > 1000 {
					session.History = session.History[1:]
				}
			}
			session.HistoryIndex = len(session.History)
		}
	}

	// PTY terminals expect carriage return for Enter. Control-only input
	// (for example Ctrl+C) should be forwarded as-is.
	if !controlOnly && !strings.HasSuffix(command, "\r") {
		command += "\r"
	}

	// Write command to PTY
	if session.Pty != nil {
		fmt.Printf("Terminal %s: Writing command: %q\n", sessionID, command)
		n, err := session.Pty.Write([]byte(command))
		fmt.Printf("Terminal %s: Wrote %d bytes, err: %v\n", sessionID, n, err)
		if err != nil {
			return fmt.Errorf("failed to write command to PTY: %w", err)
		}
	} else {
		// Fallback for systems without PTY
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	session.LastUsed = time.Now()
	return nil
}

// WriteRawInput writes raw terminal input bytes directly to PTY without
// command history mutation or implicit carriage-return handling.
func (tm *TerminalManager) WriteRawInput(sessionID, input string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}

	if session.Pty == nil {
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	if _, err := session.Pty.Write([]byte(input)); err != nil {
		return fmt.Errorf("failed to write raw input to PTY: %w", err)
	}

	session.LastUsed = time.Now()
	return nil
}

func isControlOnlyCommand(command string) bool {
	if command == "" {
		return false
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if ch >= 32 {
			return false
		}
	}
	return true
}

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
			firstErr = err
		}
	}
	return firstErr
}

// monitorSession monitors a terminal session and handles output
func (tm *TerminalManager) monitorSession(session *TerminalSession) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Terminal session %s monitor panic: %v\n", session.ID, r)
		}
		// Close output channel when done
		if session.OutputCh != nil {
			close(session.OutputCh)
		}
	}()

	// Signal that this goroutine has stopped.
	defer func() {
		if session.monitorDone != nil {
			select {
			case <-session.monitorDone:
				// Already closed (by Detach/Close/Reattach)
			default:
				close(session.monitorDone)
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
				case session.OutputCh <- output:
					// Output sent successfully
				case <-session.monitorDone:
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
	case <-session.monitorDone:
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

// CleanupInactiveSessions removes sessions that have been inactive for too long
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

// ListSessions returns a list of active session IDs
func (tm *TerminalManager) ListSessions() []string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	var sessions []string
	for sessionID := range tm.sessions {
		sessions = append(sessions, sessionID)
	}
	return sessions
}

// GetSessionCount returns the number of active sessions
func (tm *TerminalManager) GetSessionCount() int {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	count := 0
	for _, session := range tm.sessions {
		session.mutex.RLock()
		if session.Active {
			count++
		}
		session.mutex.RUnlock()
	}
	return count
}

// SessionCount returns the number of active sessions (alias for GetSessionCount)
func (tm *TerminalManager) SessionCount() int {
	return tm.GetSessionCount()
}

// StartCleanupWorker starts a background worker to clean up inactive sessions
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

// AddToHistory adds a command to the session history
func (tm *TerminalManager) AddToHistory(sessionID, command string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	// Trim whitespace and skip empty commands
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	// Avoid duplicates
	if len(session.History) > 0 && session.History[len(session.History)-1] == command {
		return nil
	}

	// Add to history (limit to 1000 commands)
	session.History = append(session.History, command)
	if len(session.History) > 1000 {
		session.History = session.History[1:]
	}

	// Reset history index to end
	session.HistoryIndex = len(session.History)

	return nil
}

// GetHistory returns the command history for a session
func (tm *TerminalManager) GetHistory(sessionID string) ([]string, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.RLock()
	defer session.mutex.RUnlock()

	// Return a copy to prevent external modification
	history := make([]string, len(session.History))
	copy(history, session.History)
	return history, nil
}

// NavigateHistory navigates through command history
func (tm *TerminalManager) NavigateHistory(sessionID string, direction string) (string, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if len(session.History) == 0 {
		return "", nil
	}

	switch direction {
	case "up":
		if session.HistoryIndex > 0 {
			session.HistoryIndex--
		}
	case "down":
		if session.HistoryIndex < len(session.History)-1 {
			session.HistoryIndex++
		} else if session.HistoryIndex == len(session.History)-1 {
			// If we're at the last command and go down, return empty string
			session.HistoryIndex = len(session.History)
			return "", nil
		}
	default:
		return "", fmt.Errorf("invalid direction: %s", direction)
	}

	if session.HistoryIndex < len(session.History) {
		return session.History[session.HistoryIndex], nil
	}
	return "", nil
}

// ResetHistoryIndex resets the history index to the end (for new input)
func (tm *TerminalManager) ResetHistoryIndex(sessionID string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	session.HistoryIndex = len(session.History)
	return nil
}

// ResizeTerminal resizes the terminal for the given session.
// For tmux-backed sessions, the tmux pane is also resized.
func (tm *TerminalManager) ResizeTerminal(sessionID string, rows, cols uint16) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}

	if session.Pty == nil {
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	// Create new window size
	newSize := &pty.Winsize{
		Rows: rows,
		Cols: cols,
	}

	// Resize the PTY
	if err := pty.Setsize(session.Pty, newSize); err != nil {
		return fmt.Errorf("failed to resize PTY: %w", err)
	}

	// For tmux-backed sessions, also resize the tmux pane
	if session.TmuxBacked {
		tmuxName := tm.TmuxSessionName(sessionID)
		// Note: we release the lock on session before calling tmux,
		// but we can't here because of the defer. So run it in a goroutine.
		go func() {
			resizeCmd := exec.Command("tmux", "resize-pane",
				"-t", tmuxName,
				"-x", fmt.Sprintf("%d", cols),
				"-y", fmt.Sprintf("%d", rows),
			)
			if err := resizeCmd.Run(); err != nil {
				// Non-fatal: the PTY resize already happened, tmux adjust may
				// not be critical in all cases
				fmt.Printf("TerminalManager: failed to resize tmux pane %s: %v\n", tmuxName, err)
			}
		}()
	}

	// Update the stored size
	session.Size = newSize

	return nil
}

// GetTerminalSize returns the current terminal size for the session
func (tm *TerminalManager) GetTerminalSize(sessionID string) (*pty.Winsize, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.RLock()
	defer session.mutex.RUnlock()

	if session.Size == nil {
		return nil, fmt.Errorf("terminal size not set for session %s", sessionID)
	}

	// Return a copy to prevent external modification
	size := &pty.Winsize{
		Rows: session.Size.Rows,
		Cols: session.Size.Cols,
	}

	return size, nil
}