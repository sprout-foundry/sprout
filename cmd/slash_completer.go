//go:build !js

package cmd

import (
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// completionCacheTTL is how long argument-completion results are cached
// before re-querying. Short enough that newly registered MCP servers or
// config changes appear within a second, long enough to prevent
// repeated network calls or config reads during rapid typing.
const completionCacheTTL = 500 * time.Millisecond

// slashCommandCache caches the command registry and argument-completion
// results so the autocomplete dropdown doesn't rebuild the registry or
// re-query providers/config on every keystroke.
type slashCommandCache struct {
	registry *agent_commands.CommandRegistry

	mu       sync.Mutex
	argCache map[string]argCacheEntry
}

type argCacheEntry struct {
	candidates []string
	expiresAt  time.Time
}

var globalSlashCache = &slashCommandCache{
	argCache: make(map[string]argCacheEntry),
}

// getRegistry returns the cached command registry, building it on first
// use. The registry is static within a session (MCP commands are
// resolved at execution time, not registration time).
func (c *slashCommandCache) getRegistry() *agent_commands.CommandRegistry {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.registry == nil {
		c.registry = agent_commands.NewCommandRegistry()
	}
	return c.registry
}

// getArgCompletions returns cached argument completions for a command,
// or calls the completion function and caches the result for completionCacheTTL.
func (c *slashCommandCache) getArgCompletions(cmdName string, args []string, chatAgent *agent.Agent, cmd agent_commands.Command) []string {
	// Build a cache key from the command name and args using NUL delimiter
	// to avoid collisions (args are whitespace-split so can't contain NUL).
	cacheKey := cmdName + "\x00" + strings.Join(args, "\x00")

	c.mu.Lock()
	if entry, ok := c.argCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.Unlock()
		return append([]string(nil), entry.candidates...)
	}
	c.mu.Unlock()

	// Compute the completion outside the lock
	var candidates []string
	if completable, ok := cmd.(agent_commands.CompletableCommand); ok {
		candidates = completable.Complete(args, chatAgent)
	}

	c.mu.Lock()
	c.argCache[cacheKey] = argCacheEntry{
		candidates: candidates,
		expiresAt:  time.Now().Add(completionCacheTTL),
	}
	// Prune expired entries to prevent unbounded growth
	now := time.Now()
	for k, v := range c.argCache {
		if !now.Before(v.expiresAt) {
			delete(c.argCache, k)
		}
	}
	c.mu.Unlock()

	return candidates
}

// buildSlashCommandCompleter returns a CompletionProvider that completes
// slash-command names against the current command registry, and
// delegates argument completion to commands that implement
// CompletableCommand (Phase 1 of argument autocomplete). Re-used by
// both the REPL prompt (Tab, via inputReader.SetCompleter) and the
// mid-turn steer panel (Ctrl-], via steerCoord.SetCompleter — SP-078
// Phase 2).
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

		registry := globalSlashCache.getRegistry()

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

		candidates := globalSlashCache.getArgCompletions(cmdName, args, chatAgent, cmd)
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
}
