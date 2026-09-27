//go:build !js

package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils/pidalive"
)

// StartOptions configures optional behavior when starting a background process.
type StartOptions struct {
	EventBus *events.EventBus // non-nil to enable output-chunk streaming for automate sessions

	// TTL caps how long a running session may live before the cleanup pass
	// reaps it. Zero uses the manager default (2h). A TTL is a safety net,
	// not a liveness signal: a running process is never reaped for being
	// *unpolled* — only for exceeding its TTL.
	TTL time.Duration
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

// cleanup removes exited processes (after 5 min idle) and applies TTL expiry.
//
// Running sessions are NOT reaped for being unpolled: LastPolled measures
// the agent's attention, not the process's health, and watcher sessions
// (gh run watch, tail -f, wait-loops) are silent for hours by design. A
// running session survives until its TTL (default 2h) elapses regardless
// of polling; when the TTL fires, a pid probe decides — an actually-alive
// process is left running and its TTL window is extended (the TTL is a
// periodic re-confirmation, not a hard kill), while a dead-but-unreaped
// process is cleaned up immediately.
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
		ttl := proc.ttl
		if ttl <= 0 {
			ttl = m.expiry
		}
		proc.mu.Unlock()

		if isExited {
			if now.Sub(lastUsed) > 5*time.Minute {
				// Exited process idle for > 5 minutes — delete
				_ = os.Remove(proc.OutputPath)
				toDelete = append(toDelete, id)
			}
			continue
		}

		if now.Sub(lastUsed) < ttl {
			// TTL window not elapsed — nothing to do.
			continue
		}

		// TTL elapsed. Decide by actual liveness, not by polling.
		if pid := proc.GetPID(); pid > 0 && pidalive.IsAlive(pid) {
			// Alive: renew the window so a long-running quiet watcher
			// survives indefinitely while its process lives. Renewal
			// happens on every cleanup pass after TTL elapses, so the
			// effective behavior is "reap when the process dies".
			proc.mu.Lock()
			proc.LastPolled = now
			proc.mu.Unlock()
			continue
		}

		// Dead (or unreapable pid): clean up. Nil out process fields BEFORE
		// killing so the monitor goroutine's exit handler becomes a no-op on
		// state updates.
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
	}

	for _, id := range toDelete {
		delete(m.processes, id)
	}
}

// KeepAlive renews a session's activity timer so the cleanup pass treats
// it as recently used. Used by `sprout shell-bg keepalive` and by agents
// that know a session is a long-lived watcher. No-op error when the
// session is unknown.
func (m *BackgroundProcessManager) KeepAlive(sessionID string) error {
	proc, exists := m.GetProcess(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}
	proc.mu.Lock()
	proc.LastPolled = time.Now()
	proc.mu.Unlock()
	return nil
}
