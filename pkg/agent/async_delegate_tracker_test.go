package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// NewAsyncDelegateTracker
// ---------------------------------------------------------------------------

func TestNewAsyncDelegateTracker(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	require.NotNil(t, tracker)
	assert.NotNil(t, tracker.entries)
	assert.Empty(t, tracker.entries)
}

// ---------------------------------------------------------------------------
// Start: success path
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_StartAndGetStatus_Completed(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{
		Prompt: "write tests",
		Role:   "tester",
	}

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{
			Summary:    "tests written",
			ExitStatus: "success",
			TokensUsed: 100,
		}, nil
	}

	require.NoError(t, tracker.Start("del-1", cfg, nil, runFn))

	// Immediately check — should be "running"
	status, result, found := tracker.GetStatus("del-1")
	require.True(t, found)
	assert.Equal(t, "running", status)
	assert.Nil(t, result)

	// Wait a tick for the goroutine to complete
	time.Sleep(50 * time.Millisecond)

	// Now check — should be "completed"
	status, result, found = tracker.GetStatus("del-1")
	require.True(t, found)
	assert.Equal(t, "completed", status)
	require.NotNil(t, result)
	assert.Equal(t, "tests written", result.Summary)
	assert.Equal(t, "success", result.ExitStatus)
	assert.Equal(t, 100, result.TokensUsed)
}

func TestAsyncDelegateTracker_Start_PublishesStartedEvent(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	bus := events.NewEventBus()

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	agent := &Agent{
		eventBus:      bus,
		delegateDepth: 0,
	}

	cfg := DelegateConfig{
		Prompt: "do a task",
		Role:   "coder",
	}

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("del-event", cfg, agent, runFn))

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeDelegateAsyncStarted, event.Type)
		data := event.Data.(map[string]interface{})
		assert.Equal(t, "del-event", data["delegate_id"])
		assert.Equal(t, "started", data["action"])
		assert.Equal(t, 1, data["depth"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for delegate_async_started event")
	}
}

func TestAsyncDelegateTracker_Start_NilAgent_NoPanic(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{
		Prompt: "test",
	}

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "ok", ExitStatus: "success"}, nil
	}

	// Should not panic even with nil agent
	require.NoError(t, tracker.Start("del-nil", cfg, nil, runFn))

	time.Sleep(50 * time.Millisecond)

	status, _, found := tracker.GetStatus("del-nil")
	require.True(t, found)
	assert.Equal(t, "completed", status)
}

// ---------------------------------------------------------------------------
// Start: failure path
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_StartWithFailure(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{
		Prompt: "fail this",
	}

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("something went wrong")
	}

	require.NoError(t, tracker.Start("del-fail", cfg, nil, runFn))

	time.Sleep(50 * time.Millisecond)

	status, result, found := tracker.GetStatus("del-fail")
	require.True(t, found)
	assert.Equal(t, "failed", status)
	require.NotNil(t, result)
	assert.Equal(t, "Delegate failed: something went wrong", result.Summary)
	assert.Equal(t, "error", result.ExitStatus)
	assert.Equal(t, "something went wrong", result.ErrorMessage)
}

func TestAsyncDelegateTracker_StartWithFailure_PublishesFailedEvent(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	bus := events.NewEventBus()

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	agent := &Agent{
		eventBus:      bus,
		delegateDepth: 1,
	}

	cfg := DelegateConfig{
		Prompt: "fail",
	}

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("boom")
	}

	require.NoError(t, tracker.Start("del-fail-event", cfg, agent, runFn))

	// First event is "started" — drain it
	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeDelegateAsyncStarted, event.Type)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for delegate_async_started event")
	}

	// Second event should be "failed"
	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeDelegateAsyncFailed, event.Type)
		data := event.Data.(map[string]interface{})
		assert.Equal(t, "del-fail-event", data["delegate_id"])
		assert.Equal(t, "failed", data["action"])
		assert.Equal(t, 2, data["depth"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for delegate_async_failed event")
	}
}

