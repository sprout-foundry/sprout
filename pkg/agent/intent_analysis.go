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
	commandPrefixes := []string{"run ", "execute ", "start ", "stop ", "build ", "test ", "deploy ", "install ", "uninstall "}
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
	codeWords := []string{"add ", "create ", "implement ", "fix ", "update ", "change ", "modify ", "refactor ", "delete ", "remove ", "rename ", "move ", "extract ", "function", "class", "method", "variable"}
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
