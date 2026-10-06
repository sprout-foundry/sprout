package commands

import (
	"errors"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// OriginalCommand prints the most recent held original the language guard
// kept for "view original": when a reply came back in the wrong language
// and was replaced (by the regenerated text or the localized notice), the
// mismatched text stays on the assistant message, and this is how the CLI
// user reads it.
type OriginalCommand struct {
	outputSink
}

func (c *OriginalCommand) Name() string {
	return "original"
}

// SafeDuringSteer returns true - /original only reads conversation state.
func (c *OriginalCommand) SafeDuringSteer() bool {
	return true
}

func (c *OriginalCommand) Description() string {
	return "Show the original text of the last language-corrected reply"
}

// Usage returns the detailed help text shown by `/help original`.
func (c *OriginalCommand) Usage() string {
	return strings.Join([]string{
		"/original   Show the original text of the last reply the language",
		"            guard replaced (a reply that came back in the wrong",
		"            language). Prints a notice when no reply has been",
		"            replaced in this session.",
	}, "\n")
}

func (c *OriginalCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}
	original := agent.LastLanguageGuardOriginal(chatAgent.GetMessages())
	if original == "" {
		console.GlyphInfo.Fprintf(c.out(), "No language-corrected reply in this session yet — nothing to show")
		return nil
	}
	console.GlyphDim.Fprintf(c.out(), "Original text of the last language-corrected reply:")
	c.print(original + "\r\n")
	return nil
}
