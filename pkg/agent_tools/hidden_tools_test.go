//go:build !js

package tools

import (
	"testing"
)

// `search` supersedes search_files: it runs the same literal walker and groups
// results per file.
//
// search_files is hidden rather than deleted. Hiding drops its schema from
// every turn's context and removes a choice the model should not have to make,
// while keeping the name resolvable for callers that already reference it —
// replayed sessions, saved automations, subagent configs. Deleting would turn
// each of those into an unknown-tool failure.
func TestSupersededSearchToolsAreHiddenButCallable(t *testing.T) {
	registry := GetNewToolRegistry()

	for _, name := range []string{"search_files"} {
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
		t.Fatal("`search` is not registered — the replacement for search_files is missing")
	}
	if search.Definition().Hidden {
		t.Error("`search` is hidden; it is the tool that replaces search_files")
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
