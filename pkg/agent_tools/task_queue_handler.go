package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// taskQueueHandler implements ToolHandler for the consolidated task_queue tool.
//
// Dispatches on the `operation` parameter:
//   - "read"    — list tasks filtered by status and limit
//   - "add"     — create a new task
//   - "publish" — update a task's status/result and optionally create subtasks
//
// Uses the TaskQueue from task_queue.go (NewTaskQueue, DefaultTaskQueuePath)
// which provides file-locked, atomic operations on ~/.config/sprout/task_queue.json.
//
// This handler replaces the previous individual task_queue_add, task_queue_read,
// and task_queue_publish handlers with a single consolidated entry point.
type taskQueueHandler struct{}

func (h *taskQueueHandler) Name() string { return "task_queue" }

func (h *taskQueueHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "task_queue",
		Description: "Persistent cross-session task queue at ~/.config/sprout/task_queue.json. " +
			"Processed by the Executive Assistant persona.\n\n" +
			"• `read` — list tasks sorted by priority. Optional: `status` (pending|in_progress|completed|failed|blocked|all, default \"pending\"), `limit` (default 10).\n" +
			"• `add` — create. Required: `title`. Optional: `description`, `priority` (high|medium|low, default medium), `working_dir`, `persona`.\n" +
			"• `publish` — update existing (claim, progress, completion, failure). Required: `task_id`, `status` (in_progress|completed|failed|blocked). Optional: `result`, `subtasks` (array of `{title, working_dir?, persona?, priority?}`).\n\n" +
			"Use `read` for \"what's on my queue?\". Use `add` when the user wants a task remembered beyond this session. Use `publish` (EA persona) to claim or complete queued tasks.",
		Required: []string{"operation"},
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true, Description: "One of: 'read', 'add', 'publish'."},
			// Read filters
			{Name: "status", Type: "string", Description: "Read: status filter (pending|in_progress|completed|failed|blocked|all). Publish: new status to set."},
			{Name: "limit", Type: "integer", Description: "Read-only: maximum tasks to return (default 10)."},
			// Add fields
			{Name: "title", Type: "string", Description: "Add-only: task title."},
			{Name: "description", Type: "string", Description: "Add-only: detailed description."},
			{Name: "priority", Type: "string", Description: "Add-only: high|medium|low (default medium)."},
			{Name: "working_dir", Type: "string", Description: "Add-only: working directory for the task."},
			{Name: "persona", Type: "string", Description: "Add-only: persona to use when executing."},
			// Publish fields
			{Name: "task_id", Type: "string", Description: "Publish-only: task ID to update."},
			{Name: "result", Type: "string", Description: "Publish-only: summary of work done or error message."},
			{Name: "subtasks", Type: "array", Description: "Publish-only: break the task down. Each item: {title, working_dir?, persona?, priority?}."},
		},
	}
}

func (h *taskQueueHandler) Validate(args map[string]any) error {
	op, err := extractString(args, "operation")
	if err != nil {
		return err
	}
	op = strings.TrimSpace(strings.ToLower(op))

	switch op {
	case "add":
		if _, err := extractString(args, "title"); err != nil {
			return agenterrors.NewValidation("task_queue: 'title' is required for add", nil)
		}
		// Priority is optional but must be valid if provided
		if val, ok := args["priority"]; ok && val != nil {
			if s, ok := val.(string); ok && s != "" && s != "high" && s != "medium" && s != "low" {
				return agenterrors.NewValidation(fmt.Sprintf("parameter 'priority' must be 'high', 'medium', or 'low', got %q", s), nil)
			}
		}
		return nil
	case "read":
		return nil
	case "publish":
		if _, err := extractString(args, "task_id"); err != nil {
			return agenterrors.NewValidation("task_queue: 'task_id' is required for publish", nil)
		}
		if _, err := extractString(args, "status"); err != nil {
			return agenterrors.NewValidation("task_queue: 'status' is required for publish", nil)
		}
		status, _ := extractString(args, "status")
		if !validStatuses[status] {
			return agenterrors.NewValidation(fmt.Sprintf("task_queue: 'status' must be one of: pending, in_progress, completed, failed, blocked, got %q", status), nil)
		}
		return nil
	default:
		return agenterrors.NewValidation(fmt.Sprintf("task_queue: unknown operation %q (want read, add, or publish)", op), nil)
	}
}

func (h *taskQueueHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	op, _ := extractString(args, "operation")
	op = strings.TrimSpace(strings.ToLower(op))

	tq := NewTaskQueue(DefaultTaskQueuePath())

	switch op {
	case "read":
		return h.executeRead(ctx, tq, args)
	case "add":
		return h.executeAdd(ctx, tq, args)
	case "publish":
		return h.executePublish(ctx, tq, args)
	default:
		return ToolResult{
			Output:  fmt.Sprintf("task_queue: unknown operation %q", op),
			IsError: true,
		}, nil
	}
}