func TestAsyncDelegateTracker_StartWithSuccess_PublishesCompletedEvent(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	bus := events.NewEventBus()

	ch := bus.Subscribe("test-client")
	defer bus.Unsubscribe("test-client")

	agent := &Agent{
		eventBus:      bus,
		delegateDepth: 0,
	}

	cfg := DelegateConfig{
		Prompt: "succeed",
	}

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{
			Summary:    "task completed",
			ExitStatus: "success",
		}, nil
	}

	require.NoError(t, tracker.Start("del-ok-event", cfg, agent, runFn))

	// Wait for both the started event and the completed event
	_ = <-ch // started event

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeDelegateAsyncCompleted, event.Type)
		data := event.Data.(map[string]interface{})
		assert.Equal(t, "del-ok-event", data["delegate_id"])
		assert.Equal(t, "completed", data["action"])
		assert.Equal(t, "task completed", data["summary"])
		assert.Equal(t, 1, data["depth"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for delegate_async_completed event")
	}
}

// ---------------------------------------------------------------------------
// GetStatus: not found
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_GetStatus_NotFound(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	status, result, found := tracker.GetStatus("nonexistent")
	assert.False(t, found)
	assert.Empty(t, status)
	assert.Nil(t, result)
}

func TestAsyncDelegateTracker_GetStatus_EmptyID(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	status, result, found := tracker.GetStatus("")
	assert.False(t, found)
	assert.Empty(t, status)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_Cancel_Running(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{Prompt: "slow task"}

	// This runFn blocks until context is cancelled
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("del-cancel", cfg, nil, runFn))

	// Should be running
	status, _, found := tracker.GetStatus("del-cancel")
	require.True(t, found)
	assert.Equal(t, "running", status)

	// Cancel it
	ok := tracker.Cancel("del-cancel")
	assert.True(t, ok)

	// After cancellation, it should transition to "failed"
	time.Sleep(50 * time.Millisecond)

	status, _, found = tracker.GetStatus("del-cancel")
	require.True(t, found)
	assert.Equal(t, "failed", status)
}

func TestAsyncDelegateTracker_Cancel_NotFound(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	ok := tracker.Cancel("nonexistent")
	assert.False(t, ok)
}

func TestAsyncDelegateTracker_Cancel_AlreadyCompleted(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{Prompt: "quick task"}
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("del-done", cfg, nil, runFn))
	time.Sleep(50 * time.Millisecond)

	// Should be completed now
	status, _, found := tracker.GetStatus("del-done")
	require.True(t, found)
	assert.Equal(t, "completed", status)

	// Cancel on completed should return false
	ok := tracker.Cancel("del-done")
	assert.False(t, ok)
}

func TestAsyncDelegateTracker_Cancel_AlreadyFailed(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{Prompt: "fail"}
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("fail")
	}

	require.NoError(t, tracker.Start("del-failed", cfg, nil, runFn))
	time.Sleep(50 * time.Millisecond)

	status, _, found := tracker.GetStatus("del-failed")
	require.True(t, found)
	assert.Equal(t, "failed", status)

	ok := tracker.Cancel("del-failed")
	assert.False(t, ok)
}

func TestAsyncDelegateTracker_Cancel_MultipleCancelReturnsFalse(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{Prompt: "slow"}
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("del-multi-cancel", cfg, nil, runFn))

	ok1 := tracker.Cancel("del-multi-cancel")
	assert.True(t, ok1)

	// Second cancel should return false since status is no longer "running"
	// (the goroutine will have set it to "failed" after context cancellation)
	time.Sleep(50 * time.Millisecond)

	ok2 := tracker.Cancel("del-multi-cancel")
	assert.False(t, ok2)
}

// ---------------------------------------------------------------------------
// ListRunning
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_ListRunning_Empty(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	ids := tracker.ListRunning()
	assert.Empty(t, ids)
}

func TestAsyncDelegateTracker_ListRunning_MultipleDelegates(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{Prompt: "slow"}

	// Slow runFn that holds until context is done
	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("slow-1", cfg, nil, slowFn))
	require.NoError(t, tracker.Start("slow-2", cfg, nil, slowFn))
	require.NoError(t, tracker.Start("slow-3", cfg, nil, slowFn))

	time.Sleep(20 * time.Millisecond) // let goroutines start

	ids := tracker.ListRunning()
	require.Len(t, ids, 3)
	assert.Contains(t, ids, "slow-1")
	assert.Contains(t, ids, "slow-2")
	assert.Contains(t, ids, "slow-3")
}

