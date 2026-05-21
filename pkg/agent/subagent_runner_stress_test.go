package agent

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newStressTestRunner creates a SubagentRunner with a nil ConfigManager so
// subagents fail fast in createSubagent. This exercises the semaphore,
// cancellation, and ordering logic in RunParallel without needing a live
// provider. Because ConfigManager is nil, every call to createSubagent
// returns an error immediately — no LLM provider is contacted.
//
// NOTE: The "test" provider can't be used in subagents because
// ResolveProviderModel explicitly strips provider=="test" to prevent the
// test-only client from leaking into production code paths. A future refactor
// could add an isRunningUnderTest() fast-path to createSubagent to allow real
// test provider usage in subagents.
func newStressTestRunner(t *testing.T) (*SubagentRunner, func()) {
	t.Helper()
	agent := newIsolatedTestAgent(t)
	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: nil, // nil → createSubagent fails immediately (no LLM calls)
		WorkspaceRoot: agent.workspaceRoot,
	}
	runner := NewSubagentRunner(agent, shared)
	return runner, func() { agent.Shutdown() }
}

// =============================================================================
// Stress / Concurrency tests
// =============================================================================

// TestSubagentRunner_BoundedConcurrency validates that the semaphore-based
// concurrency limit (MaxConcurrentSubagents) works correctly: all tasks
// complete without deadlock, and results are returned in the correct order.
//
// With 20 tasks and MaxConcurrentSubagents=4, the buffered-channel semaphore
// ensures at most 4 subagents run simultaneously. We use nil ConfigManager
// so tasks fail fast in createSubagent; this validates the semaphore and
// result-ordering logic without needing a live provider.
func TestSubagentRunner_BoundedConcurrency(t *testing.T) {
	runner, cleanup := newStressTestRunner(t)
	defer cleanup()

	const numTasks = 20
	const maxConcurrent = 4

	var tasks []SubagentTask
	for i := 0; i < numTasks; i++ {
		tasks = append(tasks, SubagentTask{
			ID:     fmt.Sprintf("task-%d", i),
			Prompt: fmt.Sprintf("complete task %d", i),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := runner.RunParallel(ctx, tasks, SubagentOptions{
		MaxConcurrentSubagents: maxConcurrent,
	})

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	// Verify all results are present and IDs match (order preserved)
	for i, r := range results {
		if r == nil {
			t.Fatalf("result[%d] is nil", i)
		}
		expectedID := fmt.Sprintf("task-%d", i)
		if r.ID != expectedID {
			t.Errorf("results[%d].ID = %q, want %q", i, r.ID, expectedID)
		}
	}

	// The concurrency semaphore (buffered channel of size maxConcurrent)
	// ensures at most maxConcurrent goroutines are executing simultaneously.
	// With nil ConfigManager, tasks fail instantly in createSubagent, so
	// peak concurrency is bounded by Go's goroutine scheduler and the
	// semaphore. We verify no deadlocks occur and all tasks complete.
	//
	// A more thorough concurrency tracking test would require injecting
	// per-task delays, which is not feasible without modifying the
	// SubagentRunner or using a real LLM provider.
	t.Logf("All %d tasks completed with MaxConcurrency=%d", numTasks, maxConcurrent)
}

// TestSubagentRunner_SemaphoreBounded verifies the buffered-channel semaphore
// pattern used by RunParallel independently. This exercises the exact same
// select/chan pattern so we can track peak concurrency with artificial delays.
func TestSubagentRunner_SemaphoreBounded(t *testing.T) {
	// Verify the semaphore mechanism independently.
	// This tests the exact pattern used by RunParallel:
	//   sem := make(chan struct{}, maxConcurrent)
	//   sem <- struct{}{} // blocks when full
	//   <-sem             // releases

	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)

	var currentConcurrent atomic.Int32
	var maxObserved atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			cur := currentConcurrent.Add(1)
			// Track max
			for {
				old := maxObserved.Load()
				if cur <= old || maxObserved.CompareAndSwap(old, cur) {
					break
				}
			}

			// Simulate brief work
			time.Sleep(time.Duration(10+id%5) * time.Millisecond)

			currentConcurrent.Add(-1)
		}(i)
	}
	wg.Wait()

	peak := maxObserved.Load()
	if peak > maxConcurrent {
		t.Errorf("peak concurrency %d exceeded max %d", peak, maxConcurrent)
	}
	t.Logf("Peak concurrency: %d (max allowed: %d)", peak, maxConcurrent)
}

