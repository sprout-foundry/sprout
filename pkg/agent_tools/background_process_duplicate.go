//go:build !js

package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RunningSessionMatching returns the still-running background session whose
// command and working directory match command in dir exactly (after trimming
// the command text). It exists to stop an agent from starting a second copy
// of a heavy command — a build, a test suite — while the first is still
// running: on a small machine two or three copies starve it, and even
// `echo hi` stops answering.
//
// Matching is deliberately literal: any difference in the command text or
// the directory means the command runs. A command that was promoted from
// the foreground and one started with background=true match each other,
// because both are sessions here.
func (m *BackgroundProcessManager) RunningSessionMatching(command, dir string) (string, time.Duration, bool) {
	want := strings.TrimSpace(command)
	if want == "" {
		return "", 0, false
	}
	wantDir := normalizeSessionDir(dir)

	m.mu.RLock()
	procs := make([]*BackgroundProcess, 0, len(m.processes))
	for _, proc := range m.processes {
		procs = append(procs, proc)
	}
	m.mu.RUnlock()

	now := time.Now()
	for _, proc := range procs {
		proc.mu.Lock()
		running := proc.Process != nil
		procCommand := proc.Command
		procDir := proc.Dir
		startedAt := proc.StartedAt
		id := proc.ID
		proc.mu.Unlock()
		if !running {
			continue
		}
		if strings.TrimSpace(procCommand) != want {
			continue
		}
		if normalizeSessionDir(procDir) != wantDir {
			continue
		}
		return id, now.Sub(startedAt), true
	}
	return "", 0, false
}

// normalizeSessionDir reduces a working directory to the form sessions store,
// so a caller passing "." matches a session recorded with the real path.
func normalizeSessionDir(dir string) string {
	if abs, err := filepath.Abs(strings.TrimSpace(dir)); err == nil {
		return abs
	}
	return strings.TrimSpace(dir)
}

// duplicateCommandMessage is what an agent sees instead of a second copy of
// a command that is already running. It names the session, says how long it
// has been running, and says plainly what the alternatives are — waiting,
// reading its output, or stopping it — without asking the agent to run the
// command again.
func duplicateCommandMessage(sessionID string, elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	return fmt.Sprintf(
		"Not started: the same command is already running in this directory in background session %s (started %s ago).\n\n"+
			"Do not run it again — a second copy competes with the first for the machine.\n"+
			"- Wait for it: shell_command check_background=\"%s\" wait_seconds=600 (repeat to keep waiting)\n"+
			"- Read its output so far: shell_command check_background=\"%s\"\n"+
			"- Stop it first, if you want to change approach: shell_command stop_background=\"%s\"",
		sessionID, formatSessionElapsed(elapsed), sessionID, sessionID, sessionID,
	)
}

// formatSessionElapsed renders a session age for an agent-facing message:
// a whole number of seconds with its unit, so "0 seconds ago" is a real age
// rather than a rounded-away "0s".
func formatSessionElapsed(d time.Duration) string {
	secs := int64(d.Round(time.Second) / time.Second)
	switch {
	case secs == 1:
		return "1 second"
	case secs < 60:
		return fmt.Sprintf("%d seconds", secs)
	default:
		mins := secs / 60
		rest := secs % 60
		if rest == 0 {
			if mins == 1 {
				return "1 minute"
			}
			return fmt.Sprintf("%d minutes", mins)
		}
		if mins == 1 {
			return fmt.Sprintf("1 minute %d seconds", rest)
		}
		return fmt.Sprintf("%d minutes %d seconds", mins, rest)
	}
}
