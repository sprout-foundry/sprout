package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Run spawns an in-process subagent and waits for completion
func (r *SubagentRunner) Run(ctx context.Context, prompt string, opts SubagentOptions) *SubagentResult {
	taskID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	return r.runTask(ctx, taskID, prompt, opts, nil, 0)
}

// RunParallel spawns multiple subagents concurrently.
// If the parent context is cancelled, remaining subagents are cancelled
// and their results are set to cancellation errors.
func (r *SubagentRunner) RunParallel(ctx context.Context, tasks []SubagentTask, opts SubagentOptions) []*SubagentResult {
	if len(tasks) == 0 {
		return nil
	}

	results := make([]*SubagentResult, len(tasks))
	var wg sync.WaitGroup

	// Create a derived context so we can cancel remaining subagents
	// when the parent context is cancelled or when we detect early
	// termination is needed.
	parallelCtx, parallelCancel := context.WithCancel(ctx)
	defer parallelCancel()

	// Semaphore for limiting concurrent subagents
	var sem chan struct{}
	if opts.MaxConcurrentSubagents > 0 {
		sem = make(chan struct{}, opts.MaxConcurrentSubagents)
	}

	// Fleet token budget tracking
	var cumulativeTokens atomic.Int64

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t SubagentTask) {
			// Resolve persona early so all lifecycle events use the same value
			persona := opts.Persona
			if t.Persona != "" {
				persona = t.Persona
			}

			r.metricQueued.Add(1)
			queueStart := time.Now()

			// Emit: queued
			r.publishLifecycleEvent(t.ID, persona, "queued", "", 0, 0)

			// Acquire semaphore (if limited), respecting context cancellation
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-parallelCtx.Done():
					r.metricQueued.Add(-1)
					r.metricCancelled.Add(1)
					r.publishLifecycleEvent(t.ID, persona, "cancelled", "context_cancelled", 0, 0)
					defer wg.Done()
					results[idx] = &SubagentResult{
						ID:        t.ID,
						Error:     parallelCtx.Err(),
						Cancelled: true,
					}
					return
				}
			}

			// Track queue wait time and transition from queued to active
			r.metricQueuedWaitMS.Add(int64(time.Since(queueStart).Milliseconds()))
			r.metricQueued.Add(-1)
			r.metricActive.Add(1)

			// Budget check after acquiring semaphore, before starting work
			if opts.FleetTokenBudget > 0 && cumulativeTokens.Load() >= int64(opts.FleetTokenBudget) {
				r.metricActive.Add(-1)
				r.metricCancelled.Add(1)
				r.publishLifecycleEvent(t.ID, persona, "cancelled", "budget_exceeded", 0, 0)
				defer wg.Done()
				results[idx] = &SubagentResult{
					ID:             t.ID,
					Error:          fmt.Errorf("fleet token budget exceeded"),
					BudgetExceeded: true,
				}
				return
			}

			// Emit: started
			r.publishLifecycleEvent(t.ID, persona, "started", "", 0, 0)

			defer wg.Done()
			taskOpts := opts
			if t.Model != "" {
				taskOpts.Model = t.Model
			}
			if t.Provider != "" {
				taskOpts.Provider = t.Provider
			}
			if t.Persona != "" {
				taskOpts.Persona = t.Persona
			}
			if t.WorkingDir != "" {
				taskOpts.WorkingDir = t.WorkingDir
			}
			// Note: tokens are already debited per-LLM-call by the subagent via
			// SetFleetBudget → tracker.Add() in trackFleetBudgetForResponse.
			// Do NOT add result.TokensUsed again here — that would double-count.
			result := r.runTask(parallelCtx, t.ID, t.Prompt, taskOpts, &cumulativeTokens, int64(opts.FleetTokenBudget))
			results[idx] = result
			if result != nil {
				if result.Cancelled {
					r.metricActive.Add(-1)
					r.metricCancelled.Add(1)
				} else if result.Error != nil {
					r.metricActive.Add(-1)
					r.metricFailed.Add(1)
				} else {
					r.metricActive.Add(-1)
					r.metricCompleted.Add(1)
				}

				// Emit: completed / cancelled after runTask returns
				completedStatus := "completed"
				completedReason := ""
				if result.Cancelled {
					completedStatus = "cancelled"
					completedReason = "context_cancelled"
				} else if result.BudgetExceeded {
					completedStatus = "cancelled"
					completedReason = "budget_exceeded"
				}
				r.publishLifecycleEventWithCost(
					t.ID, persona, completedStatus, completedReason,
					result.TokensUsed, result.Elapsed.Milliseconds(), result.Cost,
				)
			}
		}(i, task)
	}

	wg.Wait()
	return results
}

// GetActiveSubagents returns information about currently running subagents
func (r *SubagentRunner) GetActiveSubagents() []*runningSubagent {
	var active []*runningSubagent
	r.active.Range(func(key, value interface{}) bool {
		if sub, ok := value.(*runningSubagent); ok {
			if !sub.Completed.Load() {
				active = append(active, sub)
			}
		}
		return true
	})
	return active
}

// CancelSubagent cancels a specific running subagent by ID.
// Cancels both the run context (truncates pending work) and the subagent
// agent's interrupt signal (preempts the in-flight ProcessQuery loop,
// which doesn't observe runCtx — see SP-059 Phase 1a).
func (r *SubagentRunner) CancelSubagent(id string) bool {
	if val, ok := r.active.Load(id); ok {
		if sub, ok := val.(*runningSubagent); ok {
			cancelRunningSubagent(sub)
			return true
		}
	}
	return false
}

// CancelAll cancels all running subagents. Called when the user clicks
// Stop on the primary — without this, the primary's TriggerInterrupt
// returns but subagent work continues until self-completion.
func (r *SubagentRunner) CancelAll() {
	r.active.Range(func(key, value interface{}) bool {
		if sub, ok := value.(*runningSubagent); ok {
			if !sub.Completed.Load() {
				cancelRunningSubagent(sub)
			}
		}
		return true
	})
}

// InjectInputIntoActive delivers a steering message to the deepest
// (most-recently-started) running subagent. Returns the target ID when
// delivery succeeds, or empty string when no subagent is currently active
// — the caller falls back to the primary's input channel in that case.
//
// "Deepest" wins so that nested-subagent setups route to the one the
// user is most likely watching activity from in the Subagents tab.
// Selection ties broken by start time (latest wins).
func (r *SubagentRunner) InjectInputIntoActive(input string) (string, bool) {
	if r == nil || input == "" {
		return "", false
	}
	var best *runningSubagent
	r.active.Range(func(_, value interface{}) bool {
		sub, ok := value.(*runningSubagent)
		if !ok || sub == nil || sub.Completed.Load() {
			return true
		}
		if best == nil || sub.StartedAt.After(best.StartedAt) {
			best = sub
		}
		return true
	})
	if best == nil || best.Agent == nil {
		return "", false
	}
	if err := best.Agent.InjectInputContext(input); err != nil {
		// Buffer full or other transient — caller falls back to primary.
		return "", false
	}
	return best.ID, true
}

// cancelRunningSubagent signals both cancel paths the subagent observes:
// the context (for tool calls and outbound LLM requests) and the agent's
// interrupt mechanism (for the seed conversation loop, which has no
// context.Context parameter on ProcessQuery).
func cancelRunningSubagent(sub *runningSubagent) {
	if sub.Cancel != nil {
		sub.Cancel()
	}
	if sub.Agent != nil {
		sub.Agent.TriggerInterrupt()
	}
}
