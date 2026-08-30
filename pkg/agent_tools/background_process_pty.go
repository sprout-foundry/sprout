//go:build !js

package tools

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
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

	// jobHandle is the Windows Job Object handle that owns this process.
	// Set by StartWithOptions on Windows; closing it kills all descendants.
	// Always 0 on non-Windows platforms — guarded by runtime.GOOS checks
	// at the call sites. Using uintptr to avoid importing golang.org/x/sys/windows
	// in this cross-platform file (the handle is only used on Windows).
	jobHandle uintptr
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

// bpmContextKey is the context key for BackgroundProcessManager.
type bpmContextKey struct{}

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

	// SP-112-1: assign the process to a Job Object so descendants are
	// cleaned up when the Job handle is closed. The handle is registered
	// in jobRegistry by AttachProcessToJob.
	jobHandle := attachProcessToJobAndGetHandle(cmd.Process.Pid)
	if jobHandle == 0 && runtime.GOOS == "windows" {
		log.Printf("warn: failed to assign background PID %d to Job Object (descendants may leak)", cmd.Process.Pid)
	}

	// Write the PID file alongside the output file for orphan cleanup.
	// Format: "<child-pid> <owner-pid>" — the owner is this sprout
	// process; orphan cleanup skips sessions whose owner is still alive
	// so a second sprout (test binary, CLI invocation sharing the config
	// dir) can't kill live sessions it doesn't own.
	pidPath := filepath.Join(m.baseDir, sessionID+".pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d %d\n", cmd.Process.Pid, os.Getpid())), 0600); err != nil {
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
		jobHandle:  jobHandle,
	}

	m.processes[sessionID] = proc
	m.mu.Unlock()

	// Monitor process exit in a goroutine (started after releasing lock)
	go func() {
		waitErr := cmd.Wait() // reap the zombie
		exitCode := extractExitCode(waitErr)

		// SP-112-1: close the Job Object handle (kills any remaining
		// descendants in case the process exited without cleaning up its
		// children). We do this before nil-ing out proc.Process so we
		// can still access the jobHandle field.
		if proc.jobHandle != 0 {
			closeJobHandleOnProcessExit(proc.jobHandle, cmd.Process.Pid)
			proc.jobHandle = 0
		}

		// Flush any remaining output chunks and close the output file
		// BEFORE signalling done: completion watchers read the file for
		// their notification tail, so the file must be fully written
		// (and, on Windows, closed) by the time done closes.
		if proc.publisher != nil {
			proc.publisher.Flush()
		}
		outputFile.Close()

		proc.mu.Lock()
		proc.exitCode = exitCode
		proc.Cmd = nil
		proc.Process = nil
		proc.mu.Unlock()
		close(proc.done)
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

	// Write the PID file for orphan cleanup support. Same owner-aware
	// format as StartWithOptions: "<child-pid> <owner-pid>".
	pidPath := filepath.Join(m.baseDir, sessionID+".pid")
	if cmd.Process != nil {
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d %d\n", cmd.Process.Pid, os.Getpid())), 0600); err != nil {
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
