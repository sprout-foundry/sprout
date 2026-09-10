package personas

import "testing"

// TestDefaultDefinitions_CorePersonas verifies the catalog ships the core
// personas. The refactor/debugger/web_scraper personas were consolidated in
// 2026-09 (see ids.go); their IDs now exist only as aliases on coder and
// researcher, asserted in TestDefaultDefinitions_RetiredIDsResolveAsAliases.
func TestDefaultDefinitions_CorePersonas(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}

	for _, id := range []string{"orchestrator", "coder", "tester", "reviewer", "researcher", "general"} {
		def, exists := definitions[id]
		if !exists {
			t.Fatalf("expected %s in default persona definitions", id)
		}
		if !def.Enabled {
			t.Fatalf("expected %s to be enabled", id)
		}
		if len(def.AllowedTools) == 0 {
			t.Fatalf("expected %s to define allowed tools", id)
		}
	}
}

// TestDefaultDefinitions_RetiredIDsResolveAsAliases verifies the retired
// persona IDs (refactor, debugger, web_scraper) resolve to their merge
// targets through the alias mechanism, so old configs and workflow files
// keep working.
func TestDefaultDefinitions_RetiredIDsResolveAsAliases(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}

	cases := map[string]struct {
		retired string
		target  string
	}{
		"refactor → coder":         {"refactor", "coder"},
		"debugger → coder":         {"debugger", "coder"},
		"web_scraper → researcher": {"web_scraper", "researcher"},
	}
	for name, tc := range cases {
		if _, exists := definitions[tc.retired]; exists {
			t.Errorf("%s: retired ID %s must no longer be a canonical persona", name, tc.retired)
		}
		target, exists := definitions[tc.target]
		if !exists {
			t.Fatalf("%s: merge target %s missing from catalog", name, tc.target)
		}
		found := false
		for _, alias := range target.Aliases {
			if normalizeID(alias) == tc.retired {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: target %s must list %s in aliases", name, tc.target, tc.retired)
		}
	}
}

func TestDefaultDefinitionsCloneIsolation(t *testing.T) {
	first, _ := DefaultDefinitions()
	second, _ := DefaultDefinitions()

	first["general"] = Definition{}
	if second["general"].ID == "" {
		t.Fatalf("expected cloned definitions; mutation should not leak across callers")
	}
}
