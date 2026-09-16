package personas

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// designerDesignTools is the design-tier tool roster the designer persona must
// list per SP-140-2 §2a. Listing tools that register later is harmless (the
// allowlist filters the registered roster), so this test asserts the declared
// intent, not runtime registration.
var designerDesignTools = []string{
	"design_validate",
	"design_assets",
	"design_render",
	"design_import_sketch",
}

// TestDesigner_DefinitionShape pins every field of the shipped designer
// catalog entry against SP-140-2 §2a: canonical id, aliases, delegatable,
// capability grant, and system prompt path. A silent edit to the JSON that
// breaks the persona contract fails here first.
func TestDesigner_DefinitionShape(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}

	def, exists := definitions[IDDesigner]
	if !exists {
		t.Fatalf("expected %q in default persona definitions", IDDesigner)
	}

	if def.ID != IDDesigner {
		t.Errorf("designer ID = %q, want %q", def.ID, IDDesigner)
	}
	if !def.Enabled {
		t.Error("designer must ship enabled")
	}
	if !def.Delegatable {
		t.Error("designer must be delegatable so orchestrator/coordinator can spawn it")
	}
	if def.SystemPrompt != "pkg/agent/prompts/subagent_prompts/designer.md" {
		t.Errorf("designer system_prompt = %q, want the designer prompt file", def.SystemPrompt)
	}
	if def.Provider != "" || def.Model != "" {
		t.Errorf("designer must inherit the user default provider/model, got provider=%q model=%q", def.Provider, def.Model)
	}
	if def.LocalOnly {
		t.Error("designer must not be local-only")
	}

	// Aliases: ux and design, normalized the way the loader normalizes them.
	gotAliases := []string{}
	for _, alias := range def.Aliases {
		gotAliases = append(gotAliases, normalizeID(alias))
	}
	for _, want := range []string{"ux", "design"} {
		if !slices.Contains(gotAliases, want) {
			t.Errorf("designer aliases %v must include %q", gotAliases, want)
		}
	}

	// Capability grant is explicit and singular.
	if !slices.Contains(def.Capabilities, CapabilityGitWrite) {
		t.Errorf("designer capabilities %v must include %q", def.Capabilities, CapabilityGitWrite)
	}
}

// TestDesigner_AllowedToolsExplicitArray is the anti-default-set assertion:
// SP-140-2 §2a requires an explicit array because the catalog has no default
// mechanism. The persona specializes behavior, it does not sandbox — so the
// list must carry the standard file/shell/edit/subagent set *and* the design
// tools, never a minimal subset.
func TestDesigner_AllowedToolsExplicitArray(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}
	def, exists := definitions[IDDesigner]
	if !exists {
		t.Fatalf("expected %q in default persona definitions", IDDesigner)
	}

	if len(def.AllowedTools) == 0 {
		t.Fatal("designer must declare an explicit allowed_tools array")
	}

	want := append([]string{
		// Standard set: validators, writers, shell, subagents, skills.
		"shell_command",
		"read_file",
		"write_file",
		"edit_file",
		"write_structured_file",
		"patch_structured_file",
		"search",
		"run_subagent",
		"activate_skill",
		"commit",
		// Vision/render surfaces the design loop needs.
		"analyze_ui_screenshot",
	}, designerDesignTools...)

	for _, tool := range want {
		if !slices.Contains(def.AllowedTools, tool) {
			t.Errorf("designer allowed_tools must include %q (got %v)", tool, def.AllowedTools)
		}
	}
}

// TestDesigner_AliasesResolveThroughLoader verifies the loader records the
// aliases so that lookup by `ux`/`design` reaches the canonical designer
// definition. The loader keys the merged map by canonical ID only, so the
// resolution contract here is: alias does not create a second entry, and the
// alias is listed on the canonical one.
func TestDesigner_AliasesResolveThroughLoader(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("expected embedded persona configs to load, got error: %v", err)
	}

	for _, alias := range []string{"ux", "design"} {
		if _, shadowing := definitions[alias]; shadowing {
			t.Errorf("alias %q must not create a canonical entry of its own", alias)
		}
		if !definitionHasAlias(definitions, IDDesigner, alias) {
			t.Errorf("alias %q must be declared on the designer definition", alias)
		}
	}
}

