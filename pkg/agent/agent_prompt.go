package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SetSystemPromptFromFile loads a custom system prompt from a file
func (a *Agent) SetSystemPromptFromFile(filePath string) error {
	resolvedPath, err := resolvePromptPath(filePath)
	if err != nil {
		return agenterrors.NewPermanentError("failed to resolve system prompt file", err)
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			embeddedContent, embeddedErr := readEmbeddedPromptFile(filePath)
			if embeddedErr != nil {
				return agenterrors.NewPermanentError("failed to read system prompt file", err)
			}
			content = embeddedContent
		} else {
			return agenterrors.NewPermanentError("failed to read system prompt file", err)
		}
	}

	promptContent := strings.TrimSpace(string(content))
	if promptContent == "" {
		return agenterrors.NewInvalidInputError(fmt.Sprintf("system prompt file %q is empty", filePath), nil)
	}

	a.systemPrompt = a.ensureStopInformation(promptContent)
	return nil
}

func resolvePromptPath(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", errors.New("system prompt file path is empty")
	}

	// Preserve existing behavior first: relative paths resolve from cwd.
	if _, err := os.Stat(trimmed); err == nil {
		return trimmed, nil
	}

	if filepath.IsAbs(trimmed) {
		return trimmed, nil
	}

	// Fallback for repo-relative prompt paths like pkg/agent/prompts/... when cwd is nested.
	repoRoot, err := findRepoRootFromCWD()
	if err != nil {
		return trimmed, nil
	}
	candidate := filepath.Join(repoRoot, trimmed)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return trimmed, nil
}

func findRepoRootFromCWD() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", agenterrors.NewPermanentError("failed to get current working directory", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found from cwd")
		}
		dir = parent
	}
}

// ensureStopInformation preserves the original prompt content
func (a *Agent) ensureStopInformation(prompt string) string {
	return prompt
}

func resolveConfiguredSystemPrompt(cfg *configuration.Config, fallback string) string {
	if cfg == nil {
		return fallback
	}
	if prompt := strings.TrimSpace(cfg.SystemPromptText); prompt != "" {
		return prompt
	}
	return fallback
}
