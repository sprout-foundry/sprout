package cmd

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// countingCompletableCommand is a fake CompletableCommand that records
// how many times Complete is called so tests can assert the cache
// actually avoids recomputation.
type countingCompletableCommand struct {
	name  string
	calls int
	// complete returns the candidate list for the given args. Mirrors the
	// real contract: last arg is the prefix being typed.
	complete func(args []string) []string
}

func (c *countingCompletableCommand) Name() string        { return c.name }
func (c *countingCompletableCommand) Description() string { return "test command" }
func (c *countingCompletableCommand) Execute([]string, *agent.Agent) error {
	return nil
}
func (c *countingCompletableCommand) Complete(args []string, _ *agent.Agent) []string {
	c.calls++
	if c.complete != nil {
		return c.complete(args)
	}
	return nil
}

func newTestSlashCache() *slashCommandCache {
	return &slashCommandCache{argCache: make(map[string]argCacheEntry)}
}

func TestGetArgCompletionsProgressiveTypingReusesCache(t *testing.T) {
	cache := newTestSlashCache()
	cmd := &countingCompletableCommand{
		name: "model",
		complete: func(args []string) []string {
			return []string{"deepseek-ai/DeepSeek-V3.1-Terminus", "gpt-5-mini", "GLM-4.6"}
		},
	}

	// The space keystroke: args = [""], prefix "" -> full list cached.
	got := cache.getArgCompletions("model", []string{""}, nil, cmd)
	if len(got) != 3 {
		t.Fatalf("space: got %d candidates, want 3", len(got))
	}
	if cmd.calls != 1 {
		t.Fatalf("space: Complete called %d times, want 1", cmd.calls)
	}

	// Progressive typing must be served from the cache — no recompute.
	for _, prefix := range []string{"d", "de", "deep", "g", "gl"} {
		got = cache.getArgCompletions("model", []string{prefix}, nil, cmd)
		wantPrefix := strings.ToLower(prefix)
		for _, cand := range got {
			if !strings.HasPrefix(strings.ToLower(cand), wantPrefix) {
				t.Errorf("prefix %q: candidate %q does not match prefix", prefix, cand)
			}
		}
	}
	if cmd.calls != 1 {
		t.Fatalf("progressive typing: Complete called %d times, want 1 (cache should serve all prefixes)", cmd.calls)
	}

	// Backspace to a shorter prefix also stays cached (full list was
	// computed with the empty prefix, so every prefix extends it).
	got = cache.getArgCompletions("model", []string{"d"}, nil, cmd)
	if len(got) != 1 || !strings.HasPrefix(got[0], "deepseek-ai/DeepSeek-V3.1-Terminus") {
		t.Fatalf("backspace: got %v, want [deepseek-ai/DeepSeek-V3.1-Terminus]", got)
	}
	if cmd.calls != 1 {
		t.Fatalf("backspace: Complete called %d times, want 1", cmd.calls)
	}
}

func TestGetArgCompletionsContextChangeRecomputes(t *testing.T) {
	cache := newTestSlashCache()
	cmd := &countingCompletableCommand{
		name: "mcp",
		complete: func(args []string) []string {
			if len(args) > 0 && args[0] == "remove" {
				return []string{"server-a", "server-b"}
			}
			return []string{"add", "list", "remove", "test"}
		},
	}

	// First context: subcommand list.
	got := cache.getArgCompletions("mcp", []string{""}, nil, cmd)
	if len(got) != 4 {
		t.Fatalf("subcommand context: got %d, want 4", len(got))
	}
	if cmd.calls != 1 {
		t.Fatalf("subcommand context: calls=%d, want 1", cmd.calls)
	}

	// Typing the subcommand reuses the cache.
	got = cache.getArgCompletions("mcp", []string{"re"}, nil, cmd)
	if len(got) != 1 || got[0] != "remove" {
		t.Fatalf("typed subcommand: got %v, want [remove]", got)
	}
	if cmd.calls != 1 {
		t.Fatalf("typed subcommand: calls=%d, want 1", cmd.calls)
	}

	// Switching to the "remove" context must recompute.
	got = cache.getArgCompletions("mcp", []string{"remove", ""}, nil, cmd)
	if len(got) != 2 {
		t.Fatalf("remove context: got %d, want 2", len(got))
	}
	if cmd.calls != 2 {
		t.Fatalf("remove context: calls=%d, want 2", cmd.calls)
	}

	// Progressive typing within the remove context stays cached.
	got = cache.getArgCompletions("mcp", []string{"remove", "ser"}, nil, cmd)
	if len(got) != 2 {
		t.Fatalf("remove typed: got %v, want both servers", got)
	}
	if cmd.calls != 2 {
		t.Fatalf("remove typed: calls=%d, want 2", cmd.calls)
	}
}

