package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// GetConfigDir returns the configuration directory path
func GetConfigDir() (string, error) {
	return envutil.GetConfigDir()
}

func getDefaultConfigDir() (string, error) {
	xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "sprout"), nil
	}

	homeEnv := strings.TrimSpace(os.Getenv("HOME"))
	if homeEnv != "" {
		return filepath.Join(homeEnv, ".config", "sprout"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "sprout"), nil
}

// GetConfigPath returns the full path to the config file
func GetConfigPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, ConfigFileName), nil
}

// GetWorkspaceConfigPath returns the path to workspace-level config
func GetWorkspaceConfigPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ConfigDirName, ConfigFileName)
}

// IsWorkspaceConfigPresent checks if a workspace config file exists
func IsWorkspaceConfigPresent(workspaceRoot string) bool {
	_, err := os.Stat(GetWorkspaceConfigPath(workspaceRoot))
	return err == nil
}
