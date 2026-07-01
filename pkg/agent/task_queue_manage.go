// Consolidated task queue tool.
//
// One handler that dispatches on `operation` to the existing per-op
// helpers — replaces task_queue_read / task_queue_publish /
// task_queue_add so the LLM only sees one entry for queue management.
package agent

import (
	"context"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// handleTaskQueue routes to the legacy per-operation handlers based on
// `operation`. Errors when `operation` is missing or unknown.
//
// Supported operations:
//
//   - "read"    args: status (optional), limit (optional)
//   - "add"     args: title (required), description, priority,
//     working_dir, persona (optional)
//   - "publish" args: task_id (required), status (required),
//     result (optional), subtasks (optional)
func handleTaskQueue(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rawOp, _ := args["operation"].(string)
	op := strings.TrimSpace(strings.ToLower(rawOp))
	if op == "" {
		return "", agenterrors.NewValidation("task_queue: 'operation' is required (one of: read, add, publish)", nil)
	}

	switch op {
	case "read":
		return handleTaskQueueRead(ctx, a, args)
	case "add":
		return handleTaskQueueAdd(ctx, a, args)
	case "publish":
		return handleTaskQueuePublish(ctx, a, args)
	default:
		return "", agenterrors.NewValidation(fmt.Sprintf("task_queue: unknown operation %q (want read, add, or publish)", rawOp), nil)
	}
}
