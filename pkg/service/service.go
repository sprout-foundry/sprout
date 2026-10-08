//go:build !js

package service

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// serviceManager defines the interface for platform-specific service management.
type serviceManager interface {
	Install() error
	Uninstall() error
	Start() error
	Stop() error
	Status() (running bool, err error)
}

// serviceDiagnostics defines the interface for diagnostic capabilities.
type serviceDiagnostics interface {
	Diagnose() error
}

// newServiceManager is set by platform-specific init() functions.
var newServiceManager func() serviceManager

const (
	serviceName = "sprout-daemon"
	servicePort = 56000
	serviceURL  = "http://localhost:56000"
)

// ForceConfirm skips confirmation prompts when true (set by -y flag).
var ForceConfirm bool

// ServiceCmd is the root command for service management.
var ServiceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage the sprout daemon service",
	Long: `Manage the sprout daemon as a system service.

Integration with systemd (Linux) or launchd (macOS) allows the sprout
web UI to start automatically on boot and run persistently in the background.`,
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the sprout daemon as a system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check for legacy services before installing
		legacyPaths, err := detectLegacyService()
		if err != nil {
			return fmt.Errorf("failed to check for legacy services: %w", err)
		}
		if len(legacyPaths) > 0 {
			fmt.Println("\nLegacy service configuration(s) detected from a previous 'sprout' installation:")
			for _, p := range legacyPaths {
				fmt.Printf("  %s\n", p)
			}
			if !ForceConfirm {
				fmt.Print("\nRemove legacy service files? (y/N): ")
				reader := bufio.NewReader(os.Stdin)
				resp, _ := reader.ReadString('\n')
				resp = strings.TrimSpace(strings.ToLower(resp))
				if resp != "y" {
					fmt.Println("Aborting. Please remove legacy services manually or re-run with -y.")
					return fmt.Errorf("aborted: legacy services not removed")
				}
			}

			if err := removeLegacyServices(legacyPaths); err != nil {
				return fmt.Errorf("failed to remove legacy services: %w", err)
			}
			fmt.Println("Legacy service files removed successfully.")
		}

		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		if err := sm.Install(); err != nil {
			return fmt.Errorf("failed to install service: %w", err)
		}
		fmt.Printf("Service '%s' installed successfully.\n", serviceName)
		fmt.Printf("The daemon will start automatically (RunAtLoad=true).\n")
		fmt.Printf("Access the web UI at: %s\n", serviceURL)
		fmt.Println("Run 'sprout service status' to confirm it is running.")
		return nil
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the sprout daemon system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		if err := sm.Uninstall(); err != nil {
			return fmt.Errorf("failed to uninstall service: %w", err)
		}
		fmt.Printf("Service '%s' uninstalled successfully.\n", serviceName)
		return nil
	},
}

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the sprout daemon service",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		if err := sm.Start(); err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
		fmt.Printf("Service '%s' started successfully.\n", serviceName)
		fmt.Printf("Access the web UI at: %s\n", serviceURL)
		return nil
	},
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the sprout daemon service",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		if err := sm.Stop(); err != nil {
			return fmt.Errorf("failed to stop service: %w", err)
		}
		fmt.Printf("Service '%s' stopped successfully.\n", serviceName)
		return nil
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the sprout daemon service",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		running, err := sm.Status()
		if err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
		fmt.Printf("Service '%s': ", serviceName)
		if running {
			fmt.Printf("running (%s)\n", serviceURL)
			// Report what the RUNNING daemon was started from. An upgrade
			// swaps the binary on disk but leaves the process on the old
			// build until a restart; this line makes that skew visible.
			if hs := fetchHealthStatus(); hs != nil && hs.Version != "" {
				fmt.Printf("  running build: %s (%s)\n", hs.Version, hs.Commit)
			}
		} else {
			fmt.Println("stopped")
		}
		return nil
	},
}

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the sprout daemon service (waits for in-flight queries)",
	Long: `Restart the sprout daemon service, picking up the newly installed binary.

Before stopping the daemon it waits (up to SPROUT_SERVICE_DRAIN_TIMEOUT
seconds, default 15) for active agent queries to finish, so an upgrade
restart does not cut work mid-flight. Pass --force to skip the drain and
restart immediately, or -y to accept the restart without the interactive
confirm when queries are still running.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}

		running, err := sm.Status()
		if err != nil {
			return fmt.Errorf("failed to query service status: %w", err)
		}
		if !running {
			// Distinguish "nothing at all" from "a daemon that isn't
			// service-managed": restarting the service would silently
			// START a service the user never installed alongside their
			// standalone daemon.
			if hs := fetchHealthStatus(); hs != nil {
				fmt.Println("A sprout daemon is running, but not via the system service.")
				fmt.Println("Restart it directly (stop the 'sprout agent -d' process and start it again);")
				fmt.Println("auto-started daemons also stop themselves when idle.")
				return nil
			}
			fmt.Println("Service is not running — starting it.")
			return sm.Start()
		}

		old := fetchHealthStatus()
		if err := drainActiveQueries(restartDrainTimeout); err != nil {
			if !ForceConfirm {
				fmt.Printf("%d agent quer(y/ies) still active after %s.\n", busyCount(old), restartDrainTimeout)
				fmt.Print("Restart now and interrupt them? (y/N): ")
				reader := bufio.NewReader(os.Stdin)
				resp, _ := reader.ReadString('\n')
				resp = strings.TrimSpace(strings.ToLower(resp))
				if resp != "y" {
					fmt.Println("Restart deferred. Run 'sprout service restart' when the queries finish.")
					return nil
				}
			} else {
				fmt.Printf("Proceeding with restart despite %d active quer(y/ies) (--yes).\n", busyCount(old))
			}
		}

		if restarter, ok := sm.(serviceRestarter); ok {
			if err := restarter.Restart(); err != nil {
				return fmt.Errorf("failed to restart service: %w", err)
			}
		} else {
			// Generic path: stop then start.
			if err := sm.Stop(); err != nil {
				return fmt.Errorf("failed to stop service: %w", err)
			}
			if err := sm.Start(); err != nil {
				return fmt.Errorf("failed to start service: %w", err)
			}
		}

		// Wait for the daemon to come back healthy and report the version
		// it now runs, so the operator can confirm the new binary took over.
		if hs := waitHealthy(15 * time.Second); hs != nil {
			newV := hs.Version
			if newV == "" {
				newV = "unknown (pre-version daemon)"
			}
			if old != nil && old.Version != "" {
				fmt.Printf("Daemon restarted: %s → %s\n", old.Version, newV)
			} else {
				fmt.Printf("Daemon restarted; running %s\n", newV)
			}
		} else {
			fmt.Println("Daemon restarted; health check did not return OK within 15s.")
			fmt.Printf("Check it with: sprout service status\n")
		}
		return nil
	},
}

var serviceDiagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Diagnose service installation issues",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		if diagnostics, ok := sm.(serviceDiagnostics); ok {
			return diagnostics.Diagnose()
		}
		return fmt.Errorf("diagnostics not supported on this platform")
	},
}

func init() {
	serviceInstallCmd.Flags().BoolVarP(&ForceConfirm, "yes", "y", false, "Skip confirmation prompts and auto-remove legacy services")
	serviceUninstallCmd.Flags().BoolVarP(&ForceConfirm, "yes", "y", false, "Skip confirmation prompts")
	serviceRestartCmd.Flags().BoolVarP(&ForceConfirm, "yes", "y", false, "Restart without confirming when queries are still active")

	ServiceCmd.AddCommand(serviceInstallCmd)
	ServiceCmd.AddCommand(serviceUninstallCmd)
	ServiceCmd.AddCommand(serviceStartCmd)
	ServiceCmd.AddCommand(serviceStopCmd)
	ServiceCmd.AddCommand(serviceRestartCmd)
	ServiceCmd.AddCommand(serviceStatusCmd)
	ServiceCmd.AddCommand(serviceDiagnoseCmd)
}

// detectLegacyService searches for legacy "sprout" service configuration files
// that may conflict with a new service installation.
//
// Darwin checks ~/Library/LaunchAgents/com.ledit.*.plist (legacy naming)
// Linux checks ~/.config/systemd/user/ledit*.service (legacy naming)
func detectLegacyService() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		pattern := filepath.Join(homeDir, "Library", "LaunchAgents", "com.ledit.*.plist")
		return filepath.Glob(pattern)
	case "linux":
		pattern := filepath.Join(homeDir, ".config", "systemd", "user", "ledit*.service")
		return filepath.Glob(pattern)
	default:
		return nil, nil
	}
}

// getOrCreateServiceManager returns a platform-specific service manager.
func getOrCreateServiceManager() (serviceManager, error) {
	if newServiceManager == nil {
		return nil, fmt.Errorf("service management is not supported on this platform")
	}
	return newServiceManager(), nil
}

// getBinaryPath returns the absolute path to the running sprout binary.
func getBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to determine sprout binary path: %w", err)
	}
	return exe, nil
}
