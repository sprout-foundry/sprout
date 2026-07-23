// Package envutil provides environment variable helpers with SPROUT_* prefix support.
package envutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetEnv returns the value of os.Getenv("SPROUT_"+suffix), or "" if unset.
func GetEnv(suffix string) string {
	return os.Getenv("SPROUT_" + suffix)
}

// GetEnvSimple checks SPROUT_* for a variable name suffix.
// E.g., GetEnvSimple("CONFIG") checks SPROUT_CONFIG.
func GetEnvSimple(suffix string) string {
	return os.Getenv("SPROUT_" + suffix)
}

// SetEnv sets the SPROUT_* version of an env var.
func SetEnv(suffix, value string) error {
	return os.Setenv("SPROUT_"+suffix, value)
}

// LookupEnv checks SPROUT_*. Returns the value and whether it was found.
func LookupEnv(suffix string) (string, bool) {
	return os.LookupEnv("SPROUT_" + suffix)
}

// UnsetEnv removes the SPROUT_* version of an env var.
func UnsetEnv(suffix string) {
	_ = os.Unsetenv("SPROUT_" + suffix)
}

// HasPrefix checks if an env var name starts with SPROUT_.
func HasPrefix(name string) bool {
	return strings.HasPrefix(name, "SPROUT_")
}

// GetConfigDir returns the sprout configuration directory path.
func GetConfigDir() (string, error) {
	configDir := strings.TrimSpace(GetEnvSimple("CONFIG"))
	if configDir == "" {
		xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if xdgConfigHome != "" {
			configDir = filepath.Join(xdgConfigHome, "sprout")
		} else {
			homeEnv := strings.TrimSpace(os.Getenv("HOME"))
			if homeEnv != "" {
				configDir = filepath.Join(homeEnv, ".config", "sprout")
			} else {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return "", fmt.Errorf("failed to get home directory: %w", err)
				}
				configDir = filepath.Join(homeDir, ".config", "sprout")
			}
		}
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	return configDir, nil
}
