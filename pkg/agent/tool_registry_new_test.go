package agent

import (
	"context"
	"testing"
)

// TestToolRegistry is the test function prefix to avoid naming conflicts
// with the ToolRegistry type.

func TestToolRegistry_RegisterTool(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	r.RegisterTool(ToolConfig{
		Name:        "test_tool",
		Description: "A test tool",
		Handler:     func(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) { return "ok", nil },
	})

	if _, ok := r.tools["test_tool"]; !ok {
		t.Error("tool should be registered")
	}

	// Overwriting should replace
	r.RegisterTool(ToolConfig{
		Name:        "test_tool",
		Description: "Updated description",
	})
	if r.tools["test_tool"].Description != "Updated description" {
		t.Error("overwriting should replace the tool")
	}
}

func TestToolRegistry_GetAvailableTools(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	tools := r.GetAvailableTools()
	if len(tools) != 0 {
		t.Errorf("empty registry should return 0 tools, got %d", len(tools))
	}

	r.RegisterTool(ToolConfig{Name: "tool_a", Description: "A"})
	r.RegisterTool(ToolConfig{Name: "tool_b", Description: "B"})
	r.RegisterTool(ToolConfig{Name: "tool_c", Description: "C"})

	tools = r.GetAvailableTools()
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}

	// Check all names are present
	toolMap := make(map[string]bool)
	for _, name := range tools {
		toolMap[name] = true
	}
	for _, expected := range []string{"tool_a", "tool_b", "tool_c"} {
		if !toolMap[expected] {
			t.Errorf("missing tool %q in result", expected)
		}
	}
}

func TestToolRegistry_extractParameter(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	args := map[string]interface{}{
		"name":     "value",
		"alt_name": "alt_value",
	}

	// Primary name exists
	val, found := r.extractParameter(ParameterConfig{Name: "name"}, args)
	if !found {
		t.Error("should find primary name")
	}
	if val != "value" {
		t.Errorf("got %v, want value", val)
	}

	// Primary name missing, alternative found
	val, found = r.extractParameter(ParameterConfig{Name: "other", Alternatives: []string{"alt_name"}}, args)
	if !found {
		t.Error("should find alternative name")
	}
	if val != "alt_value" {
		t.Errorf("got %v, want alt_value", val)
	}

	// Neither primary nor alternatives found
	_, found = r.extractParameter(ParameterConfig{Name: "missing", Alternatives: []string{"also_missing"}}, args)
	if found {
		t.Error("should not find missing parameter")
	}
}

func TestToolRegistry_extractParameter_NoAlternatives(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	args := map[string]interface{}{"primary": "value"}

	// No alternatives configured
	_, found := r.extractParameter(ParameterConfig{Name: "missing"}, args)
	if found {
		t.Error("should not find missing parameter with no alternatives")
	}
}

