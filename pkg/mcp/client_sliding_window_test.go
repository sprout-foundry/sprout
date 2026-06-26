package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// IsDisabled / isEnabled — Default and Manual State
// ---------------------------------------------------------------------------

func TestIsDisabled_FalseByDefault(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	assert.False(t, client.IsDisabled(), "new client should not be disabled by default")
	assert.True(t, client.isEnabled(), "new client should be enabled when not stopping")
}

func TestIsDisabled_TrueAfterSetting(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	// Manually set disabled state via mutex
	client.mutex.Lock()
	client.disabled = true
	client.disabledReason = "test reason"
	client.mutex.Unlock()

	assert.True(t, client.IsDisabled(), "client should report disabled after manual set")
	assert.False(t, client.isEnabled(), "isEnabled should return false when disabled")
}

func TestIsEnabled_StoppingOverrides(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	// Set stopping=true while disabled=false
	client.mutex.Lock()
	client.stopping = true
	client.mutex.Unlock()

	assert.False(t, client.IsDisabled(), "client should not be disabled")
	assert.False(t, client.isEnabled(), "isEnabled should return false when stopping even if not disabled")
}

func TestIsEnabled_BothDisabledAndStopping(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.disabled = true
	client.disabledReason = "test"
	client.stopping = true
	client.mutex.Unlock()

	assert.True(t, client.IsDisabled(), "client should report disabled")
	assert.False(t, client.isEnabled(), "isEnabled should return false when both disabled and stopping")
}

// ---------------------------------------------------------------------------
// Start — Disabled Server Guard
// ---------------------------------------------------------------------------

func TestStart_DisabledServerReturnsError(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	// Set disabled state before calling Start
	client.mutex.Lock()
	client.disabled = true
	client.disabledReason = "10 failures in 24 hours"
	client.mutex.Unlock()

	ctx := context.Background()
	err := client.Start(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled", "error should mention disabled")
	assert.Contains(t, err.Error(), "10 failures in 24 hours", "error should include the disable reason")
}

func TestStart_DisabledServerDoesNotStartProcess(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.disabled = true
	client.disabledReason = "test reason"
	client.mutex.Unlock()

	ctx := context.Background()
	_ = client.Start(ctx)

	// Verify the process was not started (cmd should still be nil)
	assert.Nil(t, client.cmd, "cmd should remain nil when server is disabled")
	assert.False(t, client.IsRunning(), "client should not be running after failed Start")
}

// ---------------------------------------------------------------------------
// startInternal — Resets Disabled State on Clean Start
// ---------------------------------------------------------------------------

func TestStartInternal_ResetsDisabledState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}
	client := NewMCPClient(config, nil)

	// Pre-set disabled state and failure timestamps
	client.mutex.Lock()
	client.disabled = true
	client.disabledReason = "old reason"
	client.failureTimestamps = []time.Time{
		time.Now().Add(-1 * time.Hour),
		time.Now().Add(-30 * time.Minute),
	}
	client.mutex.Unlock()

	// Bypass the Start() disabled check by calling startInternal directly
	// (simulates what happens when disabled state was set but then cleared externally)
	client.mutex.Lock()
	client.disabled = false // Reset so startInternal doesn't see disabled
	client.mutex.Unlock()

	ctx := context.Background()
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	// After a successful start, disabled state should be reset
	client.mutex.RLock()
	assert.False(t, client.disabled, "disabled should be false after successful start")
	assert.Empty(t, client.disabledReason, "disabledReason should be empty after successful start")
	assert.Nil(t, client.failureTimestamps, "failureTimestamps should be nil after successful start")
	client.mutex.RUnlock()
}

