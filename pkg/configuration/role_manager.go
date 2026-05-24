package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const RoleDirName = "roles"

// RoleManager manages role definitions with file-backed storage across
// global (~/.sprout/roles/) and workspace ({workspace}/.sprout/roles/) directories.
// Resolution order: workspace → global.
type RoleManager struct {
	mu           sync.RWMutex
	globalDir    string // ~/.sprout/roles/ (or equivalent config dir + /roles)
	workspaceDir string // {workspace}/.sprout/roles/ (may be "")
}

// validateName ensures a role name is non-empty and contains only safe
// characters to prevent path traversal attacks.
func (rm *RoleManager) validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("role name cannot be empty")
	}
	if !isValidRoleName(name) {
		return fmt.Errorf("invalid role name %q: only alphanumeric, hyphens, underscores, and dots allowed", name)
	}
	return nil
}

// NewRoleManager creates a new RoleManager with the given global and workspace
// directories. The globalDir is required and will be created if it does not exist.
// The workspaceDir is optional (pass "" if no workspace-level roles are needed).
func NewRoleManager(globalDir, workspaceDir string) *RoleManager {
	// Create global directory if it doesn't exist
	_ = os.MkdirAll(globalDir, 0700)

	rm := &RoleManager{
		globalDir:    globalDir,
		workspaceDir: workspaceDir,
	}

	// Create workspace directory if provided
	if workspaceDir != "" {
		_ = os.MkdirAll(workspaceDir, 0700)
	}

	return rm
}

// Resolve loads a role by name. Resolution order: workspace → global → error.
// If the role exists in both locations, the workspace version takes precedence
// (merged on top of the global version if both exist).
func (rm *RoleManager) Resolve(name string) (*RoleConfig, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	name = strings.TrimSpace(strings.ToLower(name))
	if err := rm.validateName(name); err != nil {
		return nil, err
	}

	// Try workspace first
	var workspaceCfg *RoleConfig
	wsPath := filepath.Join(rm.workspaceDir, name+".yaml")
	if rm.workspaceDir != "" {
		cfg, err := LoadRoleFromFile(wsPath)
		if err == nil {
			workspaceCfg = cfg
		}
	}

	// Try global
	var globalCfg *RoleConfig
	globalPath := filepath.Join(rm.globalDir, name+".yaml")
	if globalCfgFile, err := LoadRoleFromFile(globalPath); err == nil {
		globalCfg = globalCfgFile
	}

	// If neither found, return error
	if workspaceCfg == nil && globalCfg == nil {
		return nil, fmt.Errorf("role %q not found in workspace or global", name)
	}

	// If both exist, merge: workspace overrides global
	if workspaceCfg != nil && globalCfg != nil {
		merged := MergeRoleConfig(*globalCfg, *workspaceCfg)
		return &merged, nil
	}

	// Return whichever was found
	if workspaceCfg != nil {
		return workspaceCfg, nil
	}
	return globalCfg, nil
}

// List returns metadata for all roles across both directories.
// Roles that exist in both workspace and global appear once (workspace source).
func (rm *RoleManager) List() ([]RoleMeta, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	seen := make(map[string]bool)
	var result []RoleMeta

	// Scan global directory
	if rm.globalDir != "" {
		entries, err := os.ReadDir(rm.globalDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := RoleFileFromPath(entry.Name())
				if strings.HasSuffix(entry.Name(), ".yaml") && !seen[name] {
					seen[name] = true
					meta, err := RoleMetaFromFile(filepath.Join(rm.globalDir, entry.Name()), "global")
					if err == nil {
						result = append(result, meta)
					}
				}
			}
		}
	}

	// Scan workspace directory (overrides global source tag)
	if rm.workspaceDir != "" {
		entries, err := os.ReadDir(rm.workspaceDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := RoleFileFromPath(entry.Name())
				if strings.HasSuffix(entry.Name(), ".yaml") {
					meta, err := RoleMetaFromFile(filepath.Join(rm.workspaceDir, entry.Name()), "workspace")
					if err == nil {
						if seen[name] {
							// Update existing entry to show workspace source
							for i, existing := range result {
								if existing.Name == name {
									result[i] = meta
									break
								}
							}
						} else {
							seen[name] = true
							result = append(result, meta)
						}
					}
				}
			}
		}
	}

	return result, nil
}

