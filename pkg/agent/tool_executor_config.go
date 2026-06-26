// Tool executor configuration: timeout defaults and constants.
package agent

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const maxToolFailureMessageChars = 4000     // ~1000 tokens worst-case (4 chars/token heuristic)
const defaultFetchURLResultMaxChars = 80000 // Raised from 60000 to 80000 (better web content coverage)
const defaultFetchURLArchiveDir = "/tmp/sprout/downloads"
const defaultAnalyzeImageResultExcerptChars = 4000
const defaultToolResultMaxChars = 50000 // Universal cap on tool result size (~12K tokens)

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
	// Long-running operations should use background=true to avoid blocking the agent.
	// The 2-minute limit prevents stuck commands from holding up the agent indefinitely.
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

// truncateToolResult truncates large tool results to prevent blowing up the LLM context window.
// Keeps the first 45K chars and last 5K chars with a truncation notice in between.
func truncateToolResult(result string) string {
	if len(result) <= defaultToolResultMaxChars {
		return result
	}

	headChars := 45000
	tailChars := 5000
	omitted := len(result) - headChars - tailChars

	packageLogWarnf("tool result truncated: %d -> %d chars (omitted %d)", len(result), defaultToolResultMaxChars, omitted)

	return result[:headChars] + fmt.Sprintf("\n[... truncated: %d chars omitted. Total was %d chars ...]\n", omitted, len(result)) + result[len(result)-tailChars:]
}