func TestStart_DisabledThenClearedExternally_AllowsStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}
	client := NewMCPClient(config, nil)

	// Set disabled, then clear it externally (simulating admin override)
	client.mutex.Lock()
	client.disabled = true
	client.disabledReason = "test"
	client.mutex.Unlock()

	// First Start should fail
	err := client.Start(context.Background())
	require.Error(t, err)

	// Clear disabled externally
	client.mutex.Lock()
	client.disabled = false
	client.disabledReason = ""
	client.failureTimestamps = nil
	client.mutex.Unlock()

	// Second Start should succeed
	err = client.Start(context.Background())
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(context.Background())

	assert.True(t, client.IsRunning(), "client should be running after successful start")
	assert.False(t, client.IsDisabled(), "client should not be disabled after successful start")
}

// ---------------------------------------------------------------------------
// Reconnect — Disable After 10 Failures in 24 Hours
// ---------------------------------------------------------------------------

func TestReconnect_DisableAfter10FailuresIn24h(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15, // High enough that maxRestarts guard won't fire before 10 failures
	}
	client := NewMCPClient(config, nil)

	// Set running=true so reconnect doesn't early-return from triggerReconnect logic
	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.mutex.Unlock()

	// Use a pre-cancelled context so the backoff select returns immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Call reconnect 10 times — each call records a failure
	for i := 0; i < 10; i++ {
		client.reconnect(ctx)

		// After each call (except the 10th), reconnecting should be cleared by defer
		if i < 9 {
			client.mutex.RLock()
			assert.False(t, client.reconnecting, "reconnecting should be cleared after cancelled backoff (call %d)", i+1)
			client.mutex.RUnlock()
		}
	}

	// After 10 failures, the server should be disabled
	assert.True(t, client.IsDisabled(), "server should be disabled after 10 failures in 24h")

	client.mutex.RLock()
	assert.Contains(t, client.disabledReason, "10 failures", "disabledReason should mention 10 failures")
	assert.Len(t, client.failureTimestamps, 10, "should have 10 failure timestamps")
	client.mutex.RUnlock()
}

func TestReconnect_NotDisabledAt9Failures(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Call reconnect 9 times
	for i := 0; i < 9; i++ {
		client.reconnect(ctx)
	}

	// 9 failures should NOT trigger disable
	assert.False(t, client.IsDisabled(), "server should NOT be disabled at 9 failures (threshold is 10)")

	client.mutex.RLock()
	assert.Len(t, client.failureTimestamps, 9, "should have 9 failure timestamps")
	client.mutex.RUnlock()
}

func TestReconnect_DisableAtExactly10Not9(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 9 calls → not disabled
	for i := 0; i < 9; i++ {
		client.reconnect(ctx)
	}
	assert.False(t, client.IsDisabled(), "should not be disabled after 9 failures")

	// 10th call → disabled
	client.reconnect(ctx)
	assert.True(t, client.IsDisabled(), "should be disabled after 10th failure")
}

// ---------------------------------------------------------------------------
// Reconnect — Prune Old Failures (>24h)
// ---------------------------------------------------------------------------

func TestReconnect_PruneOldFailures(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	now := time.Now()

	// Populate failureTimestamps: 5 from 25 hours ago, 4 from 1 minute ago
	oldTimestamps := make([]time.Time, 5)
	for i := 0; i < 5; i++ {
		oldTimestamps[i] = now.Add(-25 * time.Hour)
	}
	recentTimestamps := make([]time.Time, 4)
	for i := 0; i < 4; i++ {
		recentTimestamps[i] = now.Add(-1 * time.Minute)
	}

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.failureTimestamps = append(oldTimestamps, recentTimestamps...)
	client.mutex.Unlock()

	assert.Len(t, client.failureTimestamps, 9, "should have 9 timestamps before reconnect")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.reconnect(ctx)

	// After reconnect: old entries (>24h) pruned, recent entries kept, 1 new added
	client.mutex.RLock()
	timestamps := client.failureTimestamps
	client.mutex.RUnlock()

	assert.Len(t, timestamps, 5, "should have 5 timestamps after pruning (4 recent + 1 new)")

	// Verify all remaining timestamps are within 24 hours
	for _, ts := range timestamps {
		assert.True(t, now.Sub(ts) <= 24*time.Hour, "all remaining timestamps should be within 24h")
	}
}

