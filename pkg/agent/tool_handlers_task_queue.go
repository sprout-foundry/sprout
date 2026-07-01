package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// handleTaskQueueRead reads tasks from the persistent task queue
func handleTaskQueueRead(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract status parameter (default: "pending")
	status := "pending"
	if s, ok := args["status"].(string); ok && s != "" {
		status = s
	}

	// Extract limit parameter (default: 0, which means 10 in TaskQueue)
	limit := 0
	if v, ok := args["limit"]; ok {
		limit = normalizePositiveInt(v)
	}

	// Get queue path and create queue
	queuePath := getTaskQueuePath(a)
	queue := tools.NewTaskQueue(queuePath)

	// Read tasks (ReadTasks loads fresh from disk under a shared lock)
	tasks, err := queue.ReadTasks(ctx, status, limit)
	if err != nil {
		return "", agenterrors.NewTool("task_queue", "failed to read tasks", err)
	}

	// Format output
	if len(tasks) == 0 {
		return fmt.Sprintf("No tasks found with status '%s'.", status), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Tasks (%d of %s)\n\n", len(tasks), status))

	for _, task := range tasks {
		sb.WriteString(fmt.Sprintf("**Task:** %s\n", task.Title))
		sb.WriteString(fmt.Sprintf("**ID:** %s\n", task.ID))
		sb.WriteString(fmt.Sprintf("**Status:** %s | **Priority:** %s\n", task.Status, task.Priority))

		if task.WorkingDir != "" {
			sb.WriteString(fmt.Sprintf("**Working Dir:** %s\n", task.WorkingDir))
		}
		if task.Persona != "" {
			sb.WriteString(fmt.Sprintf("**Persona:** %s\n", task.Persona))
		}
		if task.Description != "" {
			desc := task.Description
			if len([]rune(desc)) > 200 {
				desc = string([]rune(desc)[:197]) + "..."
			}
			sb.WriteString(fmt.Sprintf("**Description:** %s\n", desc))
		}

		sb.WriteString(fmt.Sprintf("**Created:** %s | **Updated:** %s\n",
			task.CreatedAt.Format(time.RFC3339),
			task.UpdatedAt.Format(time.RFC3339)))
		sb.WriteString("---\n")
	}

	return sb.String(), nil
}

// handleTaskQueuePublish updates a task in the persistent queue
func handleTaskQueuePublish(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract required parameters
	taskID, err := getStringArg(args, "task_id")
	if err != nil {
		return "", agenterrors.Wrap(err, "task_id is required")
	}

	status, err := getStringArg(args, "status")
	if err != nil {
		return "", agenterrors.Wrap(err, "status is required")
	}

	// Extract optional result parameter
	result := ""
	if r, ok := args["result"].(string); ok {
		result = r
	}

	// Parse subtasks array
	var subtasks []tools.SubtaskInput
	if st, ok := args["subtasks"].([]interface{}); ok {
		subtasks = make([]tools.SubtaskInput, 0, len(st))
		for i, item := range st {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				return "", agenterrors.NewValidation(fmt.Sprintf("subtask at index %d must be an object", i), nil)
			}

			subtask := tools.SubtaskInput{}
			if title, ok := itemMap["title"].(string); ok {
				subtask.Title = title
			}
			if workingDir, ok := itemMap["working_dir"].(string); ok {
				subtask.WorkingDir = workingDir
			}
			if persona, ok := itemMap["persona"].(string); ok {
				subtask.Persona = persona
			}
			if priority, ok := itemMap["priority"].(string); ok {
				subtask.Priority = priority
			}

			if subtask.Title == "" {
				return "", agenterrors.NewValidation(fmt.Sprintf("subtask at index %d is missing required 'title' field", i), nil)
			}
			subtasks = append(subtasks, subtask)
		}
	}

	// Get queue path and create queue
	queuePath := getTaskQueuePath(a)
	queue := tools.NewTaskQueue(queuePath)

	// Publish task update (PublishTask loads fresh from disk under an exclusive lock)
	updatedTasks, err := queue.PublishTask(ctx, taskID, status, result, subtasks)
	if err != nil {
		return "", agenterrors.NewTool("task_queue", "failed to publish task update", err)
	}

	// Format output
	var sb strings.Builder
	if len(updatedTasks) == 0 {
		return "", agenterrors.NewTool("task_queue", "no tasks returned from PublishTask", nil)
	}

	updatedTask := updatedTasks[0]
	sb.WriteString(fmt.Sprintf("**Task Updated:** %s\n", updatedTask.Title))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", updatedTask.ID))
	sb.WriteString(fmt.Sprintf("**Status:** %s | **Priority:** %s\n", updatedTask.Status, updatedTask.Priority))
	if updatedTask.Result != "" {
		sb.WriteString(fmt.Sprintf("**Result:** %s\n", updatedTask.Result))
	}
	sb.WriteString(fmt.Sprintf("**Updated:** %s\n", updatedTask.UpdatedAt.Format(time.RFC3339)))

	// Add subtasks info (first task is the updated parent, rest are new subtasks)
	if len(updatedTasks) > 1 {
		sb.WriteString(fmt.Sprintf("**Subtasks Created:** %d\n", len(updatedTasks)-1))
		for i := 1; i < len(updatedTasks); i++ {
			st := updatedTasks[i]
			sb.WriteString(fmt.Sprintf("  - %s (ID: %s)\n", st.Title, st.ID))
		}
	}

	return sb.String(), nil
}

// handleTaskQueueAdd adds a new task to the persistent queue
func handleTaskQueueAdd(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract required parameter
	title, err := getStringArg(args, "title")
	if err != nil {
		return "", agenterrors.Wrap(err, "title is required")
	}

	// Extract optional parameters
	description := ""
	if d, ok := args["description"].(string); ok {
		description = d
	}

	priority := "medium" // default
	if p, ok := args["priority"].(string); ok && p != "" {
		priority = p
	}

	workingDir := ""
	if wd, ok := args["working_dir"].(string); ok {
		workingDir = wd
	}

	persona := ""
	if p, ok := args["persona"].(string); ok {
		persona = p
	}

	// Get queue path and create queue
	queuePath := getTaskQueuePath(a)
	queue := tools.NewTaskQueue(queuePath)

	// Add task (AddTask loads fresh from disk under an exclusive lock)
	task, err := queue.AddTask(ctx, title, description, priority, workingDir, persona)
	if err != nil {
		return "", agenterrors.NewTool("task_queue", "failed to add task", err)
	}

	// Format output
	var sb strings.Builder
	sb.WriteString("**Task Created:**\n\n")
	sb.WriteString(fmt.Sprintf("**Title:** %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("**Status:** %s | **Priority:** %s\n", task.Status, task.Priority))
	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:** %s\n", task.Description))
	}
	if task.WorkingDir != "" {
		sb.WriteString(fmt.Sprintf("**Working Dir:** %s\n", task.WorkingDir))
	}
	if task.Persona != "" {
		sb.WriteString(fmt.Sprintf("**Persona:** %s\n", task.Persona))
	}
	sb.WriteString(fmt.Sprintf("**Created:** %s\n", task.CreatedAt.Format(time.RFC3339)))

	return sb.String(), nil
}

// getTaskQueuePath returns the path to the task queue file
func getTaskQueuePath(a *Agent) string {
	// Check if agent has config with TaskQueuePath field
	if a != nil {
		cfg := a.GetConfig()
		if cfg != nil {
			// Note: TaskQueuePath field doesn't exist yet in Config
			// When it's added, we'll use: cfg.TaskQueuePath
			// For now, use default path
		}
	}
	return tools.DefaultTaskQueuePath()
}
