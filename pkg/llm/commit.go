package llm

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"time"
)

// GetCommitMessage generates a git commit message based on the code changes and the original prompt.
func GetCommitMessage(cfg *config.Config, changelog string, originalPrompt string, filename string) (string, error) {
	// For now, we can use the same model as for editing.
	// A future improvement could be to have a dedicated model for commits in the config.
	modelName := cfg.SummaryModel
	fmt.Printf("Using model for commit message: %s\n", modelName)

	messages := prompts.BuildCommitMessages(changelog, originalPrompt)

	// The first return value is the model name used, which we already have.
	// The second is the content, which is the commit message.
	// The third is the error.
	// The filename parameter is not relevant for this task, so we pass an empty string.
	// Adding a timeout of 1 minute for generating the commit message.
	_, commitMessage, err := GetLLMResponse(modelName, messages, filename, cfg, 1*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get commit message from LLM: %w", err)
	}

	return commitMessage, nil
}
