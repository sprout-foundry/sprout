// Package agent provides subagent management via the SubagentRunner, which supports
// both serial (Run) and parallel (RunParallel) execution of subagent tasks.
//
// SubagentRunner Concurrency Invariants:
//
//   - MaxConcurrentSubagents: When > 0, a buffered channel semaphore limits the number
//     of concurrently executing subagents. Tasks waiting for a slot respect parent context
//     cancellation and return Cancelled=true.
//
//   - FleetTokenBudget: When > 0, tracks cumulative token usage across the fleet via
//     atomic.Int64. Once the budget is reached, not-yet-started tasks are skipped with
//     BudgetExceeded=true. Currently running tasks are NOT interrupted.
//
//   - Order Preservation: RunParallel returns results in the same order as the input tasks,
//     regardless of execution order.
package agent
