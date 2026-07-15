//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// removeLegacyServices stops, unloads, and removes legacy launchd plist files.
func removeLegacyServices(paths []string) error {
	userID := strconv.Itoa(os.Getuid())

	for _, path := range paths {
		// Extract the label from the filename (e.g., com.ledit.daemon.plist -> com.ledit.daemon)
		label := strings.TrimSuffix(filepath.Base(path), ".plist")

		// Bootout (stop + unload) the legacy service
		cmd := exec.Command("launchctl", "bootout", "gui/"+userID+"/"+label)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Non-zero exit is OK if the service wasn't loaded
			fmt.Printf("  Warning: launchctl bootout %s: %s (may not have been loaded)\n", label, strings.TrimSpace(string(out)))
		}

		// Remove the plist file
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
		fmt.Printf("  Removed: %s\n", path)
	}

	return nil
}
