//go:build !js

package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// BackgroundProcess represents a tracked background process for CLI mode.
// Unlike WebUI background sessions (PTY-based), these use os/exec with
// output piped to a temp file for polling via check_background.
type BackgroundProcess struct {
	ID         string    // "bg-<sanitized-prefix>-<random-hex>"
	Cmd        *exec.Cmd // the running process (nil after exit)
	Process    *os.Process
	OutputPath string // path to accumulated output temp file
	Dir        string // working directory
	Command    string // original command string
	Kind       string // "shell" (default), "automate", etc.
	StartedAt  time.Time
	LastPolled time.Time
	done       chan struct{} // closed when process exits
	exitCode   int
	mu         sync.Mutex
	publisher  *OutputChunkPublisher // non-nil for automate sessions with an event bus
}

// GetPID returns the process PID under the lock. Returns 0 if the process
// is nil (not yet started or already exited).
func (p *BackgroundProcess) GetPID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Process != nil {
		return p.Process.Pid
	}
	return 0
}

// GetOutputPath returns the output file path under the lock.
func (p *BackgroundProcess) GetOutputPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.OutputPath
}

// GetExitCode returns the exit code of the background process.
// Returns -1 if the process has not yet exited.
func (p *BackgroundProcess) GetExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

// Done returns a channel that closes when the background process exits.
// Callers can select on this channel to wait for process completion.
// If the process has already exited, the returned channel is already closed.
func (p *BackgroundProcess) Done() <-chan struct{} {
	return p.done
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
	baseDir, err := envutil.GetConfigDir()
	if err != nil {
		baseDir = filepath.Join(os.TempDir(), "sprout-bg")
	} else {
		baseDir = filepath.Join(baseDir, "bg-processes")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		log.Printf("warn: failed to create background output directory %s: %v", baseDir, err)
	}

	m := &BackgroundProcessManager{
		processes:   make(map[string]*BackgroundProcess),
		expiry:      2 * time.Hour,
		maxSessions: 5, // per-chat cap matching WebUI's maxBackgroundSessionsPerChat
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
	return m.StartWithKind(ctx, command, dir, "shell")
}

// StartWithKind works like Start but allows specifying the process kind
// (e.g., "automate" vs "shell").
func (m *BackgroundProcessManager) StartWithKind(ctx context.Context, command string, dir string, kind string) (string, error) {
	return m.StartWithOptions(ctx, command, dir, kind, nil)
}

// StartOptions configures optional behavior when starting a background process.
type StartOptions struct {
	EventBus *events.EventBus // non-nil to enable output-chunk streaming for automate sessions
}

// StartWithOptions works like StartWithKind but also accepts options that
// control output streaming. When kind == "automate" and opts.EventBus is
// non-nil, output is teed through an OutputChunkPublisher that emits
// automate.output_chunk events on a coalesced basis (≥250ms or ≥4KB).
func (m *BackgroundProcessManager) StartWithOptions(ctx context.Context, command string, dir string, kind string, opts *StartOptions) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

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

	// Detach from parent session so the process survives parent agent exit.
	// This is the background spawn path (automate runners, background shells);
	// interactive shells and password prompts use setProcessGroup directly
	// via shell_native.go to preserve TTY access.
	detachFromSession(cmd)

	// Close stdin so the process doesn't inherit the parent's terminal pipe.
	// Without this, the process can receive EOF or SIGPIPE when the parent
	// exits, causing premature termination. nil means /dev/null on Unix.
	cmd.Stdin = nil

	// Atomic cap check, session ID generation, output file creation,
	// process start, and map insertion — all under a single lock to
	// prevent TOCTOU races under concurrent Start() calls.
	m.mu.Lock()
	if len(m.processes) >= m.maxSessions {
		m.mu.Unlock()
		return "", fmt.Errorf("background session limit reached (%d active)", m.maxSessions)
	}

	// Generate session ID inside the lock to prevent collisions
	prefix := extractCommandPrefixCLI(command)
	sanitizedPrefix := sanitizeSessionIDPartCLI(prefix)
	randomHex, err := generateRandomHexCLI(4)
	if err != nil {
		m.mu.Unlock()
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}
	sessionID := fmt.Sprintf("bg-%s-%s", sanitizedPrefix, randomHex)

	// Create output file in the base directory with owner-only permissions
	outputPath := filepath.Join(m.baseDir, sessionID+".output")
	outputFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		m.mu.Unlock()
		return "", fmt.Errorf("create output file: %w", err)
	}

	// Build the writer chain: always include the file.
	// For automate sessions with an event bus, tee through the chunk publisher.
	var writers []io.Writer
	writers = append(writers, outputFile)

	var publisher *OutputChunkPublisher
	if kind == "automate" && opts != nil && opts.EventBus != nil {
		publisher = NewOutputChunkPublisher(sessionID, opts.EventBus)
		writers = append(writers, publisher)
	}

	writer := io.MultiWriter(writers...)
	cmd.Stdout = writer
	cmd.Stderr = writer

	// Start the process
	if err := cmd.Start(); err != nil {
		outputFile.Close()
		os.Remove(outputPath)
		m.mu.Unlock()
		return "", fmt.Errorf("start command: %w", err)
	}

	// Write the PID file alongside the output file for orphan cleanup
	pidPath := filepath.Join(m.baseDir, sessionID+".pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0600); err != nil {
		log.Printf("warn: failed to write PID file %s: %v", pidPath, err)
	}

	proc := &BackgroundProcess{
		ID:         sessionID,
		Cmd:        cmd,
		Process:    cmd.Process,
		OutputPath: outputPath,
		Dir:        cmd.Dir,
		Command:    command,
		Kind:       kind,
		StartedAt:  time.Now(),
		LastPolled: time.Now(),
		exitCode:   -1,
		done:       make(chan struct{}),
		publisher:  publisher,
	}

	m.processes[sessionID] = proc
	m.mu.Unlock()

	// Monitor process exit in a goroutine (started after releasing lock)
	go func() {
		waitErr := cmd.Wait() // reap the zombie
		exitCode := extractExitCode(waitErr)
		proc.mu.Lock()
		proc.exitCode = exitCode
		proc.Cmd = nil
		proc.Process = nil
		proc.mu.Unlock()
		close(proc.done)
		// Flush any remaining output chunks before closing the file
		if proc.publisher != nil {
			proc.publisher.Flush()
		}
		// Close the output file handle after process exits
		outputFile.Close()
	}()

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

	// Write the PID file for orphan cleanup support
	pidPath := filepath.Join(m.baseDir, sessionID+".pid")
	if cmd.Process != nil {
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0600); err != nil {
			log.Printf("warn: failed to write PID file %s: %v", pidPath, err)
		}
	}

	proc := &BackgroundProcess{
		ID:         sessionID,
		Cmd:        cmd,
		Process:    cmd.Process,
		OutputPath: outputPath,
		Dir:        dir,
		Command:    command,
		Kind:       "shell",
		StartedAt:  time.Now(),
		LastPolled: time.Now(),
		exitCode:   -1,
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
	proc, exists := m.GetProcess(sessionID)
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

// Stop terminates a background session using a graduated signal sequence:
// SIGINT → wait for grace period → SIGTERM → wait 5s → SIGKILL if still alive.
func (m *BackgroundProcessManager) Stop(sessionID string, grace time.Duration) error {
	proc, exists := m.GetProcess(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	proc.mu.Lock()
	process := proc.Process
	proc.mu.Unlock()

	if process == nil {
		// Already exited
		return nil
	}

	// Send SIGINT to the process group (same as Ctrl+C). On Windows
	// this degrades to a per-process kill — see the helper's comment.
	_ = interruptProcessGroup(process)

	// Wait for grace period
	time.Sleep(grace)

	// Check if still alive
	proc.mu.Lock()
	stillActive := proc.Process != nil
	proc.mu.Unlock()

	if stillActive {
		// Send SIGTERM to the process group
		_ = terminateProcessGroup(process)

		// Wait for SIGTERM grace
		time.Sleep(5 * time.Second)

		// Check if still alive
		proc.mu.Lock()
		stillActive := proc.Process != nil
		proc.mu.Unlock()

		if stillActive {
			// Force kill the process group.
			_ = killProcessGroup(process)
			// Don't call cmd.Wait() here — the monitor goroutine owns that.
			// The monitor goroutine will reap and update state.
			proc.mu.Lock()
			if proc.Process != nil {
				proc.exitCode = 1 // killed
				proc.Cmd = nil
				proc.Process = nil
			}
			proc.mu.Unlock()
		}
	}

	return nil
}

// IsActive checks whether a session is still running.
func (m *BackgroundProcessManager) IsActive(sessionID string) bool {
	proc, exists := m.GetProcess(sessionID)
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
		_ = m.Stop(id, 10*time.Second)
	}
}

// Close stops the cleanup goroutine and terminates all background processes.
func (m *BackgroundProcessManager) Close() {
	close(m.done)
	m.cleanupWg.Wait() // wait for cleanupLoop to actually exit
	m.StopAll()
}

// GetBaseDir returns the base directory used for output and PID files.
func (m *BackgroundProcessManager) GetBaseDir() string {
	return m.baseDir
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
			// Nil out process fields BEFORE killing so the monitor goroutine's
			// exit handler becomes a no-op on state updates.
			proc.mu.Lock()
			p := proc.Process
			proc.Process = nil
			proc.Cmd = nil
			proc.mu.Unlock()
			if p != nil {
				_ = killProcessGroup(p)
			}
			// Don't call cmd.Wait() — the monitor goroutine may still be
			// waiting. It will see nil fields and skip its state changes.
			_ = os.Remove(proc.OutputPath)
			toDelete = append(toDelete, id)
			continue
		}
	}

	for _, id := range toDelete {
		delete(m.processes, id)
	}
}

