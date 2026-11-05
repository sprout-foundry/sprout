//go:build !js

package tools

import (
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
