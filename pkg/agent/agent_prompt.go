package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// SetSystemPromptFromFile loads a custom system prompt from a file
func (a *Agent) SetSystemPromptFromFile(filePath string) error {
	resolvedPath, err := resolvePromptPath(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve system prompt file: %w", err)
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			embeddedContent, embeddedErr := readEmbeddedPromptFile(filePath)
			if embeddedErr != nil {
				return fmt.Errorf("failed to read system prompt file: %w", err)
			}
			content = embeddedContent
		} else {
			return fmt.Errorf("failed to read system prompt file: %w", err)
		}
	}

	promptContent := strings.TrimSpace(string(content))
	if promptContent == "" {
		return fmt.Errorf("system prompt file is empty")
	}

	a.systemPrompt = a.ensureStopInformation(promptContent)
	return nil
}

func resolvePromptPath(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
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
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from cwd")
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
