//go:build linux

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func init() {
	newServiceManager = func() serviceManager { return &systemdManager{} }
}

type systemdManager struct{}

// systemdExecArg quotes a path for use in a systemd ExecStart line.
// Paths containing spaces or special characters must be quoted. systemd
// accepts double-quoted syntax which matches Go's strconv.Quote output.
func systemdExecArg(s string) string {
	return strconv.Quote(s)
}

// generateSystemdUnit produces a systemd user unit file for the ledit daemon.
func generateSystemdUnit(binaryPath, homeDir string) ([]byte, error) {
	if binaryPath == "" {
		return nil, fmt.Errorf("binary path must not be empty")
	}
	if homeDir == "" {
		return nil, fmt.Errorf("home directory must not be empty")
	}

	unit := fmt.Sprintf(`[Unit]
Description=ledit daemon - AI coding assistant web UI
After=default.target

[Service]
Type=simple
ExecStart=%s agent -d --no-connection-check
WorkingDirectory=%s
Restart=on-failure
RestartSec=5
Environment=LEDIT_SERVICE=1
Environment=HOME=%s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`, systemdExecArg(binaryPath), systemdExecArg(homeDir), systemdExecArg(homeDir))

	return []byte(unit), nil
}

func (m *systemdManager) Install() error {
	binaryPath, err := getBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get binary path: %w", err)
	}
	if filepath.Base(binaryPath) != "ledit" {
		return fmt.Errorf("unexpected binary name %q — service unit requires the ledit binary", filepath.Base(binaryPath))
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	unitDir := filepath.Join(homeDir, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	content, err := generateSystemdUnit(binaryPath, homeDir)
	if err != nil {
		return fmt.Errorf("failed to generate unit file: %w", err)
	}

	unitFile := filepath.Join(unitDir, "ledit.service")
	tmpFile := unitFile + ".tmp"
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		return fmt.Errorf("failed to write unit file: %w", err)
	}
	if err := os.Rename(tmpFile, unitFile); err != nil {
		return fmt.Errorf("failed to rename unit file: %w", err)
	}

	fmt.Printf("Installed systemd user unit to %s\n", unitFile)

	// daemon-reload may fail in containers/VMs without a running systemd user instance
	// The unit file is still written and can be started manually or on next login
	if _, err := runSystemctl("daemon-reload"); err != nil {
		fmt.Printf("Warning: daemon-reload failed (systemd user instance may not be running): %v\n", err)
		fmt.Printf("\nTo start the daemon without systemd, run:\n  %s\n", fallbackCommand(homeDir))
	}

	if _, err := runSystemctl("enable", "ledit.service"); err != nil {
		fmt.Printf("Warning: failed to enable service: %v\n", err)
	} else {
		fmt.Println("Service enabled.")
	}
	return nil
}

func (m *systemdManager) Uninstall() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Stop and disable — ignore errors since the service may not be running.
	runSystemctl("stop", "ledit.service")
	runSystemctl("disable", "ledit.service")

	unitFile := filepath.Join(homeDir, ".config", "systemd", "user", "ledit.service")
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file: %w", err)
	}

	if _, err := runSystemctl("daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload failed: %w", err)
	}

	fmt.Printf("Uninstalled systemd user unit from %s\n", unitFile)
	return nil
}

func (m *systemdManager) Start() error {
	// Try systemctl first; if it fails (e.g., systemd user instance not running in containers),
	// fall back to running the daemon directly in the background
	if _, err := runSystemctl("start", "ledit.service"); err != nil {
		// Check if it's a systemd availability issue (not a service config issue)
		if strings.Contains(err.Error(), "Failed to start") {
			return fmt.Errorf("failed to start service: %w", err)
		}
		// For other errors (like unit not found), try to start the daemon directly
		fmt.Printf("systemctl start failed (systemd may not be running): %v\n", err)
		fmt.Printf("To start the daemon manually in the background:\n  %s\n", fallbackCommand(""))
		return nil
	}
	fmt.Println("Service started.")
	return nil
}

func (m *systemdManager) Stop() error {
	if _, err := runSystemctl("stop", "ledit.service"); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	fmt.Println("Service stopped.")
	return nil
}

func (m *systemdManager) Status() (bool, error) {
	output, err := runSystemctl("show", "--property=SubState", "ledit.service")
	if err != nil {
		return false, nil // service not known — treat as stopped
	}
	// Output format: "SubState=running\n"
	return strings.HasPrefix(strings.TrimSpace(output), "SubState=running"), nil
}

// fallbackCommand returns the manual start command for non-systemd environments.
func fallbackCommand(homeDir string) string {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			homeDir = "$HOME"
		}
	}
	logPath := filepath.Join(homeDir, ".ledit", "daemon.log")
	return fmt.Sprintf("nohup ledit agent -d > %s 2>&1 &\nView logs with: tail -f %s", logPath, logPath)
}

// runSystemctl executes a systemctl command at user scope and returns its stdout.
func runSystemctl(args ...string) (string, error) {
	userArgs := append([]string{"--user"}, args...)
	cmd := exec.Command("systemctl", userArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("systemctl %s: %s", strings.Join(userArgs, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
