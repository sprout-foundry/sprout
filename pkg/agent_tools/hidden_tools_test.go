//go:build !js

package tools

import (
	"testing"
)

// `search` supersedes search_files and semantic_search: it runs the same
// literal walker and the same embedding index, and measured at an equal
// top-10 result budget it matches semantic alone (10/14) and doubles ripgrep
// (5/14) on the held-out set in search_fusion_eval_test.go.
//
// They are hidden rather than deleted. Hiding drops two schemas from every
// turn's context and removes a choice the model should not have to make, while
// keeping the names resolvable for callers that already reference them —
// replayed sessions, saved automations, subagent configs. Deleting would turn
// each of those into an unknown-tool failure.
func TestSupersededSearchToolsAreHiddenButCallable(t *testing.T) {
	registry := GetNewToolRegistry()

	for _, name := range []string{"search_files", "semantic_search"} {
		h, ok := registry.Lookup(name)
		if !ok || h == nil {
			t.Errorf("%s is not in the registry — hiding must not make a tool uncallable, "+
				"or existing sessions and automations that name it will fail", name)
			continue
		}
		if !h.Definition().Hidden {
			t.Errorf("%s is advertised; it is superseded by `search` and should be hidden", name)
		}
	}

	search, ok := registry.Lookup("search")
	if !ok || search == nil {
		t.Fatal("`search` is not registered — the replacement for both hidden tools is missing")
	}
	if search.Definition().Hidden {
		t.Error("`search` is hidden; it is the tool that replaces the other two")
	}
}

// The advertised roster is what costs context on every turn. Guard the
// property (hidden tools are excluded) rather than a count, so the test does
// not fail every time an unrelated tool is added.
func TestHiddenToolsAreExcludedFromAdvertisedRoster(t *testing.T) {
	var advertised, hidden int
	for _, h := range GetNewToolRegistry().All() {
		if h.Definition().Hidden {
			hidden++
			continue
		}
		advertised++
	}
	if hidden == 0 {
		t.Error("no tool is marked Hidden — the mechanism is not in use, so it is untested in practice")
	}
	if advertised == 0 {
		t.Fatal("every tool is hidden")
	}
	t.Logf("registry: %d advertised, %d hidden", advertised, hidden)
}