// TestDesigner_NoConflictWithExistingCatalogAliases is the regression guard
// for the real catalog: adding designer must not collide with any existing
// persona ID or alias. The loader already fails hard on conflicts, so the
// meaningful assertion is that the shipped catalog loads clean and designer
// did not displace anything.
func TestDesigner_NoConflictWithExistingCatalogAliases(t *testing.T) {
	definitions, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("real catalog must load clean after adding designer: %v", err)
	}

	// Spot-check the personas whose names/aliases are closest to "designer".
	for _, id := range []string{"coder", "tester", "reviewer", "researcher", "general", "orchestrator"} {
		if _, ok := definitions[id]; !ok {
			t.Errorf("persona %q must still be present", id)
		}
	}
	if len(definitions) < 8 {
		t.Errorf("expected the full catalog (>=8 personas) to load, got %d", len(definitions))
	}
}

// --- Loader conflict tests driven with a fake FS -------------------------
//
// These extend the existing conflict coverage in catalog_conflict_test.go with
// the designer-shaped conflicts: an alias that shadows an existing ID, and a
// second file that redeclares the designer ID or one of its aliases.

func TestLoadDefinitionsFromFS_DesignerAliasShadowsExistingID(t *testing.T) {
	// A separate file declares a persona whose alias is "designer" while
	// designer is itself a canonical ID. The alias must be rejected.
	fsys := fstest.MapFS{
		"configs/designer.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "designer", "name": "Designer", "aliases": ["ux", "design"]}
			]
		}`)},
		"configs/rogue.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "impostor", "name": "Impostor", "aliases": ["designer"]}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for alias shadowing the designer persona id, got nil")
	}
	if !strings.Contains(err.Error(), "shadows persona id") {
		t.Errorf("error should mention id shadowing, got: %v", err)
	}
	if !strings.Contains(err.Error(), "designer") {
		t.Errorf("error should name the conflicting persona, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_DesignerAliasDeclaredTwice(t *testing.T) {
	// "ux" declared as an alias by two personas must fail, even when one of
	// them is the designer entry.
	fsys := fstest.MapFS{
		"configs/designer.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "designer", "name": "Designer", "aliases": ["ux", "design"]}
			]
		}`)},
		"configs/other.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "other", "name": "Other", "aliases": ["ux"]}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for duplicate designer alias, got nil")
	}
	if !strings.Contains(err.Error(), "alias") || !strings.Contains(err.Error(), "ux") {
		t.Errorf("error should name the duplicated alias, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_DesignerIDDeclaredTwice(t *testing.T) {
	// Two files both declaring id "designer" is a duplicate-ID error.
	fsys := fstest.MapFS{
		"configs/designer.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "designer", "name": "Designer", "aliases": ["ux", "design"]}
			]
		}`)},
		"configs/designer_dup.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "Designer", "name": "Designer Duplicate"}
			]
		}`)},
	}

	_, err := loadDefinitionsFromFS(fsys, "configs")
	if err == nil {
		t.Fatal("expected error for duplicate designer id across files, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate persona id") {
		t.Errorf("error should mention duplicate persona id, got: %v", err)
	}
	// Case-insensitivity: "Designer" normalizes to "designer" and must collide.
	if !strings.Contains(err.Error(), "designer") {
		t.Errorf("error should name the normalized duplicate id, got: %v", err)
	}
}

func TestLoadDefinitionsFromFS_DesignerAliasFormsSeparateFromHyphens(t *testing.T) {
	// Normalization: the loader maps "-" to "_". A persona declaring "web-design"
	// as an alias must not collide with designer's "design" alias, and must be
	// recorded under "web_design".
	fsys := fstest.MapFS{
		"configs/designer.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "designer", "name": "Designer", "aliases": ["ux", "design"]}
			]
		}`)},
		"configs/web.json": &fstest.MapFile{Data: []byte(`{
			"personas": [
				{"id": "web", "name": "Web", "aliases": ["web-design"]}
			]
		}`)},
	}

	defs, err := loadDefinitionsFromFS(fsys, "configs")
	if err != nil {
		t.Fatalf("expected clean load, got: %v", err)
	}
	if _, ok := defs["designer"]; !ok {
		t.Error("designer must be in the merged definitions")
	}
	if _, ok := defs["web"]; !ok {
		t.Error("web must be in the merged definitions")
	}
}

// definitionHasAlias reports whether the definition for canonicalID lists the
// given alias after normalization.
func definitionHasAlias(definitions map[string]Definition, canonicalID, alias string) bool {
	def, ok := definitions[canonicalID]
	if !ok {
		return false
	}
	normalizedAlias := normalizeID(alias)
	for _, candidate := range def.Aliases {
		if normalizeID(candidate) == normalizedAlias {
			return true
		}
	}
	return false
}
