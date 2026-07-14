//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// runQueueMode handles autonomous EA queue mode. It reads pending tasks from
// the persistent task queue and processes each one by delegating to the agent
// via ProcessQuery. The agent's tool handlers (task_queue_read, task_queue_publish,
// run_subagent, etc.) are available so the LLM can manage the task lifecycle.
// After processing a task, it loops back to check for more pending tasks.
// Exits cleanly when the queue is empty.
func runQueueMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator) error {
	fmt.Println()
	console.GlyphInfo.Printf("sprout · EA queue · %s · %s",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	// Status footer: pinned bottom row with model/cost/context.
	// Suppressed automatically on non-TTY.
	queueFooterSource := &agentFooterSource{agent: chatAgent}
	footer := console.NewStatusFooter(os.Stderr, queueFooterSource)
	console.RegisterGlobalStatusFooter(footer)
	footer.Start()
	defer footer.Stop()

	// CLI-UX-12: register Alt+T (footer tooltip) and Alt+V (verbosity
	// toggle) keybindings. The registry is Once-protected, so this
	// call is a no-op when interactive mode already ran in the same
	// process (shared-agent mode). In queue mode the verbosity toggle
	// is still useful for power users tailing the session log.
	console.RegisterKeymapForFooter(footer, chatAgent.GetConfigManager())

	// Wire event-driven output routing (streaming, tool timeline,
	// footer refresh) — same as interactive mode. SetupAgentEvents
	// registers a streaming callback that respects the per-turn
	// renderer and WebUI handoff, replacing the old raw fmt.Print.
	SetupAgentEvents(chatAgent, eventBus, indicator)

	// Subscribe to tool events for per-tool progress lines and
	// footer refreshes after each tool.
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	resetSpawnTracking := startTerminalToolSubscriber(subCtx, chatAgent, eventBus, indicator, footer)
	defer resetSpawnTracking()

	tq := tools.NewTaskQueue(tools.DefaultTaskQueuePath())

	tasksProcessed := 0

	for {
		// Check for cancellation before each iteration
		if err := ctx.Err(); err != nil {
			fmt.Println()
			console.GlyphStopped.Printf("Queue mode cancelled: %v", err)
			break
		}

		// Read pending tasks from the queue
		tasks, err := tq.ReadTasks(ctx, "pending", 10)
		if err != nil {
			return fmt.Errorf("failed to read task queue: %w", err)
		}

		// Exit cleanly when queue is empty
		if len(tasks) == 0 {
			fmt.Println()
			if tasksProcessed > 0 {
				console.GlyphSuccess.Printf("Queue mode complete — processed %d task(s)", tasksProcessed)
			} else {
				console.GlyphInfo.Print("No pending tasks in queue — nothing to process")
			}
			break
		}

		// Process each pending task
		for _, task := range tasks {
			// Check for cancellation before processing each task
			if err := ctx.Err(); err != nil {
				fmt.Println()
				console.GlyphStopped.Printf("Queue mode cancelled: %v", err)
				break
			}

			fmt.Println()
			console.GlyphAction.Printf("Processing task: %s [%s] (priority: %s)",
				task.Title, task.ID, task.Priority)

			// Mark task as in_progress
			_, err = tq.PublishTask(ctx, task.ID, "in_progress", "", nil)
			if err != nil {
				console.GlyphWarning.Fprintf(os.Stderr, "Failed to mark task %s as in_progress: %v", task.ID, err)
			}

			// Construct a query for the agent to process this task.
			// The EA system prompt already knows how to handle task processing,
			// and the agent has access to run_subagent, task_queue_publish, etc.
			query := buildQueueTaskQuery(task)

			// Per-task assistant renderer: indents prose and optionally
			// re-renders with markdown formatting at task-end. Wire
			// reasoning chunks to the renderer's collapsed header so
			// they don't flood the terminal with raw monologue —
			// matches the interactive mode wiring.
			turnRenderer := beginTurn(chatAgent)
			if router := chatAgent.OutputRouter(); router != nil {
				if fold := currentReasoningFold; fold != nil {
					fold.Start()
					router.SetReasoningCallback(fold.Chunk)
				} else {
					router.SetReasoningCallback(turnRenderer.WriteReasoningChunk)
				}
			}

			indicator.Start(fmt.Sprintf("Processing · %s", chatAgent.GetModel()))
			err = ProcessQuery(ctx, chatAgent, eventBus, query)
			indicator.Stop()

			// Tear down the renderer hooks and finalize so the
			// re-render's own writes don't loop back through them.
			endTurn(chatAgent, turnRenderer)

			if err != nil {
				fmt.Fprint(os.Stderr, "\n"+console.FormatErrorBlock(fmt.Sprintf("Error processing task %s", task.ID), err))
				// Mark task as failed
				_, _ = tq.PublishTask(ctx, task.ID, "failed", fmt.Sprintf("Error during processing: %v", err), nil)
				footer.Refresh()
				continue
			}

			// Check if the agent marked this task as completed/failed via its tool handlers
			// If not, check the current status. The agent should use task_queue_publish
			// during processing, so we re-read the task to see its state.
			updatedTasks, err := tq.ReadTasks(ctx, "all", 100)
			taskCompleted := false
			if err == nil {
				for _, t := range updatedTasks {
					if t.ID == task.ID {
						if t.Status == "completed" || t.Status == "failed" {
							taskCompleted = true
						}
						break
					}
				}
			}

			if !taskCompleted {
				// Agent didn't update task status; mark as completed by default
				console.GlyphInfo.Printf("Task %s processed — marking as completed", task.Title)
				result := "Task processed via queue mode. Agent did not explicitly set a result."
				_, _ = tq.PublishTask(ctx, task.ID, "completed", result, nil)
			} else {
				console.GlyphSuccess.Printf("Task %s completed", task.Title)
			}

			footer.Refresh()
			tasksProcessed++
		}
	}

	return nil
}

// buildQueueTaskQuery constructs a prompt for the agent to process a queued task.
func buildQueueTaskQuery(task tools.Task) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Process queued task: %s", task.Title))
	parts = append(parts, fmt.Sprintf("Task ID: %s", task.ID))

	if task.Description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", task.Description))
	}
	if task.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("Working directory: %s", task.WorkingDir))
	}
	if task.Persona != "" {
		parts = append(parts, fmt.Sprintf("Persona: %s", task.Persona))
	}
	parts = append(parts, fmt.Sprintf("Priority: %s", task.Priority))

	if task.ParentTaskID != "" {
		parts = append(parts, fmt.Sprintf("Parent task: %s", task.ParentTaskID))
	}

	parts = append(parts, "")
	parts = append(parts, "Use run_subagent to delegate this task if a persona was specified,")
	parts = append(parts, "or process it directly. When done, use task_queue_publish to mark")
	parts = append(parts, "the task as completed or failed with a summary of what you did.")
	if task.Persona != "" {
		parts = append(parts, fmt.Sprintf("Recommended persona: %s", task.Persona))
	}

	return strings.Join(parts, "\n")
}
