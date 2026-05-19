package mcp

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ---------------------------------------------------------------------------
// TestFormatPath
// ---------------------------------------------------------------------------

func TestFormatPath(t *testing.T) {
	tests := []struct {
		input  string
		want   string
	}{
		{"", ""},
		{"/birth_year", "'birth_year'"},
		{"/name", "'name'"},
		{"/nested/field", "'nested.field'"},
		{"/a/b/c", "'a.b.c'"},
		{"/items/0/name", "'items[0].name'"},
		{"/items/0/0/value", "'items[0][0].value'"},
		{"/items/12/props/0/name", "'items[12].props[0].name'"},
	}

	for _, tt := range tests {
		got := formatPath(tt.input)
		if got != tt.want {
			t.Errorf("formatPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper: compileSchemaAndValidate builds a schema, compiles it, and
// validates the given input. Returns the error from validation (nil if valid).
// ---------------------------------------------------------------------------

func compileSchemaAndValidate(schema map[string]interface{}, input map[string]interface{}) error {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema://test", schema); err != nil {
		panic("failed to add schema: " + err.Error())
	}
	s, err := compiler.Compile("schema://test")
	if err != nil {
		panic("failed to compile schema: " + err.Error())
	}
	return s.Validate(input)
}

// ---------------------------------------------------------------------------
// TestInvalidArgsError_FormatForLLM_SingleRequiredField
// ---------------------------------------------------------------------------

func TestInvalidArgsError_FormatForLLM_SingleRequiredField(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"url"},
		"properties": map[string]interface{}{
			"url": map[string]interface{}{"type": "string"},
		},
	}

	verr := compileSchemaAndValidate(schema, map[string]interface{}{})
	if verr == nil {
		t.Fatal("expected validation error for missing required field")
	}

	err := &InvalidArgsError{
		Server:  "webserver",
		Tool:    "fetch_page",
		Wrapped: verr,
	}

	msg := err.FormatForLLM()

	// Should contain the header line
	if !strings.Contains(msg, "Tool 'fetch_page' from server 'webserver' validation failed:") {
		t.Errorf("missing expected header; got:\n%s", msg)
	}

	// Should contain the missing property message as a bullet
	if !strings.Contains(msg, "- missing property 'url'") {
		t.Errorf("missing expected bullet for missing property; got:\n%s", msg)
	}
}

// ---------------------------------------------------------------------------
// TestInvalidArgsError_FormatForLLM_TypeMismatch
// ---------------------------------------------------------------------------

func TestInvalidArgsError_FormatForLLM_TypeMismatch(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"properties": map[string]interface{}{
			"timeout_ms": map[string]interface{}{"type": "integer"},
		},
	}

	verr := compileSchemaAndValidate(schema, map[string]interface{}{
		"timeout_ms": "not-a-number",
	})
	if verr == nil {
		t.Fatal("expected validation error for wrong type")
	}

	err := &InvalidArgsError{
		Server:  "config-server",
		Tool:    "set_timeout",
		Wrapped: verr,
	}

	msg := err.FormatForLLM()

	if !strings.Contains(msg, "Tool 'set_timeout' from server 'config-server' validation failed:") {
		t.Errorf("missing expected header; got:\n%s", msg)
	}

	// Should contain the path and type error
	if !strings.Contains(msg, "- 'timeout_ms' got string, want integer") {
		t.Errorf("missing expected type-mismatch bullet; got:\n%s", msg)
	}
}

// ---------------------------------------------------------------------------
// TestInvalidArgsError_FormatForLLM_NestedPath
// ---------------------------------------------------------------------------

func TestInvalidArgsError_FormatForLLM_NestedPath(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nested": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"count": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}

	verr := compileSchemaAndValidate(schema, map[string]interface{}{
		"nested": map[string]interface{}{
			"count": "wrong",
		},
	})
	if verr == nil {
		t.Fatal("expected validation error for nested type mismatch")
	}

	err := &InvalidArgsError{
		Server:  "data-server",
		Tool:    "process",
		Wrapped: verr,
	}

	msg := err.FormatForLLM()

	if !strings.Contains(msg, "- 'nested.count' got string, want integer") {
		t.Errorf("missing expected nested-path bullet; got:\n%s", msg)
	}
}

// ---------------------------------------------------------------------------
// TestInvalidArgsError_FormatForLLM_MultipleErrors
// ---------------------------------------------------------------------------

func TestInvalidArgsError_FormatForLLM_MultipleErrors(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"url", "timeout_ms"},
		"properties": map[string]interface{}{
			"url":        map[string]interface{}{"type": "string"},
			"timeout_ms": map[string]interface{}{"type": "integer"},
			"nested": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"count": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}

	verr := compileSchemaAndValidate(schema, map[string]interface{}{
		"timeout_ms": "not-a-number",
		"nested": map[string]interface{}{
			"count": "wrong",
		},
	})
	if verr == nil {
		t.Fatal("expected validation errors for multiple issues")
	}

	err := &InvalidArgsError{
		Server:  "webserver",
		Tool:    "fetch_page",
		Wrapped: verr,
	}

	msg := err.FormatForLLM()

	// Should have the header
	if !strings.Contains(msg, "Tool 'fetch_page' from server 'webserver' validation failed:") {
		t.Errorf("missing expected header; got:\n%s", msg)
	}

	// Should contain all three errors as bullet points
	if !strings.Contains(msg, "- missing property 'url'") {
		t.Errorf("missing bullet for missing required field; got:\n%s", msg)
	}
	if !strings.Contains(msg, "- 'timeout_ms' got string, want integer") {
		t.Errorf("missing bullet for timeout_ms type error; got:\n%s", msg)
	}
	if !strings.Contains(msg, "- 'nested.count' got string, want integer") {
		t.Errorf("missing bullet for nested.count type error; got:\n%s", msg)
	}
}

// ---------------------------------------------------------------------------
// TestInvalidArgsError_FormatForLLM_NilWrapped
// ---------------------------------------------------------------------------

func TestInvalidArgsError_FormatForLLM_NilWrapped(t *testing.T) {
	err := &InvalidArgsError{
		Server:  "mysrv",
		Tool:    "mytool",
		Wrapped: nil,
	}

	msg := err.FormatForLLM()

	// Should fall back to Error() output
	expected := err.Error()
	if msg != expected {
		t.Errorf("FormatForLLM() with nil Wrapped = %q, want %q (Error() fallback)", msg, expected)
	}
}

// ---------------------------------------------------------------------------
// TestFormatPath_ArrayIndices (additional edge case coverage)
// ---------------------------------------------------------------------------

func TestFormatPath_ArrayIndices(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/items/0/name", "'items[0].name'"},
		{"/data/99", "'data[99]'"},
	}
	for _, tt := range tests {
		got := formatPath(tt.input)
		if got != tt.want {
			t.Errorf("formatPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
