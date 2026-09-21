//go:build !js

package webui

import "testing"

// newTestTerminalManager returns a manager that cannot outlive the test:
// every session still open at test end is closed via t.Cleanup. Leaked PTY
// sessions each hold a shell subprocess; across a 175-file test package that
// multiplied into hundreds of stranded login shells and machine-freezing
// memory pressure (three OOM incidents, 2026-09). New tests must use this
// constructor, not bare NewTerminalManager.
func newTestTerminalManager(t *testing.T, workspaceRoot string) *TerminalManager {
	t.Helper()
	tm := NewTerminalManager(workspaceRoot)
	t.Cleanup(func() {
		_ = tm.CloseAllSessions()
	})
	return tm
}