func TestToolRegistry_convertParameterType_String(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	// Already a string
	result, err := r.convertParameterType("hello", "string", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(string) != "hello" {
		t.Errorf("got %v, want hello", result)
	}

	// Map to JSON string
	result, err = r.convertParameterType(map[string]interface{}{"key": "val"}, "string", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := result.(string)
	if s == "" {
		t.Error("should convert map to non-empty JSON string")
	}
	// Should contain the key and value
	if len(s) < 15 {
		t.Errorf("JSON string too short: %q", s)
	}
}

func TestToolRegistry_convertParameterType_String_FailsForInt(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	_, err := r.convertParameterType(42, "string", nil)
	if err == nil {
		t.Error("expected error for int -> string conversion")
	}
}

func TestToolRegistry_convertParameterType_Int(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	// Already int
	result, err := r.convertParameterType(42, "int", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(int) != 42 {
		t.Errorf("got %v, want 42", result)
	}

	// Float64 to int (common from JSON unmarshaling)
	result, err = r.convertParameterType(float64(99), "int", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(int) != 99 {
		t.Errorf("got %v, want 99", result)
	}
}

func TestToolRegistry_convertParameterType_Int_FailsForString(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	_, err := r.convertParameterType("hello", "int", nil)
	if err == nil {
		t.Error("expected error for string -> int conversion")
	}
}

func TestToolRegistry_convertParameterType_Float64(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	// Already float64
	result, err := r.convertParameterType(3.14, "float64", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(float64) != 3.14 {
		t.Errorf("got %v, want 3.14", result)
	}

	// Int to float64
	result, err = r.convertParameterType(42, "float64", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(float64) != 42.0 {
		t.Errorf("got %v, want 42.0", result)
	}
}

func TestToolRegistry_convertParameterType_Float64_FailsForString(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	_, err := r.convertParameterType("hello", "float64", nil)
	if err == nil {
		t.Error("expected error for string -> float64 conversion")
	}
}

func TestToolRegistry_convertParameterType_Bool(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	result, err := r.convertParameterType(true, "bool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(bool) != true {
		t.Error("got false, want true")
	}

	result, err = r.convertParameterType(false, "bool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(bool) != false {
		t.Error("got true, want false")
	}
}

func TestToolRegistry_convertParameterType_Bool_FailsForString(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	_, err := r.convertParameterType("yes", "bool", nil)
	if err == nil {
		t.Error("expected error for string -> bool conversion")
	}
}

func TestToolRegistry_convertParameterType_Array(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	arr := []interface{}{"a", "b", "c"}
	result, err := r.convertParameterType(arr, "array", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.([]interface{})) != 3 {
		t.Errorf("got %d items, want 3", len(result.([]interface{})))
	}
}

func TestToolRegistry_convertParameterType_Array_FailsForString(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	_, err := r.convertParameterType("hello", "array", nil)
	if err == nil {
		t.Error("expected error for string -> array conversion")
	}
}

func TestToolRegistry_convertParameterType_Object(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	obj := map[string]interface{}{"key": "value"}
	result, err := r.convertParameterType(obj, "object", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(map[string]interface{})["key"] != "value" {
		t.Error("object should be preserved")
	}

	// Also accepts []interface{} (array as object for structured content)
	arr := []interface{}{"a", "b"}
	result, err = r.convertParameterType(arr, "object", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.([]interface{}); !ok {
		t.Error("should accept []interface{} as object")
	}
}

func TestToolRegistry_convertParameterType_Object_FailsForString(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	_, err := r.convertParameterType("hello", "object", nil)
	if err == nil {
		t.Error("expected error for string -> object conversion")
	}
}

func TestToolRegistry_convertParameterType_Unknown(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	// Unknown types pass through
	val := 42
	result, err := r.convertParameterType(val, "unknown_type", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != val {
		t.Errorf("got %v, want %v", result, val)
	}
}

func TestToolRegistry_validateParameters(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	tool := ToolConfig{
		Name: "test",
		Parameters: []ParameterConfig{
			{Name: "name", Type: "string", Required: true},
			{Name: "count", Type: "int", Required: false},
		},
	}

	// All required present
	args := map[string]interface{}{
		"name":  "hello",
		"count": 5,
	}
	result, err := r.validateParameters(tool, args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "hello" {
		t.Errorf("name = %v, want hello", result["name"])
	}
	if result["count"] != 5 {
		t.Errorf("count = %v, want 5", result["count"])
	}

	// Required missing
	args = map[string]interface{}{
		"count": 5,
	}
	_, err = r.validateParameters(tool, args, nil)
	if err == nil {
		t.Error("expected error for missing required parameter")
	}

	// Wrong type
	args = map[string]interface{}{
		"name": 123, // int instead of string
	}
	_, err = r.validateParameters(tool, args, nil)
	if err == nil {
		t.Error("expected error for wrong parameter type")
	}
}

func TestToolRegistry_validateParameters_AlternativeNames(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	tool := ToolConfig{
		Name: "test",
		Parameters: []ParameterConfig{
			{Name: "path", Type: "string", Required: true, Alternatives: []string{"file_path"}},
		},
	}

	// Using alternative name
	args := map[string]interface{}{
		"file_path": "/some/path",
	}
	result, err := r.validateParameters(tool, args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["path"] != "/some/path" {
		t.Errorf("path = %v, want /some/path", result["path"])
	}

	// Primary name takes precedence over alternative
	args = map[string]interface{}{
		"path":      "/primary",
		"file_path": "/alternative",
	}
	result, err = r.validateParameters(tool, args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["path"] != "/primary" {
		t.Errorf("path = %v, want /primary (primary should take precedence)", result["path"])
	}
}

func TestToolRegistry_validateParameters_NoParameters(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	tool := ToolConfig{
		Name:       "no_params",
		Parameters: []ParameterConfig{},
	}

	result, err := r.validateParameters(tool, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d params", len(result))
	}
}

func TestToolRegistry_mapToJSONString(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	// Simple map
	m := map[string]interface{}{
		"name": "test",
		"num":  float64(42),
	}
	result, err := r.mapToJSONString(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("should return non-empty JSON string")
	}
	// Should contain the key
	if len(result) < 10 {
		t.Errorf("JSON string too short: %q", result)
	}

	// Empty map
	m = map[string]interface{}{}
	result, err = r.mapToJSONString(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "{}" {
		t.Errorf("empty map should produce {}, got %q", result)
	}

	// Nested map
	m = map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": "value",
		},
	}
	result, err = r.mapToJSONString(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("nested map should produce valid JSON")
	}
}

func TestToolRegistry_ValidateParameters_FloatToIntConversion(t *testing.T) {
	r := &ToolRegistry{tools: make(map[string]ToolConfig)}

	tool := ToolConfig{
		Name: "test",
		Parameters: []ParameterConfig{
			{Name: "limit", Type: "int", Required: true},
		},
	}

	// JSON unmarshals numbers as float64; should convert to int
	args := map[string]interface{}{
		"limit": float64(10),
	}
	result, err := r.validateParameters(tool, args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["limit"] != 10 {
		t.Errorf("limit = %v, want 10", result["limit"])
	}
}
