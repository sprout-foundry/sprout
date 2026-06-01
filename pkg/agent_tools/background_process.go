//go:build !js

package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackgroundProcess represents a tracked background process for CLI mode.
// Unlike WebUI background sessions (PTY-based), these use os/exec with
// output piped to a temp file for polling via check_background.
type BackgroundProcess struct {
	ID         string    // "bg-<sanitized-prefix>-<random-hex>"
	Cmd        *exec.Cmd // the running process (nil after exit)
	Process    *os.Process
	OutputPath string  // path to accumulated output temp file
	Dir        string  // working directory
	Command    string  // original command string
	StartedAt  time.Time
	LastPolled time.Time
	done       chan struct{} // closed when process exits
	exitCode   int
	mu         sync.Mutex
}

// BackgroundProcessManager manages background processes for CLI mode.
// Provides the same lifecycle as the WebUI's TerminalManager background
// sessions but without PTY support.
type BackgroundProcessManager struct {
	processes   map[string]*BackgroundProcess
	mu          sync.RWMutex
	expiry      time.Duration // default: 2 hours
	maxSessions int           // default: 10
	baseDir     string        // directory for output files
	done        chan struct{} // for stopping cleanup goroutine
	cleanupWg   sync.WaitGroup
}

// NewBackgroundProcessManager creates a new BackgroundProcessManager and starts the cleanup goroutine.
func NewBackgroundProcessManager() *BackgroundProcessManager {
	baseDir := filepath.Join(os.TempDir(), "sprout-bg")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		log.Printf("warn: failed to create background output directory %s: %v", baseDir, err)
	}

	m := &BackgroundProcessManager{
		processes:   make(map[string]*BackgroundProcess),
		expiry:      2 * time.Hour,
		maxSessions: 10,
		baseDir:     baseDir,
		done:        make(chan struct{}),
	}

	m.cleanupWg.Add(1)
	go m.cleanupLoop()
	return m
}

// bpmContextKey is the context key for BackgroundProcessManager.
type bpmContextKey struct{}

// WithBackgroundProcessManager returns a new context that carries the BackgroundProcessManager.
// Use BackgroundProcessManagerFromContext to retrieve it.
func WithBackgroundProcessManager(ctx context.Context, bpm *BackgroundProcessManager) context.Context {
	return context.WithValue(ctx, bpmContextKey{}, bpm)
}

// BackgroundProcessManagerFromContext extracts the BackgroundProcessManager from the context.
// Returns nil if no manager is available.
func BackgroundProcessManagerFromContext(ctx context.Context) *BackgroundProcessManager {
	if bpm, ok := ctx.Value(bpmContextKey{}).(*BackgroundProcessManager); ok {
		return bpm
	}
	return nil
}

// Start creates a new background process, pipes its output to a temp file,
// and returns a session ID for later polling.
func (m *BackgroundProcessManager) Start(ctx context.Context, command string, dir string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	m.mu.RLock()
	if len(m.processes) >= m.maxSessions {
		m.mu.RUnlock()
		return "", fmt.Errorf("background session limit reached (%d active)", m.maxSessions)
	}
	m.mu.RUnlock()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", command)

	if dir != "" {
		cmd.Dir = dir
	} else {
		if wd, err := os.Getwd(); err == nil {
			cmd.Dir = wd
		}
	}

	// Set process group so we can kill the entire group on stop
	setProcessGroup(cmd)

	// Generate session ID
	prefix := extractCommandPrefixCLI(command)
	sanitizedPrefix := sanitizeSessionIDPartCLI(prefix)
	randomHex, err := generateRandomHexCLI(4)
	if err != nil {
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}
	sessionID := fmt.Sprintf("bg-%s-%s", sanitizedPrefix, randomHex)

	// Create output file in the base directory with owner-only permissions
	outputPath := filepath.Join(m.baseDir, sessionID+".output")
	outputFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}

	// Buffer for early output capture
	var outputBuf bytes.Buffer
	writer := io.MultiWriter(outputFile, &outputBuf)
	cmd.Stdout = writer
	cmd.Stderr = writer

	// Start the process
	if err := cmd.Start(); err != nil {
		outputFile.Close()
		os.Remove(outputPath)
		return "", fmt.Errorf("start command: %w", err)
	}

	proc := &BackgroundProcess{
		ID:         sessionID,
		Cmd:        cmd,
		Process:    cmd.Process,
		OutputPath: outputPath,
		Dir:        cmd.Dir,
		Command:    command,
		StartedAt:  time.Now(),
		LastPolled: time.Now(),
		done:       make(chan struct{}),
	}

	// Monitor process exit in a goroutine
	go func() {
		waitErr := cmd.Wait() // reap the zombie
		exitCode := extractExitCode(waitErr)
		proc.mu.Lock()
		proc.exitCode = exitCode
		proc.Cmd = nil
		proc.Process = nil
		proc.mu.Unlock()
		close(proc.done)
		// Close the output file handle after process exits
		outputFile.Close()
	}()

	m.mu.Lock()
	m.processes[sessionID] = proc
	m.mu.Unlock()

	return sessionID, nil
}

