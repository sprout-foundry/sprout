package configuration

// manager_verbosity.go — the narrow verbosity surface the console keymap
// (Alt+V) toggles through. pkg/console may not import this package (it
// is the leaf UI package the core imports), so the keymap handler
// programs against console.OutputVerbosityToggler and this pair of
// methods is what a *Manager satisfies on that interface.

// CurrentOutputVerbosity returns the live output.verbosity value, or ""
// when no config is loaded.
func (m *Manager) CurrentOutputVerbosity() string {
	cfg := m.GetConfig()
	if cfg == nil {
		return ""
	}
	return cfg.OutputVerbosity
}

// SetOutputVerbosity mutates the live config's output.verbosity without
// persisting it, mirroring the UpdateConfigNoSave semantics the keymap
// toggle used directly.
func (m *Manager) SetOutputVerbosity(verbosity string) error {
	return m.UpdateConfigNoSave(func(c *Config) error {
		c.OutputVerbosity = verbosity
		return nil
	})
}
