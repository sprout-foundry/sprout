package approvals

// AccessModeForTool returns "write" for mutating tools and "read" for read-only tools.
func AccessModeForTool(toolName string) string {
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return "write"
	default:
		return "read"
	}
}
