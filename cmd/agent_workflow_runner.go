//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

func runAgentWorkflow(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, cfg *AgentWorkflowConfig, state *workflowExecutionState) (bool, error) {
	if cfg == nil || len(cfg.Steps) == 0 {
		return false, nil
	}
	if state == nil {
		state = newWorkflowExecutionState()
	}
	if state.NextStepIndex >= len(cfg.Steps) {
		state.Complete = true
		return false, nil
	}

	hasError := state.HasError
	var firstErr error
	if strings.TrimSpace(state.FirstError) != "" {
		firstErr = fmt.Errorf("workflow error: %s", state.FirstError)
	}

	for i := state.NextStepIndex; i < len(cfg.Steps); i++ {
		step := cfg.Steps[i]
		stepName := step.Name
		if stepName == "" {
			stepName = fmt.Sprintf("step-%d", i+1)
		}

		if shouldYieldBeforeWorkflowStep(cfg, state, step, chatAgent) {
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_yielded", map[string]interface{}{
				"reason":          "provider_handoff",
				"next_step_index": i,
				"next_step_name":  stepName,
				"from_provider":   strings.TrimSpace(state.LastProvider),
				"to_provider":     workflowEffectiveStepProvider(chatAgent, step),
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow yield event")
			}
			state.NextStepIndex = i
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			fmt.Println()
			console.GlyphPaused.Printf("Workflow yielded for orchestration before step %s", stepName)
			return true, nil
		}

		if !shouldRunWorkflowStep(step.When, hasError) {
			state.NextStepIndex = i + 1
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			continue
		}

		fmt.Println()
		console.GlyphAction.Printf("Workflow step %d/%d (%s)", i+1, len(cfg.Steps), stepName)
		if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_started", map[string]interface{}{
			"step_index": i,
			"step_name":  stepName,
			"provider":   workflowEffectiveStepProvider(chatAgent, step),
		}); err != nil {
			return false, utils.WrapError(err, "emit workflow step started event")
		}

		triggersSatisfied, triggerErr := stepFileTriggersSatisfied(step)
		if triggerErr != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q trigger evaluation failed: %w", stepName, triggerErr)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}
		if !triggersSatisfied {
			console.GlyphInfo.Fprintf(os.Stdout, "\nSkipping workflow step %s: file trigger conditions not met", stepName)
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_skipped", map[string]interface{}{
				"step_index": i,
				"step_name":  stepName,
				"reason":     "file_triggers_not_satisfied",
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow step skipped event")
			}
			state.NextStepIndex = i + 1
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			continue
		}

		if step.IsShellStep() {
			shellErr := runWorkflowShellStep(ctx, step)
			if shellErr != nil {
				hasError = true
				if firstErr == nil {
					firstErr = fmt.Errorf("workflow step %q failed: %w", stepName, shellErr)
				}
				state.NextStepIndex = i + 1
				state.HasError = hasError
				if firstErr != nil {
					state.FirstError = firstErr.Error()
				}
				state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
				if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_failed", map[string]interface{}{
					"step_index": i,
					"step_name":  stepName,
					"kind":       "shell",
					"error":      shellErr.Error(),
				}); err != nil {
					return false, utils.WrapError(err, "emit workflow step failed event")
				}
				if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
					return false, utils.WrapError(err, "persist workflow checkpoint")
				}
				if !cfg.ContinueOnError {
					break
				}
				continue
			}

			hasError = false
			state.NextStepIndex = i + 1
			state.HasError = false
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_completed", map[string]interface{}{
				"step_index": i,
				"step_name":  stepName,
				"kind":       "shell",
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow step completed event")
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			continue
		}

		if err := applyWorkflowRuntimeOverrides(chatAgent, step.AgentWorkflowRuntime); err != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q runtime setup failed: %w", stepName, err)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}

		stepPrompt, err := resolveStepPrompt(step)
		if err != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q prompt resolution failed: %w", stepName, err)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}
		if stepPrompt == "" {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q resolved an empty prompt", stepName)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}

		err = ProcessQuery(ctx, chatAgent, eventBus, stepPrompt)
		if err != nil {
			hasError = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow step %q failed: %w", stepName, err)
			}
			state.NextStepIndex = i + 1
			state.HasError = hasError
			if firstErr != nil {
				state.FirstError = firstErr.Error()
			}
			state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_failed", map[string]interface{}{
				"step_index": i,
				"step_name":  stepName,
				"provider":   state.LastProvider,
				"error":      err.Error(),
			}); err != nil {
				return false, utils.WrapError(err, "emit workflow step failed event")
			}
			if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
				return false, utils.WrapError(err, "persist workflow checkpoint")
			}
			if !cfg.ContinueOnError {
				break
			}
			continue
		}

		hasError = false
		state.NextStepIndex = i + 1
		state.HasError = false
		state.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
		if err := emitWorkflowOrchestrationEvent(cfg, "workflow_step_completed", map[string]interface{}{
			"step_index": i,
			"step_name":  stepName,
			"provider":   state.LastProvider,
		}); err != nil {
			return false, utils.WrapError(err, "emit workflow step completed event")
		}
		if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
			return false, utils.WrapError(err, "persist workflow checkpoint")
		}
	}

	state.Complete = true
	if firstErr != nil {
		state.FirstError = firstErr.Error()
		state.HasError = true
	}
	if err := persistWorkflowCheckpoint(cfg, state, chatAgent); err != nil {
		return false, utils.WrapError(err, "persist workflow checkpoint")
	}
	if err := emitWorkflowOrchestrationEvent(cfg, "workflow_completed", map[string]interface{}{
		"has_error": state.HasError,
	}); err != nil {
		return false, utils.WrapError(err, "emit workflow completed event")
	}

	return false, firstErr
}

