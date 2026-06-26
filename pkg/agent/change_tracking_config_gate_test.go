package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// boolPtr returns a pointer to b. Used to populate the *bool fields of
// ChangeTrackingConfig in table-style tests.
func boolPtr(b bool) *bool { return &b }

// TestChangeTrackingConfigGate_DefaultNoConfig verifies the production
// default: an agent whose config has NO change_tracking section at all
// must end up with tracking ENABLED. This is the most common path —
// most users never touch change_tracking — so a regression here would
// silently disable the entire subsystem for the majority of installs.
//
// The config gate logic lives in isChangeTrackingEnabledByConfig: a
// nil ChangeTracking pointer means "use the default", and the default
// is enabled (the git-awareness guards protect committed work).
func TestChangeTrackingConfigGate_DefaultNoConfig(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// NewTestManager yields a fresh config with no change_tracking
	// section. Explicitly assert that to guard against a future
	// default that seeds ChangeTracking non-nil.
	cfg := mgr.GetConfig()
	if cfg.ChangeTracking != nil {
		t.Fatalf("precondition: fresh test config should have nil ChangeTracking, got %+v", cfg.ChangeTracking)
	}

	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: mgr,
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if !a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = false, want true (no change_tracking section ⇒ default enabled)")
	}
	if a.GetChangeTracker() == nil {
		t.Error("GetChangeTracker() = nil, want non-nil tracker (default is enabled, so a tracker must be created)")
	}
	if a.GetRevisionID() == "" {
		t.Error("GetRevisionID() = empty, want non-empty revision ID for an enabled tracker")
	}
}

// TestChangeTrackingConfigGate_ExplicitlyEnabled verifies that setting
// change_tracking.enabled = true explicitly matches the default
// behavior: tracking is on and a tracker is created.
func TestChangeTrackingConfigGate_ExplicitlyEnabled(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ChangeTracking = &configuration.ChangeTrackingConfig{Enabled: boolPtr(true)}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: mgr,
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if !a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = false, want true (change_tracking.enabled = true)")
	}
	if a.GetChangeTracker() == nil {
		t.Error("GetChangeTracker() = nil, want non-nil tracker")
	}
}

// TestChangeTrackingConfigGate_ExplicitlyDisabled verifies the
// kill-switch: when the user sets change_tracking.enabled = false the
// ENTIRE subsystem stays dormant. EnableChangeTracking must early-return
// before creating a tracker, so IsChangeTrackingEnabled() reports false
// and GetChangeTracker() is nil.
//
// This is the critical safety assertion: a broken implementation that
// ignored the gate would still create a tracker, and IsChangeTrackingEnabled
// would flip to true. Asserting both the method AND the nil tracker
// catches that regression.
func TestChangeTrackingConfigGate_ExplicitlyDisabled(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ChangeTracking = &configuration.ChangeTrackingConfig{Enabled: boolPtr(false)}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: mgr,
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = true, want false (change_tracking.enabled = false must gate the subsystem)")
	}
	if a.GetChangeTracker() != nil {
		t.Errorf("GetChangeTracker() = non-nil, want nil (disabled config must not create a tracker), revisionID=%q", a.GetRevisionID())
	}
	if a.GetRevisionID() != "" {
		t.Errorf("GetRevisionID() = %q, want empty (no tracker should exist)", a.GetRevisionID())
	}
}

// TestChangeTrackingConfigGate_NilConfigManager verifies the test path:
// an agent constructed without a configManager (the pattern used by
// most existing unit tests in this package) keeps tracking enabled.
// This preserves backward compatibility so the dozens of tests that
// call EnableChangeTracking without wiring up config don't break.
func TestChangeTrackingConfigGate_NilConfigManager(t *testing.T) {
	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: nil, // test path: no config manager
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if !a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = false, want true (nil configManager ⇒ test path ⇒ enabled)")
	}
	if a.GetChangeTracker() == nil {
		t.Error("GetChangeTracker() = nil, want non-nil tracker (test path enables tracking)")
	}
}