func TestGetArgCompletionsNonMatchingPrefixRecomputes(t *testing.T) {
	// A fake path-completion command: the candidate list depends on the
	// full prefix (like PathCompleter walking a directory named by the
	// prefix), so an empty filter result must trigger a fresh call.
	cache := newTestSlashCache()
	cmd := &countingCompletableCommand{
		name: "review",
		complete: func(args []string) []string {
			prefix := ""
			if len(args) > 0 {
				prefix = args[len(args)-1]
			}
			switch prefix {
			case "", ".", "./":
				return []string{"src/", "main.go", "README.md"}
			case "src/":
				return []string{"src/main.go", "src/util.go"}
			case "src/ma":
				return []string{"src/main.go"}
			default:
				return nil
			}
		},
	}

	// Space: full "." listing cached.
	got := cache.getArgCompletions("review", []string{""}, nil, cmd)
	if len(got) != 3 {
		t.Fatalf("space: got %v, want the dir listing", got)
	}
	if cmd.calls != 1 {
		t.Fatalf("space: calls=%d, want 1", cmd.calls)
	}

	// Typing "s" filters the "." listing locally.
	got = cache.getArgCompletions("review", []string{"s"}, nil, cmd)
	if len(got) != 1 || got[0] != "src/" {
		t.Fatalf("s: got %v, want [src/]", got)
	}
	if cmd.calls != 1 {
		t.Fatalf("s: calls=%d, want 1 (local filter)", cmd.calls)
	}

	// Crossing into the directory ("src/") filters to nothing from the
	// "." listing, so it must recompute and walk src/.
	got = cache.getArgCompletions("review", []string{"src/"}, nil, cmd)
	if len(got) != 2 {
		t.Fatalf("src/: got %v, want the src/ listing", got)
	}
	if cmd.calls != 2 {
		t.Fatalf("src/: calls=%d, want 2 (dir crossing recompute)", cmd.calls)
	}

	// Deeper prefix extends the cached src/ listing.
	got = cache.getArgCompletions("review", []string{"src/ma"}, nil, cmd)
	if len(got) != 1 || got[0] != "src/main.go" {
		t.Fatalf("src/ma: got %v, want [src/main.go]", got)
	}
	if cmd.calls != 2 {
		t.Fatalf("src/ma: calls=%d, want 2 (extend cached listing)", cmd.calls)
	}

	// Backspace out of the directory recomputes with the shorter prefix.
	got = cache.getArgCompletions("review", []string{"src/"}, nil, cmd)
	if len(got) != 2 {
		t.Fatalf("backspace to src/: got %v, want the src/ listing", got)
	}
	if cmd.calls != 3 {
		t.Fatalf("backspace to src/: calls=%d, want 3", cmd.calls)
	}
}

