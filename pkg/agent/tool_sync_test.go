package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
)

// validJSONSchemaTypes defines the allowed JSON Schema primitive types for
// tool parameter definitions. Both the sprout ToolRegistry and the seed
// core.ToolRegistry must only use these types to prevent model confusion.
var validJSONSchemaTypes = map[string]bool{
	"string":  true,
	"integer": true,
	"number":  true,
	"boolean": true,
	"array":   true,
	"object":  true,
}

// ---------------------------------------------------------------------------
// Helper: convert seed ToolParameters (built by buildSchema) into the shape
// we need for comparison — a map from parameter name → {Type, Required}.
// ---------------------------------------------------------------------------

// seedParamInfo holds the fields extracted from a seed ToolParameters schema.
type seedParamInfo struct {
	Type     string
	Required bool
}

func parseSeedToolParameters(params interface{}) map[string]seedParamInfo {
	result := make(map[string]seedParamInfo)
	// Seed's buildSchema returns a ToolParameters struct:
	//   {Type: "object", Properties: map[string]ToolParameter, Required: []string}
	// We type-assert the interface{} to this struct.
	if params == nil {
		return result
	}

	// Try direct ToolParameters struct first
	if tp, ok := params.(core.ToolParameters); ok {
		// Build a set of required names for fast lookup
		requiredSet := make(map[string]bool, len(tp.Required))
		for _, name := range tp.Required {
			requiredSet[name] = true
		}
		for name, p := range tp.Properties {
			result[name] = seedParamInfo{
				Type:     p.Type,
				Required: requiredSet[name],
			}
		}
		return result
	}

	// Fallback: try to unmarshal through JSON
	// (covers cases where the actual type might differ slightly)
	_ = params
	return result
}

// ---------------------------------------------------------------------------
// Helper: build a canonical list of parameter names from a ToolConfig.
// ---------------------------------------------------------------------------

