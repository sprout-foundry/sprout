package config

// SecurityConfig contains all security-related configuration
type SecurityConfig struct {
	// Security Features
	EnableSecurityChecks bool     `json:"enable_security_checks"` // Enable security checks
	ShellAllowlist       []string `json:"shell_allowlist"`        // Allowed shell commands

	// Command Restrictions
	AllowedCommands  []string `json:"allowed_commands"`   // Whitelist of allowed commands
	BlockedCommands  []string `json:"blocked_commands"`   // Blacklist of blocked commands
	MaxCommandLength int      `json:"max_command_length"` // Maximum command length
	RequireApproval  bool     `json:"require_approval"`   // Require approval for dangerous commands

	// File System Restrictions
	AllowedPaths       []string `json:"allowed_paths"`        // Allowed filesystem paths
	BlockedPaths       []string `json:"blocked_paths"`        // Blocked filesystem paths
	ReadOnlyPaths      []string `json:"read_only_paths"`      // Read-only paths
	AllowNetworkAccess bool     `json:"allow_network_access"` // Allow network access
}

// DefaultSecurityConfig returns sensible defaults for security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		EnableSecurityChecks: true,
		ShellAllowlist:       []string{"git", "go", "node", "npm", "python", "make", "docker"},
		AllowedCommands:      []string{}, // empty means allow all
		BlockedCommands:      []string{"rm -rf /", "format", "fdisk", "mkfs"},
		MaxCommandLength:     1000,
		RequireApproval:      false,
		AllowedPaths:         []string{}, // empty means allow all
		BlockedPaths:         []string{"/etc", "/usr", "/var", "/System", "C:\\Windows"},
		ReadOnlyPaths:        []string{".git"},
		AllowNetworkAccess:   true,
	}
}

// IsCommandAllowed checks if a shell command is allowed to execute
func (c *SecurityConfig) IsCommandAllowed(command string) bool {
	// Check blacklist first
	for _, blocked := range c.BlockedCommands {
		if contains(command, blocked) {
			return false
		}
	}

	// If whitelist is defined, command must be in it
	if len(c.AllowedCommands) > 0 {
		for _, allowed := range c.AllowedCommands {
			if contains(command, allowed) {
				return true
			}
		}
		return false
	}

	// Check command length
	if len(command) > c.MaxCommandLength {
		return false
	}

	return true
}

// IsPathAllowed checks if a filesystem path is allowed to be accessed
func (c *SecurityConfig) IsPathAllowed(path string) bool {
	// Check blocked paths
	for _, blocked := range c.BlockedPaths {
		if contains(path, blocked) {
			return false
		}
	}

	// If allowed paths is defined, path must be in it
	if len(c.AllowedPaths) > 0 {
		for _, allowed := range c.AllowedPaths {
			if contains(path, allowed) {
				return true
			}
		}
		return false
	}

	return true
}

// IsPathReadOnly checks if a path should be treated as read-only
func (c *SecurityConfig) IsPathReadOnly(path string) bool {
	for _, readOnly := range c.ReadOnlyPaths {
		if contains(path, readOnly) {
			return true
		}
	}
	return false
}

// ShouldRequireApproval checks if a command requires manual approval
func (c *SecurityConfig) ShouldRequireApproval(command string) bool {
	if !c.RequireApproval {
		return false
	}

	// Commands that typically require approval
	dangerousCommands := []string{
		"rm", "del", "format", "fdisk", "mkfs",
		"shutdown", "reboot", "halt",
		"passwd", "usermod", "userdel",
	}

	for _, dangerous := range dangerousCommands {
		if contains(command, dangerous) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(hasPrefix(s, substr) || hasSuffix(s, substr) || containsSubstring(s, substr)))
}

// hasPrefix checks if string starts with prefix (case-insensitive)
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) &&
		toLower(s[:len(prefix)]) == toLower(prefix)
}

// hasSuffix checks if string ends with suffix (case-insensitive)
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) &&
		toLower(s[len(s)-len(suffix):]) == toLower(suffix)
}

// containsSubstring checks if string contains substring anywhere (case-insensitive)
func containsSubstring(s, substr string) bool {
	sLower := toLower(s)
	substrLower := toLower(substr)
	return len(sLower) >= len(substrLower) &&
		stringIndex(sLower, substrLower) >= 0
}

// toLower converts string to lowercase (simple implementation)
func toLower(s string) string {
	result := make([]rune, len(s))
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + 32 // 'a' - 'A' = 32
		} else {
			result[i] = r
		}
	}
	return string(result)
}

// stringIndex finds index of substring in string
func stringIndex(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(substr) > len(s) {
		return -1
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
