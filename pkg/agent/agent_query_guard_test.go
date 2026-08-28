package agent

import (
	"errors"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestTryBeginQueryAsRecordsOwner verifies that acquiring the guard with an
// explicit source records that source and a non-zero acquisition time on the
// guard owner.
func TestTryBeginQueryAsRecordsOwner(t *testing.T) {
	a := &Agent{}
	if err := a.TryBeginQueryAs(QuerySourceCLI); err != nil {
		t.Fatalf("TryBeginQueryAs(%q) error = %v, want nil", QuerySourceCLI, err)
	}
	defer a.EndQuery()

	owner := a.QueryGuardOwner()
	if owner.Source != QuerySourceCLI {
		t.Fatalf("owner.Source = %q, want %q", owner.Source, QuerySourceCLI)
	}
	if owner.StartedAt.IsZero() {
		t.Fatal("owner.StartedAt is zero; TryBeginQueryAs must record the acquisition time")
	}
	if !a.IsQueryInProgress() {
		t.Fatal("IsQueryInProgress() = false after TryBeginQueryAs, want true")
	}
}

// TestTryBeginQueryDefaultsToUnknownSource verifies the unqualified
// TryBeginQuery entry point records QuerySourceUnknown as the owner.
func TestTryBeginQueryDefaultsToUnknownSource(t *testing.T) {
	a := &Agent{}
	if err := a.TryBeginQuery(); err != nil {
		t.Fatalf("TryBeginQuery() error = %v, want nil", err)
	}
	defer a.EndQuery()

	if owner := a.QueryGuardOwner(); owner.Source != QuerySourceUnknown {
		t.Fatalf("owner.Source = %q, want %q", owner.Source, QuerySourceUnknown)
	}
}

// TestEndQueryResetsOwner verifies EndQuery clears both the guard flag and
// the recorded owner, and that the guard can be re-acquired afterwards.
func TestEndQueryResetsOwner(t *testing.T) {
	a := &Agent{}
	if err := a.TryBeginQueryAs(QuerySourceWebUI); err != nil {
		t.Fatalf("TryBeginQueryAs(%q) error = %v, want nil", QuerySourceWebUI, err)
	}
	a.EndQuery()

	if owner := a.QueryGuardOwner(); owner != (QueryGuardOwner{}) {
		t.Fatalf("owner after EndQuery = %+v, want zero value", owner)
	}
	if a.IsQueryInProgress() {
		t.Fatal("IsQueryInProgress() = true after EndQuery, want false")
	}

	// The guard must be re-acquirable after release.
	if err := a.TryBeginQuery(); err != nil {
		t.Fatalf("re-acquiring the guard after EndQuery failed: %v", err)
	}
	a.EndQuery()
}

// TestQueryGuardOwnerNilAgent verifies all guard accessors are safe on a nil
// *Agent: no panics, zero-value owner, and a real error from the acquisition
// path.
func TestQueryGuardOwnerNilAgent(t *testing.T) {
	var a *Agent

	if owner := a.QueryGuardOwner(); owner != (QueryGuardOwner{}) {
		t.Fatalf("nil agent QueryGuardOwner() = %+v, want zero value", owner)
	}

	err := a.TryBeginQueryAs(QuerySourceCLI)
	if err == nil {
		t.Fatal("nil agent TryBeginQueryAs succeeded, want error")
	}
	if errors.Is(err, ErrQueryInProgress) {
		t.Fatalf("nil agent TryBeginQueryAs returned %v, want a nil-agent error", err)
	}

	// These must not panic on a nil receiver.
	a.EndQuery()
	if a.IsQueryInProgress() {
		t.Fatal("nil agent IsQueryInProgress() = true, want false")
	}
}

// TestTryBeginQueryAsRejectedDoesNotClobberOwner verifies a rejected
// acquisition (guard already held) leaves the active owner untouched —
// a second caller must not overwrite the holder's source or timestamp.
func TestTryBeginQueryAsRejectedDoesNotClobberOwner(t *testing.T) {
	a := &Agent{}
	if err := a.TryBeginQueryAs(QuerySourceWebUI); err != nil {
		t.Fatalf("TryBeginQueryAs(%q) error = %v, want nil", QuerySourceWebUI, err)
	}
	original := a.QueryGuardOwner()

	err := a.TryBeginQueryAs(QuerySourceCLI)
	if !errors.Is(err, ErrQueryInProgress) {
		t.Fatalf("second TryBeginQueryAs error = %v, want ErrQueryInProgress", err)
	}

	owner := a.QueryGuardOwner()
	if owner.Source != QuerySourceWebUI {
		t.Fatalf("rejected caller clobbered the owner: Source = %q, want %q", owner.Source, QuerySourceWebUI)
	}
	if !owner.StartedAt.Equal(original.StartedAt) {
		t.Fatalf("rejected caller clobbered StartedAt: %v, want %v", owner.StartedAt, original.StartedAt)
	}
	a.EndQuery()
}

// TestProcessQueryAsThreadsSourceToGuard drives the full admitted-query path
// with an explicit source and verifies the failure-path cleanup: when
// prepareQueryRun fails (nil provider client), the owner defer must release
// the guard AND clear the recorded owner, leaving the agent reusable.
func TestProcessQueryAsThreadsSourceToGuard(t *testing.T) {
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
		workspaceRoot: t.TempDir(),
	}

	// A nil provider client makes prepareQueryRun fail after admission. The
	// owner defer must still release the guard and clear the threaded source.
	_, err := a.processQueryWithSeed(QuerySourceAutoResume, "query that fails during preparation")
	if err == nil {
		t.Fatal("processQueryWithSeed returned nil error with no provider client")
	}
	if a.IsQueryInProgress() {
		t.Fatal("admitted query failure left the query guard active")
	}
	if owner := a.QueryGuardOwner(); owner != (QueryGuardOwner{}) {
		t.Fatalf("owner after admitted-query failure = %+v, want zero value (Source = %q, want \"\")", owner, owner.Source)
	}
	if err := a.TryBeginQuery(); err != nil {
		t.Fatalf("query guard was not reusable after cleanup: %v", err)
	}
	a.EndQuery()
}
