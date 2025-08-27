//go:build !agent2refactor

package agent

import (
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// analyzeIntentType determines what type of request this is
func analyzeIntentType(userIntent string, logger *utils.Logger) IntentType {
	intentLower := strings.ToLower(userIntent)

	// Check for documentation tasks first - they're often misclassified
	documentationWords := []string{
		"document", "documentation", "generate docs", "create docs", "api docs",
		"generate documentation", "create documentation", "document endpoints",
		"document api", "api documentation", "endpoint documentation",
		"create api documentation", "generate api documentation",
	}
	for _, phrase := range documentationWords {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeDocumentation
		}
	}

	// Check for creation tasks
	creationWords := []string{
		"create new", "generate new", "build new", "setup new", "initialize new",
		"create a new", "generate a new", "build a new", "make a new",
		"create comprehensive", "generate comprehensive",
	}
	for _, phrase := range creationWords {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeCreation
		}
	}

	// Check for analysis tasks
	analysisWords := []string{
		"analyze", "analysis", "examine", "review", "inspect", "investigate",
		"understand", "explore", "assess", "evaluate",
	}
	for _, phrase := range analysisWords {
		if strings.Contains(intentLower, phrase) && !strings.Contains(intentLower, "create") && !strings.Contains(intentLower, "generate") {
			return IntentTypeAnalysis
		}
	}

	// Check for questions - be more specific to avoid false positives
	questionWords := []string{"what is", "what are", "what ", "how do", "how does", "how can", "how to", "why is", "why does", "when is", "where is", "which is", "who is", "can you explain", "can you describe"}
	for _, phrase := range questionWords {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeQuestion
		}
	}

	// Also check for common question starters
	questionStarters := []string{"what ", "how ", "why ", "when ", "where ", "which ", "who ", "list "}
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

// analyzeTaskIntent provides more detailed intent classification for strategy selection
func analyzeTaskIntent(userIntent string, logger *utils.Logger) TaskIntent {
	intentLower := strings.ToLower(userIntent)

	// Documentation tasks
	if strings.Contains(intentLower, "document") || strings.Contains(intentLower, "documentation") {
		return TaskIntentDocumentation
	}

	// Creation tasks
	creationKeywords := []string{"create", "generate", "build", "setup", "initialize", "new"}
	for _, keyword := range creationKeywords {
		if strings.Contains(intentLower, keyword) {
			// Check if it's creating documentation specifically
			if strings.Contains(intentLower, "doc") {
				return TaskIntentDocumentation
			}
			return TaskIntentCreation
		}
	}

	// Analysis tasks
	analysisKeywords := []string{"analyze", "examine", "review", "inspect", "understand", "explore"}
	for _, keyword := range analysisKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskIntentAnalysis
		}
	}

	// Refactoring tasks
	refactorKeywords := []string{"refactor", "restructure", "reorganize", "redesign", "migrate", "overhaul"}
	for _, keyword := range refactorKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskIntentRefactoring
		}
	}

	// Default to modification for typical code changes
	return TaskIntentModification
}

// inferProjectContext attempts to determine project context from workspace and intent
func inferProjectContext(workspacePath, userIntent string, logger *utils.Logger) *ProjectContext {
	ctx := &ProjectContext{
		Patterns: make(map[string]string),
	}

	intentLower := strings.ToLower(userIntent)

	// Language detection
	if strings.Contains(intentLower, "python") || strings.Contains(intentLower, ".py") {
		ctx.Language = "python"
	} else if strings.Contains(intentLower, "go") || strings.Contains(intentLower, ".go") {
		ctx.Language = "go"
	} else if strings.Contains(intentLower, "javascript") || strings.Contains(intentLower, ".js") {
		ctx.Language = "javascript"
	} else if strings.Contains(intentLower, "typescript") || strings.Contains(intentLower, ".ts") {
		ctx.Language = "typescript"
	}

	// Framework detection
	if strings.Contains(intentLower, "chalice") {
		ctx.Framework = "chalice"
		ctx.Language = "python"
		ctx.ProjectType = "api"
		ctx.Patterns["route_decorator"] = "@app.route"
		ctx.Patterns["route_file_suffix"] = "_routes.py"
	} else if strings.Contains(intentLower, "fastapi") {
		ctx.Framework = "fastapi"
		ctx.Language = "python"
		ctx.ProjectType = "api"
		ctx.Patterns["route_decorator"] = "@app."
	} else if strings.Contains(intentLower, "flask") {
		ctx.Framework = "flask"
		ctx.Language = "python"
		ctx.ProjectType = "api"
		ctx.Patterns["route_decorator"] = "@app.route"
	} else if strings.Contains(intentLower, "echo") || strings.Contains(intentLower, "gin") {
		ctx.Framework = "echo"
		ctx.Language = "go"
		ctx.ProjectType = "api"
	}

	// Project type detection
	if strings.Contains(intentLower, "api") || strings.Contains(intentLower, "endpoint") {
		ctx.ProjectType = "api"
	} else if strings.Contains(intentLower, "web") {
		ctx.ProjectType = "web"
	} else if strings.Contains(intentLower, "cli") {
		ctx.ProjectType = "cli"
	}

	// Output format for documentation
	if strings.Contains(intentLower, "markdown") || strings.Contains(intentLower, ".md") {
		ctx.OutputFormat = "markdown"
	} else if strings.Contains(intentLower, "json") {
		ctx.OutputFormat = "json"
	} else if ctx.ProjectType == "api" {
		ctx.OutputFormat = "markdown" // Default for API docs
	}

	return ctx
}
