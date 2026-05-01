// Tool executor configuration: timeout defaults and constants.
package agent

import (
	"strconv"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const maxToolFailureMessageChars = 4000     // ~1000 tokens worst-case (4 chars/token heuristic)
const defaultFetchURLResultMaxChars = 80000 // Raised from 60000 to 80000 (better web content coverage)
const defaultFetchURLArchiveDir = "/tmp/sprout/downloads"
const defaultAnalyzeImageResultExcerptChars = 4000

// getToolTimeout returns the timeout duration for tool execution
// Subagents get 30 minutes (for large file operations), other tools get 5 minutes
// Can be overridden via SPROUT_TOOL_TIMEOUT environment variable (in seconds)
func getToolTimeout(toolName string) time.Duration {
	// Check for environment variable override first
	if envTimeout := configuration.GetEnvSimple("TOOL_TIMEOUT"); envTimeout != "" {
		if seconds, err := strconv.Atoi(envTimeout); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	// Tool-specific defaults
	if isSubagentTool(toolName) {
		return 30 * time.Minute
	}

	// Shell commands are fast when working correctly; long timeout just masks failures.
	if toolName == "shell_command" {
		return 2 * time.Minute
	}

	// Default timeout for regular tools
	return 5 * time.Minute
}

// isSubagentTool checks if a tool is a subagent that needs extended timeout
func isSubagentTool(toolName string) bool {
	switch toolName {
	case "run_subagent", "run_parallel_subagents":
		return true
	default:
		return false
	}
}
