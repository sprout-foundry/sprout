package agent

import (
	"slices"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestBuildToolConfigsFromHandlers_MatchesLegacy compares tool definitions from both
// the legacy registry and the new handler registry. For tools present in BOTH,
// it checks that Name, Description, Parameters, Aliases, Timeout, MaxResultSize,
// SafeForParallel, and Interactive match.
//
// This is a verification test — it reports diffs but does NOT fail yet.
// Once all diffs are resolved, this test can be hardened to fail on mismatches.
func TestBuildToolConfigsFromHandlers_MatchesLegacy(t *testing.T) {
	// Build from both sources
	handlerTools := BuildToolConfigsFromHandlers()
	legacyTools := GetToolRegistry().GetAllToolConfigs()

	// Index handler tools by name
	handlerBy := make(map[string]ToolConfig, len(handlerTools))
	for _, cfg := range handlerTools {
		handlerBy[cfg.Name] = cfg
	}

	// Find tools present in both registries
	var commonTools []string
	for name := range legacyTools {
		if _, ok := handlerBy[name]; ok {
			commonTools = append(commonTools, name)
		}
	}
	slices.Sort(commonTools)

	if len(commonTools) == 0 {
		t.Fatal("no common tools found between legacy and handler registries")
	}

	t.Logf("Comparing %d common tools between legacy and handler registries", len(commonTools))

	// Report tools only in one registry
	onlyInLegacy := make(map[string]struct{})
	for name := range legacyTools {
		if _, ok := handlerBy[name]; !ok {
			onlyInLegacy[name] = struct{}{}
		}
	}
	onlyInHandler := make(map[string]struct{})
	for name := range handlerBy {
		if _, ok := legacyTools[name]; !ok {
			onlyInHandler[name] = struct{}{}
		}
	}

	if len(onlyInLegacy) > 0 {
		t.Logf("Tools only in legacy registry (%d): %v", len(onlyInLegacy), sortedKeys(onlyInLegacy))
	}
	if len(onlyInHandler) > 0 {
		t.Logf("Tools only in handler registry (%d): %v", len(onlyInHandler), sortedKeys(onlyInHandler))
	}

	// Compare common tools
	matchCount := 0
	diffCount := 0
	for _, name := range commonTools {
		legacyCfg := legacyTools[name]
		handlerCfg := handlerBy[name]

		diffs := compareToolConfigFields(legacyCfg, handlerCfg)
		if len(diffs) == 0 {
			matchCount++
		} else {
			diffCount++
			t.Logf("MISMATCH %s: %s", name, diffs)
			for _, field := range diffs {
				switch field {
				case "Description":
					t.Logf("  Description: legacy=%q, handler=%q", legacyCfg.Description, handlerCfg.Description)
				case "Aliases":
					t.Logf("  Aliases: legacy=%v, handler=%v", legacyCfg.Aliases, handlerCfg.Aliases)
				case "Timeout":
					t.Logf("  Timeout: legacy=%v, handler=%v", legacyCfg.Timeout, handlerCfg.Timeout)
				case "MaxResultSize":
					t.Logf("  MaxResultSize: legacy=%d, handler=%d", legacyCfg.MaxResultSize, handlerCfg.MaxResultSize)
				case "SafeForParallel":
					t.Logf("  SafeForParallel: legacy=%v, handler=%v", legacyCfg.SafeForParallel, handlerCfg.SafeForParallel)
				case "Interactive":
					t.Logf("  Interactive: legacy=%v, handler=%v", legacyCfg.Interactive, handlerCfg.Interactive)
				case "Parameters":
					t.Logf("  Parameters: legacy has %d, handler has %d", len(legacyCfg.Parameters), len(handlerCfg.Parameters))
					for i := range legacyCfg.Parameters {
						if i >= len(handlerCfg.Parameters) {
							t.Logf("    param %d: legacy=%q, handler=<missing>", i, legacyCfg.Parameters[i].Name)
							continue
						}
						lp := legacyCfg.Parameters[i]
						hp := handlerCfg.Parameters[i]
						if lp.Name != hp.Name || lp.Type != hp.Type || lp.Required != hp.Required || lp.Description != hp.Description {
							t.Logf("    param %d: legacy=(%q,%s,%v), handler=(%q,%s,%v)",
								i, lp.Name, lp.Type, lp.Required, hp.Name, hp.Type, hp.Required)
						}
					}
				}
			}
		}
	}

	t.Logf("Result: %d matched, %d differed out of %d common tools", matchCount, diffCount, len(commonTools))
}

// TestConvertHandlerToToolConfig_Fields verifies that all fields are correctly
// mapped from a handler to ToolConfig. Uses the readFileHandler as a concrete
// example since it has well-known parameters and metadata.
func TestConvertHandlerToToolConfig_Fields(t *testing.T) {
	// Use a real handler from the registry
	h, ok := tools.GetNewToolRegistry().Lookup("read_file")
	if !ok {
		t.Fatal("read_file handler not found in new registry")
	}

	cfg := convertHandlerToToolConfig(h)

	// Check name
	if cfg.Name != "read_file" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "read_file")
	}

	// Check description is non-empty
	if cfg.Description == "" {
		t.Error("Description is empty")
	}

	// Check parameters are mapped correctly
	def := h.Definition()
	if len(cfg.Parameters) != len(def.Parameters) {
		t.Errorf("Parameters count: got %d, want %d", len(cfg.Parameters), len(def.Parameters))
	}

	// Check that required parameters are correctly identified
	requiredSet := make(map[string]struct{})
	for _, rn := range def.Required {
		requiredSet[rn] = struct{}{}
	}
	for _, pc := range cfg.Parameters {
		_, shouldBeRequired := requiredSet[pc.Name]
		// Also check the ParameterDef.Required field
		if !shouldBeRequired {
			for _, pd := range def.Parameters {
				if pd.Name == pc.Name && pd.Required {
					shouldBeRequired = true
					break
				}
			}
		}
		if pc.Required != shouldBeRequired {
			t.Errorf("Parameter %q Required: got %v, want %v", pc.Name, pc.Required, shouldBeRequired)
		}
	}

	// Check metadata methods are mapped
	if cfg.Aliases != nil && len(cfg.Aliases) != len(h.Aliases()) {
		t.Errorf("Aliases count: got %d, want %d", len(cfg.Aliases), len(h.Aliases()))
	}
	if cfg.Timeout != h.Timeout() {
		t.Errorf("Timeout: got %v, want %v", cfg.Timeout, h.Timeout())
	}
	if cfg.MaxResultSize != h.MaxResultSize() {
		t.Errorf("MaxResultSize: got %d, want %d", cfg.MaxResultSize, h.MaxResultSize())
	}
	if cfg.SafeForParallel != h.SafeForParallel() {
		t.Errorf("SafeForParallel: got %v, want %v", cfg.SafeForParallel, h.SafeForParallel())
	}
	if cfg.Interactive != h.Interactive() {
		t.Errorf("Interactive: got %v, want %v", cfg.Interactive, h.Interactive())
	}
}