// AdoptProcess takes an already-started exec.Cmd (from timeout promotion) and
// registers it into the background process manager. The output file is already
// created by the caller.
//
// If waitCh is non-nil, AdoptProcess assumes the caller has already started a
// goroutine calling cmd.Wait() and reads its result from waitCh instead of
// calling cmd.Wait() itself. Calling cmd.Wait() concurrently from two
// goroutines on the same exec.Cmd is undefined behavior and trips the race
// detector. The shell-promotion path uses this to hand off its existing Wait
// goroutine. Callers that haven't yet started a Wait (e.g. tests) pass nil
// and AdoptProcess starts one internally.
func (m *BackgroundProcessManager) AdoptProcess(cmd *exec.Cmd, outputPath string, command string, dir string, waitCh <-chan error) (string, error) {
	// Generate session ID
	prefix := extractCommandPrefixCLI(command)
	sanitizedPrefix := sanitizeSessionIDPartCLI(prefix)
	randomHex, err := generateRandomHexCLI(4)
	if err != nil {
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}
	sessionID := fmt.Sprintf("bg-%s-%s", sanitizedPrefix, randomHex)

	proc := &BackgroundProcess{
		ID:         sessionID,
		Cmd:        cmd,
		Process:    cmd.Process,
		OutputPath: outputPath,
		Dir:        dir,
		Command:    command,
		StartedAt:  time.Now(),
		LastPolled: time.Now(),
		done:       make(chan struct{}),
	}

	// Resolve the wait channel: reuse the caller's if provided, else start
	// our own Wait goroutine.
	resolvedWait := waitCh
	if resolvedWait == nil {
		ch := make(chan error, 1)
		go func() { ch <- cmd.Wait() }()
		resolvedWait = ch
	}

	// Monitor process exit in a goroutine to reap the zombie
	go func() {
		waitErr := <-resolvedWait
		exitCode := extractExitCode(waitErr)
		proc.mu.Lock()
		proc.exitCode = exitCode
		proc.Cmd = nil
		proc.Process = nil
		proc.mu.Unlock()
		close(proc.done)
	}()

	m.mu.Lock()
	m.processes[sessionID] = proc
	m.mu.Unlock()

	return sessionID, nil
}

// CheckOutput reads accumulated output from a background session.
// Returns the raw output string, status ("running" or "exited"), and any error.
func (m *BackgroundProcessManager) CheckOutput(sessionID string) (string, string, error) {
	proc, exists := m.getProcess(sessionID)
	if !exists {
		return "", "", fmt.Errorf("session %s not found", sessionID)
	}

	// Update LastPolled
	proc.mu.Lock()
	proc.LastPolled = time.Now()
	proc.mu.Unlock()

	// Determine status
	proc.mu.Lock()
	isActive := proc.Process != nil
	proc.mu.Unlock()

	status := "running"
	if !isActive {
		status = "exited"
	}

	// Read accumulated output from the file
	output, err := os.ReadFile(proc.OutputPath)
	if err != nil {
		return "", status, fmt.Errorf("read output file: %w", err)
	}

	return string(output), status, nil
}

