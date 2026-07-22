package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestProcessQueryWithSeedRejectedCallerPreservesActiveTurnTimestamp(t *testing.T) {
	activeTimestamp := time.Date(2026, time.July, 22, 13, 40, 20, 0, time.FixedZone("CDT", -5*60*60))
	a := &Agent{turnTimestamp: activeTimestamp}
	if err := a.TryBeginQuery(); err != nil {
		t.Fatalf("admit active query: %v", err)
	}
	defer a.EndQuery()

	_, err := a.processQueryWithSeed("concurrent query")
	if !errors.Is(err, ErrQueryInProgress) {
		t.Fatalf("error = %v, want ErrQueryInProgress", err)
	}

	a.turnTimestampMu.RLock()
	got := a.turnTimestamp
	a.turnTimestampMu.RUnlock()
	if !got.Equal(activeTimestamp) {
		t.Fatalf("active timestamp changed: got %v, want %v", got, activeTimestamp)
	}
	if !a.IsQueryInProgress() {
		t.Fatal("rejected caller released the active query guard")
	}
	if err := a.TryBeginQuery(); !errors.Is(err, ErrQueryInProgress) {
		t.Fatalf("query guard accepted another caller after rejection: got %v, want ErrQueryInProgress", err)
	}
}

func TestProcessQueryWithSeedAdmittedFailureClearsTimestampAndReleasesGuard(t *testing.T) {
	configManager, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	disabled := false
	if err := configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ChangeTracking = &configuration.ChangeTrackingConfig{Enabled: &disabled}
		return nil
	}); err != nil {
		t.Fatalf("disable change tracking: %v", err)
	}

	a := &Agent{
		configManager: configManager,
		contextProfile: configuration.ContextProfile{
			SkipProactiveContext: true,
		},
		turnTimestamp: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		workspaceRoot: t.TempDir(),
	}

	// A nil provider client makes prepareQueryRun fail after admission and after
	// the per-turn timestamp is assigned. The owner defer must still clean up
	// both pieces of turn state.
	_, err := a.processQueryWithSeed("query that fails during preparation")
	if err == nil {
		t.Fatal("processQueryWithSeed returned nil error with no provider client")
	}
	if a.IsQueryInProgress() {
		t.Fatal("admitted query failure left the query guard active")
	}
	a.turnTimestampMu.RLock()
	got := a.turnTimestamp
	a.turnTimestampMu.RUnlock()
	if !got.IsZero() {
		t.Fatalf("admitted query failure left timestamp set: %v", got)
	}
	if err := a.TryBeginQuery(); err != nil {
		t.Fatalf("query guard was not reusable after cleanup: %v", err)
	}
	a.EndQuery()
}
