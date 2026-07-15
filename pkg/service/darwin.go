//go:build darwin

package service

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
)

const (
	launchdLabel    = "com.sprout.daemon"
	launchdPlistDir = "Library/LaunchAgents"
)

// launchdManager implements serviceManager for macOS launchd.
type launchdManager struct{}

func init() {
	newServiceManager = func() serviceManager { return &launchdManager{} }
}

// ── plist generation ────────────────────────────────────────────────

// generateLaunchdPlist generates a launchd plist with environment variables from service.env.
// The plist is built as a string rather than via Go's xml.Marshal to guarantee correct
// structure — in particular to avoid the double-nested <dict> that xml.Marshal produces
// when a struct has both XMLName "dict" and a child field also tagged xml:"dict".
func generateLaunchdPlist(binaryPath, homeDir string) ([]byte, error) {
	// Load API keys and other environment variables from service.env.
	envVars, err := LoadServiceEnvFile(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load service.env: %w", err)
	}

	// Build the EnvironmentVariables dict entries.
	var envBuf strings.Builder
	addEnvEntry := func(key, value string) {
		fmt.Fprintf(&envBuf, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n",
			xmlEscapeStr(key), xmlEscapeStr(value))
	}
	addEnvEntry("SPROUT_SERVICE", "1")
	addEnvEntry("HOME", homeDir)
	// Authoritative daemon root, baked in at install time when $HOME is
	// reliable. The runtime reads this first so the workspace browser is
	// scoped to the user's home even if launchd doesn't propagate $HOME.
	addEnvEntry("SPROUT_DAEMON_ROOT", homeDir)
	// Include the user's PATH so the daemon can locate developer tools.
	if path := os.Getenv("PATH"); path != "" {
		addEnvEntry("PATH", path)
	}
	for key, value := range envVars {
		addEnvEntry(key, value)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>agent</string>
		<string>-d</string>
		<string>--no-connection-check</string>
	</array>
	<key>WorkingDirectory</key>
	<string>%s</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
		<!-- ExponentialBackoff requires macOS 12+ (Monterey) -->
		<key>ExponentialBackoff</key>
		<true/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>EnvironmentVariables</key>
	<dict>
%s	</dict>
</dict>
</plist>`,
		xmlEscapeStr(launchdLabel),
		xmlEscapeStr(binaryPath),
		xmlEscapeStr(homeDir),
		envBuf.String(),
	)

	return []byte(plist), nil
}

// xmlEscapeStr returns s with XML special characters (&, <, >, ", ') escaped.
func xmlEscapeStr(s string) string {
	var buf strings.Builder
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// ── helpers ──────────────────────────────────────────────────────────

func launchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, launchdPlistDir, launchdLabel+".plist"), nil
}

func runLaunchctl(args ...string) (string, error) {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return trimmed, fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), trimmed)
	}
	return trimmed, nil
}

// ── serviceManager implementation ────────────────────────────────────

func (m *launchdManager) Install() error {
	binaryPath, err := getBinaryPath()
	if err != nil {
		return err
	}
	if filepath.Base(binaryPath) != "sprout" {
		return fmt.Errorf("unexpected binary name %q — service requires the sprout binary", filepath.Base(binaryPath))
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	// Capture API keys from the current environment and write to service.env
	// This is done before generating the plist so the environment variables can be inlined
	if err := GenerateServiceEnvFile(homeDir); err != nil {
		fmt.Printf("Warning: failed to generate service.env: %v\n", err)
		fmt.Println("The service will be installed but may not have access to API keys.")
	}

	agentsDir := filepath.Join(homeDir, launchdPlistDir)
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}
	logDir := filepath.Join(homeDir, ".sprout/logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	data, err := generateLaunchdPlist(binaryPath, homeDir)
	if err != nil {
		return err
	}

	pPath, err := plistPath()
	if err != nil {
		return fmt.Errorf("failed to determine plist path: %w", err)
	}
	tmpPath := pPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}
	if err := os.Rename(tmpPath, pPath); err != nil {
		return fmt.Errorf("failed to rename plist: %w", err)
	}

	domain := launchdDomain()
	servicePath := domain + "/" + launchdLabel

	// Check if service is already loaded
	loaded := false
	if _, err := runLaunchctl("print", servicePath); err == nil {
		loaded = true
		fmt.Printf("Service already loaded. Unloading old version...\n")
		if _, err := runLaunchctl("bootout", servicePath); err != nil {
			// If bootout fails, try to continue anyway - it might already be gone
			if !isESRCH(err) {
				fmt.Printf("Warning: failed to unload service: %v\n", err)
			}
		}
	}

	// Bootstrap the service
	if _, err := runLaunchctl("bootstrap", domain, pPath); err != nil {
		// Provide more helpful error messages
		errMsg := err.Error()
		if strings.Contains(errMsg, "Bootstrap failed: 5") || strings.Contains(errMsg, "Input/output error") {
			return fmt.Errorf("launchctl bootstrap failed: %w\n\nThis error typically means:\n"+
				"  1. The service is already loaded (try: sprout service uninstall first)\n"+
				"  2. The launchd database needs rebuilding (try: launchctl reboot 2>/dev/null || sudo killall launchd)\n"+
				"  3. There's a permission issue with the plist file\n\n"+
				"Try running: sprout service uninstall && sprout service install", err)
		}
		return fmt.Errorf("launchctl bootstrap failed: %w", err)
	}

	// If we just loaded a new service, start it
	if !loaded {
		fmt.Println("Service loaded successfully.")
	} else {
		fmt.Println("Service reloaded successfully.")
	}

	fmt.Printf("Installed launchd agent: %s\n", pPath)
	return nil
}

func (m *launchdManager) Uninstall() error {
	// Check for active sessions before uninstalling
	count, err := checkActiveSessions()
	if err != nil {
		// shouldn't happen, but log and continue
		fmt.Printf("Warning: failed to check active sessions: %v\n", err)
	}
	if count > 0 {
		fmt.Printf("Warning: %d active agent session(s) detected. Uninstalling will stop the daemon and terminate these sessions.\n", count)
		if !ForceConfirm {
			fmt.Printf("Continue? %s ", console.FormatYesNoPromptStdout(false))
			reader := bufio.NewReader(os.Stdin)
			resp, _ := reader.ReadString('\n')
			resp = strings.TrimSpace(strings.ToLower(resp))
			if resp != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		} else {
			fmt.Println("Skipping confirmation (--yes flag set).")
		}
	}

	domain := launchdDomain()
	servicePath := domain + "/" + launchdLabel

	_, _ = runLaunchctl("bootout", servicePath)

	pPath, err := plistPath()
	if err != nil {
		return fmt.Errorf("failed to determine plist path: %w", err)
	}
	if err := os.Remove(pPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	// Remove the service.env file if it exists
	homeDir, err := os.UserHomeDir()
	if err == nil {
		envFile := ServiceEnvPath(homeDir)
		if err := os.Remove(envFile); err != nil && !os.IsNotExist(err) {
			// Don't fail the whole uninstall if we can't remove service.env
			fmt.Printf("Warning: failed to remove %s: %v\n", envFile, err)
		}
	}

	fmt.Printf("Uninstalled launchd agent: %s\n", pPath)
	return nil
}

func (m *launchdManager) Start() error {
	servicePath := launchdDomain() + "/" + launchdLabel

	// Check if the service is currently loaded in launchd.
	printOutput, err := runLaunchctl("print", servicePath)
	if err != nil {
		// Service not loaded — bootstrap it from the plist on disk.
		pPath, pathErr := plistPath()
		if pathErr != nil {
			return fmt.Errorf("service not loaded and cannot find plist: %w", pathErr)
		}
		fmt.Println("Service not loaded. Loading now...")
		if _, bsErr := runLaunchctl("bootstrap", launchdDomain(), pPath); bsErr != nil {
			return fmt.Errorf("failed to load service: %w", bsErr)
		}
		fmt.Println("Daemon started successfully.")
		return nil
	}

	// Service is loaded — check if it's running
	if strings.Contains(printOutput, "state = running") {
		fmt.Println("Daemon is already running.")
		return nil
	}

	// Service is loaded but not running — kickstart it.
	fmt.Println("Starting loaded daemon...")
	if _, err := runLaunchctl("kickstart", "-k", servicePath); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	fmt.Println("Daemon started successfully.")
	return nil
}

func (m *launchdManager) Stop() error {
	servicePath := launchdDomain() + "/" + launchdLabel
	_, err := runLaunchctl("bootout", servicePath)
	if err != nil {
		// ESRCH (exit code 3) means the service isn't loaded — already stopped.
		if isESRCH(err) {
			fmt.Println("Daemon not running.")
			return nil
		}
		return fmt.Errorf("launchctl bootout failed: %w", err)
	}
	fmt.Println("Daemon stopped successfully.")
	return nil
}

// isESRCH returns true if the error is from a process that doesn't exist
// (ESRCH = exit code 3, used by launchctl when a service isn't loaded).
func isESRCH(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		return true
	}
	// String-based fallback for robustness across macOS versions.
	return strings.Contains(err.Error(), "No such process") ||
		strings.Contains(err.Error(), "Could not find service")
}

func (m *launchdManager) Status() (bool, error) {
	servicePath := launchdDomain() + "/" + launchdLabel
	out, err := runLaunchctl("print", servicePath)
	if err != nil {
		// Service not loaded
		return false, nil
	}
	// Check if it's actually running vs just loaded
	return strings.Contains(out, "state = running"), nil
}

// Diagnose provides detailed diagnostic information about the service state.
func (m *launchdManager) Diagnose() error {
	domain := launchdDomain()
	servicePath := domain + "/" + launchdLabel
	pPath, err := plistPath()
	if err != nil {
		return fmt.Errorf("failed to determine plist path: %w", err)
	}

	fmt.Println("=== sprout Service Diagnostics ===")
	fmt.Println()

	// Check plist file
	fmt.Println("📋 Checking plist file:")
	plistContent, plistReadErr := os.ReadFile(pPath)
	if plistReadErr != nil {
		if os.IsNotExist(plistReadErr) {
			fmt.Printf("  ❌ plist file not found: %s\n", pPath)
		} else {
			fmt.Printf("  ❌ Error accessing plist: %v\n", plistReadErr)
		}
	} else {
		fmt.Printf("  ✅ plist file exists: %s\n", pPath)
		fmt.Printf("     Size: %d bytes\n", len(plistContent))
		// Flag a stale plist that predates the SPROUT_DAEMON_ROOT fix —
		// without it, the daemon falls back to $HOME (which launchd may have
		// set wrong) and the workspace browser starts in the wrong directory.
		if !strings.Contains(string(plistContent), "SPROUT_DAEMON_ROOT") {
			console.GlyphWarning.Fprintln(os.Stdout, "  STALE plist: missing SPROUT_DAEMON_ROOT — workspace browser may start in the wrong directory.")
			fmt.Println("      Reinstall to regenerate: sprout service uninstall && sprout service install")
		}
	}
	fmt.Println()

	// Check service state
	fmt.Println("🔍 Checking service state:")
	output, err := runLaunchctl("print", servicePath)
	if err != nil {
		fmt.Printf("  ℹ️  Service not loaded in launchd\n")
	} else {
		fmt.Printf("  ✅ Service is loaded\n")
		// Parse and show key info
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "state =") {
				fmt.Printf("     %s\n", strings.TrimSpace(line))
			}
			if strings.Contains(line, "pid =") {
				fmt.Printf("     %s\n", strings.TrimSpace(line))
			}
		}
	}
	fmt.Println()

	// Check binary
	fmt.Println("🔧 Checking sprout binary:")
	binaryPath, err := getBinaryPath()
	if err != nil {
		fmt.Printf("  ❌ Error determining binary path: %v\n", err)
	} else {
		fmt.Printf("  ✅ Binary: %s\n", binaryPath)
		if info, err := os.Stat(binaryPath); err == nil {
			fmt.Printf("     Size: %d bytes, Mode: %s\n", info.Size(), info.Mode())
		} else {
			console.GlyphWarning.Fprintf(os.Stdout, "  Cannot access binary: %v", err)
		}
	}
	fmt.Println()

	// Check log files
	homeDir, err := os.UserHomeDir()
	if err == nil {
		fmt.Println("📝 Checking log files:")
		logDir := filepath.Join(homeDir, ".sprout/logs")
		stdoutPath := filepath.Join(logDir, "daemon.stdout.log")
		stderrPath := filepath.Join(logDir, "daemon.stderr.log")

		for _, logPath := range []string{stdoutPath, stderrPath} {
			if info, err := os.Stat(logPath); err == nil {
				fmt.Printf("  ✅ %s (%d bytes)\n", filepath.Base(logPath), info.Size())
			} else if os.IsNotExist(err) {
				fmt.Printf("  ℹ️  %s does not exist\n", filepath.Base(logPath))
			} else {
				console.GlyphWarning.Fprintf(os.Stdout, "  %s error: %v", filepath.Base(logPath), err)
			}
		}
		fmt.Println()
	}

	// Check service.env
	if err == nil {
		fmt.Println("🔑 Checking service.env:")
		envPath := ServiceEnvPath(homeDir)
		envVars, err := LoadServiceEnvFile(homeDir)
		if err != nil {
			console.GlyphWarning.Fprintf(os.Stdout, "  Error loading service.env: %v", err)
		} else if len(envVars) == 0 {
			fmt.Printf("  ℹ️  service.env exists but is empty: %s\n", envPath)
		} else {
			fmt.Printf("  ✅ service.env contains %d variable(s): %s\n", len(envVars), envPath)
			var keys []string
			for k := range envVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, key := range keys {
				if i > 0 && i%4 == 0 {
					fmt.Println()
				}
				fmt.Printf("     %s", key)
				if i < len(keys)-1 {
					fmt.Print(", ")
				}
			}
			if len(keys) > 0 {
				fmt.Println()
			}
		}
		fmt.Println()
	}

	// Troubleshooting suggestions
	fmt.Println("💡 Common fixes:")
	if !isServiceLoaded(servicePath) {
		fmt.Println("  • Service not loaded: Try 'sprout service start'")
	} else {
		fmt.Println("  • Service loaded but may not be running: Try 'sprout service start'")
		fmt.Println("  • Check logs in ~/.sprout/logs/ for errors")
	}
	fmt.Println("  • If problems persist, try: 'sprout service uninstall && sprout service install'")
	fmt.Println("  • Rebuild launchd database: 'launchctl reboot 2>/dev/null || sudo killall launchd'")
	fmt.Println()

	return nil
}

// isServiceLoaded checks if the service is loaded in launchd (regardless of running state)
func isServiceLoaded(servicePath string) bool {
	_, err := runLaunchctl("print", servicePath)
	return err == nil
}
