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
		if !strings.HasPrefix(line, "/") || cursorPos != len(line) {
			return nil
		}

		registry := agent_commands.NewCommandRegistry()

		if !strings.ContainsAny(line, " \t") {
			// No space yet → command name completion (existing behavior).
			prefix := strings.ToLower(line[1:])
			var matches []string
			for _, name := range registry.CompletionCandidates() {
				if strings.HasPrefix(strings.ToLower(name), prefix) {
					matches = append(matches, "/"+name)
				}
			}
			return matches
		}

		// Space typed → try argument completion via CompletableCommand.
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil
		}
		cmdName := strings.TrimPrefix(strings.ToLower(parts[0]), "/")
		args := parts[1:]
		cmd, exists := registry.GetCommand(cmdName)
		if !exists {
			return nil
		}
					if completable, ok := cmd.(agent_commands.CompletableCommand); ok {
			candidates := completable.Complete(args, chatAgent)
			if len(candidates) == 0 {
				return nil
			}
			// Reconstruct the full-line prefix (everything before the word
			// being completed) so the CompletionProvider contract — which
			// expects full-line replacements — is satisfied.
			prefix := strings.Join(parts[:len(parts)-1], " ") + " "
			result := make([]string, len(candidates))
			for i, c := range candidates {
				result[i] = prefix + c
			}
			return result
		}
		return nil
	}
}