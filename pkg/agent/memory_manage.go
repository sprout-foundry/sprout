// Consolidated memory tool.
//
// One handler that dispatches on `operation` to the existing per-op
// helpers — replaces add_memory / read_memory / list_memories /
// delete_memory / search_memories so the LLM only sees one entry for
// memory management.
package agent

import (
	"context"
	"fmt"
	"strings"
)

// handleManageMemory routes to the legacy per-operation handlers based
// on `operation`. Errors when `operation` is missing or unknown.
//
// Supported operations:
//
//   - "add"    args: name (required), content (required)
//   - "read"   args: name (required)
//   - "list"   args: (none)
//   - "delete" args: name (required)
//   - "search" args: query (required), threshold (optional, default 0.75),
//     top_k (optional, default 5)
func handleManageMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rawOp, _ := args["operation"].(string)
	op := strings.TrimSpace(strings.ToLower(rawOp))
	if op == "" {
		return "", fmt.Errorf("manage_memory: 'operation' is required (one of: add, read, list, delete, search)")
	}

	switch op {
	case "add":
		return handleAddMemory(ctx, a, args)
	case "read":
		return handleReadMemory(ctx, a, args)
	case "list":
		return handleListMemories(ctx, a, args)
	case "delete":
		return handleDeleteMemory(ctx, a, args)
	case "search":
		return handleSearchMemories(ctx, a, args)
	default:
		return "", fmt.Errorf("manage_memory: unknown operation %q (want add, read, list, delete, or search)", rawOp)
	}
}
