//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// Detach-mode helpers for `sprout automate run --detach`. Paired with
// automate_run_detach_test.go.

// openDetachLogFile creates the session log directory under sproutDir and
// opens the per-session log file the detached workflow child will write to.
// Returned path is recorded in the session PID file (OutputFilePath) so
// `sprout automate logs` can find it.
func openDetachLogFile(sproutDir, sessionID string) (*os.File, string, error) {
	// 0o700 dir / 0o600 file match the session-dir convention
	// (WriteSessionFile/GetAutomateSessionDir): workflow output can
	// contain source and secrets, so no group/world access.
	logDir := filepath.Join(sproutDir, "automate", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create automate log directory: %w", err)
	}
	logPath := filepath.Join(logDir, sessionID+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("open detach log file: %w", err)
	}
	return f, logPath, nil
}