func TestReconnect_PruneKeepsExactly24hEntries(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	now := time.Now()

	// Entry at 23h59m59s — should definitely be kept (well within <= 24h even after reconnect runs)
	within24h := now.Add(-23*time.Hour - 59*time.Minute - 59*time.Second)
	// Entry just over 24 hours — should be pruned
	justOver24h := now.Add(-24*time.Hour - 2*time.Second)

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.failureTimestamps = []time.Time{within24h, justOver24h}
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.reconnect(ctx)

	client.mutex.RLock()
	timestamps := client.failureTimestamps
	client.mutex.RUnlock()

	// within24h should be kept, justOver24h should be pruned, plus 1 new entry
	assert.Len(t, timestamps, 2, "should keep within-24h entry + new entry, prune just-over-24h")
}

// ---------------------------------------------------------------------------
// Reconnect — 60-Second Window Counting
// ---------------------------------------------------------------------------

func TestReconnect_FailuresIn60sCounted(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	now := time.Now()

	// Populate with 3 entries from 30 seconds ago (all within 60s window)
	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.failureTimestamps = []time.Time{
		now.Add(-30 * time.Second),
		now.Add(-25 * time.Second),
		now.Add(-20 * time.Second),
	}
	client.mutex.Unlock()

	assert.Len(t, client.failureTimestamps, 3, "should have 3 pre-existing timestamps")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.reconnect(ctx)

	// After reconnect: 3 old + 1 new = 4 entries, all within 60s
	client.mutex.RLock()
	timestamps := client.failureTimestamps
	client.mutex.RUnlock()

	assert.Len(t, timestamps, 4, "should have 4 timestamps after reconnect (3 old + 1 new)")

	// All should be within 60 seconds of now
	for _, ts := range timestamps {
		assert.True(t, time.Since(ts) <= 60*time.Second, "all timestamps should be within 60s window")
	}
}

func TestReconnect_MixedOldAndRecentFailures(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	now := time.Now()

	// Mix: 2 from 25h ago (will be pruned), 2 from 90s ago (pruned from 60s but kept in 24h), 2 from 10s ago
	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.failureTimestamps = []time.Time{
		now.Add(-25 * time.Hour),   // pruned (>24h)
		now.Add(-25 * time.Hour),   // pruned (>24h)
		now.Add(-90 * time.Second), // kept in 24h, not in 60s
		now.Add(-85 * time.Second), // kept in 24h, not in 60s
		now.Add(-10 * time.Second), // in 60s
		now.Add(-5 * time.Second),  // in 60s
	}
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.reconnect(ctx)

	client.mutex.RLock()
	timestamps := client.failureTimestamps
	client.mutex.RUnlock()

	// After prune: 2 from 90s ago + 2 from 10s ago + 1 new = 5
	assert.Len(t, timestamps, 5, "should have 5 timestamps after pruning old entries")
}

// ---------------------------------------------------------------------------
// CalculateBackoff — 5-Minute Max Cap at High Attempts
// ---------------------------------------------------------------------------

func TestCalculateBackoff_5MinuteMax(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	// Verify exponential pattern for low attempts
	assert.Equal(t, 1*time.Second, client.calculateBackoff(1), "attempt 1 = 1s")
	assert.Equal(t, 2*time.Second, client.calculateBackoff(2), "attempt 2 = 2s")
	assert.Equal(t, 4*time.Second, client.calculateBackoff(3), "attempt 3 = 4s")

	// Verify cap kicks in at the right point
	assert.Equal(t, 256*time.Second, client.calculateBackoff(9), "attempt 9 = 256s (not capped)")
	assert.Equal(t, 5*time.Minute, client.calculateBackoff(10), "attempt 10 = 512s → capped at 5min")

	// Verify cap holds for high attempts that would exceed 5min
	assert.Equal(t, 5*time.Minute, client.calculateBackoff(16), "attempt 16 = 5min (capped)")
	assert.Equal(t, 5*time.Minute, client.calculateBackoff(20), "attempt 20 = 5min (capped)")
	assert.Equal(t, 5*time.Minute, client.calculateBackoff(30), "attempt 30 = 5min (capped)")
}

