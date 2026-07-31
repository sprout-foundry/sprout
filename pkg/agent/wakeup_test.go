package agent

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestLifetimeCtx_LazilyInitialized tests that LifetimeCtx is created on
// first access and returns the same context on subsequent calls.
func TestLifetimeCtx_LazilyInitialized(t *testing.T) {
	NewTestStateDir(t)
	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	ctx1 := a.LifetimeCtx()
	if ctx1 == nil {
		t.Fatal("LifetimeCtx() returned nil")
	}
	select {
	case <-ctx1.Done():
		t.Fatal("fresh LifetimeCtx should not be done")
	default:
	}

	// Second call returns the same context.
	ctx2 := a.LifetimeCtx()
	if ctx1 != ctx2 {
		t.Fatal("LifetimeCtx() should return the same context on repeated calls")
	}
}

// TestLifetimeCtx_CancelledByShutdown tests that Shutdown cancels the
// lifetime context so background watchers stop.
func TestLifetimeCtx_CancelledByShutdown(t *testing.T) {
	NewTestStateDir(t)
	a := newTestAgent(t)

	ctx := a.LifetimeCtx()
	if ctx == nil {
		t.Fatal("LifetimeCtx() returned nil")
	}

	a.Shutdown()

	select {
	case <-ctx.Done():
		// Expected — context should be cancelled after Shutdown.
	case <-time.After(time.Second):
		t.Fatal("LifetimeCtx was not cancelled after Shutdown()")
	}
}

// TestTryAutoResume_NoNotifications tests that TryAutoResume returns false
// and does nothing when there are no pending notifications.
func TestTryAutoResume_NoNotifications(t *testing.T) {
	a := newTestAgentWithWakeup(t, true)
	t.Cleanup(func() { a.Shutdown() })

	resumed := a.TryAutoResume()
	if resumed {
		t.Fatal("TryAutoResume should return false when no notifications pending")
	}
	if a.HasPendingNotifications() {
		t.Fatal("should have no pending notifications")
	}
}

// TestTryAutoResume_WithNotifications tests that TryAutoResume drains
// notifications and returns true when wakeup is enabled and there are
// pending notifications.
func TestTryAutoResume_WithNotifications(t *testing.T) {
	a := newTestAgentWithWakeup(t, true)
	t.Cleanup(func() { a.Shutdown() })

	a.QueueNotification(Notification{
		Content:   "Background task completed",
		SessionID: "test-session",
		Kind:      NotifShellBg,
	})

	if !a.HasPendingNotifications() {
		t.Fatal("notification should be queued")
	}

	resumed := a.TryAutoResume()
	if !resumed {
		t.Fatal("TryAutoResume should return true when notifications are pending")
	}

	// Notifications should be drained.
	if a.HasPendingNotifications() {
		t.Fatal("notifications should be drained after TryAutoResume")
	}
}

// TestTryAutoResume_WakeupDisabled tests that TryAutoResume does nothing
// when wakeup is disabled, and the notification remains pending.
func TestTryAutoResume_WakeupDisabled(t *testing.T) {
	a := newTestAgentWithWakeup(t, false)
	t.Cleanup(func() { a.Shutdown() })

	a.QueueNotification(Notification{
		Content:   "Background task completed",
		SessionID: "test-session",
		Kind:      NotifShellBg,
	})

	resumed := a.TryAutoResume()
	if resumed {
		t.Fatal("TryAutoResume should return false when wakeup is disabled")
	}
	if !a.HasPendingNotifications() {
		t.Fatal("notification should still be pending when wakeup is disabled")
	}
}

// TestTryAutoResume_BudgetExhausted tests that the wakeup budget is
// respected — after MaxResumesPerSession resumes, TryAutoResume stops.
func TestTryAutoResume_BudgetExhausted(t *testing.T) {
	NewTestStateDir(t)
	a := newTestAgent(t)
	t.Cleanup(func() { a.Shutdown() })

	// Enable wakeup with a budget of 1 resume.
	if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Wakeup = configuration.WakeupConfig{
			Enabled:              true,
			MaxResumesPerSession: 1,
			MaxTokensPerSession:  10000,
		}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// First notification — should resume (budget = 1).
	a.QueueNotification(Notification{
		Content:   "First background task completed",
		SessionID: "test-session-1",
		Kind:      NotifShellBg,
	})

	if !a.TryAutoResume() {
		t.Fatal("first TryAutoResume should succeed with budget remaining")
	}

	// Give the goroutine a moment to run.
	time.Sleep(200 * time.Millisecond)

	// Second notification — should NOT resume (budget exhausted).
	a.QueueNotification(Notification{
		Content:   "Second background task completed",
		SessionID: "test-session-2",
		Kind:      NotifShellBg,
	})

	if a.TryAutoResume() {
		t.Fatal("second TryAutoResume should fail — budget exhausted")
	}
	if !a.HasPendingNotifications() {
		t.Fatal("second notification should still be pending when budget exhausted")
	}
}

// TestDefaultWakeupConfig_EnabledByDefault tests that wakeup is enabled
// by default so the feature works out of the box.
func TestDefaultWakeupConfig_EnabledByDefault(t *testing.T) {
	cfg := configuration.DefaultWakeupConfig()
	if !cfg.Enabled {
		t.Fatal("DefaultWakeupConfig should have Enabled=true")
	}
	if cfg.MaxTokensPerSession <= 0 {
		t.Fatal("MaxTokensPerSession should be positive")
	}
	if cfg.MaxResumesPerSession <= 0 {
		t.Fatal("MaxResumesPerSession should be positive")
	}
}

// newTestAgentWithWakeup creates a test agent and sets the wakeup config
// to enabled or disabled based on the parameter.
func newTestAgentWithWakeup(t *testing.T, enabled bool) *Agent {
	t.Helper()
	// Isolate the state directory so session files don't leak into the
	// real ~/.sprout/sessions.
	NewTestStateDir(t)
	a := newTestAgent(t)
	if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Wakeup = configuration.WakeupConfig{
			Enabled:              enabled,
			MaxResumesPerSession: 10,
			MaxTokensPerSession:  5000,
		}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}
	return a
}