// Stop terminates a background session by sending SIGINT, waiting briefly,
// then SIGKILL if still running. Cleans up the process tracking.
func (m *BackgroundProcessManager) Stop(sessionID string) error {
	proc, exists := m.getProcess(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	proc.mu.Lock()
	process := proc.Process
	cmd := proc.Cmd
	proc.mu.Unlock()

	if process == nil {
		// Already exited
		return nil
	}

	// Send SIGINT to the process group (same as Ctrl+C). On Windows
	// this degrades to a per-process kill — see the helper's comment.
	_ = interruptProcessGroup(process)

	// Wait briefly for graceful shutdown
	time.Sleep(100 * time.Millisecond)

	// Check if still alive
	proc.mu.Lock()
	stillActive := proc.Process != nil
	proc.mu.Unlock()

	if stillActive {
		// Force kill the process group
		_ = killProcessGroup(process)
		if cmd != nil {
			_ = cmd.Wait() // reap
		}
		// Update process state so IsActive() returns false immediately.
		// Don't close proc.done — the monitor goroutine owns that.
		proc.mu.Lock()
		if proc.Process != nil {
			proc.exitCode = 1 // killed
			proc.Cmd = nil
			proc.Process = nil
		}
		proc.mu.Unlock()
	}

	return nil
}

// IsActive checks whether a session is still running.
func (m *BackgroundProcessManager) IsActive(sessionID string) bool {
	proc, exists := m.getProcess(sessionID)
	if !exists {
		return false
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	return proc.Process != nil
}

// SessionIDs returns all tracked session IDs.
func (m *BackgroundProcessManager) SessionIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.processes))
	for id := range m.processes {
		ids = append(ids, id)
	}
	return ids
}

// StopAll terminates all managed background processes.
func (m *BackgroundProcessManager) StopAll() {
	m.mu.RLock()
	sessionIDs := make([]string, 0, len(m.processes))
	for id := range m.processes {
		sessionIDs = append(sessionIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range sessionIDs {
		_ = m.Stop(id)
	}
}

// Close stops the cleanup goroutine and terminates all background processes.
func (m *BackgroundProcessManager) Close() {
	close(m.done)
	m.cleanupWg.Wait() // wait for cleanupLoop to actually exit
	m.StopAll()
}

// cleanupLoop runs every 60 seconds to reap exited and expired processes.
func (m *BackgroundProcessManager) cleanupLoop() {
	defer m.cleanupWg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

// cleanup removes exited processes (after 5 min idle) and kills expired ones.
func (m *BackgroundProcessManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	toDelete := make([]string, 0)

	for id, proc := range m.processes {
		proc.mu.Lock()
		isExited := proc.Process == nil
		lastUsed := proc.LastPolled
		if lastUsed.IsZero() {
			lastUsed = proc.StartedAt
		}
		proc.mu.Unlock()

		if isExited && now.Sub(lastUsed) > 5*time.Minute {
			// Exited process idle for > 5 minutes — delete
			_ = os.Remove(proc.OutputPath)
			toDelete = append(toDelete, id)
			continue
		}

		// Check for inactivity expiry (2 hours)
		if !isExited && now.Sub(lastUsed) > m.expiry {
			// Kill expired process
			p := proc.Process
			if p != nil {
				_ = killProcessGroup(p)
			}
			_ = os.Remove(proc.OutputPath)
			toDelete = append(toDelete, id)
			continue
		}
	}

	for _, id := range toDelete {
		delete(m.processes, id)
	}
}

func (m *BackgroundProcessManager) getProcess(sessionID string) (*BackgroundProcess, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	proc, exists := m.processes[sessionID]
	return proc, exists
}

// extractCommandPrefixCLI extracts the first word from a command for session ID generation.
func extractCommandPrefixCLI(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	for i, r := range command {
		if r == ' ' || r == '\t' || r == '\n' || r == '&' || r == '|' || r == ';' ||
			r == '>' || r == '<' || r == '(' || r == ')' || r == '\\' ||
			r == '"' || r == '\'' || r == '`' {
			return command[:i]
		}
	}
	return command
}

// sanitizeSessionIDPartCLI sanitizes a string for use in a session ID.
func sanitizeSessionIDPartCLI(part string) string {
	const maxLen = 32
	var b strings.Builder
	for i, r := range part {
		if i >= maxLen {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := b.String()
	if result == "" {
		return "unknown"
	}
	return result
}

// generateRandomHexCLI generates a random hex string.
func generateRandomHexCLI(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
