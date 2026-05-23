//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

// forceConfirm skips confirmation prompts when true (set by -y flag).
var forceConfirm bool

// serviceCmd is the root command for service management.
var serviceCmd = &cobra.Command{
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
			if !forceConfirm {
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
		} else {
			fmt.Println("stopped")
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
	serviceInstallCmd.Flags().BoolVarP(&forceConfirm, "yes", "y", false, "Skip confirmation prompts and auto-remove legacy services")
	serviceUninstallCmd.Flags().BoolVarP(&forceConfirm, "yes", "y", false, "Skip confirmation prompts")

	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	serviceCmd.AddCommand(serviceDiagnoseCmd)
	rootCmd.AddCommand(serviceCmd)
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