func (h *taskQueueHandler) executeRead(ctx context.Context, tq *TaskQueue, args map[string]any) (ToolResult, error) {
	status, _ := extractString(args, "status")
	if status == "" {
		status = "pending"
	}
	limit, _ := extractInt(args, "limit")
	if limit <= 0 {
		limit = 10
	}

	tasks, err := tq.ReadTasks(ctx, status, limit)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to read tasks: %v", err), IsError: true}, nil
	}

	if len(tasks) == 0 {
		return ToolResult{Output: fmt.Sprintf("No tasks found with status %q", status)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d task(s) (status: %s, limit: %d):\n", len(tasks), status, limit))
	for i, task := range tasks {
		sb.WriteString(fmt.Sprintf("\n%d. [%s] %s (priority: %s)\n", i+1, task.Status, task.Title, task.Priority))
		sb.WriteString(fmt.Sprintf("   ID: %s\n", task.ID))
		sb.WriteString(fmt.Sprintf("   Created: %s\n", task.CreatedAt.Format("2006-01-02 15:04")))
		if task.Description != "" {
			sb.WriteString(fmt.Sprintf("   Description: %s\n", task.Description))
		}
		if task.Persona != "" {
			sb.WriteString(fmt.Sprintf("   Persona: %s\n", task.Persona))
		}
		if task.WorkingDir != "" {
			sb.WriteString(fmt.Sprintf("   Working Dir: %s\n", task.WorkingDir))
		}
		if task.Result != "" {
			sb.WriteString(fmt.Sprintf("   Result: %s\n", task.Result))
		}
	}

	return ToolResult{Output: sb.String()}, nil
}

func (h *taskQueueHandler) executeAdd(ctx context.Context, tq *TaskQueue, args map[string]any) (ToolResult, error) {
	title, _ := extractString(args, "title")
	description, _ := extractString(args, "description")
	persona, _ := extractString(args, "persona")
	priority, _ := extractString(args, "priority")
	workingDir, _ := extractString(args, "working_dir")

	if priority == "" {
		priority = "medium"
	}

	task, err := tq.AddTask(ctx, title, description, priority, workingDir, persona)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to add task: %v", err), IsError: true}, nil
	}

	result := fmt.Sprintf("Task added successfully:\n  ID: %s\n  Title: %s\n  Priority: %s\n  Status: pending",
		task.ID, task.Title, task.Priority)
	return ToolResult{Output: result}, nil
}

func (h *taskQueueHandler) executePublish(ctx context.Context, tq *TaskQueue, args map[string]any) (ToolResult, error) {
	taskID, _ := extractString(args, "task_id")
	status, _ := extractString(args, "status")
	result, _ := extractString(args, "result")

	// Parse subtasks
	subtasks := parseSubtaskInput(args)

	updated, err := tq.PublishTask(ctx, taskID, status, result, subtasks)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to publish task: %v", err), IsError: true}, nil
	}

	// Format output
	var output string
	if len(updated) > 0 {
		output = fmt.Sprintf("Task %s updated to %s", updated[0].ID, updated[0].Status)
		if updated[0].Result != "" {
			output += fmt.Sprintf("\nResult: %s", updated[0].Result)
		}
		if len(updated) > 1 {
			output += fmt.Sprintf("\n\nCreated %d subtask(s):", len(updated)-1)
			for _, st := range updated[1:] {
				output += fmt.Sprintf("\n  - %s (%s)", st.Title, st.Priority)
			}
		}
	} else {
		output = "No tasks updated"
	}

	return ToolResult{
		Output:        output,
		StructuredOut: updated,
	}, nil
}

func (h *taskQueueHandler) Aliases() []string         { return nil }
func (h *taskQueueHandler) Timeout() time.Duration    { return 0 }
func (h *taskQueueHandler) MaxResultSize() int        { return 0 }
func (h *taskQueueHandler) SafeForParallel() bool     { return false }
func (h *taskQueueHandler) Interactive() bool         { return false }

// parseSubtaskInput extracts subtasks from the args map.
// Accepts the "subtasks" arg as []any of map[string]any.
func parseSubtaskInput(args map[string]any) []SubtaskInput {	raw, ok := args["subtasks"]
	if !ok || raw == nil {
		return nil
	}

	subSlice, ok := raw.([]any)
	if !ok {
		return nil
	}

	subtasks := make([]SubtaskInput, 0, len(subSlice))
	for _, s := range subSlice {
		subMap, ok := s.(map[string]any)
		if !ok {
			continue
		}
		subtask := SubtaskInput{}
		if title, ok := subMap["title"].(string); ok {
			subtask.Title = title
		}
		if wd, ok := subMap["working_dir"].(string); ok {
			subtask.WorkingDir = wd
		}
		if persona, ok := subMap["persona"].(string); ok {
			subtask.Persona = persona
		}
		if priority, ok := subMap["priority"].(string); ok {
			subtask.Priority = priority
		}
		if subtask.Title != "" {
			subtasks = append(subtasks, subtask)
		}
	}
	return subtasks
}