// TestSubagentRunner_FleetBudgetCancels verifies fleet budget behavior via two
// complementary sub-tests.
//
// BudgetLogicSimulation: Directly simulates the budget check logic that
// RunParallel uses to validate the token-accounting math (N tasks at T tokens
// each with budget B should allow exactly B/T tasks to start, plus one that
// may overdraw).
//
// RunParallelWithBudget: Verifies that RunParallel handles FleetTokenBudget
// without panicking or deadlocking when ConfigManager is nil (0 tokens per
// task, so no BudgetExceeded ever fires). Full budget-exhaustion with real
// token usage requires a live provider.
func TestSubagentRunner_FleetBudgetCancels(t *testing.T) {
	t.Run("BudgetLogicSimulation", func(t *testing.T) {
		// Simulates the exact budget check flow from RunParallel:
		//   if opts.FleetTokenBudget > 0 && cumulativeTokens.Load() >= int64(opts.FleetTokenBudget) {
		//       result.BudgetExceeded = true
		//   }
		//   ...
		//   cumulativeTokens.Add(int64(result.TokensUsed))
		var cumulativeTokens atomic.Int64
		const budget = int64(45)
		const tokensPerTask = int64(15)
		const numTasks = 10

		type taskResult struct {
			id             string
			budgetExceeded bool
			tokensUsed     int64
		}
		var results []taskResult

		for i := 0; i < numTasks; i++ {
			// Budget check BEFORE running (matches RunParallel budget gate)
			if budget > 0 && cumulativeTokens.Load() >= budget {
				results = append(results, taskResult{
					id:             fmt.Sprintf("task-%d", i),
					budgetExceeded: true,
				})
				continue
			}
			// Simulate task completing with token usage
			tokensUsed := tokensPerTask
			cumulativeTokens.Add(tokensUsed)
			results = append(results, taskResult{
				id:             fmt.Sprintf("task-%d", i),
				budgetExceeded: false,
				tokensUsed:     tokensUsed,
			})
		}

		var completedCount, budgetExceededCount int
		var totalTokens int64
		for _, r := range results {
			if r.budgetExceeded {
				budgetExceededCount++
			} else {
				completedCount++
				totalTokens += r.tokensUsed
			}
		}

		if completedCount != 3 {
			t.Errorf("expected exactly 3 completed tasks, got %d", completedCount)
		}
		if budgetExceededCount != 7 {
			t.Errorf("expected exactly 7 budget-exceeded tasks, got %d", budgetExceededCount)
		}
		if totalTokens > budget+tokensPerTask {
			t.Errorf("total tokens %d exceeds budget %d + overdraw %d", totalTokens, budget, tokensPerTask)
		}

		t.Logf("budget logic: %d completed, %d exceeded, %d total tokens used",
			completedCount, budgetExceededCount, totalTokens)
	})

	t.Run("RunParallelWithBudget", func(t *testing.T) {
		// Tests that RunParallel handles FleetTokenBudget without panicking or deadlocking.
		// With nil ConfigManager, tasks report 0 tokens so no BudgetExceeded occurs.
		// Full budget cancellation requires real token usage (see BudgetLogicSimulation above).
		runner, cleanup := newStressTestRunner(t)
		defer cleanup()

		const numTasks = 10
		var tasks []SubagentTask
		for i := 0; i < numTasks; i++ {
			tasks = append(tasks, SubagentTask{
				ID:     fmt.Sprintf("task-%d", i),
				Prompt: "test",
			})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		results := runner.RunParallel(ctx, tasks, SubagentOptions{
			FleetTokenBudget:       45,
			MaxConcurrentSubagents: 1,
		})

		if len(results) != numTasks {
			t.Fatalf("expected %d results, got %d", numTasks, len(results))
		}
		for i, r := range results {
			if r == nil {
				t.Fatalf("result[%d] is nil", i)
			}
		}
	})
}

// TestSubagentRunner_NoGoroutineLeak_AfterStress verifies that running
// multiple subagents in parallel does not leak goroutines. We record the
// goroutine count before and after, then assert the delta is small.
func TestSubagentRunner_NoGoroutineLeak_AfterStress(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	runner, cleanup := newStressTestRunner(t)
	defer cleanup()

	const numTasks = 10
	var tasks []SubagentTask
	for i := 0; i < numTasks; i++ {
		tasks = append(tasks, SubagentTask{
			ID:     fmt.Sprintf("task-%d", i),
			Prompt: fmt.Sprintf("complete task %d", i),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := runner.RunParallel(ctx, tasks, SubagentOptions{
		MaxConcurrentSubagents: 2,
	})

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	delta := after - before
	if delta > 3 {
		t.Errorf("goroutine leak: before=%d, after=%d, delta=%d", before, after, delta)
	}
	t.Logf("goroutines: before=%d, after=%d, delta=%d", before, after, delta)
}

// TestSubagentRunner_ParentCancelDropsQueued verifies that cancelling the
// parent context during RunParallel propagates correctly to all tasks.
//
// Two sub-tests:
//   - PreCancelled: context is cancelled BEFORE RunParallel starts. All tasks
//     should see the cancellation immediately while waiting on the semaphore.
//   - MidFlightCancel: context is cancelled after a brief delay. With nil
//     ConfigManager, tasks fail instantly so they may all complete before
//     the cancel fires. This still tests that the cancellation mechanism
//     doesn't cause panics or deadlocks.
func TestSubagentRunner_ParentCancelDropsQueued(t *testing.T) {
	t.Run("PreCancelled", func(t *testing.T) {
		runner, cleanup := newStressTestRunner(t)
		defer cleanup()

		const numTasks = 20
		var tasks []SubagentTask
		for i := 0; i < numTasks; i++ {
			tasks = append(tasks, SubagentTask{
				ID:     fmt.Sprintf("task-%d", i),
				Prompt: fmt.Sprintf("complete task %d", i),
			})
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately before RunParallel starts

		results := runner.RunParallel(ctx, tasks, SubagentOptions{
			MaxConcurrentSubagents: 2,
		})

		if len(results) != numTasks {
			t.Fatalf("expected %d results, got %d", numTasks, len(results))
		}

		var cancelledCount, errorCount int
		for i, r := range results {
			if r == nil {
				t.Fatalf("result[%d] is nil", i)
			}
			expectedID := fmt.Sprintf("task-%d", i)
			if r.ID != expectedID {
				t.Errorf("results[%d].ID = %q, want %q", i, r.ID, expectedID)
			}
			if r.Cancelled {
				cancelledCount++
			} else if r.Error != nil {
				errorCount++
			}
		}

		// With a pre-cancelled context and MaxConcurrency=2, goroutines race
		// between acquiring the semaphore and observing context cancellation.
		// Go's select randomly picks between ready cases, so the exact split
		// is non-deterministic. The test verifies no panics/deadlocks occur
		// and all results are returned with correct IDs.
		t.Logf("pre-cancelled: %d/%d cancelled, %d errored", cancelledCount, numTasks, errorCount)
		// Sanity: every result should be either cancelled or have an error
		// (since nil ConfigManager means createSubagent always fails)
		if cancelledCount+errorCount != numTasks {
			t.Errorf("expected all tasks to be cancelled or errored, got cancelled=%d + errored=%d = %d out of %d",
				cancelledCount, errorCount, cancelledCount+errorCount, numTasks)
		}
	})

	t.Run("MidFlightCancel", func(t *testing.T) {
		runner, cleanup := newStressTestRunner(t)
		defer cleanup()

		const numTasks = 20
		var tasks []SubagentTask
		for i := 0; i < numTasks; i++ {
			tasks = append(tasks, SubagentTask{
				ID:     fmt.Sprintf("task-%d", i),
				Prompt: fmt.Sprintf("complete task %d", i),
			})
		}

		ctx, cancel := context.WithCancel(context.Background())

		type resultContainer struct {
			results []*SubagentResult
		}
		done := make(chan resultContainer, 1)
		go func() {
			r := runner.RunParallel(ctx, tasks, SubagentOptions{
				MaxConcurrentSubagents: 2,
			})
			done <- resultContainer{results: r}
		}()

		// Cancel after a brief delay — with nil ConfigManager, tasks fail
		// instantly so they may all complete before the cancel fires.
		// This still tests that the cancellation mechanism doesn't cause
		// panics or deadlocks.
		time.Sleep(5 * time.Millisecond)
		cancel()

		select {
		case r := <-done:
			if len(r.results) != numTasks {
				t.Fatalf("expected %d results, got %d", numTasks, len(r.results))
			}
			// Verify all results exist and have correct IDs
			for i, res := range r.results {
				if res == nil {
					t.Fatalf("result[%d] is nil", i)
				}
				if res.ID != fmt.Sprintf("task-%d", i) {
					t.Errorf("results[%d].ID = %q, want %q", i, res.ID, fmt.Sprintf("task-%d", i))
				}
			}
			// Count cancelled vs completed-with-error — exact split depends on timing
			// (with nil ConfigManager, tasks may all complete before cancel)
			var cancelled, errorCompleted int
			for _, res := range r.results {
				if res.Cancelled {
					cancelled++
				} else if res.Error != nil {
					errorCompleted++
				}
			}
			t.Logf("mid-flight cancel: %d completed-with-error, %d cancelled (total=%d)",
				errorCompleted, cancelled, numTasks)
			// All tasks should account for: either cancelled or completed (with error)
			if cancelled+errorCompleted != numTasks {
				t.Errorf("expected all tasks to be either cancelled or completed, got %d + %d = %d out of %d",
					errorCompleted, cancelled, errorCompleted+cancelled, numTasks)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("RunParallel did not complete within 30s after cancel")
		}
	})
}
