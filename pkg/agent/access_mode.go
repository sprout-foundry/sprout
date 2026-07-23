package agent

// accessModeForTool returns "write" for tools that mutate files and "read"
// for tools that only read. Used by ResolveToolRisk and other callers that
// need to distinguish read vs write operations on the same path.
//
// SP-068 SP-127 synergy: path-tier verdicts differ between read and write
// (read_only allowlist entries only restrict writes), so the resolver must
// know the mode when classifying a file operation.
func accessModeForTool(toolName string) string {
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return "write"
	default:
		return "read"
	}
}
