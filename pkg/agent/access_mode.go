package agent

// accessModeForTool returns "write" for mutating tools and "read" for read-only tools.
func accessModeForTool(toolName string) string {
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return "write"
	default:
		return "read"
	}
}
