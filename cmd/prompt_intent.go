//go:build !js

package cmd

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/zsh"
)

// PromptIntent labels a piece of submitted text by which of the main
// REPL's pre-LLM interception classes it would fall into. The empty
// string means freeform text destined for the model.
//
// Used by the steer / queue submit handlers (cmd/steer_coordinator.go)
// to reject submissions that would silently lose their command meaning
// if injected mid-turn or wrapped into the deferred-queue blockquote.
type PromptIntent string

const (
	IntentNone       PromptIntent = ""
	IntentSlash      PromptIntent = "slash command"
	IntentBangShell  PromptIntent = "shell command (! prefix)"
	IntentDetectedSh PromptIntent = "shell command"
)

// ClassifyPromptIntent mirrors the dispatch decisions the main REPL
// makes BEFORE handing a query to the LLM. The classifier returns
// the first matching category in the same precedence order the REPL
// uses:
//
//  1. Slash / bang prefix → registry.IsSlashCommand
//  2. Zsh-detected command (config-gated) → zsh.IsCommand
//
// Returns IntentNone for plain text. The chatAgent argument may be nil
// in tests; in that case the config-gated checks are skipped.
//
// Keep this in lockstep with cmd/agent_modes.go's main-prompt dispatch
// (the IsSlashCommand check and the TryZshCommandExecution fast-path
// block). If a new pre-LLM interception lands at the prompt, add it
// here too — otherwise the steer/queue panels will diverge from the
// prompt's behavior.
func ClassifyPromptIntent(chatAgent *agent.Agent, text string) PromptIntent {
	text = strings.TrimSpace(text)
	if text == "" {
		return IntentNone
	}

	// Slash + bang share the same registry check. IsSlashCommand
	// recognizes both "/foo" (with name validation) and "!cmd"
	// (with non-empty payload) so we distinguish them here only to
	// surface a more specific hint to the user.
	if agent_commands.DefaultRegistry().IsSlashCommand(text) {
		if strings.HasPrefix(text, "!") {
			return IntentBangShell
		}
		return IntentSlash
	}

	if chatAgent != nil {
		if cfg := chatAgent.GetConfig(); cfg != nil && cfg.EnableZshCommandDetection {
			if ok, info, err := zsh.IsCommand(text); err == nil && ok && info != nil {
				return IntentDetectedSh
			}
		}
	}

	return IntentNone
}
