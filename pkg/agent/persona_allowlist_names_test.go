package agent

import (
	"sort"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// dynamicallyRegisteredTools are advertised by runtime registration rather than
// the static tool registry, so they legitimately appear in an allowlist without
// being findable in BuildToolDefinitions.
//
// computer-use tools are installed by computer_use_registration.go only when
// the computer_use config flag is on; mcp_tools is a synthetic entry added by
// BuildToolDefinitions itself for MCP discovery.
var dynamicallyRegisteredTools = map[string]bool{
	"take_screenshot": true,
	"mouse_click":     true,
	"mouse_drag":      true,
	"keyboard_type":   true,
	"keyboard_press":  true,
	"scroll":          true,
	"wait":            true,
	"mcp_tools":       true,
}

// A persona allowlist entry that matches no registered tool is a silently
// missing ADVERTISEMENT. filterToolsByName compares tool.Function.Name with an
// exact string equal, so a stale name never matches and the tool is absent from
// the schema that persona shows the model, with no error anywhere.
//
// It is not a capability boundary. getActivePersonaToolAllowlist is consulted
// in exactly one place — conversation.go, where the tool schema is assembled —
// and nothing on the execution path checks it. A model that knows the name from
// the system prompt or from a previous session can call an unadvertised tool
// and it will run. Verified directly: with the orchestrator persona active,
// list_directory is absent from the advertised list and ExecuteTool still
// succeeds. What a stale name actually costs is the schema: the model calls the
// tool guessing its parameters instead of being told them.
//
// This has bitten three separate times in this codebase:
//
//   - "TodoWrite" / "TodoRead" in EVERY persona, while the handlers register as
//     todo_write / todo_read — so the todo tools were unreachable for every
//     persona including the default, while the system prompt instructed the
//     model to use them.
//   - "search_memories" / "save_memory", which are operations of manage_memory
//     rather than tools in their own right.
//   - "show_my_change" / "summarize_my_session" / "my_recent_changes" in eight
//     personas, naming tools that do not exist anywhere in the source.
//
// Each was invisible because nothing cross-checks the two lists. This test is
// that cross-check.
func TestEveryPersonaAllowlistEntryNamesARegisteredTool(t *testing.T) {
	registered := map[string]bool{}
	for _, tool := range BuildToolDefinitions() {
		registered[tool.Function.Name] = true
	}
	// Hidden tools stay callable and may legitimately be named in an allowlist,
	// so consult the registry rather than only the advertised roster.
	for name := range allRegisteredToolNames() {
		registered[name] = true
	}
	if len(registered) == 0 {
		t.Fatal("no tools registered — the check would pass vacuously")
	}

	defs, err := personas.DefaultDefinitions()
	if err != nil {
		t.Fatalf("load persona definitions: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("no personas loaded — the check would pass vacuously")
	}

	type miss struct{ persona, tool string }
	var misses []miss
	for id, def := range defs {
		for _, name := range def.AllowedTools {
			name = strings.TrimSpace(name)
			if name == "" || registered[name] || dynamicallyRegisteredTools[name] {
				continue
			}
			misses = append(misses, miss{id, name})
		}
	}

	sort.Slice(misses, func(i, j int) bool {
		if misses[i].persona != misses[j].persona {
			return misses[i].persona < misses[j].persona
		}
		return misses[i].tool < misses[j].tool
	})
	for _, m := range misses {
		t.Errorf("persona %q allows %q, which matches no registered tool — "+
			"the entry does nothing, so that tool is never advertised to the model "+
			"(allowlist matching is an exact string compare)",
			m.persona, m.tool)
	}
}

// The inverse is not an error, but it is worth surfacing: a registered tool that
// no persona advertises is still callable, so an agent that knows its name uses
// it WITHOUT a schema and has to guess the parameters. That is worse than either
// advertising it or removing it, and it is invisible unless something reports it.
func TestReportToolsNoPersonaAdvertises(t *testing.T) {
	advertised := map[string]bool{}
	defs, err := personas.DefaultDefinitions()
	if err != nil {
		t.Fatalf("load persona definitions: %v", err)
	}
	for _, def := range defs {
		for _, name := range def.AllowedTools {
			advertised[strings.TrimSpace(name)] = true
		}
	}

	var unadvertised []string
	for name := range allRegisteredToolNames() {
		if !advertised[name] {
			unadvertised = append(unadvertised, name)
		}
	}
	sort.Strings(unadvertised)
	if len(unadvertised) > 0 {
		t.Logf("registered but advertised by no persona (%d) — callable, but the model "+
			"gets no schema for them: %v", len(unadvertised), unadvertised)
	}
}

// allRegisteredToolNames returns every tool the registry can dispatch,
// including Hidden ones — a hidden tool is still callable by name.
func allRegisteredToolNames() map[string]bool {
	names := map[string]bool{}
	for name := range tools.GetNewToolRegistry().All() {
		names[name] = true
	}
	return names
}