// TestBuildToolConfigsFromHandlers_SortedOutput verifies that BuildToolConfigsFromHandlers
// returns tools sorted alphabetically by name.
func TestBuildToolConfigsFromHandlers_SortedOutput(t *testing.T) {
	handlerTools := BuildToolConfigsFromHandlers()

	if len(handlerTools) == 0 {
		t.Fatal("BuildToolConfigsFromHandlers returned no tools")
	}

	// Check that the slice is sorted by name
	for i := 1; i < len(handlerTools); i++ {
		if handlerTools[i].Name < handlerTools[i-1].Name {
			t.Errorf("Tools not sorted: %q (index %d) should come before %q (index %d)",
				handlerTools[i].Name, i, handlerTools[i-1].Name, i-1)
		}
	}

	t.Logf("BuildToolConfigsFromHandlers returned %d tools, sorted: %v", len(handlerTools),
		handlerTools[0].Name == "activate_skill") // first tool alphabetically
}

// TestConvertHandlerToSeedToolConfig_Basic verifies that the seed config
// conversion produces a valid core.ToolConfig with all fields populated.
func TestConvertHandlerToSeedToolConfig_Basic(t *testing.T) {
	h, ok := tools.GetNewToolRegistry().Lookup("read_file")
	if !ok {
		t.Fatal("read_file handler not found in new registry")
	}

	seedCfg := convertHandlerToSeedToolConfig(h, nil)

	// Check basic fields
	if seedCfg.Name != "read_file" {
		t.Errorf("Name: got %q, want %q", seedCfg.Name, "read_file")
	}
	if seedCfg.Description == "" {
		t.Error("Description is empty")
	}
	if seedCfg.Handler == nil {
		t.Error("Handler is nil")
	}
	if seedCfg.HandlerWithImages == nil {
		t.Error("HandlerWithImages is nil")
	}

	// Check parameters
	if len(seedCfg.Parameters) == 0 {
		t.Error("Parameters is empty")
	}

	// Check that metadata is carried over
	if seedCfg.Timeout != h.Timeout() {
		t.Errorf("Timeout: got %v, want %v", seedCfg.Timeout, h.Timeout())
	}
	if seedCfg.MaxResultSize != h.MaxResultSize() {
		t.Errorf("MaxResultSize: got %d, want %d", seedCfg.MaxResultSize, h.MaxResultSize())
	}
	if seedCfg.SafeForParallel != h.SafeForParallel() {
		t.Errorf("SafeForParallel: got %v, want %v", seedCfg.SafeForParallel, h.SafeForParallel())
	}
}