// GetProcess returns a BackgroundProcess by its session ID.
// Returns the process and true if found, or nil and false otherwise.
//
// The returned pointer must not be accessed without first acquiring
// proc.mu.Lock() or proc.mu.RLock(). The BackgroundProcessManager does not
// keep the process in the map permanently — cleanup may remove entries at
// any time. Acquire proc.mu immediately after calling GetProcess.
func (m *BackgroundProcessManager) GetProcess(sessionID string) (*BackgroundProcess, bool) {
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

// GetBackgroundOutputBaseDir returns the standard default baseDir path used
// by BackgroundProcessManager for output and PID files. Callers outside the
// tools package (e.g., agent startup code) can use this to locate the
// directory for orphan cleanup without knowing BPM internals.
func GetBackgroundOutputBaseDir() string {
	configDir, err := envutil.GetConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sprout-bg")
	}
	return filepath.Join(configDir, "bg-processes")
}

// orphanCleanupItem holds parsed info from a .pid file for batch processing.
type orphanCleanupItem struct {
	pid        int
	pidFile    string
	outputFile string
}

// CleanupOrphanedBackgroundProcesses scans the baseDir for .pid files left
// behind by background processes whose sprout parent exited uncleanly.
// For each orphaned PID, it attempts to terminate the process (SIGTERM →
// SIGKILL) and removes both the .pid and .output files.
//
// Returns an error only if the baseDir itself can't be read. Individual file
// errors are logged but don't cause the function to return an error.
func CleanupOrphanedBackgroundProcesses(baseDir string) error {
	return CleanupOrphanedBackgroundProcessesWithContext(context.Background(), baseDir)
}

