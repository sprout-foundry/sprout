//go:build !js

package cmd

import (
	"strings"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// buildSlashCommandCompleter returns a CompletionProvider that completes
// slash-command names against the current command registry. Re-used by
// both the REPL prompt (Tab, via inputReader.SetCompleter) and the
// mid-turn steer panel (Ctrl-], via steerCoord.SetCompleter — SP-078
// Phase 2).
//
// Behavior matches the original SP-048-2a wiring:
//   - Only matches when the buffer is a bare slash-prefix (`/foo`) with
//     the cursor at end-of-line.
//   - Stops matching once the user has typed a space or tab (the command
//     name is complete; we'd otherwise suggest argument completions we
//     don't have a registry for).
//   - Case-insensitive prefix match against the canonical command names
//     returned by agent_commands.CompletionCandidates.
//   - Re-builds the registry per call so newly-installed MCP commands
//     appear immediately.
func buildSlashCommandCompleter() console.CompletionProvider {
	return func(line string, cursorPos int) []string {
		if !strings.HasPrefix(line, "/") || cursorPos != len(line) {
			return nil
		}
		if strings.ContainsAny(line, " \t") {
			return nil
		}
		prefix := strings.ToLower(line[1:])
		registry := agent_commands.NewCommandRegistry()
		var matches []string
		for _, name := range registry.CompletionCandidates() {
			if strings.HasPrefix(strings.ToLower(name), prefix) {
				matches = append(matches, "/"+name)
			}
		}
		return matches
	}
}