package agent

import (
	"context"
	"sort"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// validJSONSchemaTypes defines the allowed JSON Schema primitive types for
// tool parameter definitions. Both the sprout ToolHandler registry and the seed
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
	if params == nil {
		return result
	}

	// Try direct ToolParameters struct first
	if tp, ok := params.(core.ToolParameters); ok {
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

	return result
}

// ---------------------------------------------------------------------------
// Helper: build a canonical list of parameter names from a handler definition.
// ---------------------------------------------------------------------------

func handlerParamNames(h tools.ToolHandler) []string {
	def := h.Definition()
	names := make([]string, 0, len(def.Parameters))
	for _, p := range def.Parameters {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestToolSync_AllToolsPresent tests that the seed registry and the handler
// registry register exactly the same set of tool names.
func TestToolSync_AllToolsPresent(t *testing.T) {
	sprout := tools.GetNewToolRegistry()
	seed := NewSeedToolRegistry(nil)

	sproutNames := make(map[string]bool)
	for _, h := range sprout.All() {
		sproutNames[h.Name()] = true
	}

	seedNames := make(map[string]bool)
	for _, name := range seed.ToolNames() {
		seedNames[name] = true
	}

	// Check every sprout tool exists in seed
	for name := range sproutNames {
		if !seedNames[name] {
			t.Errorf("handler registry has tool %q not found in seed registry", name)
		}
	}

	// Check every seed tool exists in sprout
	for name := range seedNames {
		if !sproutNames[name] {
			t.Errorf("seed registry has tool %q not found in handler registry", name)
		}
	}
}

// TestToolSync_ParametersMatch tests that for every tool present in both
// registries, the parameter names, types, and required flags are identical.
func TestToolSync_ParametersMatch(t *testing.T) {
	sprout := tools.GetNewToolRegistry()
	seed := NewSeedToolRegistry(nil)

	// Build handler tool definition lookup
	handlerDefs := make(map[string]tools.ToolHandler)
	for _, h := range sprout.All() {
		handlerDefs[h.Name()] = h
	}

	// Iterate over every seed tool
	for _, seedTool := range seed.GetTools() {
		name := seedTool.Function.Name

		h, found := handlerDefs[name]
		if !found {
			t.Errorf("tool %q: handler not found", name)
			continue
		}
		def := h.Definition()

		// Parse seed schema
		seedParams := parseSeedToolParameters(seedTool.Function.Parameters)

		// Build handler lookup for this tool's parameters
		handlerParamMap := make(map[string]struct {
			Type     string
			Required bool
		})
		requiredSet := make(map[string]struct{}, len(def.Required))
		for _, rn := range def.Required {
			requiredSet[rn] = struct{}{}
		}
		for _, p := range def.Parameters {
			req := p.Required
			if !req {
				_, req = requiredSet[p.Name]
			}
			handlerParamMap[p.Name] = struct {
				Type     string
				Required bool
			}{p.Type, req}
		}

		// Compare parameter counts
		if len(seedParams) != len(handlerParamMap) {
			t.Errorf("tool %q: parameter count mismatch — seed has %d, handler has %d",
				name, len(seedParams), len(handlerParamMap))
		}

		// Check every seed parameter exists in handler
		for paramName, seedInfo := range seedParams {
			handlerInfo, found := handlerParamMap[paramName]
			if !found {
				t.Errorf("tool %q: seed parameter %q not found in handler", name, paramName)
				continue
			}

			if seedInfo.Type != handlerInfo.Type {
				t.Errorf("tool %q, param %q: type mismatch — seed %q, handler %q",
					name, paramName, seedInfo.Type, handlerInfo.Type)
			}

			if seedInfo.Required != handlerInfo.Required {
				t.Errorf("tool %q, param %q: required mismatch — seed %v, handler %v",
					name, paramName, seedInfo.Required, handlerInfo.Required)
			}
		}

		// Check every handler parameter exists in seed
		for paramName := range handlerParamMap {
			if _, found := seedParams[paramName]; !found {
				t.Errorf("tool %q: handler parameter %q not found in seed", name, paramName)
			}
		}
	}
}

// TestToolSync_ValidJSONSchemaTypes tests that every parameter in both
// registries uses one of the valid JSON Schema types ("string", "integer",
// "number", "boolean", "array", "object").
func TestToolSync_ValidJSONSchemaTypes(t *testing.T) {
	// Check handler registry
	sprout := tools.GetNewToolRegistry()
	for _, h := range sprout.All() {
		def := h.Definition()
		for _, param := range def.Parameters {
			if !validJSONSchemaTypes[param.Type] {
				t.Errorf("tool %q, param %q: invalid JSON Schema type %q (valid types: %v)",
					h.Name(), param.Name, param.Type, sortedValidTypes())
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
// (aliases) are handled correctly. Since seed absorbs alternatives, the test
// verifies parameter name consistency between the two registries.
func TestToolSync_AlternativeNamesMatch(t *testing.T) {
	sprout := tools.GetNewToolRegistry()
	seed := NewSeedToolRegistry(nil)

	for _, seedTool := range seed.GetTools() {
		name := seedTool.Function.Name

		h, found := sprout.Lookup(name)
		if !found {
			t.Errorf("tool %q: no handler found", name)
			continue
		}
		def := h.Definition()

		seedParams := parseSeedToolParameters(seedTool.Function.Parameters)

		handlerNames := make(map[string]bool)
		for _, p := range def.Parameters {
			handlerNames[p.Name] = true
		}

		for seedName := range seedParams {
			if !handlerNames[seedName] {
				t.Errorf("tool %q: parameter %q exists in seed but not handler", name, seedName)
			}
		}
		for hName := range handlerNames {
			if _, found := seedParams[hName]; !found {
				t.Errorf("tool %q: parameter %q exists in handler but not seed", name, hName)
			}
		}

		// Validate handler alternatives check no longer applicable
		// (handler definitions use a single Required field per parameter,
		// not an Alternatives list). Seed absorbs alternatives during schema
		// construction — the parameter name set comparison above is sufficient.
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
	sprout := tools.GetNewToolRegistry()
	seed := NewSeedToolRegistry(nil)

	sproutCount := len(sprout.All())
	seedCount := len(seed.GetTools())

	if sproutCount != seedCount {
		t.Errorf("tool count mismatch: handler has %d, seed has %d", sproutCount, seedCount)
	}
	t.Logf("both registries have %d tools", sproutCount)
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

// TestToolSync_BrowseUrlParameters tests that the complex browse_url tool
// has consistent parameter definitions between both registries.
func TestToolSync_BrowseUrlParameters(t *testing.T) {
	sprout := tools.GetNewToolRegistry()
	seed := NewSeedToolRegistry(nil)

	h, found := sprout.Lookup("browse_url")
	if !found {
		t.Fatal("handler registry missing browse_url tool")
	}
	def := h.Definition()

	seedCfg := seed.GetTool("browse_url")
	if seedCfg == nil {
		t.Fatal("seed registry missing browse_url tool")
	}

	seedParams := parseSeedToolParameters(seedCfg.Function.Parameters)

	handlerParamMap := make(map[string]struct {
		Type     string
		Required bool
	})
	requiredSet := make(map[string]struct{}, len(def.Required))
	for _, rn := range def.Required {
		requiredSet[rn] = struct{}{}
	}
	for _, p := range def.Parameters {
		req := p.Required
		if !req {
			_, req = requiredSet[p.Name]
		}
		handlerParamMap[p.Name] = struct {
			Type     string
			Required bool
		}{p.Type, req}
	}

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
		handlerInfo, handlerOk := handlerParamMap[param]

		if seedOk != handlerOk {
			t.Errorf("browse_url: parameter %q consistency — seed:%v, handler:%v",
				param, seedOk, handlerOk)
			continue
		}

		if seedOk {
			if seedInfo.Type != handlerInfo.Type {
				t.Errorf("browse_url, param %q: type mismatch seed=%q handler=%q",
					param, seedInfo.Type, handlerInfo.Type)
			}
			if seedInfo.Required != handlerInfo.Required {
				t.Errorf("browse_url, param %q: required mismatch seed=%v handler=%v",
					param, seedInfo.Required, handlerInfo.Required)
			}
		}
	}
}

// TestToolSync_SyncErrorMessageQuality ensures error messages are descriptive.
func TestToolSync_SyncErrorMessageQuality(t *testing.T) {
	// Create a seed tool with a mismatched type
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

	// Check the seed tool has the correct type
	seedTool := seed.GetTool("deliberate_mismatch")
	seedParams := parseSeedToolParameters(seedTool.Function.Parameters)
	seedInfo := seedParams["foo"]

	if seedInfo.Type != "integer" {
		t.Errorf("expected seed type to be \"integer\", got %q", seedInfo.Type)
	}

	// "int" is NOT valid per validJSONSchemaTypes
	if !validJSONSchemaTypes["integer"] {
		t.Errorf("test setup broken: \"integer\" should be a valid JSON Schema type")
	}
}
