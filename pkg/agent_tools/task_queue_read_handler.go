package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type taskQueueReadHandler struct{}

func (h *taskQueueReadHandler) Name() string { return "task_queue_read" }

func (h *taskQueueReadHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "task_queue_read",
		Description: "Read pending tasks from the persistent task queue. Returns tasks sorted by priority (high > medium > low). The queue persists across sessions and is stored at ~/.config/sprout/task_queue.json.",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "status", Type: "string", Description: "Filter tasks by status: pending, in_progress, completed, failed, blocked, or all (default: pending)"},
			{Name: "limit", Type: "integer", Description: "Maximum number of tasks to return (default: 10)"},
		},
	}
}

func (h *taskQueueReadHandler) Validate(args map[string]any) error {
	if args == nil || len(args) == 0 {
		return fmt.Errorf("arguments must not be nil or empty")
	}
	return nil
}

func (h *taskQueueReadHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	status, _ := extractString(args, "status")
	if status == "" {
		status = "pending"
	}
	limit, _ := extractInt(args, "limit")
	if limit <= 0 {
		limit = 10
	}

	tq := NewTaskQueue(DefaultTaskQueuePath())
	tasks, err := tq.ReadTasks(status, limit)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to read tasks: %v", err), IsError: true}, nil
	}

	if len(tasks) == 0 {
		return ToolResult{Output: fmt.Sprintf("No tasks found with status %q", status)}, nil
	}

	var output string
	output = fmt.Sprintf("Found %d task(s) (status: %s, limit: %d):\n", len(tasks), status, limit)
	for i, task := range tasks {
		output += fmt.Sprintf("\n%d. [%s] %s (priority: %s)\n   ID: %s\n   Created: %s\n", i+1, task.Status, task.Title, task.Priority, task.ID, task.CreatedAt.Format("2006-01-02 15:04"))
		if task.Description != "" {
			output += fmt.Sprintf("   Description: %s\n", task.Description)
		}
		if task.Persona != "" {
			output += fmt.Sprintf("   Persona: %s\n", task.Persona)
		}
		if task.WorkingDir != "" {
			output += fmt.Sprintf("   Working Dir: %s\n", task.WorkingDir)
		}
		if task.Result != "" {
			output += fmt.Sprintf("   Result: %s\n", task.Result)
		}
	}

	return ToolResult{Output: output}, nil
}
