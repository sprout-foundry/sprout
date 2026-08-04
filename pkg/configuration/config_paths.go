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

// WorkspaceConfigDir returns the .sprout directory for a workspace root.
func WorkspaceConfigDir(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	return filepath.Join(workspaceRoot, ConfigDirName)
}

// WorkspaceConfigWritePath returns where workspace-level config is written.
// Always the new filename — legacy files are read but never written back to.
func WorkspaceConfigWritePath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	return filepath.Join(workspaceRoot, ConfigDirName, WorkspaceConfigFileName)
}

// GetWorkspaceConfigPath returns the workspace-level config file to READ.
//
// Resolution: workspace.json if present, else the legacy config.json, else the
// workspace.json path (so callers can stat it and find nothing). Existing
// workspaces keep working untouched; nothing is moved or rewritten.
func GetWorkspaceConfigPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	return ResolveWorkspaceConfigFile(WorkspaceConfigDir(workspaceRoot), isHomeDir(workspaceRoot))
}

// ResolveWorkspaceConfigFile picks the workspace config file inside dir.
//
// dirIsHome disables the legacy fallback. This is load-bearing: `.sprout` is
// the per-directory sprout folder AND ~/.sprout is the user-level state
// directory, so at $HOME the legacy config.json is the user's global config,
// not a workspace config. Falling back to it there is precisely the aliasing
// this split exists to remove — and every existing install has that file, so
// without this guard the split would fix nothing for current users.
//
// A user who deliberately runs with $HOME as the workspace still gets a real
// workspace layer; it just has to be an explicit ~/.sprout/workspace.json.
func ResolveWorkspaceConfigFile(dir string, dirIsHome bool) string {
	if dir == "" {
		return ""
	}
	current := filepath.Join(dir, WorkspaceConfigFileName)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	if dirIsHome {
		return current
	}
	legacy := filepath.Join(dir, ConfigFileName)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

// isHomeDir reports whether dir resolves to the user's home directory.
// Symlinks are evaluated on both sides so macOS /Users → /private/Users (or a
// symlinked home) doesn't produce a false negative.
func isHomeDir(dir string) bool {
	if dir == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	resolvedDir, dirErr := filepath.EvalSymlinks(dir)
	resolvedHome, homeErr := filepath.EvalSymlinks(home)
	if dirErr != nil || homeErr != nil {
		return filepath.Clean(dir) == filepath.Clean(home)
	}
	return resolvedDir == resolvedHome
}

// IsWorkspaceConfigPresent checks if a workspace config file exists
func IsWorkspaceConfigPresent(workspaceRoot string) bool {
	path := GetWorkspaceConfigPath(workspaceRoot)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
