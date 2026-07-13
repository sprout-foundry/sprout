//go:build !js

package cmd

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// buildSlashCommandCompleter returns a CompletionProvider that completes
// slash-command names against the current command registry, and
// delegates argument completion to commands that implement
// CompletableCommand (Phase 1 of argument autocomplete). Re-used by
// both the REPL prompt (Tab, via inputReader.SetCompleter) and the
// mid-turn steer panel (Ctrl-], via steerCoord.SetCompleter — SP-078
// Phase 2).
//
// Behavior:
//   - Without a space: command name completion (existing behavior).
//   - With a space: tries argument completion via CompletableCommand
//     on the resolved command.
//   - Case-insensitive prefix match in both paths.
//   - Re-builds the registry per call so newly-installed MCP commands
//     appear immediately.
func buildSlashCommandCompleter(chatAgent *agent.Agent) console.CompletionProvider {
	return func(line string, cursorPos int) []string {
		candidates := buildRichSlashCommandCompleter(chatAgent)(line, cursorPos)
		out := make([]string, len(candidates))
		for i, c := range candidates {
			out[i] = c.Text
		}
		return out
	}
}

// buildRichSlashCommandCompleter returns a RichCompletionProvider that
// includes command descriptions alongside the command names. Used by
// the live autocomplete dropdown so the user sees what each command does.
func buildRichSlashCommandCompleter(chatAgent *agent.Agent) console.RichCompletionProvider {
	return func(line string, cursorPos int) []console.CompletionCandidate {
		if !strings.HasPrefix(line, "/") || cursorPos != len(line) {
			return nil
		}

		registry := agent_commands.NewCommandRegistry()

		if !strings.ContainsAny(line, " \t") {
			prefix := strings.ToLower(line[1:])
			var matches []console.CompletionCandidate
			for _, name := range registry.CompletionCandidates() {
				if strings.HasPrefix(strings.ToLower(name), prefix) {
					desc := ""
					if cmd, ok := registry.GetCommand(name); ok {
						desc = cmd.Description()
					}
					matches = append(matches, console.CompletionCandidate{
						Text:        "/" + name,
						Description: desc,
					})
				}
			}
			return matches
		}

		// Argument completion path — return plain text candidates
		// (descriptions are less useful for sub-arguments).
		parts := strings.Fields(line)
		cmdName := strings.TrimPrefix(strings.ToLower(parts[0]), "/")
		cmd, exists := registry.GetCommand(cmdName)
		if !exists {
			return nil
		}

		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}
		if strings.HasSuffix(line, " ") {
			args = append(args, "")
		}

		if completable, ok := cmd.(agent_commands.CompletableCommand); ok {
			candidates := completable.Complete(args, chatAgent)
			if len(candidates) == 0 {
				return nil
			}
			var prefix string
			if len(parts) > 1 {
				prefix = strings.Join(parts[:len(parts)-1], " ") + " "
			} else {
				prefix = parts[0] + " "
			}
			result := make([]console.CompletionCandidate, len(candidates))
			for i, c := range candidates {
				result[i] = console.CompletionCandidate{Text: prefix + c}
			}
			return result
		}
		return nil
	}
}
