package configuration

import (
	"testing"
)

// TestApplyDaemonMultiSessionDefault_SetsMissingKey verifies the
// migration flips the rollout default ON: a fresh config (no
// daemon_multi_session key) gets daemon_multi_session=true after
// applyV2Defaults runs. This is the SP-118 Phase 4 default-on
// behavior.
func TestApplyDaemonMultiSessionDefault_SetsMissingKey(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	cfg := mgr.GetConfig()
	if cfg == nil {
		t.Fatal("GetConfig returned nil")
	}
	if !cfg.DaemonMultiSession {
		t.Fatalf("default DaemonMultiSession = false, want true (Phase 4 rollout default)")
	}
}

// TestApplyDaemonMultiSessionDefault_PreservesUserFalse verifies the
// rollout escape hatch: an operator who sets daemon_multi_session=false
// in config keeps that value across loads (migration is non-destructive).
func TestApplyDaemonMultiSessionDefault_PreservesUserFalse(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	// Write a config with daemon_multi_session=false explicitly.
	if err := mgr.UpdateConfig(func(c *Config) error {
		c.DaemonMultiSession = false
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if err := mgr.UpdateConfig(func(c *Config) error {
		c.DaemonMultiSession = false
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if err := mgr.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	cfg := mgr.GetConfig()
	if cfg.DaemonMultiSession {
		t.Fatal("explicit false override did not stick after SaveConfig")
	}
}

// TestApplyDaemonMultiSessionDefault_PreservesUserTrue verifies the
// explicit-true case: a user who sets daemon_multi_session=true keeps
// that value. Mostly a regression guard against the migration
// accidentally toggling back to a hardcoded default.
func TestApplyDaemonMultiSessionDefault_PreservesUserTrue(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfig(func(c *Config) error {
		c.DaemonMultiSession = true
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !mgr.GetConfig().DaemonMultiSession {
		t.Fatal("UpdateConfig did not stick: DaemonMultiSession = false after setting true")
	}
}