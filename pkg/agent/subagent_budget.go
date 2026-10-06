package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// A persona's iteration/time budget is soft: on reaching it the subagent is
// told to stop and report. The hard limits sit a margin beyond so the wrap-up
// turn can finish — seed's max-iteration stop returns an error with no final
// text, which would discard everything the subagent found.
const (
	subagentWrapUpIterations = 8
	subagentWrapUpGrace      = 3 * time.Minute
)

var subagentBudgetPollInterval = time.Second

type subagentBudget struct {
	iterations int
	duration   time.Duration
}

func (b subagentBudget) isZero() bool { return b.iterations <= 0 && b.duration <= 0 }

// personaBudget returns the soft run budget declared by persona's catalog
// entry, or the zero budget when it declares none.
func (r *SubagentRunner) personaBudget(persona string) subagentBudget {
	if persona == "" || r.shared == nil || r.shared.ConfigManager == nil {
		return subagentBudget{}
	}
	st := r.shared.ConfigManager.GetConfig().GetSubagentType(persona)
	if st == nil {
		return subagentBudget{}
	}
	return subagentBudget{
		iterations: st.IterationBudget,
		duration:   time.Duration(st.TimeBudgetSeconds) * time.Second,
	}
}

// hardMaxIterations returns the iteration cap for a subagent with budget b.
func (b subagentBudget) hardMaxIterations() int {
	if b.iterations <= 0 {
		return defaultSubagentMaxIterations
	}
	return min(b.iterations+subagentWrapUpIterations, defaultSubagentMaxIterations)
}

func subagentWrapUpMessage(reason string) string {
	return fmt.Sprintf("[budget] This task has reached its %s budget. Stop opening files and running commands. "+
		"Write your final response now from what you have already examined, and list anything you did not get to check. "+
		"You have at most %d more turns.", reason, subagentWrapUpIterations)
}

// monitorWrapUp injects a single wrap-up instruction into agent once it
// reaches either soft budget, then exits. injected records whether it fired.
func monitorWrapUp(ctx context.Context, agent *Agent, b subagentBudget, start time.Time, injected *atomic.Bool) {
	if agent == nil || b.isZero() {
		return
	}
	ticker := time.NewTicker(subagentBudgetPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reason := ""
			switch {
			case b.iterations > 0 && agent.state.GetCurrentIteration() >= b.iterations:
				reason = fmt.Sprintf("%d-iteration", b.iterations)
			case b.duration > 0 && time.Since(start) >= b.duration:
				reason = fmt.Sprintf("%s time", b.duration)
			}
			if reason == "" {
				continue
			}
			if err := agent.InjectInputContext(subagentWrapUpMessage(reason)); err != nil {
				agent.Logger().Debug("[subagent] wrap-up injection failed: %v\n", err)
			} else if injected != nil {
				injected.Store(true)
			}
			return
		}
	}
}

// subagentRunRecord is the per-run telemetry written to the runlog so subagent
// cost and duration can be measured per persona (e.g. whether reviews are slow
// because of turn count or per-turn latency).
func subagentRunRecord(persona string, result *SubagentResult, maxIterations int, hitMaxIterations, wrapUpInjected bool) map[string]any {
	outcome := "completed"
	switch {
	case result.Cancelled:
		outcome = "cancelled"
	case result.BudgetExceeded || result.Truncated:
		outcome = "budget_exceeded"
	case result.Error != nil:
		outcome = "error"
	}
	return map[string]any{
		"task_id":            result.ID,
		"persona":            persona,
		"outcome":            outcome,
		"elapsed_ms":         result.Elapsed.Milliseconds(),
		"iterations":         result.Iterations,
		"max_iterations":     maxIterations,
		"hit_max_iterations": hitMaxIterations,
		"wrap_up_injected":   wrapUpInjected,
		"tool_calls":         result.ToolCalls,
		"tokens_used":        result.TokensUsed,
		"cost":               result.Cost,
		"output_complete":    result.OutputComplete,
	}
}

func logSubagentRun(record map[string]any) {
	if logger := utils.GetRunLogger(); logger != nil {
		logger.LogEvent("subagent_run", record)
	}
}