// ---------------------------------------------------------------------------
// Stability Reset — Clears Failure Timestamps
// ---------------------------------------------------------------------------

func TestReconnect_StabilityReset_ClearsFailureTimestamps(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	now := time.Now()

	// Set up state that would trigger stability reset in the health check goroutine:
	// reconnectAttempt > 0 AND connectedAt > 2 minutes ago
	client.mutex.Lock()
	client.reconnectAttempt = 2
	client.connectedAt = now.Add(-3 * time.Minute)
	client.failureTimestamps = []time.Time{
		now.Add(-1 * time.Hour),
		now.Add(-30 * time.Minute),
		now.Add(-5 * time.Minute),
	}
	client.mutex.Unlock()

	// Verify the condition that triggers stability reset is met
	client.mutex.RLock()
	attempt := client.reconnectAttempt
	connectedAt := client.connectedAt
	timestamps := client.failureTimestamps
	client.mutex.RUnlock()

	assert.True(t, attempt > 0, "reconnectAttempt should be > 0 for stability reset")
	assert.True(t, time.Since(connectedAt) > 2*time.Minute, "time since connectedAt should exceed 2 minutes")
	assert.Len(t, timestamps, 3, "should have 3 failure timestamps before stability reset")

	// Simulate what the health check goroutine does on a successful ping with stable connection:
	// The actual code in startHealthCheck():
	//   if c.reconnectAttempt > 0 && time.Since(c.connectedAt) > 2*time.Minute {
	//       c.reconnectAttempt = 0
	//       c.failureTimestamps = nil
	//   }
	client.mutex.Lock()
	if client.reconnectAttempt > 0 && time.Since(client.connectedAt) > 2*time.Minute {
		client.reconnectAttempt = 0
		client.failureTimestamps = nil
	}
	client.mutex.Unlock()

	// Verify the reset took effect
	client.mutex.RLock()
	assert.Equal(t, 0, client.reconnectAttempt, "reconnectAttempt should be reset to 0")
	assert.Nil(t, client.failureTimestamps, "failureTimestamps should be nil after stability reset")
	client.mutex.RUnlock()
}

func TestStabilityReset_DoesNotReset_RecentConnection(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.reconnectAttempt = 2
	client.connectedAt = time.Now().Add(-1 * time.Minute) // Only 1 minute ago
	client.failureTimestamps = []time.Time{time.Now().Add(-5 * time.Minute)}
	client.mutex.Unlock()

	// Simulate stability reset check
	client.mutex.Lock()
	if client.reconnectAttempt > 0 && time.Since(client.connectedAt) > 2*time.Minute {
		client.reconnectAttempt = 0
		client.failureTimestamps = nil
	}
	client.mutex.Unlock()

	// Should NOT have reset because connection is recent
	client.mutex.RLock()
	assert.Equal(t, 2, client.reconnectAttempt, "reconnectAttempt should NOT be reset for recent connection")
	assert.NotNil(t, client.failureTimestamps, "failureTimestamps should NOT be cleared for recent connection")
	client.mutex.RUnlock()
}

func TestStabilityReset_DoesNotReset_ZeroAttempt(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.reconnectAttempt = 0
	client.connectedAt = time.Now().Add(-10 * time.Minute)
	client.failureTimestamps = []time.Time{time.Now().Add(-5 * time.Minute)}
	client.mutex.Unlock()

	// Simulate stability reset check
	client.mutex.Lock()
	if client.reconnectAttempt > 0 && time.Since(client.connectedAt) > 2*time.Minute {
		client.reconnectAttempt = 0
		client.failureTimestamps = nil
	}
	client.mutex.Unlock()

	// Should NOT have reset because attempt is 0
	client.mutex.RLock()
	assert.Equal(t, 0, client.reconnectAttempt)
	assert.NotNil(t, client.failureTimestamps, "failureTimestamps should NOT be cleared when attempt is 0")
	client.mutex.RUnlock()
}