// Save persists a role config to either the workspace or global directory.
// When workspaceDir is configured, it always saves to workspace regardless
// of the source parameter. When workspaceDir is not set, source "global" saves
// to global, and any other value also defaults to global.
func (rm *RoleManager) Save(role RoleConfig, source string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	name := strings.TrimSpace(role.Name)
	if err := rm.validateName(name); err != nil {
		return err
	}

	// When workspaceDir is set, always save to workspace
	if rm.workspaceDir != "" {
		return SaveRoleToFile(rm.workspaceDir, role)
	}

	return SaveRoleToFile(rm.globalDir, role)
}

// Delete removes a role. It attempts to delete from workspace first, then global.
// Returns an error if the role is not found in either location.
func (rm *RoleManager) Delete(name string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	name = strings.TrimSpace(strings.ToLower(name))
	if err := rm.validateName(name); err != nil {
		return err
	}

	deleted := false

	// Try workspace first
	if rm.workspaceDir != "" {
		path := filepath.Join(rm.workspaceDir, name+".yaml")
		if err := os.Remove(path); err == nil {
			deleted = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("delete role from workspace: %w", err)
		}
	}

	// Try global
	globalPath := filepath.Join(rm.globalDir, name+".yaml")
	if err := os.Remove(globalPath); err == nil {
		deleted = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("delete role from global: %w", err)
	}

	if !deleted {
		return fmt.Errorf("role %q not found in workspace or global", name)
	}

	return nil
}

// Exists checks whether a role with the given name is available in either
// workspace or global directories.
func (rm *RoleManager) Exists(name string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	name = strings.TrimSpace(strings.ToLower(name))
	if !isValidRoleName(name) {
		return false
	}

	// Check workspace
	if rm.workspaceDir != "" {
		if _, err := os.Stat(filepath.Join(rm.workspaceDir, name+".yaml")); err == nil {
			return true
		}
	}

	// Check global
	if _, err := os.Stat(filepath.Join(rm.globalDir, name+".yaml")); err == nil {
		return true
	}

	return false
}

// GlobalDir returns the global roles directory path.
func (rm *RoleManager) GlobalDir() string {
	return rm.globalDir
}

// WorkspaceDir returns the workspace roles directory path (may be empty).
func (rm *RoleManager) WorkspaceDir() string {
	return rm.workspaceDir
}

// LoadRaw loads a role YAML from a specific directory and returns the raw config
// without merging. Useful for editing/saving workflows.
func (rm *RoleManager) LoadRaw(name, source string) (*RoleConfig, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	name = strings.TrimSpace(strings.ToLower(name))
	if err := rm.validateName(name); err != nil {
		return nil, err
	}

	var dir string
	switch source {
	case "workspace":
		if rm.workspaceDir == "" {
			return nil, fmt.Errorf("no workspace directory configured")
		}
		dir = rm.workspaceDir
	case "global":
		dir = rm.globalDir
	default:
		return nil, fmt.Errorf("invalid source %q (must be 'workspace' or 'global')", source)
	}

	path := filepath.Join(dir, name+".yaml")
	return LoadRoleFromFile(path)
}

// EncodeRoleToYAML marshals a RoleConfig to YAML bytes.
func EncodeRoleToYAML(cfg RoleConfig) ([]byte, error) {
	return yaml.Marshal(&cfg)
}

// DecodeRoleFromYAML unmarshals YAML bytes into a RoleConfig.
func DecodeRoleFromYAML(data []byte) (*RoleConfig, error) {
	var cfg RoleConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode role YAML: %w", err)
	}
	return &cfg, nil
}