// CleanupOrphanedBackgroundProcessesWithContext works like
// CleanupOrphanedBackgroundProcesses but accepts a context for cancellation
// and timeout control. PIDs are processed concurrently with a worker pool of
// 16 goroutines. A 5-second deadline is applied to the entire operation.
func CleanupOrphanedBackgroundProcessesWithContext(ctx context.Context, baseDir string) error {
	// Ensure the baseDir exists (it may not have been created yet)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		log.Printf("warn: failed to create background output directory %s: %v", baseDir, err)
		return fmt.Errorf("create background output directory: %w", err)
	}

	// Apply a 5-second timeout to the entire cleanup operation
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Scan for .pid files. These are the authoritative orphans — they mark
	// processes whose sprout parent exited before the process did. The
	// matching .output file is removed alongside.
	pidPattern := filepath.Join(baseDir, "*.pid")
	pidFiles, _ := filepath.Glob(pidPattern)
	if pidFiles == nil {
		pidFiles = []string{}
	}

	// Also scan for orphaned .output files (no matching .pid) — these are
	// stale leftovers from sessions whose .pid was cleaned up by a previous
	// run or whose process was never tracked by the BPM. Without this
	// pass they accumulate in the bg-processes directory forever.
	outputPattern := filepath.Join(baseDir, "*.output")
	outputFiles, _ := filepath.Glob(outputPattern)
	if outputFiles == nil {
		outputFiles = []string{}
	}

	pidSet := make(map[string]struct{}, len(pidFiles))
	for _, f := range pidFiles {
		pidSet[filepath.Base(f)] = struct{}{}
	}
	var strayOutputs []string
	for _, f := range outputFiles {
		sid := strings.TrimSuffix(filepath.Base(f), ".output")
		if _, paired := pidSet[sid+".pid"]; paired {
			continue
		}
		strayOutputs = append(strayOutputs, f)
	}

	if len(pidFiles) == 0 && len(strayOutputs) == 0 {
		return nil
	}

	// Pre-parse all .pid files into work items (fast I/O, no sleeps)
	var items []orphanCleanupItem
	for _, pidFile := range pidFiles {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			log.Printf("warn: failed to read PID file %s: %v", pidFile, err)
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			log.Printf("warn: failed to parse PID from %s: %v", pidFile, err)
			// Stale/unparseable file — remove both the .pid and its
			// (likely-stale) .output companion. Without removing the
			// output, it sits in the bg-processes directory forever.
			_ = os.Remove(pidFile)
			sid := strings.TrimSuffix(filepath.Base(pidFile), ".pid")
			_ = os.Remove(filepath.Join(baseDir, sid+".output"))
			continue
		}

		// Derive the session ID from the .pid file name
		// e.g., "bg-sleep-abc123.pid" → "bg-sleep-abc123"
		base := filepath.Base(pidFile)
		sessionID := strings.TrimSuffix(base, ".pid")

		items = append(items, orphanCleanupItem{
			pid:        pid,
			pidFile:    pidFile,
			outputFile: filepath.Join(baseDir, sessionID+".output"),
		})
	}

	if len(items) == 0 && len(strayOutputs) == 0 {
		return nil
	}

	// Process PIDs concurrently with a worker pool (only if we have PIDs)
	if len(items) > 0 {
		const workers = 16
		itemCh := make(chan orphanCleanupItem, len(items))
		for _, item := range items {
			itemCh <- item
		}
		close(itemCh)

		var wg sync.WaitGroup
		var processedCount atomic.Int64

		for i := 0; i < workers && i < len(items); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for item := range itemCh {
					// Check context before starting work on this item
					select {
					case <-ctx.Done():
						return
					default:
					}

					// Terminate the orphan (fast for dead processes, ~200ms for alive)
					terminateOrphanedPIDWithTimeout(item.pid, 200*time.Millisecond)

					// Clean up both files (whether the process was alive or not).
					// Missing files are expected during concurrent cleanup or when
					// a previous run already removed them — not an error.
					if err := os.Remove(item.pidFile); err != nil && !os.IsNotExist(err) {
						log.Printf("warn: failed to remove PID file %s: %v", item.pidFile, err)
					}
					if err := os.Remove(item.outputFile); err != nil && !os.IsNotExist(err) {
						log.Printf("warn: failed to remove output file %s: %v", item.outputFile, err)
					}
					processedCount.Add(1)
				}
			}()
		}

		wg.Wait()

		// If context was cancelled mid-batch, log a summary
		if err := ctx.Err(); err != nil && err != context.DeadlineExceeded {
			done := processedCount.Load()
			log.Printf("warn: orphan cleanup cancelled after processing %d of %d files: %v", done, len(items), err)
		} else if err == context.DeadlineExceeded {
			done := processedCount.Load()
			log.Printf("warn: orphan cleanup timed out after processing %d of %d files", done, len(items))
		}
	}

	// Remove stray .output files (no matching .pid). These accumulate when
	// the .pid was cleaned up but the .output file lingered, or when a
	// process was started outside the BPM. Done outside the worker pool
	// because there's no I/O wait — just file removes.
	for _, stray := range strayOutputs {
		if err := os.Remove(stray); err != nil && !os.IsNotExist(err) {
			log.Printf("warn: failed to remove stray output file %s: %v", stray, err)
		}
	}

	return nil
}

