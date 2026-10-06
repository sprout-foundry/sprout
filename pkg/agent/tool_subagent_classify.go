// Subagent tool classification: which tools spawn subagents (and so are gated
// by subagent depth) and how the seed event publisher labels them for the UI.
package agent

// isSubagentTool reports whether a tool runs subagents, for UI classification.
func isSubagentTool(toolName string) bool {
	switch toolName {
	case "run_subagent", "run_parallel_subagents", "review_changes":
		return true
	default:
		return false
	}
}

// subagentToolType is the UI grouping for a subagent tool's child runs.
func subagentToolType(toolName string) string {
	switch toolName {
	case "run_subagent":
		return "single"
	case "run_parallel_subagents", "review_changes":
		return "parallel"
	default:
		return ""
	}
}

// spawnsSubagentsAnyMode reports tools that spawn or manage subagents and are
// allowed in every context mode, subject only to the subagent depth limit.
// run_parallel_subagents is gated separately (also hidden in low-context mode).
func spawnsSubagentsAnyMode(toolName string) bool {
	switch toolName {
	case "run_subagent", "review_changes", "check_subagent", "stop_subagent":
		return true
	default:
		return false
	}
}
