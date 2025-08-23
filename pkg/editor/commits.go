package editor

import (
	"bufio"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
)

// GetChangeSummaries generates or asks for change summaries used in change records and commits.
func GetChangeSummaries(cfg *config.Config, newCode string, instructions string, newFilename string, reader *bufio.Reader) (note string, description string, commit string, err error) {
	note = "Changes made by ledit based on LLM suggestions."
	description = ""

	// Try to generate commit message up to 2 times
	for attempt := 0; attempt < 2; attempt++ {
		generatedDescription, err := llm.GetCommitMessage(cfg, newCode, instructions, newFilename)
		if err == nil && generatedDescription != "" {
			// Clean up the message first
			generatedDescription = git.CleanCommitMessage(generatedDescription)

			// Try to parse the cleaned message
			note, description, err = git.ParseCommitMessage(generatedDescription)
			if err == nil {
				return note, description, generatedDescription, nil
			}
		}
	}

	// If skip-prompt is true, do not ask the user for a description.
	if cfg.SkipPrompt {
		return "Changes made by ledit (skipped prompt)", "", "", nil
	}

	// Fallback to manual input
	ui.Out().Print(prompts.EnterDescriptionPrompt(newFilename))
	note, _ = reader.ReadString('\n')
	note = strings.TrimSpace(note)

	return note, description, "", nil
}