func TestAsyncDelegateTracker_ListRunning_ExcludesCompleted(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("slow", DelegateConfig{Prompt: "slow"}, nil, slowFn))
	require.NoError(t, tracker.Start("fast", DelegateConfig{Prompt: "fast"}, nil, fastFn))

	time.Sleep(50 * time.Millisecond) // fast should complete

	ids := tracker.ListRunning()
	require.Len(t, ids, 1)
	assert.Equal(t, "slow", ids[0])
}

func TestAsyncDelegateTracker_ListRunning_ExcludesFailed(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	failFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("fail")
	}

	require.NoError(t, tracker.Start("slow", DelegateConfig{Prompt: "slow"}, nil, slowFn))
	require.NoError(t, tracker.Start("fail", DelegateConfig{Prompt: "fail"}, nil, failFn))

	time.Sleep(50 * time.Millisecond)

	ids := tracker.ListRunning()
	require.Len(t, ids, 1)
	assert.Equal(t, "slow", ids[0])
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_Cleanup_RemovesOldCompleted(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	// Fast function that completes immediately
	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("old-done", DelegateConfig{Prompt: "done"}, nil, fastFn))
	time.Sleep(50 * time.Millisecond) // let it complete

	// Cleanup with a very short TTL should remove it
	tracker.Cleanup(10 * time.Millisecond)

	_, _, found := tracker.GetStatus("old-done")
	assert.False(t, found)
}

func TestAsyncDelegateTracker_Cleanup_PreservesRunning(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("running", DelegateConfig{Prompt: "running"}, nil, slowFn))
	time.Sleep(20 * time.Millisecond)

	// Cleanup should NOT remove running entries
	tracker.Cleanup(10 * time.Millisecond)

	status, _, found := tracker.GetStatus("running")
	require.True(t, found)
	assert.Equal(t, "running", status)
}

func TestAsyncDelegateTracker_Cleanup_RemovesFailed(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	failFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("fail")
	}

	require.NoError(t, tracker.Start("old-fail", DelegateConfig{Prompt: "fail"}, nil, failFn))
	time.Sleep(50 * time.Millisecond) // let it fail

	// Cleanup with short TTL
	tracker.Cleanup(10 * time.Millisecond)

	_, _, found := tracker.GetStatus("old-fail")
	assert.False(t, found)
}

func TestAsyncDelegateTracker_Cleanup_RespectsTTL(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("recent", DelegateConfig{Prompt: "done"}, nil, fastFn))
	time.Sleep(50 * time.Millisecond) // let it complete

	// Cleanup with a very long TTL should NOT remove it (started less than 1h ago)
	tracker.Cleanup(1 * time.Hour)

	_, _, found := tracker.GetStatus("recent")
	assert.True(t, found)
}

func TestAsyncDelegateTracker_Cleanup_MixedEntries(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}
	failFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("err")
	}

	require.NoError(t, tracker.Start("running-1", DelegateConfig{Prompt: "slow"}, nil, slowFn))
	require.NoError(t, tracker.Start("completed-1", DelegateConfig{Prompt: "fast"}, nil, fastFn))
	require.NoError(t, tracker.Start("failed-1", DelegateConfig{Prompt: "fail"}, nil, failFn))

	time.Sleep(50 * time.Millisecond) // let fast and fail complete/fail

	// Cleanup with short TTL
	tracker.Cleanup(10 * time.Millisecond)

	// Running should remain
	s, _, found := tracker.GetStatus("running-1")
	require.True(t, found)
	assert.Equal(t, "running", s)

	// Completed should be removed
	_, _, found = tracker.GetStatus("completed-1")
	assert.False(t, found)

	// Failed should be removed
	_, _, found = tracker.GetStatus("failed-1")
	assert.False(t, found)
}

func TestAsyncDelegateTracker_Cleanup_EmptyTracker(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	// Should not panic
	tracker.Cleanup(1 * time.Second)
}

