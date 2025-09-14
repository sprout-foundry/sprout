package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// debugLog logs a message only if debug mode is enabled
func (a *Agent) debugLog(format string, args ...interface{}) {
	if a.debug {
		fmt.Printf(format, args...)
	}
}

// getModelContextLimit returns the maximum context window for a model from the API
func (a *Agent) getModelContextLimit() int {
	limit, err := a.client.GetModelContextLimit()
	if err != nil {
		// Fallback to conservative default if API method fails
		if a.debug {
			a.debugLog("⚠️  Failed to get model context limit: %v, using default\n", err)
		}
		return 32000
	}
	return limit
}

// ToolLog logs tool execution messages that are always visible with blue formatting
func (a *Agent) ToolLog(action, target string) {
	const blue = "\033[34m"
	const reset = "\033[0m"

	// Ensure we start at column 1
	fmt.Print("\r\033[K") // Move to start of line and clear

	// Format: [4:(15.2K/120K)] read file filename.go
	contextInfo := fmt.Sprintf("[%d:(%s/%s)]",
		a.currentIteration,
		a.formatTokenCount(a.currentContextTokens),
		a.formatTokenCount(a.maxContextTokens))

	if target != "" {
		fmt.Printf("%s%s %s%s %s\n", blue, contextInfo, action, reset, target)
	} else {
		fmt.Printf("%s%s %s%s\n", blue, contextInfo, action, reset)
	}
}

// estimateContextTokens estimates the token count for messages
func (a *Agent) estimateContextTokens(messages []api.Message) int {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content)
		totalChars += len(msg.ReasoningContent)
	}
	// Rough estimate: 4 chars per token (conservative)
	return totalChars / 4
}

// formatTokenCount formats token count with thousands/millions separators
func (a *Agent) formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	} else if tokens < 1000000 {
		// Convert to thousands format with one decimal place
		thousands := float64(tokens) / 1000.0
		return fmt.Sprintf("%.1fK", thousands)
	} else {
		// Convert to millions format with two decimal places
		millions := float64(tokens) / 1000000.0
		return fmt.Sprintf("%.2fM", millions)
	}
}

// suggestCorrectToolName suggests the correct tool name for common mistakes
func (a *Agent) suggestCorrectToolName(invalidName string) string {
	// Common tool name mappings
	corrections := map[string]string{
		"exec":    "shell_command",
		"bash":    "shell_command",
		"cmd":     "shell_command",
		"command": "shell_command",
		"run":     "shell_command",
		"execute": "shell_command",
		"read":    "read_file",
		"cat":     "read_file",
		"open":    "read_file",
		"write":   "write_file",
		"save":    "write_file",
		"create":  "write_file",
		"edit":    "edit_file",
		"modify":  "edit_file",
		"change":  "edit_file",
		"replace": "edit_file",
		"todo":    "add_todo",
		"task":    "add_todo",
		"update":  "update_todo_status",
		"status":  "update_todo_status",
		"list":    "list_todos",
		"show":    "list_todos",
	}

	if suggestion, exists := corrections[strings.ToLower(invalidName)]; exists {
		return suggestion
	}

	return ""
}

// getProviderEnvVar returns the environment variable name for a provider
func (a *Agent) getProviderEnvVar(provider api.ClientType) string {
	switch provider {
	case api.OpenAIClientType:
		return "OPENAI_API_KEY"
	case api.DeepInfraClientType:
		return "DEEPINFRA_API_KEY"
	case api.CerebrasClientType:
		return "CEREBRAS_API_KEY"
	case api.OpenRouterClientType:
		return "OPENROUTER_API_KEY"
	case api.GroqClientType:
		return "GROQ_API_KEY"
	case api.DeepSeekClientType:
		return "DEEPSEEK_API_KEY"
	case api.OllamaClientType:
		return "" // Ollama doesn't use an API key
	default:
		return ""
	}
}
