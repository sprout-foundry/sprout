//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// removeLegacyServices disables, stops, and removes legacy systemd unit files.
func removeLegacyServices(paths []string) error {
	for _, path := range paths {
		// Extract the unit name from the filename (e.g., ledit-daemon.service)
		unitName := filepath.Base(path)

		// Disable the service (prevent auto-start)
		cmd := exec.Command("systemctl", "--user", "disable", unitName)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Non-zero exit is OK if the service wasn't enabled
			fmt.Printf("  Warning: systemctl disable %s: %s (may not have been enabled)\n", unitName, strings.TrimSpace(string(out)))
		}

		// Stop the service
		cmd = exec.Command("systemctl", "--user", "stop", unitName)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Non-zero exit is OK if the service wasn't running
			fmt.Printf("  Warning: systemctl stop %s: %s (may not have been running)\n", unitName, strings.TrimSpace(string(out)))
		}

		// Remove the unit file
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
		fmt.Printf("  Removed: %s\n", path)
	}

	// Reload systemd user manager to pick up the removed unit files
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reload systemd user daemon: %s", strings.TrimSpace(string(out)))
	}

	return nil
}
