//go:build darwin

package service

// darwin_diagnose.go — launchd diagnostics for the service manager, split
// out of darwin.go. Diagnose() walks the plist file, launchd service state,
// binary, logs, and service.env, printing a structured health report with
// common-fix suggestions. isServiceLoaded is its launchd-state probe.
import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

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
		// set wrong). Since SP-130, the webui forces explicit workspace
		// selection rather than silently inheriting home, so a stale plist
		// means the browser root is wrong, not just the workspace.
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
	if stateDir, stateErr := envutil.StateDir(); stateErr != nil {
		console.GlyphWarning.Fprintf(os.Stdout, "  Could not resolve state directory: %v", stateErr)
	} else {
		fmt.Println("📝 Checking log files:")
		logDir := filepath.Join(stateDir, "logs")
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
	if envPath, pathErr := ServiceEnvPath(); pathErr == nil {
		fmt.Println("🔑 Checking service.env:")
		envVars, err := LoadServiceEnvFile()
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
		fmt.Println("  • Check logs in ~/.local/state/sprout/logs/ for errors")
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
