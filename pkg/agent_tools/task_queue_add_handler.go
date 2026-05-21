package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type taskQueueAddHandler struct{}

func (h *taskQueueAddHandler) Name() string { return "task_queue_add" }

func (h *taskQueueAddHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "task_queue_add",
		Description: "Add a new task to the persistent task queue. Tasks persist across sessions and can be processed by the Executive Assistant persona.",
		Required: []string{"title"},
		Parameters: []ParameterDef{
			{Name: "title", Type: "string", Required: true, Description: "Task title (required)"},
			{Name: "description", Type: "string", Description: "Detailed task description"},
			{Name: "persona", Type: "string", Description: "Persona to use when executing this task (e.g., repo_orchestrator)"},
			{Name: "priority", Type: "string", Description: "Priority: high, medium, or low (default: medium)"},
			{Name: "working_dir", Type: "string", Description: "Working directory for the task (e.g., ~/projects/my-repo)"},
		},
	}
}

func (h *taskQueueAddHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "title")
	if err != nil {
		return err
	}
	if val, ok := args["priority"]; ok && val != nil {
		if s, ok := val.(string); ok && s != "" && s != "high" && s != "medium" && s != "low" {
			return fmt.Errorf("parameter 'priority' must be 'high', 'medium', or 'low', got %q", s)
		}
	}
	return nil
}

func (h *taskQueueAddHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	title, _ := extractString(args, "title")
	description, _ := extractString(args, "description")
	persona, _ := extractString(args, "persona")
	priority, _ := extractString(args, "priority")
	workingDir, _ := extractString(args, "working_dir")

	if priority == "" {
		priority = "medium"
	}

	tq := NewTaskQueue(DefaultTaskQueuePath())
	task, err := tq.AddTask(title, description, priority, workingDir, persona)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to add task: %v", err), IsError: true}, nil
	}

	result := fmt.Sprintf("Task added successfully:\n  ID: %s\n  Title: %s\n  Priority: %s\n  Status: pending",
		task.ID, task.Title, task.Priority)
	return ToolResult{Output: result}, nil
}
