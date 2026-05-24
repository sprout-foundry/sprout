package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// validRoleNamePattern ensures role names cannot contain path separators or
// other dangerous characters that could lead to path traversal.
var validRoleNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-.]+$`)

// isValidRoleName checks whether a role name contains only safe characters
// (alphanumeric, hyphens, underscores, dots). This prevents path traversal
// attacks where a name like "../etc/passwd" could escape the roles directory.
func isValidRoleName(name string) bool {
	return name != "" && validRoleNamePattern.MatchString(name)
}

// RoleToolsConfig controls which tools a role can use.
type RoleToolsConfig struct {
	AllowedTools []string `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"  yaml:"denied_tools,omitempty"`
}

// RoleSkillsConfig lists skills the role can activate.
type RoleSkillsConfig struct {
	Skills []string `json:"skills,omitempty" yaml:"skills,omitempty"`
}

// RoleConstraints defines execution limits for a role.
type RoleConstraints struct {
	MaxIterations int      `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"     yaml:"max_tokens,omitempty"`
	AllowedPaths  []string `json:"allowed_paths,omitempty"  yaml:"allowed_paths,omitempty"`
}

// RoleConfig represents a complete role definition.
type RoleConfig struct {
	Name         string           `json:"name"          yaml:"name"`
	Description  string           `json:"description"   yaml:"description"`
	SystemPrompt string           `json:"system_prompt" yaml:"system_prompt"`
	Tools        RoleToolsConfig  `json:"tools"         yaml:"tools"`
	Skills       RoleSkillsConfig `json:"skills"        yaml:"skills"`
	Constraints  RoleConstraints  `json:"constraints"   yaml:"constraints"`
	Provider     string           `json:"provider"      yaml:"provider"`
	Model        string           `json:"model"         yaml:"model"`
}

// RoleMeta provides metadata about a role including its source location.
type RoleMeta struct {
	Name      string `json:"name"       yaml:"name"`
	CreatedAt string `json:"created_at" yaml:"created_at"`
	UpdatedAt string `json:"updated_at" yaml:"updated_at"`
	Source    string `json:"source"     yaml:"source"` // "global", "workspace", or "session"
}

// MergeRoleConfig deep merges two RoleConfig values. Override fields take
// precedence over base, with the following rules:
//
//   - String fields: use override if non-empty, else base
//   - Int fields: use override if non-zero, else base
//   - Slice fields: use override if non-nil/non-empty, else base
//   - Name: always use base.Name (the identity is fixed)
func MergeRoleConfig(base, override RoleConfig) RoleConfig {
	result := RoleConfig{
		Name:         base.Name, // identity is always from base
		Description:  coalesceString(override.Description, base.Description),
		SystemPrompt: coalesceString(override.SystemPrompt, base.SystemPrompt),
		Provider:     coalesceString(override.Provider, base.Provider),
		Model:        coalesceString(override.Model, base.Model),
	}

	// Tools: merge allowed and denied tool lists
	result.Tools = RoleToolsConfig{
		AllowedTools: coalesceStrings(override.Tools.AllowedTools, base.Tools.AllowedTools),
		DeniedTools:  coalesceStrings(override.Tools.DeniedTools, base.Tools.DeniedTools),
	}

	// Skills: merge skill lists
	result.Skills = RoleSkillsConfig{
		Skills: coalesceStrings(override.Skills.Skills, base.Skills.Skills),
	}

	// Constraints: merge individual fields
	result.Constraints = RoleConstraints{
		MaxIterations: coalesceInt(override.Constraints.MaxIterations, base.Constraints.MaxIterations),
		MaxTokens:     coalesceInt(override.Constraints.MaxTokens, base.Constraints.MaxTokens),
		AllowedPaths:  coalesceStrings(override.Constraints.AllowedPaths, base.Constraints.AllowedPaths),
	}

	return result
}

func coalesceString(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func coalesceInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func coalesceStrings(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}

// LoadRoleFromFile reads a single role YAML file and returns the parsed RoleConfig.
func LoadRoleFromFile(path string) (*RoleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read role file %s: %w", path, err)
	}

	var cfg RoleConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse role file %s: %w", path, err)
	}

	if strings.TrimSpace(cfg.Name) == "" {
		// Derive name from filename if not set
		cfg.Name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}

	return &cfg, nil
}

// SaveRoleToFile writes a RoleConfig as a YAML file.
func SaveRoleToFile(dir string, cfg RoleConfig) error {
	name := strings.TrimSpace(cfg.Name)
	name = strings.ToLower(name)

	if name == "" {
		return fmt.Errorf("role name cannot be empty")
	}

	if !isValidRoleName(name) {
		return fmt.Errorf("role name %q contains invalid characters (only alphanumeric, hyphens, underscores, and dots allowed)", name)
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create role directory %s: %w", dir, err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal role %s: %w", name, err)
	}

	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write role file %s: %w", path, err)
	}

	return nil
}

// RoleFileFromPath extracts the role name from a filesystem path.
func RoleFileFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".yaml")
	return base
}

// RoleMetaFromFile inspects a role file on disk and returns its metadata.
func RoleMetaFromFile(path string, source string) (RoleMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return RoleMeta{}, fmt.Errorf("stat role file %s: %w", path, err)
	}

	name := RoleFileFromPath(path)
	modTime := info.ModTime()

	// Note: CreatedAt uses ModTime as a cross-platform approximation.
	// Birth time (btime) is not portable across all OS/filesystems.
	return RoleMeta{
		Name:      name,
		CreatedAt: modTime.Format(time.RFC3339),
		UpdatedAt: modTime.Format(time.RFC3339),
		Source:    source,
	}, nil
}