// ---------------------------------------------------------------------------
// WaitFor
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_WaitFor_Completed(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		time.Sleep(10 * time.Millisecond)
		return &DelegateResult{Summary: "done", ExitStatus: "success", TokensUsed: 50}, nil
	}

	require.NoError(t, tracker.Start("wait-ok", DelegateConfig{Prompt: "test"}, nil, fastFn))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, result, err := tracker.WaitFor(ctx, "wait-ok")
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	require.NotNil(t, result)
	assert.Equal(t, "done", result.Summary)
	assert.Equal(t, 50, result.TokensUsed)
}

func TestAsyncDelegateTracker_WaitFor_ContextTimeout(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("wait-slow", DelegateConfig{Prompt: "slow"}, nil, slowFn))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	status, result, err := tracker.WaitFor(ctx, "wait-slow")
	// Now returns ctx.Err() instead of nil on timeout
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
	assert.Equal(t, "running", status)
	assert.Nil(t, result)
}

func TestAsyncDelegateTracker_WaitFor_NotFound(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	ctx := context.Background()
	status, result, err := tracker.WaitFor(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Empty(t, status)
	assert.Nil(t, result)
}

func TestAsyncDelegateTracker_WaitFor_AlreadyCompleted(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("wait-done", DelegateConfig{Prompt: "test"}, nil, fastFn))
	time.Sleep(50 * time.Millisecond) // let it complete

	ctx := context.Background()
	status, result, err := tracker.WaitFor(ctx, "wait-done")
	require.NoError(t, err)
	assert.Equal(t, "completed", status)
	require.NotNil(t, result)
	assert.Equal(t, "done", result.Summary)
}

func TestAsyncDelegateTracker_WaitFor_AlreadyFailed(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	failFn := func(ctx context.Context) (*DelegateResult, error) {
		return nil, errors.New("boom")
	}

	require.NoError(t, tracker.Start("wait-fail", DelegateConfig{Prompt: "fail"}, nil, failFn))
	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	status, result, err := tracker.WaitFor(ctx, "wait-fail")
	require.NoError(t, err)
	assert.Equal(t, "failed", status)
	require.NotNil(t, result)
	assert.Equal(t, "boom", result.ErrorMessage)
}

func TestAsyncDelegateTracker_WaitFor_ContextCancelled(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	slowFn := func(ctx context.Context) (*DelegateResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("wait-cancel", DelegateConfig{Prompt: "slow"}, nil, slowFn))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	status, result, err := tracker.WaitFor(ctx, "wait-cancel")
	// Now returns ctx.Err() instead of nil on cancellation
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	assert.Equal(t, "running", status)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	var wg sync.WaitGroup
	const numDelegates = 20

	// Start multiple delegates with unique IDs (duplicate IDs now return an error)
	for i := 0; i < numDelegates; i++ {
		id := fmt.Sprintf("concurrent-del-%d", i)
		cfg := DelegateConfig{Prompt: "test"}
		runFn := func(ctx context.Context) (*DelegateResult, error) {
			time.Sleep(10 * time.Millisecond)
			return &DelegateResult{Summary: "ok", ExitStatus: "success"}, nil
		}
		require.NoError(t, tracker.Start(id, cfg, nil, runFn))
	}

	// Concurrent GetStatus calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tracker.GetStatus(fmt.Sprintf("concurrent-del-%d", i%numDelegates))
		}(i)
	}

	// Concurrent ListRunning calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.ListRunning()
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond) // let all goroutines finish

	// Verify all are completed
	for i := 0; i < numDelegates; i++ {
		id := fmt.Sprintf("concurrent-del-%d", i)
		status, result, found := tracker.GetStatus(id)
		require.True(t, found, "ID %s should exist", id)
		assert.Equal(t, "completed", status, "ID %s should be completed", id)
		require.NotNil(t, result)
	}
}

