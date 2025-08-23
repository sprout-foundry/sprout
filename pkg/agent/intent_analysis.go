//go:build !agent2refactor

package agent

import (
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// analyzeIntentType determines what type of request this is
func analyzeIntentType(userIntent string, logger *utils.Logger) IntentType {
	intentLower := strings.ToLower(userIntent)

	// Check for questions - be more specific to avoid false positives
	questionWords := []string{"what is", "what are", "how do", "how does", "how can", "how to", "why is", "why does", "when is", "where is", "which is", "who is", "can you explain", "can you describe"}
	for _, phrase := range questionWords {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeQuestion
		}
	}

	// Also check for common question starters
	questionStarters := []string{"what ", "how ", "why ", "when ", "where ", "which ", "who "}
	for _, starter := range questionStarters {
		if strings.HasPrefix(intentLower, starter) {
			return IntentTypeQuestion
		}
	}

	// Check for commands - be more specific to avoid false positives
	// Only treat as commands if they look like actual shell commands
	commandPrefixes := []string{"run ", "execute ", "start ", "stop ", "build ", "deploy ", "install ", "uninstall "}

	// Special handling for "test" - only treat as command if followed by actual test commands
	if strings.HasPrefix(intentLower, "test ") {
		testCommands := []string{"test run", "test build", "test deploy", "test install", "test start", "test stop"}
		isActualTestCommand := false
		for _, cmd := range testCommands {
			if strings.HasPrefix(intentLower, cmd) {
				isActualTestCommand = true
				break
			}
		}
		if !isActualTestCommand {
			return IntentTypeCodeUpdate // "test the agent" should be a code update, not a command
		}
	}

	for _, prefix := range commandPrefixes {
		if strings.HasPrefix(intentLower, prefix) {
			return IntentTypeCommand
		}
	}

	// Check for file extensions - if the intent mentions specific files, it's likely a code update
	if strings.Contains(intentLower, ".go") || strings.Contains(intentLower, ".py") ||
		strings.Contains(intentLower, ".js") || strings.Contains(intentLower, ".ts") {
		return IntentTypeCodeUpdate
	}

	// Check for code-related keywords that indicate code updates
	codeWords := []string{"add ", "create ", "implement ", "fix ", "update ", "change ", "modify ", "refactor ", "delete ", "remove ", "rename ", "move ", "extract ", "test ", "function", "class", "method", "variable"}
	for _, word := range codeWords {
		if strings.Contains(intentLower, word) {
			return IntentTypeCodeUpdate
		}
	}

	// Check for command-like patterns that are actually code updates
	commandLikeButCode := []string{" add", " create", " fix", " update", " change", " modify"}
	for _, phrase := range commandLikeButCode {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeCodeUpdate
		}
	}

	// Default to code update for anything else
	return IntentTypeCodeUpdate
}
