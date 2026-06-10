package agent

import (
	"context"
	"strings"
	"testing"
)

func TestParseStructuredJSONContent_SyntaxErrorIncludesLocationAndSnippet(t *testing.T) {
	_, err := parseStructuredJSONContent("{\n  \"name\": \"ok\"\n  \"price\": 10\n}", "write_file")
	if err == nil {
		t.Fatal("expected parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "line=") || !strings.Contains(msg, "col=") {
		t.Fatalf("expected line/col details in error, got: %s", msg)
	}
	if !strings.Contains(msg, "snippet=") {
		t.Fatalf("expected snippet in error, got: %s", msg)
	}
	if !strings.Contains(msg, "next_step=fix JSON syntax and retry write_file") {
		t.Fatalf("expected next_step hint in error, got: %s", msg)
	}
}

func TestParseStructuredJSONContent_RejectsScalarTopLevel(t *testing.T) {
	_, err := parseStructuredJSONContent("\"hello\"", "write_file")
	if err == nil {
		t.Fatal("expected top-level scalar to be rejected")
	}
	if !strings.Contains(err.Error(), "top-level JSON must be an object or array") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseStructuredJSONContent_EditFileHintUsesEditFile(t *testing.T) {
	_, err := parseStructuredJSONContent("{\n  \"name\": \"ok\"\n  \"price\": 10\n}", "edit_file")
	if err == nil {
		t.Fatal("expected parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "retry edit_file") {
		t.Fatalf("expected edit_file retry hint, got: %s", msg)
	}
	if strings.Contains(msg, "write_structured_file") {
		t.Fatalf("did not expect structured tool guidance in proxy path error: %s", msg)
	}
}

func TestHandleWriteFile_InvalidJSON_ReturnsForwardingDiagnostics(t *testing.T) {
	_, err := handleWriteFile(context.Background(), nil, map[string]interface{}{
		"path":    "./menu.json",
		"content": "{\n  \"name\": \"ok\"\n  \"price\": 10\n}",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "write_file JSON forwarding failed") {
		t.Fatalf("expected forwarding failure prefix, got: %s", msg)
	}
	if !strings.Contains(msg, "line=") || !strings.Contains(msg, "col=") {
		t.Fatalf("expected line/col details, got: %s", msg)
	}
}

func TestParseStructuredJSONContent_PreservesKeyOrder(t *testing.T) {
	// The exports condition in package.json is order-sensitive: "default"
	// must come after "types" and "import" for correct module resolution.
	input := `{"exports":{".":{"types":"./dist/index.d.ts","import":"./dist/index.js","default":"./dist/index.js"}}}`

	result, err := parseStructuredJSONContent(input, "edit_file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	om, ok := result.(*OrderedMap)
	if !ok {
		t.Fatalf("expected *OrderedMap, got %T", result)
	}

	exports, _ := om.Get("exports")
	exportsOm := exports.(*OrderedMap)
	dot, _ := exportsOm.Get(".")
	dotOm := dot.(*OrderedMap)

	keys := dotOm.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "types" || keys[1] != "import" || keys[2] != "default" {
		t.Fatalf("expected keys in order [types, import, default], got %v", keys)
	}
}

func TestParseStructuredJSONContent_TopLevelArray(t *testing.T) {
	input := `[{"b":2,"a":1}]`
	result, err := parseStructuredJSONContent(input, "edit_file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 element, got %d", len(arr))
	}
}
