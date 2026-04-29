// Package envutil provides environment variable helpers with SPROUT_*/LEDIT_* prefix support.
// This package has ZERO external dependencies to avoid import cycles.
package envutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	deprecatedVars sync.Map
)

// GetEnv checks for an environment variable using the primary (sproutKey) name first,
// falling back to the legacy (legacyKey) name. If only the legacy name is set,
// a one-time deprecation warning is printed to stderr.
func GetEnv(primaryKey, legacyKey string) string {
	if v := os.Getenv(primaryKey); v != "" {
		return v
	}
	if v := os.Getenv(legacyKey); v != "" {
		if _, loaded := deprecatedVars.LoadOrStore(legacyKey, true); !loaded {
			fmt.Fprintf(os.Stderr, "[deprecation] env var %s is deprecated, use %s instead\n", legacyKey, primaryKey)
		}
		return v
	}
	return ""
}

// GetEnvSimple checks SPROUT_* then LEDIT_* for a variable name suffix.
// E.g., GetEnvSimple("CONFIG") checks SPROUT_CONFIG then LEDIT_CONFIG.
func GetEnvSimple(suffix string) string {
	return GetEnv("SPROUT_"+suffix, "LEDIT_"+suffix)
}

// SetEnv sets both the SPROUT_* and LEDIT_* versions of an env var.
func SetEnv(suffix, value string) error {
	if err := os.Setenv("SPROUT_"+suffix, value); err != nil {
		return err
	}
	return os.Setenv("LEDIT_"+suffix, value)
}

// LookupEnv checks SPROUT_* first, then LEDIT_*. Returns the value and whether it was found.
func LookupEnv(suffix string) (string, bool) {
	if v, ok := os.LookupEnv("SPROUT_" + suffix); ok {
		return v, true
	}
	if v, ok := os.LookupEnv("LEDIT_" + suffix); ok {
		return v, true
	}
	return "", false
}

// UnsetEnv removes both SPROUT_* and LEDIT_* versions of an env var.
func UnsetEnv(suffix string) {
	_ = os.Unsetenv("SPROUT_" + suffix)
	_ = os.Unsetenv("LEDIT_" + suffix)
}

// HasPrefix checks if an env var name starts with SPROUT_ or LEDIT_.
func HasPrefix(name string) bool {
	return strings.HasPrefix(name, "SPROUT_") || strings.HasPrefix(name, "LEDIT_")
}

// SproutKey returns the SPROUT_* version of a LEDIT_* key.
func SproutKey(legacyKey string) string {
	return strings.Replace(legacyKey, "LEDIT_", "SPROUT_", 1)
}

// GetConfigDir returns the sprout configuration directory path.
// It mirrors the resolution logic used by the configuration package:
//  1. SPROUT_CONFIG / LEDIT_CONFIG environment variable (if set)
//  2. XDG_CONFIG_HOME/sprout
//  3. HOME/.config/sprout
//  4. os.UserHomeDir()/.config/sprout
//
// This lives in envutil (zero external dependencies) so that low-level packages
// like agent_api can resolve the config directory without importing the
// configuration package (which would create an import cycle).
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
