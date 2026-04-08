package security

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// PermissionChecker provides utilities to check file and directory permissions
// for security-sensitive files in the ledit configuration directory.
type PermissionChecker struct {
	configDir string
}

// NewPermissionChecker creates a new PermissionChecker for the given config directory.
func NewPermissionChecker(configDir string) *PermissionChecker {
	return &PermissionChecker{
		configDir: configDir,
	}
}

// CheckConfigDirPermissions checks that the config directory has secure permissions (0700).
// Returns a warning message if permissions are too open.
func (pc *PermissionChecker) CheckConfigDirPermissions() string {
	info, err := os.Stat(pc.configDir)
	if err != nil {
		return "" // Directory doesn't exist yet, that's OK
	}

	mode := info.Mode().Perm()
	expected := os.FileMode(0700)

	if mode != expected {
		return fmt.Sprintf(
			"WARNING: Config directory %q has insecure permissions (%o). "+
				"Expected 0700 (owner read/write/execute only). "+
				"Run: chmod 700 %q",
			pc.configDir, mode, pc.configDir,
		)
	}
	return ""
}

// CheckFilePermissions checks that a file has secure permissions (0600).
// Returns a warning message if permissions are too open.
func (pc *PermissionChecker) CheckFilePermissions(filePath string) string {
	info, err := os.Stat(filePath)
	if err != nil {
		return "" // File doesn't exist yet, that's OK
	}

	mode := info.Mode().Perm()
	expected := os.FileMode(0600)

	if mode != expected {
		return fmt.Sprintf(
			"WARNING: Config file %q has insecure permissions (%o). "+
				"Expected 0600 (owner read/write only). "+
				"Run: chmod 600 %q",
			filePath, mode, filePath,
		)
	}
	return ""
}

// CheckAllSecurityFiles checks all security-sensitive files in the config directory.
// Returns a list of warning messages for files with insecure permissions.
func (pc *PermissionChecker) CheckAllSecurityFiles() []string {
	warnings := []string{}

	// Check config directory
	if warn := pc.CheckConfigDirPermissions(); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check config.json
	configPath := filepath.Join(pc.configDir, "config.json")
	if warn := pc.CheckFilePermissions(configPath); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check api_keys.json
	apiKeysPath := filepath.Join(pc.configDir, "api_keys.json")
	if warn := pc.CheckFilePermissions(apiKeysPath); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check machine key file
	machineKeyPath := filepath.Join(pc.configDir, "key.age")
	if warn := pc.CheckFilePermissions(machineKeyPath); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check encryption mode file
	modePath := filepath.Join(pc.configDir, "api_keys.mode")
	if warn := pc.CheckFilePermissions(modePath); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check backend mode file
	backendModePath := filepath.Join(pc.configDir, "backend.mode")
	if warn := pc.CheckFilePermissions(backendModePath); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check keyring providers file
	keyringProvidersPath := filepath.Join(pc.configDir, "keyring_providers.json")
	if warn := pc.CheckFilePermissions(keyringProvidersPath); warn != "" {
		warnings = append(warnings, warn)
	}

	// Check MCP config file
	mcpConfigPath := filepath.Join(pc.configDir, "mcp_config.json")
	if warn := pc.CheckFilePermissions(mcpConfigPath); warn != "" {
		warnings = append(warnings, warn)
	}

	return warnings
}

