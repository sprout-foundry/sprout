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
// config changes appear within a second of the user pausing, long enough
// to prevent repeated network calls or config reads during rapid typing.
// The TTL is refreshed on every cache hit (sliding window), so a user
// typing continuously never re-queries — the recompute only fires after
// ~TTL of inactivity on a given command context.
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
	// computedPrefix is the last-argument prefix the candidates were
	// computed with. While the user's current prefix extends it, the
	// cached list is a valid superset and can be filtered in memory.
	computedPrefix string
	expiresAt      time.Time
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
		c.registry = agent_commands.DefaultRegistry()
	}
	return c.registry
}

// getArgCompletions returns cached argument completions for a command,
// filtered to the last-argument prefix currently being typed.
//
// The cache is keyed on the command name plus all args EXCEPT the last
// one — the "context". The last arg is the prefix the user is actively
// typing, so excluding it from the key means progressive keystrokes
// ("/model d", "/model de") reuse ONE computed candidate list instead
// of re-running the command's Complete() for every new prefix. Without
// this, every keystroke after a space misses the cache and re-executes
// Complete — which for commands like /model makes a network call to the
// provider API, stalling the input loop (the "laggy after a space"
// report).
//
// On a hit, if the current prefix extends the prefix the candidates were
// computed with, the cached list is filtered in memory (no recompute).
// A recompute happens only when the prefix doesn't extend it (backspace,
// unrelated edit), when filtering yields nothing (path completions
// crossing a directory boundary, e.g. "/review src/ma" needs a fresh
// walk of src/), or after the sliding TTL expires.
func (c *slashCommandCache) getArgCompletions(cmdName string, args []string, chatAgent *agent.Agent, cmd agent_commands.Command) []string {
	// The argument path is only reached when the line contains a space or
	// tab, but a trailing tab can leave args empty (Fields drops it).
	if len(args) == 0 {
		args = []string{""}
	}
	context := args[:len(args)-1]
	prefix := args[len(args)-1]

	// Build a cache key from the command name and context using NUL
	// delimiter to avoid collisions (args are whitespace-split so can't
	// contain NUL).
	cacheKey := cmdName + "\x00" + strings.Join(context, "\x00")

	c.mu.Lock()
	entry, ok := c.argCache[cacheKey]
	valid := ok && time.Now().Before(entry.expiresAt)
	// A prefix ending in a path separator ("/review src/") always needs a
	// fresh walk of the directory — the cached listing of the parent can
	// only ever match the directory entry itself, not its contents.
	dirBoundary := strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, `\`)
	if valid && !dirBoundary && strings.HasPrefix(strings.ToLower(prefix), strings.ToLower(entry.computedPrefix)) {
		if filtered := filterPrefix(entry.candidates, prefix); len(filtered) > 0 {
			// Sliding TTL: keep the entry alive while the user keeps
			// typing on this context so pauses don't re-trigger the
			// expensive Complete() call.
			entry.expiresAt = time.Now().Add(completionCacheTTL)
			c.argCache[cacheKey] = entry
			c.mu.Unlock()
			return filtered
		}
		// No candidates match. For word completions the cached list is
		// authoritative, so return empty without recomputing — this keeps
		// a failed or empty fetch (e.g. /model with a flaky registry)
		// from blocking every subsequent keystroke. Only a directory
		// crossing (prefix contains a path separator) falls through to
		// recompute, because path completions must walk the new directory.
		if !strings.ContainsAny(prefix, `/\`) {
			c.mu.Unlock()
			return nil
		}
	}
	c.mu.Unlock()

	// Compute the completion outside the lock
	var candidates []string
	if completable, ok := cmd.(agent_commands.CompletableCommand); ok {
		candidates = completable.Complete(args, chatAgent)
	}

	c.mu.Lock()
	c.argCache[cacheKey] = argCacheEntry{
		candidates:     candidates,
		computedPrefix: prefix,
		expiresAt:      time.Now().Add(completionCacheTTL),
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

// filterPrefix returns the candidates that start with prefix, matching
// the case-insensitive prefix semantics the CompletableCommand
// implementations use. An empty prefix returns all candidates.
func filterPrefix(candidates []string, prefix string) []string {
	if prefix == "" {
		return candidates
	}
	lower := strings.ToLower(prefix)
	out := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if strings.HasPrefix(strings.ToLower(cand), lower) {
			out = append(out, cand)
		}
	}
	return out
}

// buildSlashCommandCompleter returns a CompletionProvider that completes
// slash-command names against the current command registry, and
// delegates argument completion to commands that implement
// CompletableCommand (Phase 1 of argument autocomplete). Re-used by
// both the REPL prompt (Tab, via inputReader.SetCompleter) and the
// mid-turn steer panel (Ctrl-], via steerCoord.SetCompleter — SP-078
// Phase 2).
//
// This is the lightweight path — it calls the registry directly without
// building CompletionCandidate structs, avoiding the allocation that
// buildRichSlashCommandCompleter does. Used for Tab cycle completion
// where descriptions aren't rendered.
func buildSlashCommandCompleter(chatAgent *agent.Agent) console.CompletionProvider {
	return func(line string, cursorPos int) []string {
		if !strings.HasPrefix(line, "/") || cursorPos != len(line) {
			return nil
		}

		registry := globalSlashCache.getRegistry()

		if !strings.ContainsAny(line, " \t") {
			prefix := strings.ToLower(line[1:])
			var matches []string
			for _, name := range registry.CompletionCandidates() {
				if strings.HasPrefix(strings.ToLower(name), prefix) {
					matches = append(matches, "/"+name)
				}
			}
			return matches
		}

		// Argument completion path
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
		result := make([]string, len(candidates))
		for i, c := range candidates {
			result[i] = prefix + c
		}
		return result
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
