// Subagent tool classification used by the seed event publisher to classify
// subagent events for the WebUI.
package agent

// isSubagentTool checks if a tool is a subagent that needs extended timeout
func isSubagentTool(toolName string) bool {
	switch toolName {
	case "run_subagent", "run_parallel_subagents":
		return true
	default:
		return false
	}
}