// attachWorkflowBudget wires the workflow's USD budget and progress
// heartbeat onto the agent. Returns a stop function the caller MUST
// invoke before the agent shuts down — it unregisters callbacks and
// stops the heartbeat goroutine. If no budget is configured the
// returned stop is a no-op and no goroutines are started.
//
// Heartbeat semantics:
//   - Default cadence: 600s when a budget is configured, off otherwise.
//   - cfg.Progress.HeartbeatSeconds > 0 overrides the cadence.
//   - The heartbeat prints to stdout in a single line so it composes with
//     existing console output without clobbering it.
func attachWorkflowBudget(chatAgent *agent.Agent, cfg *AgentWorkflowConfig) (stop func()) {
	if chatAgent == nil || cfg == nil || cfg.Budget == nil || cfg.Budget.USD <= 0 {
		// Heartbeat without a budget is still meaningful, but only if
		// explicitly requested. Most workflows want budget+heartbeat as
		// a pair, so skip the goroutine when neither is set.
		if chatAgent != nil && cfg != nil && cfg.Progress != nil && cfg.Progress.HeartbeatSeconds > 0 {
			return startWorkflowHeartbeat(chatAgent, time.Duration(cfg.Progress.HeartbeatSeconds)*time.Second)
		}
		return func() {}
	}

	budget := agent.NewFleetUsdBudget(cfg.Budget.USD, cfg.Budget.WarnAt)
	chatAgent.SetFleetUsdBudget(budget)

	chatAgent.SetBudgetWarningCallback(func(threshold, spent, limit float64) {
		console.GlyphWarning.Fprintf(os.Stdout, "\nWARNING — crossed %.0f%% threshold: $%.2f of $%.2f spent",
			threshold*100, spent, limit)
		// SP-065-2c: Publish budget_update event for automate sessions
		chatAgent.PublishBudgetUpdate(events.EventTypeAutomateBudgetUpdate, events.AutomateBudgetUpdateEvent(
			"", spent, limit, threshold, 0,
		))
	})
	chatAgent.SetBudgetExceededCallback(func(spent, limit float64) {
		console.GlyphWarning.Fprintf(os.Stdout, "\nCAP HIT — $%.2f of $%.2f spent; workflow will truncate after the current LLM response.",
			spent, limit)
		// SP-065-2c: Publish budget_update event for automate sessions
		chatAgent.PublishBudgetUpdate(events.EventTypeAutomateBudgetUpdate, events.AutomateBudgetUpdateEvent(
			"", spent, limit, 1.0, 0,
		))
	})

	heartbeatSeconds := 600
	if cfg.Progress != nil && cfg.Progress.HeartbeatSeconds > 0 {
		heartbeatSeconds = cfg.Progress.HeartbeatSeconds
	}
	stopHeartbeat := startWorkflowHeartbeat(chatAgent, time.Duration(heartbeatSeconds)*time.Second)

	return func() {
		stopHeartbeat()
		chatAgent.SetBudgetWarningCallback(nil)
		chatAgent.SetBudgetExceededCallback(nil)
	}
}

// startWorkflowHeartbeat starts a goroutine that prints a one-line budget
// progress message to stdout on the given interval, until the returned
// stop function is called. Safe to call with a nil agent (returns a noop).
func startWorkflowHeartbeat(chatAgent *agent.Agent, interval time.Duration) func() {
	if chatAgent == nil || interval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	started := time.Now()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				spent, limit := 0.0, 0.0
				if b := chatAgent.GetFleetUsdBudget(); b != nil {
					spent, limit = b.Snapshot()
				} else {
					spent = chatAgent.GetTotalCost()
				}
				iter := chatAgent.GetCurrentIteration()
				elapsed := time.Since(started).Round(time.Second)
				if limit > 0 {
					console.GlyphInfo.Fprintf(os.Stdout, "\n$%.2f of $%.2f · iter %d · elapsed %s",
						spent, limit, iter, elapsed)
				} else {
					console.GlyphInfo.Fprintf(os.Stdout, "\n$%.2f (no cap) · iter %d · elapsed %s",
						spent, iter, elapsed)
				}
			}
		}
	}()
	return func() { close(stop) }
}

// runWorkflowShellStep executes a shell command step. Stdout and stderr are
// inherited from the workflow's terminal so progress is visible in real time.
// A non-zero exit code becomes a step failure.
//
// command_file is interpreted as a script path passed to the shell, not as a
// raw command line — this avoids quoting headaches and lets users keep
// multi-line scripts in version control.
func runWorkflowShellStep(ctx context.Context, step AgentWorkflowStep) error {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}

	command := strings.TrimSpace(step.Command)
	commandFile := strings.TrimSpace(step.CommandFile)

	var cmd *exec.Cmd
	switch {
	case command != "":
		console.GlyphShell.Fprintf(os.Stdout, "%s", singleLinePreview(command))
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	case commandFile != "":
		if _, err := os.Stat(commandFile); err != nil {
			return fmt.Errorf("command_file %q not accessible: %w", commandFile, err)
		}
		console.GlyphShell.Fprintf(os.Stdout, "%s %s", shell, commandFile)
		cmd = exec.CommandContext(ctx, shell, commandFile)
	default:
		return errors.New("shell step has neither command nor command_file")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// singleLinePreview collapses a multi-line command to a single display line.
func singleLinePreview(s string) string {
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		return strings.TrimSpace(s[:idx]) + " …"
	}
	return s
}
