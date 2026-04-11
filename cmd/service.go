package cmd

import (
	"fmt"
	"os"

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

// newServiceManager is set by platform-specific init() functions.
var newServiceManager func() serviceManager

const (
	serviceName = "ledit-daemon"
	servicePort = 54000
	serviceURL  = "http://localhost:54000"
)

// serviceCmd is the root command for service management.
var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage the ledit daemon service",
	Long: `Manage the ledit daemon as a system service.

Integration with systemd (Linux) or launchd (macOS) allows the ledit
web UI to start automatically on boot and run persistently in the background.`,
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the ledit daemon as a system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := getOrCreateServiceManager()
		if err != nil {
			return err
		}
		if err := sm.Install(); err != nil {
			return fmt.Errorf("failed to install service: %w", err)
		}
		fmt.Printf("Service '%s' installed successfully.\n", serviceName)
		fmt.Printf("Access the web UI at: %s\n", serviceURL)
		fmt.Println("Run 'ledit service start' to begin.")
		return nil
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the ledit daemon system service",
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
	Short: "Start the ledit daemon service",
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
	Short: "Stop the ledit daemon service",
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
	Short: "Check the status of the ledit daemon service",
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

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	rootCmd.AddCommand(serviceCmd)
}

// getOrCreateServiceManager returns a platform-specific service manager.
func getOrCreateServiceManager() (serviceManager, error) {
	if newServiceManager == nil {
		return nil, fmt.Errorf("service management is not supported on this platform")
	}
	return newServiceManager(), nil
}

// getBinaryPath returns the absolute path to the running ledit binary.
func getBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to determine ledit binary path: %w", err)
	}
	return exe, nil
}