// TestBuildToolConfigsFromHandlers_Count verifies the handler registry produces
// a reasonable number of tools (sanity check).
func TestBuildToolConfigsFromHandlers_Count(t *testing.T) {
	handlerTools := BuildToolConfigsFromHandlers()
	legacyTools := GetToolRegistry().GetAllToolConfigs()

	t.Logf("Handler registry: %d tools", len(handlerTools))
	t.Logf("Legacy registry: %d tools", len(legacyTools))

	// We expect the handler registry to have at least 25 tools
	// (see pkg/agent_tools/all.go for the list)
	if len(handlerTools) < 25 {
		t.Errorf("Expected at least 25 handler tools, got %d", len(handlerTools))
	}
}

// TestConvertHandlerToToolConfig_ShellCommand verifies the shell_command
// handler conversion — it has many parameters and specific metadata.
func TestConvertHandlerToToolConfig_ShellCommand(t *testing.T) {
	h, ok := tools.GetNewToolRegistry().Lookup("shell_command")
	if !ok {
		t.Fatal("shell_command handler not found in new registry")
	}

	cfg := convertHandlerToToolConfig(h)

	if cfg.Name != "shell_command" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "shell_command")
	}

	// shell_command should have 6 parameters
	def := h.Definition()
	if len(cfg.Parameters) != len(def.Parameters) {
		t.Errorf("Parameters count: got %d, want %d", len(cfg.Parameters), len(def.Parameters))
	}

	// Verify parameter types are preserved
	expectedTypes := map[string]string{
		"command":        "string",
		"background":     "boolean",
		"check_background": "string",
		"wait_seconds":   "integer",
		"stop_background": "string",
		"wakeup_timeout": "integer",
	}
	for _, pc := range cfg.Parameters {
		wantType, ok := expectedTypes[pc.Name]
		if !ok {
			t.Errorf("Unexpected parameter: %s", pc.Name)
			continue
		}
		if pc.Type != wantType {
			t.Errorf("Parameter %s type: got %q, want %q", pc.Name, pc.Type, wantType)
		}
	}
}

// TestConvertHandlerToToolConfig_TodoWrite verifies the TodoWrite handler
// which has aliases defined in the legacy registry.
func TestConvertHandlerToToolConfig_TodoWrite(t *testing.T) {
	h, ok := tools.GetNewToolRegistry().Lookup("todo_write")
	if !ok {
		t.Fatal("todo_write handler not found in new registry")
	}

	cfg := convertHandlerToToolConfig(h)

	if cfg.Name != "todo_write" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "todo_write")
	}

	// Check that the handler's aliases are carried over
	legacyCfg, _ := GetToolRegistry().GetToolConfig("TodoWrite")
	if legacyCfg.Aliases != nil {
		t.Logf("Legacy TodoWrite aliases: %v", legacyCfg.Aliases)
		t.Logf("Handler TodoWrite aliases: %v", cfg.Aliases)
		// The handler may not have aliases set — this is informational
	}
}

// TestCompareToolConfigFields tests the comparison helper directly.
func TestCompareToolConfigFields(t *testing.T) {
	cfgA := ToolConfig{
		Name:            "test",
		Description:     "same",
		Aliases:         []string{"a1"},
		Timeout:         time.Minute,
		MaxResultSize:   100,
		SafeForParallel: true,
		Interactive:     false,
		Parameters: []ParameterConfig{
			{Name: "p1", Type: "string", Required: true, Description: "param 1"},
		},
	}
	cfgB := ToolConfig{
		Name:            "test",
		Description:     "different",
		Aliases:         []string{"b1"},
		Timeout:         2 * time.Minute,
		MaxResultSize:   200,
		SafeForParallel: false,
		Interactive:     true,
		Parameters: []ParameterConfig{
			{Name: "p1", Type: "string", Required: false, Description: "param 1 modified"},
		},
	}

	diffs := compareToolConfigFields(cfgA, cfgB)
	// We expect: Description, Aliases, Timeout, MaxResultSize, SafeForParallel, Interactive, Parameters
	if len(diffs) != 7 {
		t.Errorf("Expected 7 diffs, got %d: %v", len(diffs), diffs)
	}

	// Identical configs should have no diffs
	diffs = compareToolConfigFields(cfgA, cfgA)
	if len(diffs) != 0 {
		t.Errorf("Expected 0 diffs for identical configs, got %d: %v", len(diffs), diffs)
	}
}

// TestSP109UseHandlerTools tests the env var gate.
func TestSP109UseHandlerTools(t *testing.T) {
	// Default should be false
	if sp109UseHandlerTools() {
		t.Error("Expected sp109UseHandlerTools() to be false by default")
	}

	// Set env var and check
	t.Setenv("SP109_USE_HANDLER_TOOLS", "true")
	if !sp109UseHandlerTools() {
		t.Error("Expected sp109UseHandlerTools() to be true when env var is set")
	}
}

// sortedKeys returns the keys of a map[string]struct{} sorted alphabetically.
func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