func TestAsyncDelegateTracker_MultipleUniqueIDs(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	ids := make([]string, 50)
	for i := 0; i < 50; i++ {
		ids[i] = fmt.Sprintf("unique-%d", i)
	}

	for i, id := range ids {
		idx := i
		runFn := func(ctx context.Context) (*DelegateResult, error) {
			time.Sleep(time.Duration(idx*2) * time.Millisecond)
			return &DelegateResult{
				Summary:    fmt.Sprintf("result-%d", idx),
				ExitStatus: "success",
			}, nil
		}
		require.NoError(t, tracker.Start(id, DelegateConfig{Prompt: "test"}, nil, runFn))
	}

	// Wait for all to complete
	time.Sleep(200 * time.Millisecond)

	// Verify all have unique results
	for i, id := range ids {
		status, result, found := tracker.GetStatus(id)
		require.True(t, found, "ID %s should exist", id)
		assert.Equal(t, "completed", status, "ID %s should be completed", id)
		require.NotNil(t, result)
		assert.Equal(t, fmt.Sprintf("result-%d", i), result.Summary,
			"ID %s should have correct result", id)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_Start_DuplicateID_ReturnsError(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	fastFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "first", ExitStatus: "success"}, nil
	}

	cfg := DelegateConfig{Prompt: "test"}
	require.NoError(t, tracker.Start("dup-id", cfg, nil, fastFn))
	time.Sleep(20 * time.Millisecond)

	// First start should succeed
	status, result, found := tracker.GetStatus("dup-id")
	require.True(t, found)
	assert.Equal(t, "completed", status)
	require.NotNil(t, result)
	assert.Equal(t, "first", result.Summary)

	// Second start with same ID must return an error (no overwrite)
	err := tracker.Start("dup-id", cfg, nil, fastFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already tracked")
	assert.Contains(t, err.Error(), "dup-id")

	// Original entry should still exist and be unchanged
	status, result, found = tracker.GetStatus("dup-id")
	require.True(t, found)
	assert.Equal(t, "completed", status)
	require.NotNil(t, result)
	assert.Equal(t, "first", result.Summary)
}

func TestAsyncDelegateTracker_EntryFields(t *testing.T) {
	tracker := NewAsyncDelegateTracker()
	bus := events.NewEventBus()
	agent := &Agent{
		eventBus:      bus,
		delegateDepth: 0,
	}

	cfg := DelegateConfig{
		Prompt: "my prompt",
		Role:   "my role",
	}

	ch := bus.Subscribe("test")
	defer bus.Unsubscribe("ch")

	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("fields-test", cfg, agent, runFn))

	// Wait for started event
	<-ch

	tracker.mu.Lock()
	entry := tracker.entries["fields-test"]
	tracker.mu.Unlock()

	require.NotNil(t, entry)
	assert.Equal(t, "fields-test", entry.ID)
	assert.Equal(t, cfg, entry.Config)
	assert.True(t, entry.StartedAt.After(time.Now().Add(-time.Hour)))
	assert.NotNil(t, entry.Cancel)
	// Done channel should be non-nil
	assert.NotNil(t, entry.Done)
}

func TestAsyncDelegateTracker_ContextCancellation_StopsRunFn(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	ctxReceived := make(chan context.Context, 1)
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		ctxReceived <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}

	require.NoError(t, tracker.Start("ctx-test", DelegateConfig{Prompt: "test"}, nil, runFn))

	// Wait to receive the context from the runFn
	select {
	case <-ctxReceived:
		// Cancel it via the entry
		ok := tracker.Cancel("ctx-test")
		assert.True(t, ok)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for runFn to start")
	}

	// Wait for completion
	time.Sleep(50 * time.Millisecond)

	status, _, found := tracker.GetStatus("ctx-test")
	require.True(t, found)
	assert.Equal(t, "failed", status)
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestAsyncDelegateTracker_Close_ShutsDownProcessResults(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	cfg := DelegateConfig{Prompt: "test"}
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		return &DelegateResult{Summary: "done", ExitStatus: "success"}, nil
	}

	require.NoError(t, tracker.Start("close-test", cfg, nil, runFn))

	// Wait for completion
	time.Sleep(50 * time.Millisecond)

	status, _, found := tracker.GetStatus("close-test")
	require.True(t, found)
	assert.Equal(t, "completed", status)

	// Close should not hang — processResults should exit cleanly
	done := make(chan struct{})
	go func() {
		tracker.Close()
		close(done)
	}()

	select {
	case <-done:
		// Good — Close() returned
	case <-time.After(2 * time.Second):
		t.Fatal("Close() hung — processResults goroutine did not exit")
	}
}

func TestAsyncDelegateTracker_Close_EmptyTracker(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	// Close on an empty tracker should not hang
	done := make(chan struct{})
	go func() {
		tracker.Close()
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Close() on empty tracker hung")
	}
}