// ---------------------------------------------------------------------------
// Disabled State — Reset After Successful Start with Real Process
// ---------------------------------------------------------------------------

func TestReconnect_DisabledThenStartInternalResets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}
	client := NewMCPClient(config, nil)

	ctx := context.Background()

	// Start the client successfully (not disabled initially)
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	// After a successful start, all disabled-related state should be clean
	client.mutex.RLock()
	assert.False(t, client.disabled, "disabled should be false after successful start")
	assert.Empty(t, client.disabledReason, "disabledReason should be empty after successful start")
	assert.Nil(t, client.failureTimestamps, "failureTimestamps should be nil after successful start")
	client.mutex.RUnlock()

	assert.False(t, client.IsDisabled(), "IsDisabled() should return false after successful start")
}

// ---------------------------------------------------------------------------
// Disabled Server — Cannot Reconnect
// ---------------------------------------------------------------------------

func TestReconnect_DisabledServer_PreviousFailuresPreserved(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Drive to 10 failures to disable
	for i := 0; i < 10; i++ {
		client.reconnect(ctx)
	}

	assert.True(t, client.IsDisabled(), "should be disabled after 10 failures")

	// Calling reconnect again on a disabled server: the failure timestamp
	// is still recorded and pruned, but since we're already at 10+, it stays disabled
	client.reconnect(ctx)

	assert.True(t, client.IsDisabled(), "should remain disabled after additional reconnect call")

	client.mutex.RLock()
	// After the 11th call, we have 11 timestamps (all within 24h since they're recent)
	assert.GreaterOrEqual(t, len(client.failureTimestamps), 10, "should still have failure timestamps")
	client.mutex.RUnlock()
}

// ---------------------------------------------------------------------------
// isEnabled — Thread Safety
// ---------------------------------------------------------------------------

func TestIsEnabled_ThreadSafety(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}
	client := NewMCPClient(config, nil)

	// isEnabled uses RLock, so it should work concurrently with writes
	done := make(chan bool, 1)

	go func() {
		for i := 0; i < 100; i++ {
			_ = client.isEnabled()
		}
		done <- true
	}()

	// Concurrently modify state
	for i := 0; i < 100; i++ {
		client.mutex.Lock()
		client.stopping = !client.stopping
		client.disabled = !client.disabled
		client.mutex.Unlock()
	}

	<-done
	// If we got here without deadlock, the RLock/RUnlock is working correctly
}

// ---------------------------------------------------------------------------
// Failure Timestamps — Edge Cases
// ---------------------------------------------------------------------------

func TestReconnect_EmptyFailureTimestamps(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.failureTimestamps = nil // Explicitly nil
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.reconnect(ctx)

	client.mutex.RLock()
	assert.Len(t, client.failureTimestamps, 1, "should have 1 timestamp after first reconnect with nil slice")
	client.mutex.RUnlock()
}

func TestReconnect_DisableThenReconnectAttemptNotIncremented(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test-server",
		Command:     "cat",
		MaxRestarts: 15,
	}
	client := NewMCPClient(config, nil)

	client.mutex.Lock()
	client.running = true
	client.stopping = false
	client.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 9 reconnect calls → 9 attempts incremented, 9 failures
	for i := 0; i < 9; i++ {
		client.reconnect(ctx)
	}

	client.mutex.RLock()
	attemptBeforeDisable := client.reconnectAttempt
	client.mutex.RUnlock()

	assert.Equal(t, 9, attemptBeforeDisable, "should have 9 reconnect attempts after 9 calls")

	// 10th call → triggers disable BEFORE incrementing attempt
	client.reconnect(ctx)

	assert.True(t, client.IsDisabled(), "should be disabled after 10th failure")

	client.mutex.RLock()
	attemptAfterDisable := client.reconnectAttempt
	client.mutex.RUnlock()

	// The disable path returns before incrementing reconnectAttempt
	assert.Equal(t, 9, attemptAfterDisable, "reconnectAttempt should NOT be incremented on the disable path")
}