func TestGetArgCompletionsExpiryRecomputes(t *testing.T) {
	cache := newTestSlashCache()
	cmd := &countingCompletableCommand{
		name: "tools",
		complete: func(args []string) []string {
			prefix := ""
			if len(args) > 0 {
				prefix = args[len(args)-1]
			}
			all := []string{"on", "off", "toggle"}
			var out []string
			for _, cand := range all {
				if strings.HasPrefix(strings.ToLower(cand), strings.ToLower(prefix)) {
					out = append(out, cand)
				}
			}
			return out
		},
	}

	got := cache.getArgCompletions("tools", []string{""}, nil, cmd)
	if len(got) != 3 {
		t.Fatalf("first: got %v", got)
	}
	if cmd.calls != 1 {
		t.Fatalf("first: calls=%d, want 1", cmd.calls)
	}

	// Force the entry to expire, then verify a recompute happens.
	cache.mu.Lock()
	cache.argCache["tools\x00"] = argCacheEntry{
		candidates:     []string{"on", "off", "toggle"},
		computedPrefix: "",
		expiresAt:      time.Now().Add(-time.Second),
	}
	cache.mu.Unlock()

	got = cache.getArgCompletions("tools", []string{"o"}, nil, cmd)
	if len(got) != 2 {
		t.Fatalf("after expiry: got %v, want [on off]", got)
	}
	if cmd.calls != 2 {
		t.Fatalf("after expiry: calls=%d, want 2", cmd.calls)
	}
}

func TestGetArgCompletionsEmptyFetchDoesNotBlockTyping(t *testing.T) {
	// A command whose fetch returns nothing (e.g. /model with a flaky
	// registry): typing a prefix must NOT re-trigger the expensive
	// Complete() call on every keystroke once the empty result is cached.
	cache := newTestSlashCache()
	cmd := &countingCompletableCommand{
		name: "model",
		complete: func(args []string) []string {
			return nil
		},
	}

	got := cache.getArgCompletions("model", []string{""}, nil, cmd)
	if got != nil {
		t.Fatalf("empty fetch: got %v, want nil", got)
	}
	if cmd.calls != 1 {
		t.Fatalf("empty fetch: calls=%d, want 1", cmd.calls)
	}

	// Progressive typing stays silent and cached — no recompute loop.
	for _, prefix := range []string{"d", "de", "z", "zz"} {
		got = cache.getArgCompletions("model", []string{prefix}, nil, cmd)
		if got != nil {
			t.Fatalf("prefix %q: got %v, want nil", prefix, got)
		}
	}
	if cmd.calls != 1 {
		t.Fatalf("typing after empty fetch: Complete called %d times, want 1 (no recompute loop)", cmd.calls)
	}
}

// TestBuildRichSlashCommandCompleterArgumentPath exercises the live
// dropdown completer against the real registry. /tools implements
// CompletableCommand with static arguments; a nil chatAgent is fine
// because ToolsCommand.Complete ignores the agent.
func TestBuildRichSlashCommandCompleterArgumentPath(t *testing.T) {
	completer := buildRichSlashCommandCompleter(nil)
	globalSlashCache.mu.Lock()
	globalSlashCache.argCache = make(map[string]argCacheEntry)
	globalSlashCache.mu.Unlock()

	// No space: command-name completion phase.
	cands := completer("/tool", len("/tool"))
	var names []string
	for _, c := range cands {
		names = append(names, c.Text)
	}
	if len(names) == 0 {
		t.Fatal("command-name phase: no candidates for /tool")
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "/tool") {
			t.Errorf("command-name phase: %q does not start with /tool", n)
		}
	}

	// Space: argument completion phase, full list.
	cands = completer("/tools ", len("/tools "))
	names = names[:0]
	for _, c := range cands {
		names = append(names, strings.TrimPrefix(c.Text, "/tools "))
	}
	sort.Strings(names)
	want := []string{"off", "on", "toggle"}
	if len(names) != len(want) {
		t.Fatalf("argument phase: got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("argument phase: got %v, want %v", names, want)
		}
	}

	// Typed prefix filters the cached list.
	cands = completer("/tools o", len("/tools o"))
	names = names[:0]
	for _, c := range cands {
		names = append(names, strings.TrimPrefix(c.Text, "/tools "))
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "off" || names[1] != "on" {
		t.Fatalf("typed prefix: got %v, want [off on]", names)
	}
}
