package webui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/creack/pty"
)

// CreateSession creates a new terminal session with PTY support.
// When tmux is available on Unix, the session is backed by a tmux server
// so it can survive WebSocket disconnections and be reattached.
// shellOverride, if non-empty, specifies the preferred shell binary (must be in PATH).
func (tm *TerminalManager) CreateSession(sessionID string, shellOverride ...string) (*TerminalSession, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// Check if session already exists
	if _, exists := tm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	// Resolve optional shell override from variadic arg
	var override string
	if len(shellOverride) > 0 {
		override = shellOverride[0]
	}

	// Determine shell based on OS with better fallback logic
	switch runtime.GOOS {
	case "windows":
		// On Windows, fallback to basic exec approach
		return tm.createWindowsSession(sessionID)
	default:
		// Unix-like systems - try tmux first, then raw PTY
		if tm.tmuxAvailable {
			return tm.createTmuxSession(sessionID, override)
		}
		return tm.createUnixSession(sessionID, override)
	}
}

// createTmuxSession creates a tmux-backed terminal session for persistence.
func (tm *TerminalManager) createTmuxSession(sessionID string, shellOverride ...string) (*TerminalSession, error) {
	tmuxName := tm.TmuxSessionName(sessionID)

	// Determine the shell to use
	var override string
	if len(shellOverride) > 0 {
		override = shellOverride[0]
	}
	shell, shellArgs, err := tm.resolveShell(override)
	if err != nil {
		return nil, fmt.Errorf("resolve shell for tmux session: %w", err)
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
	// Inherit useful env vars for the shell inside tmux.
	// COLUMNS and LINES are critical: many Node.js packages (e.g. regex,
	// webpack) read process.stdout.columns or $COLUMNS to determine
	// terminal width.  When running inside a PTY-backed web terminal the
	// process may not report a valid TTY width, causing NaN errors.
	createCmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=1",
		"SHELL="+shell,
		"LEDIT_WEB_TERMINAL=1",
		"COLUMNS=80",
		"LINES=24",
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
func (tm *TerminalManager) createUnixSession(sessionID string, shellOverride ...string) (*TerminalSession, error) {
	var override string
	if len(shellOverride) > 0 {
		override = shellOverride[0]
	}
	shell, shellArgs, err := tm.resolveShell(override)
	if err != nil {
		return nil, fmt.Errorf("resolve shell for unix session: %w", err)
	}

	// Create context for the command
	ctx, cancel := context.WithCancel(context.Background())

	// Setup command with interactive shell
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

	// Set environment variables for better terminal experience.
	// COLUMNS and LINES are critical: many Node.js packages (e.g. regex,
	// webpack) read process.stdout.columns or $COLUMNS to determine
	// terminal width.  When running inside a PTY-backed web terminal the
	// process may not report a valid TTY width, causing NaN errors.
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=1",
		"SHELL="+shell,
		"LEDIT_WEB_TERMINAL=1",
		"COLUMNS=80",
		"LINES=24",
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
// shellOverride, if non-empty, is used directly (after verifying it exists).
func (tm *TerminalManager) resolveShell(shellOverride string) (shell string, shellArgs []string, err error) {
	override := strings.TrimSpace(shellOverride)
	if override != "" {
		if !shellExists(override) {
			return "", nil, fmt.Errorf("requested shell %q not found in PATH", override)
		}
		return override, resolveShellArgs(override), nil
	}

	userShell := os.Getenv("SHELL")
	switch {
	case userShell != "":
		return userShell, resolveShellArgs(userShell), nil
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

// resolveShellArgs returns suitable login args for a given shell name.
func resolveShellArgs(shell string) []string {
	base := filepath.Base(shell)
	switch base {
	case "fish":
		return nil // fish doesn't use --login
	case "sh", "dash", "ash", "ksh", "csh", "tcsh":
		return []string{"-l"}
	default:
		return []string{"--login"}
	}
}

// createWindowsSession creates a fallback session for Windows (non-PTY).
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