// FixPermissions attempts to fix insecure permissions on security-sensitive files.
// Returns a list of errors encountered.
func (pc *PermissionChecker) FixPermissions() []error {
	errors := []error{}

	// Fix config directory
	dirInfo, err := os.Stat(pc.configDir)
	if err == nil {
		if dirInfo.Mode().Perm() != 0700 {
			if err := os.Chmod(pc.configDir, 0700); err != nil {
				errors = append(errors, fmt.Errorf("failed to chmod config directory: %w", err))
			}
		}
	}

	// Fix config.json
	configPath := filepath.Join(pc.configDir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		if err := os.Chmod(configPath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod config.json: %w", err))
		}
	}

	// Fix api_keys.json
	apiKeysPath := filepath.Join(pc.configDir, "api_keys.json")
	if _, err := os.Stat(apiKeysPath); err == nil {
		if err := os.Chmod(apiKeysPath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod api_keys.json: %w", err))
		}
	}

	// Fix machine key file
	machineKeyPath := filepath.Join(pc.configDir, "key.age")
	if _, err := os.Stat(machineKeyPath); err == nil {
		if err := os.Chmod(machineKeyPath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod key.age: %w", err))
		}
	}

	// Fix encryption mode file
	modePath := filepath.Join(pc.configDir, "api_keys.mode")
	if _, err := os.Stat(modePath); err == nil {
		if err := os.Chmod(modePath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod api_keys.mode: %w", err))
		}
	}

	// Fix backend mode file
	backendModePath := filepath.Join(pc.configDir, "backend.mode")
	if _, err := os.Stat(backendModePath); err == nil {
		if err := os.Chmod(backendModePath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod backend.mode: %w", err))
		}
	}

	// Fix keyring providers file
	keyringProvidersPath := filepath.Join(pc.configDir, "keyring_providers.json")
	if _, err := os.Stat(keyringProvidersPath); err == nil {
		if err := os.Chmod(keyringProvidersPath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod keyring_providers.json: %w", err))
		}
	}

	// Fix MCP config file
	mcpConfigPath := filepath.Join(pc.configDir, "mcp_config.json")
	if _, err := os.Stat(mcpConfigPath); err == nil {
		if err := os.Chmod(mcpConfigPath, 0600); err != nil {
			errors = append(errors, fmt.Errorf("failed to chmod mcp_config.json: %w", err))
		}
	}

	return errors
}

// RunStartupCheck performs a full permission check at startup and logs warnings.
// Returns true if any warnings were issued.
func RunStartupCheck(configDir string) bool {
	checker := NewPermissionChecker(configDir)
	warnings := checker.CheckAllSecurityFiles()

	if len(warnings) > 0 {
		log.Printf("[security] Permission check warnings:")
		for _, warn := range warnings {
			log.Printf("  %s", warn)
		}
		return true
	}

	return false
}

// GetPermissionError returns a descriptive error for common permission issues.
func GetPermissionError(path string, expectedMode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", path, err)
	}

	mode := info.Mode().Perm()
	if mode&0400 == 0 {
		return fmt.Errorf(
			"%q is not readable by owner (expected 0600, got %o). "+
				"Run: chmod 600 %q",
			path, mode, path,
		)
	}
	if mode&0200 == 0 {
		return fmt.Errorf(
			"%q is not writable by owner (expected 0600, got %o). "+
				"Run: chmod 600 %q",
			path, mode, path,
		)
	}
	return fmt.Errorf(
		"%q has insecure permissions (expected 0600, got %o). "+
			"Run: chmod 600 %q",
		path, mode, path,
	)
}

// IsWorldReadable returns true if the file has world-readable permissions.
func IsWorldReadable(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0004 != 0, nil
}

// IsGroupReadable returns true if the file has group-readable permissions.
func IsGroupReadable(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0040 != 0, nil
}

// GetFileMode returns the permission bits of a file as an octal string.
func GetFileMode(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%o", info.Mode().Perm()), nil
}

// GetDirMode returns the permission bits of a directory as an octal string.
func GetDirMode(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%o", info.Mode().Perm()), nil
}

// CheckSymlinkSafety checks if a path is a symlink and warns if it points outside the config dir.
func CheckSymlinkSafety(path string, configDir string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return "" // Not a symlink
	}

	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}

	// Check if target is absolute and outside config dir
	if filepath.IsAbs(target) {
		absTarget, err := filepath.Abs(target)
		if err == nil {
			if !filepath.HasPrefix(absTarget, configDir) {
				return fmt.Sprintf(
					"WARNING: %q is a symlink pointing outside config directory (%q). "+
						"This may allow unauthorized access to sensitive data.",
					path, absTarget,
				)
			}
		}
	}

	return ""
}

// CheckAllSymlinks checks all files in the config directory for symlinks pointing outside.
func CheckAllSymlinks(configDir string) []string {
	warnings := []string{}

	entries, err := os.ReadDir(configDir)
	if err != nil {
		return warnings
	}

	for _, entry := range entries {
		path := filepath.Join(configDir, entry.Name())

		if warn := CheckSymlinkSafety(path, configDir); warn != "" {
			warnings = append(warnings, warn)
		}
	}

	return warnings
}