// terminateOrphanedPID probes a PID and terminates it if it's still alive.
// On Unix, uses Signal(0) to probe and SIGTERM/SIGKILL to terminate.
// On Windows, os.FindProcess always succeeds, so we attempt Kill() directly
// and ignore "process already dead" errors.
//
// Deprecated: Use terminateOrphanedPIDWithTimeout instead. Kept for
// backward compatibility with existing callers.
func terminateOrphanedPID(pid int) {
	terminateOrphanedPIDWithTimeout(pid, 200*time.Millisecond)
}

// terminateOrphanedPIDWithTimeout probes a PID and terminates it if it's
// still alive. Takes a configurable grace period between SIGTERM and SIGKILL.
//
// On Unix, uses Signal(0) to probe and SIGTERM/grace/SIGKILL to terminate.
// On Windows, os.FindProcess always succeeds, so we attempt Kill() directly
// and ignore "process already dead" errors.
//
// Dead processes return immediately (no sleep). Only alive processes wait
// for the grace period between SIGTERM and SIGKILL.
func terminateOrphanedPIDWithTimeout(pid int, gracePeriod time.Duration) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	if runtime.GOOS == "windows" {
		// On Windows, FindProcess always succeeds. Just attempt Kill
		// and ignore errors (process may already be dead).
		_ = proc.Kill()
		return
	}

	// Unix: probe with Signal(0) to check if process exists
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		// Process is gone (ESRCH) or we can't signal it.
		// EPERM means process exists but different UID — treat as alive.
		if errno, ok := err.(syscall.Errno); ok && errno != syscall.EPERM {
			return // Process is gone
		}
		if se, ok := err.(*os.SyscallError); ok {
			if _, ok := se.Err.(syscall.Errno); ok && se.Err != syscall.EPERM {
				return // Process is gone
			}
		}
		// If we get here, it's EPERM — process exists, try to terminate
		_ = proc.Signal(syscall.SIGTERM)
		time.Sleep(gracePeriod)
		_ = proc.Signal(syscall.SIGKILL)
		return
	}

	// Process is alive — terminate it
	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(gracePeriod)
	_ = proc.Signal(syscall.SIGKILL)
}