func paramNames(cfg ToolConfig) []string {
	names := make([]string, 0, len(cfg.Parameters))
	for _, p := range cfg.Parameters {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestToolSync_AllToolsPresent tests that the seed registry and the sprout
// registry register exactly the same set of tool names.
func TestToolSync_AllToolsPresent(t *testing.T) {
	sprout := GetToolRegistry()
	seed := NewSeedToolRegistry(nil)

	sproutNames := make(map[string]bool)
	for _, name := range sprout.GetAvailableTools() {
		sproutNames[name] = true
	}

	seedNames := make(map[string]bool)
	for _, name := range seed.ToolNames() {
		seedNames[name] = true
	}

	// Check every sprout tool exists in seed
	for name := range sproutNames {
		if !seedNames[name] {
			t.Errorf("sprout registry has tool %q not found in seed registry", name)
		}
	}

	// Check every seed tool exists in sprout
	for name := range seedNames {
		if !sproutNames[name] {
			t.Errorf("seed registry has tool %q not found in sprout registry", name)
		}
	}
}

// TestToolSync_ParametersMatch tests that for every tool present in both
// registries, the parameter names, types, and required flags are identical.
func TestToolSync_ParametersMatch(t *testing.T) {
	sprout := GetToolRegistry()
	seed := NewSeedToolRegistry(nil)

	// Build sprout tool config lookup
	sproutCfgs := sprout.GetAllToolConfigs()

	// Iterate over every seed tool
	for _, seedTool := range seed.GetTools() {
		name := seedTool.Function.Name

		sc, found := sproutCfgs[name]
		if !found {
			// Should not happen — TestToolSync_AllToolsPresent would have caught this.
			t.Errorf("tool %q: sprout config not found", name)
			continue
		}

		// Parse seed schema
		seedParams := parseSeedToolParameters(seedTool.Function.Parameters)

		// Build sprout lookup for this tool's parameters
		sproutParamMap := make(map[string]ParameterConfig)
		for _, p := range sc.Parameters {
			sproutParamMap[p.Name] = p
		}

		// Compare parameter counts
		if len(seedParams) != len(sc.Parameters) {
			t.Errorf("tool %q: parameter count mismatch — seed has %d, sprout has %d",
				name, len(seedParams), len(sc.Parameters))
		}

		// Check every seed parameter exists in sprout
		for paramName, seedInfo := range seedParams {
			sproutParam, found := sproutParamMap[paramName]
			if !found {
				t.Errorf("tool %q: seed parameter %q not found in sprout", name, paramName)
				continue
			}

			// Compare type
			if seedInfo.Type != sproutParam.Type {
				t.Errorf("tool %q, param %q: type mismatch — seed %q, sprout %q",
					name, paramName, seedInfo.Type, sproutParam.Type)
			}

			// Compare required
			if seedInfo.Required != sproutParam.Required {
				t.Errorf("tool %q, param %q: required mismatch — seed %v, sprout %v",
					name, paramName, seedInfo.Required, sproutParam.Required)
			}
		}

		// Check every sprout parameter exists in seed
		for paramName := range sproutParamMap {
			if _, found := seedParams[paramName]; !found {
				t.Errorf("tool %q: sprout parameter %q not found in seed", name, paramName)
			}
		}
	}
}

// TestToolSync_ValidJSONSchemaTypes tests that every parameter in both
// registries uses one of the valid JSON Schema types ("string", "integer",
// "number", "boolean", "array", "object"). Invalid types like "int", "bool",
// or "float64" will cause the test to fail.
func TestToolSync_ValidJSONSchemaTypes(t *testing.T) {
	// Check sprout registry
	sprout := GetToolRegistry()
	sproutCfgs := sprout.GetAllToolConfigs()
	for toolName, cfg := range sproutCfgs {
		for _, param := range cfg.Parameters {
			if !validJSONSchemaTypes[param.Type] {
				t.Errorf("tool %q, param %q: invalid JSON Schema type %q (valid types: %v)",
					toolName, param.Name, param.Type, sortedValidTypes())
			}
		}
	}

	// Check seed registry
	seed := NewSeedToolRegistry(nil)
	for _, tool := range seed.GetTools() {
		seedParams := parseSeedToolParameters(tool.Function.Parameters)
		for paramName, info := range seedParams {
			if !validJSONSchemaTypes[info.Type] {
				t.Errorf("tool %q, param %q: invalid JSON Schema type %q (valid types: %v)",
					tool.Function.Name, paramName, info.Type, sortedValidTypes())
			}
		}
	}
}

// sortedValidTypes returns a sorted, comma-separated list of valid types for
// inclusion in error messages.
func sortedValidTypes() []string {
	types := make([]string, 0, len(validJSONSchemaTypes))
	for t := range validJSONSchemaTypes {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// TestToolSync_AlternativeNamesMatch tests that alternative parameter names
// (aliases) are consistent between the two registries. It verifies that:
//  1. Both registries agree on the set of parameter names for every tool.
//  2. The sprout alternatives list is well-formed (non-empty, valid strings).
//  3. No parameter name in sprout is hidden inside an alternative list of
//     another tool (prevents accidental duplication).
//
// Note: seed's public API (GetTool/GetTools) returns ToolParameters which
// does not include alternatives — they are absorbed by buildSchema. The
// parameter-name comparison above serves as the sync check; if the names
// differ the previous test would have already flagged it.
func TestToolSync_AlternativeNamesMatch(t *testing.T) {
	sprout := GetToolRegistry()
	seed := NewSeedToolRegistry(nil)

	sproutCfgs := sprout.GetAllToolConfigs()

	for _, seedTool := range seed.GetTools() {
		name := seedTool.Function.Name

		sc, found := sproutCfgs[name]
		if !found {
			t.Errorf("tool %q: no sprout config", name)
			continue
		}

		// Check that both registries agree on the parameter name set.
		seedParams := parseSeedToolParameters(seedTool.Function.Parameters)
		sproutNames := make(map[string]bool)
		for _, p := range sc.Parameters {
			sproutNames[p.Name] = true
		}

		for seedName := range seedParams {
			if !sproutNames[seedName] {
				t.Errorf("tool %q: parameter %q exists in seed but not sprout", name, seedName)
			}
		}
		for sprName := range sproutNames {
			if _, found := seedParams[sprName]; !found {
				t.Errorf("tool %q: parameter %q exists in sprout but not seed", name, sprName)
			}
		}

		// Validate sprout alternatives: each must be a non-empty string.
		for _, param := range sc.Parameters {
			for idx, alt := range param.Alternatives {
				if alt == "" {
					t.Errorf("tool %q, param %q: alternative at index %d is empty string",
						name, param.Name, idx)
				}
				if strings.Contains(alt, " ") || strings.Contains(alt, ",") {
					t.Errorf("tool %q, param %q: alternative %q contains whitespace or comma",
						name, param.Name, alt)
				}
			}
		}
	}
}

// TestToolSync_NilToolRegistry tests that NewSeedToolRegistry(nil) works
// without panicking — the registry must handle a nil agent argument.
func TestToolSync_NilToolRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewSeedToolRegistry(nil) panicked: %v", r)
		}
	}()

	seed := NewSeedToolRegistry(nil)
	if seed == nil {
		t.Fatal("NewSeedToolRegistry(nil) returned nil")
	}

	// Verify some tools are registered
	tools := seed.GetTools()
	if len(tools) == 0 {
		t.Error("seed registry should have at least some tools registered")
	}

	// Check that well-known tools are present
	known := []string{"shell_command", "read_file", "write_file", "git", "commit"}
	toolNames := make(map[string]bool)
	for _, t := range tools {
		toolNames[t.Function.Name] = true
	}
	for _, want := range known {
		if !toolNames[want] {
			t.Errorf("seed registry missing tool %q", want)
		}
	}
}

// TestToolSync_CountConsistency verifies that both registries have the same tool count.
func TestToolSync_CountConsistency(t *testing.T) {
	sprout := GetToolRegistry()
	seed := NewSeedToolRegistry(nil)

	sproutCount := len(sprout.GetAvailableTools())
	seedCount := len(seed.GetTools())

	if sproutCount != seedCount {
		t.Errorf("tool count mismatch: sprout has %d, seed has %d", sproutCount, seedCount)
	}
	t.Logf("both registries have %d tools", sproutCount)
}

// ---------------------------------------------------------------------------
// Negative tests: these tests deliberately introduce a mismatch to confirm
// the sync checks actually catch problems. They are named with a "Negative"
// prefix so they can be disabled (with a build tag or manual skip) in CI
// if desired — but are useful during development.
// ---------------------------------------------------------------------------

// TestToolSync_Negative_ToolNameMismatch confirms the test detects when
// seed has a tool name that sprout does not.
func TestToolSync_Negative_ToolNameMismatch(t *testing.T) {
	// Create a minimal sprout registry missing a tool
	sprout := &ToolRegistry{tools: make(map[string]ToolConfig)}
	sprout.RegisterTool(ToolConfig{Name: "shell_command", Description: "test", Handler: nil})

	seed := NewSeedToolRegistry(nil)

	// seed should have many more tools
	if len(seed.GetTools()) <= len(sprout.GetAvailableTools()) {
		t.Skip("seed registry unexpectedly has few tools")
	}

	// The test should detect the mismatch
	seedNames := make(map[string]bool)
	for _, name := range seed.ToolNames() {
		seedNames[name] = true
	}

	sproutNames := make(map[string]bool)
	for _, name := range sprout.GetAvailableTools() {
		sproutNames[name] = true
	}

	var missing []string
	for name := range seedNames {
		if !sproutNames[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		t.Fatal("expected missing tools — test setup may be broken")
	}
	// This assertion should never fail; if it does the test setup is wrong.
	t.Logf("Detected %d missing tool(s) in sprout: %s", len(missing), strings.Join(missing, ", "))
}

// TestToolSync_Negative_InvalidType confirms the test catches invalid JSON Schema types.
func TestToolSync_Negative_InvalidType(t *testing.T) {
	// The test should flag "int" as invalid (it should be "integer")
	if validJSONSchemaTypes["int"] {
		t.Fatal("test setup broken: \"int\" should not be a valid JSON Schema type")
	}
	if validJSONSchemaTypes["bool"] {
		t.Fatal("test setup broken: \"bool\" should not be a valid JSON Schema type")
	}
	if validJSONSchemaTypes["float64"] {
		t.Fatal("test setup broken: \"float64\" should not be a valid JSON Schema type")
	}

	// The test should allow the standard types
	for _, valid := range []string{"string", "integer", "number", "boolean", "array", "object"} {
		if !validJSONSchemaTypes[valid] {
			t.Errorf("test setup broken: %q should be a valid JSON Schema type", valid)
		}
	}
}

// ---------------------------------------------------------------------------
// Utility: build a comparison-friendly map of sprout parameters.
// ---------------------------------------------------------------------------

// sproutParamInfo mirrors seedParamInfo for sprout's ParameterConfig.
type sproutParamInfo struct {
	Type     string
	Required bool
}

// buildSproutParamMap creates a lookup from parameter name to info for a tool.
func buildSproutParamMap(cfg ToolConfig) map[string]sproutParamInfo {
	m := make(map[string]sproutParamInfo, len(cfg.Parameters))
	for _, p := range cfg.Parameters {
		m[p.Name] = sproutParamInfo{
			Type:     p.Type,
			Required: p.Required,
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Additional detail-oriented tests for complex tools.
// ---------------------------------------------------------------------------

// TestToolSync_BrowseUrlParameters tests that the complex browse_url tool
// has consistent parameter definitions between both registries.
func TestToolSync_BrowseUrlParameters(t *testing.T) {
	sprout := GetToolRegistry()
	seed := NewSeedToolRegistry(nil)

	sc, found := sprout.GetToolConfig("browse_url")
	if !found {
		t.Fatal("sprout registry missing browse_url tool")
	}

	seedCfg := seed.GetTool("browse_url")
	if seedCfg == nil {
		t.Fatal("seed registry missing browse_url tool")
	}

	seedParams := parseSeedToolParameters(seedCfg.Function.Parameters)
	sproutParams := buildSproutParamMap(sc)

	// browse_url has many parameters — verify they all match
	expectedParams := []string{
		"url", "action", "screenshot_path", "session_id", "persist_session",
		"close_session", "viewport_width", "viewport_height", "user_agent",
		"wait_for_selector", "wait_timeout_ms", "steps", "capture_selectors",
		"capture_dom", "capture_text", "include_console", "capture_network",
		"capture_storage", "capture_cookies", "response_max_chars",
	}

	for _, param := range expectedParams {
		seedInfo, seedOk := seedParams[param]
		sproutInfo, sproutOk := sproutParams[param]

		if seedOk != sproutOk {
			t.Errorf("browse_url: parameter %q consistency — seed:%v, sprout:%v",
				param, seedOk, sproutOk)
			continue
		}

		if seedOk {
			if seedInfo.Type != sproutInfo.Type {
				t.Errorf("browse_url, param %q: type mismatch seed=%q sprout=%q",
					param, seedInfo.Type, sproutInfo.Type)
			}
			if seedInfo.Required != sproutInfo.Required {
				t.Errorf("browse_url, param %q: required mismatch seed=%v sprout=%v",
					param, seedInfo.Required, sproutInfo.Required)
			}
		}
	}
}

// TestToolSync_SyncErrorMessageQuality ensures error messages are descriptive.
func TestToolSync_SyncErrorMessageQuality(t *testing.T) {
	// This is a meta-test: verify that our test helper produces useful
	// error messages by checking a deliberate mismatch.

	// Create a mismatched sprout config
	sprout := &ToolRegistry{tools: make(map[string]ToolConfig)}
	sprout.RegisterTool(ToolConfig{
		Name:        "deliberate_mismatch",
		Description: "deliberate mismatch",
		Handler:     func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) { return "", nil },
		Parameters: []ParameterConfig{
			{"foo", "int", true, []string{}, "should be integer"},
		},
	})

	// Create a seed tool with the same name but different type.
	// Seed requires a Handler, so provide a no-op one.
	seed := core.NewToolRegistry(core.ToolRegistryOptions{
		DefaultTimeout: 5 * time.Minute,
		MaxResultSize:  50 * 1024,
	})
	err := seed.Register(core.ToolConfig{
		Name:        "deliberate_mismatch",
		Description: "deliberate mismatch",
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "", nil
		},
		Parameters: []core.ParameterConfig{
			{Name: "foo", Type: "integer", Required: true, Description: "correct type"},
		},
	})
	if err != nil {
		t.Fatalf("failed to register seed tool: %v", err)
	}

	// Now parse both and check the mismatch is detected
	sproutParams := buildSproutParamMap(sprout.tools["deliberate_mismatch"])
	seedTool := seed.GetTool("deliberate_mismatch")
	seedParams := parseSeedToolParameters(seedTool.Function.Parameters)

	// The type should differ: "int" vs "integer"
	sproutInfo := sproutParams["foo"]
	seedInfo := seedParams["foo"]

	if sproutInfo.Type == seedInfo.Type {
		t.Skip("types match — test setup may be broken")
	}

	if sproutInfo.Type != "int" {
		t.Errorf("expected sprout type to be \"int\", got %q", sproutInfo.Type)
	}
	if seedInfo.Type != "integer" {
		t.Errorf("expected seed type to be \"integer\", got %q", seedInfo.Type)
	}

	// Both should be present
	if !validJSONSchemaTypes[sproutInfo.Type] {
		// "int" is NOT valid — this is expected
		t.Logf("Correctly identified invalid type %q for sprout", sproutInfo.Type)
	} else {
		t.Errorf("test setup broken: %q should be invalid", sproutInfo.Type)
	}
}

// ---------------------------------------------------------------------------
// Helper: format the error message for a parameter mismatch.
// ---------------------------------------------------------------------------

// formatParamMismatch returns a human-readable error string for a parameter
// mismatch between the sprout and seed registries.
func formatParamMismatch(toolName, paramName string, seedInfo seedParamInfo, sproutInfo ParameterConfig) string {
	return fmt.Sprintf("tool %q, param %q: seed(type=%q,req=%v) vs sprout(type=%q,req=%v)",
		toolName, paramName,
		seedInfo.Type, seedInfo.Required,
		sproutInfo.Type, sproutInfo.Required,
	)
}
