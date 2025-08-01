package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// This file previously contained a GetCommitMessage function, which has been moved
// to pkg/llm/api.go to consolidate LLM API interactions and resolve a redeclaration error.
// This file is now empty as its sole function has been relocated.

// GetCommitMessage generates a git commit message based on code changes and the original prompt using an LLM.
// This function is the canonical GetCommitMessage, replacing the one in pkg/llm/commit.go.
func GetCommitMessage(cfg *config.Config, changelog string, originalPrompt string, filename string) (string, error) {
	modelName := cfg.SummaryModel
	if modelName == "" {
		modelName = cfg.EditingModel // Fallback if summary model is not configured
		fmt.Print(prompts.NoSummaryModelFallback(modelName))
	}

	messages := prompts.BuildCommitMessages(changelog, originalPrompt)

	_, response, err := GetLLMResponse(modelName, messages, filename, cfg, 1*time.Minute, false) // Commit message generation does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get commit message from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}
