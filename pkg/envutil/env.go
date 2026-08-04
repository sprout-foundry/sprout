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

// homeDir resolves the user's home directory, preferring $HOME and
// falling back to os.UserHomeDir(). Returns ("", error) if neither
// yields a path. Every resolver in this package must call this so
// that no resolver panics when $HOME is unset (WASM, minimal sandboxes).
func homeDir() (string, error) {
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return h, nil
}

// resolveRoot resolves a category root directory from an env override,
// an XDG variable, and a home-relative default. The dir is created if
// missing.
func resolveRoot(envVar, xdgVar, homeRelative string, mode os.FileMode) (string, error) {
	if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
		return ensureDir(v, mode)
	}
	if xdg := strings.TrimSpace(os.Getenv(xdgVar)); xdg != "" {
		return ensureDir(filepath.Join(xdg, "sprout"), mode)
	}
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, homeRelative), mode)
}

func ensureDir(path string, mode os.FileMode) (string, error) {
	if err := os.MkdirAll(path, mode); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	return path, nil
}

// ConfigDir returns the sprout configuration directory path.
//
// Resolution: $SPROUT_CONFIG_DIR → $SPROUT_CONFIG (legacy alias) →
// $XDG_CONFIG_HOME/sprout → $HOME/.config/sprout.
//
// $SPROUT_CONFIG continues to work as an alias so test isolation and
// configuration.NewTestManager keep functioning unchanged.
func ConfigDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("SPROUT_CONFIG_DIR")); v != "" {
		return ensureDir(v, 0700)
	}
	if v := strings.TrimSpace(os.Getenv("SPROUT_CONFIG")); v != "" {
		return ensureDir(v, 0700)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return ensureDir(filepath.Join(xdg, "sprout"), 0700)
	}
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, ".config", "sprout"), 0700)
}

// StateDir returns the sprout state directory path (machine-generated,
// per-install, not portable).
//
// Resolution: $SPROUT_STATE_DIR → $XDG_STATE_HOME/sprout →
// $HOME/.local/state/sprout.
func StateDir() (string, error) {
	return resolveRoot("SPROUT_STATE_DIR", "XDG_STATE_HOME", filepath.Join(".local", "state", "sprout"), 0700)
}

// DataDir returns the sprout shared data directory path (regenerable
// artifacts like embeddings, models).
//
// Resolution: $SPROUT_DATA_DIR → $XDG_DATA_HOME/sprout →
// $HOME/.local/share/sprout.
func DataDir() (string, error) {
	return resolveRoot("SPROUT_DATA_DIR", "XDG_DATA_HOME", filepath.Join(".local", "share", "sprout"), 0700)
}

// CacheDir returns the sprout cache directory path (disposable,
// regenerable).
//
// Resolution: $SPROUT_CACHE_DIR → $XDG_CACHE_HOME/sprout →
// $HOME/.cache/sprout.
func CacheDir() (string, error) {
	return resolveRoot("SPROUT_CACHE_DIR", "XDG_CACHE_HOME", filepath.Join(".cache", "sprout"), 0700)
}

// GetConfigDir returns the sprout configuration directory path.
//
// Deprecated: use ConfigDir(). Retained for backward compatibility with
// callers that have not been migrated.
func GetConfigDir() (string, error) {
	return ConfigDir()
}
