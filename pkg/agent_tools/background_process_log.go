//go:build !js

package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
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

// StartOptions configures optional behavior when starting a background process.
type StartOptions struct {
	EventBus *events.EventBus // non-nil to enable output-chunk streaming for automate sessions
}

// GetOutputPath returns the output file path under the lock.
func (p *BackgroundProcess) GetOutputPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.OutputPath
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

// GetBaseDir returns the base directory used for output and PID files.
func (m *BackgroundProcessManager) GetBaseDir() string {
	return m.baseDir
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

// maxLiveOwnerSkipAge bounds how long orphan cleanup will keep sparing a
// .pid file whose owner process appears alive. Real sessions are reaped by
// their owner's BPM within the 2h inactivity expiry; anything older than
// this with a "live" owner is almost certainly a recycled PID pinning
// stale files, so cleanup reclaims it anyway.
const maxLiveOwnerSkipAge = 24 * time.Hour

// orphanCleanupItem holds parsed info from a .pid file for batch processing.
type orphanCleanupItem struct {
	pid        int
	ownerPID   int // 0 for legacy single-PID pidfiles
	pidFile    string
	outputFile string
}

// parsePIDFile parses a .pid file's contents. Current format is
// "<child-pid> <owner-pid>" (space-separated); legacy files contain only
// the child PID, in which case ownerPID is 0 and orphan cleanup treats
// the session as unowned (previous behavior).
func parsePIDFile(data []byte) (pid, ownerPID int, err error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, 0, fmt.Errorf("empty pid file")
	}
	pid, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	if len(fields) > 1 {
		ownerPID, _ = strconv.Atoi(fields[1])
	}
	return pid, ownerPID, nil
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
	var skippedLiveOwner int
	for _, pidFile := range pidFiles {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			log.Printf("warn: failed to read PID file %s: %v", pidFile, err)
			continue
		}

		pid, ownerPID, err := parsePIDFile(data)
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

		// Owner-aware liveness: a .pid file whose owner sprout process is
		// still running marks a LIVE session owned by that process, not
		// an orphan. Killing it here was the root cause of background
		// sessions dying with "exit code -1" while their output files
		// vanished — any second sprout sharing the config dir (a test
		// binary inheriting SPROUT_CONFIG, a CLI invocation) ran this
		// cleanup at agent creation and reaped the first process's
		// still-running sessions. Leave both files in place; the owner's
		// own BPM continues to track them.
		//
		// The skip is age-gated: after maxLiveOwnerSkipAge the session is
		// reaped regardless of owner liveness, so a recycled owner PID
		// can't pin stale files forever.
		if ownerPID > 0 && ownerProcessAlive(ownerPID) {
			if info, statErr := os.Stat(pidFile); statErr == nil && time.Since(info.ModTime()) < maxLiveOwnerSkipAge {
				skippedLiveOwner++
				continue
			}
		} // Derive the session ID from the .pid file name
		// e.g., "bg-sleep-abc123.pid" → "bg-sleep-abc123"
		base := filepath.Base(pidFile)
		sessionID := strings.TrimSuffix(base, ".pid")

		items = append(items, orphanCleanupItem{
			pid:        pid,
			ownerPID:   ownerPID,
			pidFile:    pidFile,
			outputFile: filepath.Join(baseDir, sessionID+".output"),
		})
	}

	if skippedLiveOwner > 0 {
		log.Printf("orphan cleanup: skipped %d session(s) with a live owner process", skippedLiveOwner)
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