func TestAsyncDelegateTracker_Close_WaitsForPendingResults(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	// Slow delegate that takes time to produce a result
	slowStarted := make(chan struct{})
	slowDone := make(chan struct{})
	runFn := func(ctx context.Context) (*DelegateResult, error) {
		close(slowStarted)
		// Block until we're told to finish or context is cancelled
		select {
		case <-slowDone:
			return &DelegateResult{Summary: "slow-done", ExitStatus: "success"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	require.NoError(t, tracker.Start("slow-close", DelegateConfig{Prompt: "slow"}, nil, runFn))

	// Wait for the goroutine to start
	select {
	case <-slowStarted:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for slow goroutine to start")
	}

	// Signal the slow delegate to finish
	close(slowDone)

	// Give it a moment to send its result to resultChan
	time.Sleep(50 * time.Millisecond)

	// Now Close() should wait for the pending result to be processed
	// before returning
	done := make(chan struct{})
	go func() {
		tracker.Close()
		close(done)
	}()

	select {
	case <-done:
		// Good — Close() waited for the pending result
	case <-time.After(2 * time.Second):
		t.Fatal("Close() hung or timed out")
	}

	// Verify the result was processed correctly
	status, result, found := tracker.GetStatus("slow-close")
	require.True(t, found)
	assert.Equal(t, "completed", status)
	require.NotNil(t, result)
	assert.Equal(t, "slow-done", result.Summary)
}

func TestAsyncDelegateTracker_ProcessResults_UnknownID(t *testing.T) {
	// Verify that if a result arrives for an unknown delegateID,
	// processResults handles it gracefully (no panic).
	tracker := NewAsyncDelegateTracker()

	// Send a result directly to resultChan for an unknown ID.
	// processResults should silently drop it.
	tracker.resultChan <- delegateResultMsg{
		delegateID: "nonexistent-unknown",
		result:     &DelegateResult{Summary: "ghost", ExitStatus: "success"},
		err:        nil,
	}

	// Give processResults time to drain the message
	time.Sleep(50 * time.Millisecond)

	// Verify no panic and the ID is not in entries
	_, _, found := tracker.GetStatus("nonexistent-unknown")
	assert.False(t, found, "unknown ID should not be stored")

	// Verify no panic and the ID is not in entries (error path)
	tracker.resultChan <- delegateResultMsg{
		delegateID: "another-unknown",
		result:     nil,
		err:        errors.New("ghost error"),
	}

	time.Sleep(50 * time.Millisecond)
	_, _, found = tracker.GetStatus("another-unknown")
	assert.False(t, found, "unknown error ID should not be stored")

	// Cleanup
	tracker.Close()
}

func TestAsyncDelegateTracker_ConcurrentStartAndClose(t *testing.T) {
	tracker := NewAsyncDelegateTracker()

	const numDelegates = 10

	// Start many fast delegates
	for i := 0; i < numDelegates; i++ {
		id := fmt.Sprintf("conc-%d", i)
		idx := i
		runFn := func(ctx context.Context) (*DelegateResult, error) {
			// Vary completion times slightly
			time.Sleep(time.Duration(idx%5) * 5 * time.Millisecond)
			return &DelegateResult{
				Summary:    fmt.Sprintf("result-%d", idx),
				ExitStatus: "success",
			}, nil
		}
		require.NoError(t, tracker.Start(id, DelegateConfig{Prompt: "test"}, nil, runFn))
	}

	// Close immediately — processResults should still handle all pending results
	// without panicking (sending on closed channel)
	done := make(chan struct{})
	go func() {
		tracker.Close()
		close(done)
	}()

	select {
	case <-done:
		// Good — Close() returned
	case <-time.After(5 * time.Second):
		t.Fatal("Close() hung")
	}

	// All delegates should have completed successfully
	for i := 0; i < numDelegates; i++ {
		id := fmt.Sprintf("conc-%d", i)
		status, result, found := tracker.GetStatus(id)
		require.True(t, found, "ID %s should exist", id)
		assert.Equal(t, "completed", status, "ID %s should be completed", id)
		require.NotNil(t, result)
		assert.Equal(t, fmt.Sprintf("result-%d", i), result.Summary)
	}
}
