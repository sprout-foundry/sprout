package configuration

import "fmt"

// manager_roles.go — per-role model selection accessors on the Manager.
// Role storage, alias fallback and field-wise resolution live on Config
// (config_roles.go); these methods expose the stored selection through the
// Manager with persistence.

// GetRole returns the stored selection for a role (the zero RoleConfig
// when the role is unset). It returns the raw stored entry without
// alias or conversation fallback; use Config.ResolveRole for the
// effective (provider, model) pair.
func (m *Manager) GetRole(name string) RoleConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.GetRole(name)
}

// SetRole stores the per-role selection and persists it to disk. The
// "test" provider is rejected explicitly (an error, the same contract as
// SetProvider and SetModelForProvider — a test provider is for in-process
// testing only and must not leak into the persisted config); Config.SetRole
// also drops it at the storage level as defense-in-depth. Surfacing it as an
// error here keeps the Manager consistent and avoids a silent no-op.
func (m *Manager) SetRole(name string, rc RoleConfig) error {
	if rc.Provider == "test" {
		return fmt.Errorf("test provider cannot be persisted as the %q role provider", name)
	}
	m.mu.Lock()
	m.config.SetRole(name, rc)
	m.mu.Unlock()
	return m.SaveConfig()
}
