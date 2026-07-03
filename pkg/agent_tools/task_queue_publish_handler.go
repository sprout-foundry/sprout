package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type taskQueuePublishHandler struct{}

func (h *taskQueuePublishHandler) Name() string { return "task_queue_publish" }

func (h *taskQueuePublishHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "task_queue_publish",
		Description: "Update a task in the persistent queue. Used to claim tasks (set status to in_progress), record completion, failed or blocked status, or publish failure. Optionally break a task into subtasks.",
		Required:    []string{"task_id", "status"},
		Parameters: []ParameterDef{
			{Name: "task_id", Type: "string", Required: true, Description: "The task ID to update"},
			{Name: "status", Type: "string", Required: true, Description: "New status: in_progress, completed, failed, or blocked"},
			{Name: "result", Type: "string", Description: "Summary of work done or error message"},
			{Name: "subtasks", Type: "array", Description: "Break down into subtasks. Each item: {title, working_dir, persona, priority}"},
		},
	}
}

func (h *taskQueuePublishHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "task_id")
	if err != nil {
		return err
	}
	_, err = extractString(args, "status")
	if err != nil {
		return err
	}
	status, _ := extractString(args, "status")
	if !validStatuses[status] {
		return agenterrors.NewValidation(fmt.Sprintf("parameter 'status' must be one of: pending, in_progress, completed, failed, blocked, got %q", status), nil)
	}
	return nil
}

func (h *taskQueuePublishHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	taskID, _ := extractString(args, "task_id")
	status, _ := extractString(args, "status")
	result, _ := extractString(args, "result")

	// Parse subtasks
	var subtasks []SubtaskInput
	if subRaw, ok := args["subtasks"]; ok && subRaw != nil {
		subSlice, ok := subRaw.([]interface{})
		if ok {
			for _, s := range subSlice {
				subMap, ok := s.(map[string]interface{})
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
				subtasks = append(subtasks, subtask)
			}
		}
	}

	tq := NewTaskQueue(DefaultTaskQueuePath())
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

	// Also return structured data
	outJSON, _ := json.Marshal(updated)
	return ToolResult{
		Output:        output,
		StructuredOut: string(outJSON),
	}, nil
}

func (h *taskQueuePublishHandler) Aliases() []string         { return nil }
func (h *taskQueuePublishHandler) Timeout() time.Duration    { return 0 }
func (h *taskQueuePublishHandler) MaxResultSize() int        { return 0 }
func (h *taskQueuePublishHandler) SafeForParallel() bool     { return false }
func (h *taskQueuePublishHandler) Interactive() bool         { return false }
