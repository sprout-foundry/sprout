package webui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// TerminalSession represents a terminal session
type TerminalSession struct {
	ID       string
	Command  *exec.Cmd
	Input    io.WriteCloser
	Output   io.Reader
	Error    io.Reader
	Cancel   context.CancelFunc
	Active   bool
	mutex    sync.RWMutex
	LastUsed time.Time
	History  []string
	HistoryIndex int
	OutputCh chan []byte
}

// TerminalManager manages terminal sessions
type TerminalManager struct {
	sessions map[string]*TerminalSession
	mutex    sync.RWMutex
}

// NewTerminalManager creates a new terminal manager
func NewTerminalManager() *TerminalManager {
	return &TerminalManager{
		sessions: make(map[string]*TerminalSession),
	}
}

// CreateSession creates a new terminal session
func (tm *TerminalManager) CreateSession(sessionID string) (*TerminalSession, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// Check if session already exists
	if _, exists := tm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	// Determine shell based on OS
	shell := "/bin/bash"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}

	// Create context for the command
	ctx, cancel := context.WithCancel(context.Background())

	// Setup command with interactive shell
	cmd := exec.CommandContext(ctx, shell)
	
	// Set environment variables for interactive shell
	cmd.Env = append(os.Environ(), 
		"TERM=xterm-256color",
		"PS1=\\[\\033[01;32m\\]\\u@\\h\\[\\033[00m\\]:\\[\\033[01;34m\\]\\w\\[\\033[00m\\]\\$ ",
	)

	// Create pipes for stdin, stdout, stderr
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

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Create session
	session := &TerminalSession{
		ID:       sessionID,
		Command:  cmd,
		Input:    stdin,
		Output:   stdout,
		Error:    stderr,
		Cancel:   cancel,
		Active:   true,
		LastUsed: time.Now(),
		OutputCh: make(chan []byte, 100),
	}

	tm.sessions[sessionID] = session

	// Start monitoring the session
	go tm.monitorSession(session)

	return session, nil
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

	// Add to history (without newline)
	cleanCommand := strings.TrimSuffix(command, "\n")
	tm.AddToHistory(sessionID, cleanCommand)

	// Add newline if not present
	if !strings.HasSuffix(command, "\n") {
		command += "\n"
	}

	// Write command to stdin
	_, err := session.Input.Write([]byte(command))
	if err != nil {
		return fmt.Errorf("failed to write command: %w", err)
	}

	session.LastUsed = time.Now()
	return nil
}

// CloseSession closes a terminal session
func (tm *TerminalManager) CloseSession(sessionID string) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	session, exists := tm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Cancel the context
	session.Cancel()

	// Close pipes
	if session.Input != nil {
		session.Input.Close()
	}

	// Wait for command to finish
	if session.Command != nil && session.Command.Process != nil {
		session.Command.Process.Wait()
	}

	session.Active = false
	delete(tm.sessions, sessionID)

	return nil
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

	// Start goroutines to read stdout and stderr
	go func() {
		buf := make([]byte, 1024)
		for {
			session.mutex.RLock()
			if !session.Active {
				session.mutex.RUnlock()
				break
			}
			session.mutex.RUnlock()

			// Read from stdout
			n, err := session.Output.Read(buf)
			if err != nil {
				if err != io.EOF {
					fmt.Printf("Terminal session %s stdout read error: %v\n", session.ID, err)
				}
				break
			}

			if n > 0 {
				session.LastUsed = time.Now()
				output := make([]byte, n)
				copy(output, buf[:n])
				
				// Send to output channel (non-blocking)
				select {
				case session.OutputCh <- output:
					// Output sent successfully
				default:
					// Channel is full, skip this output
					fmt.Printf("Terminal %s output channel full, dropping data\n", session.ID)
				}
				
				// Also print for debugging
				fmt.Printf("Terminal %s stdout: %s", session.ID, string(output))
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			session.mutex.RLock()
			if !session.Active {
				session.mutex.RUnlock()
				break
			}
			session.mutex.RUnlock()

			// Read from stderr
			n, err := session.Error.Read(buf)
			if err != nil {
				if err != io.EOF {
					fmt.Printf("Terminal session %s stderr read error: %v\n", session.ID, err)
				}
				break
			}

			if n > 0 {
				session.LastUsed = time.Now()
				output := make([]byte, n)
				copy(output, buf[:n])
				
				// Send to output channel (non-blocking)
				select {
				case session.OutputCh <- output:
					// Output sent successfully
				default:
					// Channel is full, skip this output
					fmt.Printf("Terminal %s output channel full, dropping data\n", session.ID)
				}
				
				// Also print for debugging
				fmt.Printf("Terminal %s stderr: %s", session.ID, string(output))
			}
		}
	}()

	// Wait for the command to finish
	session.Command.Wait()

	// Mark session as inactive
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