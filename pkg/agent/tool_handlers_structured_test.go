package agent

import (
	"strings"
	"testing"
)

func TestValidateDataAgainstSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"name", "tags"},
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"tags": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
		},
		"additionalProperties": false,
	}

	valid := map[string]interface{}{
		"name": "breakfast",
		"tags": []interface{}{"menu", "food"},
	}
	if errs := validateDataAgainstSchema(valid, schema, "$"); len(errs) != 0 {
		t.Fatalf("expected valid schema, got errors: %v", errs)
	}

	invalid := map[string]interface{}{
		"name":  123,
		"tags":  []interface{}{1},
		"extra": true,
	}
	if errs := validateDataAgainstSchema(invalid, schema, "$"); len(errs) == 0 {
		t.Fatalf("expected schema validation errors")
	}
}

func TestApplyPatchOperations_ObjectAndArray(t *testing.T) {
	doc := map[string]interface{}{
		"menu": map[string]interface{}{
			"items": []interface{}{"eggs", "toast"},
		},
	}

	ops := []jsonPatchOperation{
		{Op: "add", Path: "/menu/items/1", Value: "pancakes"},
		{Op: "replace", Path: "/menu/items/0", Value: "omelet"},
		{Op: "remove", Path: "/menu/items/2"},
		{Op: "test", Path: "/menu/items/0", Value: "omelet"},
	}

	var err error
	var out interface{} = doc
	for _, op := range ops {
		out, err = applyPatchOperation(out, op)
		if err != nil {
			t.Fatalf("applyPatchOperation failed for %+v: %v", op, err)
		}
	}

	obj := out.(map[string]interface{})
	menu := obj["menu"].(map[string]interface{})
	items := menu["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0] != "omelet" || items[1] != "pancakes" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestInferStructuredFormat(t *testing.T) {
	if got := inferStructuredFormat("config.json", ""); got != "json" {
		t.Fatalf("expected json, got %s", got)
	}
	if got := inferStructuredFormat("config.yaml", "yml"); got != "yaml" {
		t.Fatalf("expected yaml, got %s", got)
	}
	if got := inferStructuredFormat("config.txt", ""); got != "" {
		t.Fatalf("expected empty format, got %s", got)
	}
}

func TestFormatStructuredValidationError_IncludesPathsAndCount(t *testing.T) {
	err := formatStructuredValidationError("write_structured_file", []string{
		"$.content.name: expected string",
		"$.content.price: expected number",
		"$.content.price: expected number",
	}, "")
	msg := err.Error()
	if !strings.Contains(msg, "tool=write_structured_file") {
		t.Fatalf("expected tool name in message, got: %s", msg)
	}
	if !strings.Contains(msg, "error_count=3") {
		t.Fatalf("expected error count in message, got: %s", msg)
	}
	if !strings.Contains(msg, "failed_paths=$.content.name,$.content.price") {
		t.Fatalf("expected failed paths in message, got: %s", msg)
	}
}

func TestExtractValidationPaths(t *testing.T) {
	paths := extractValidationPaths([]string{
		"$.a.b: expected string",
		"$.a.b: expected string",
		"$.arr[0]: expected integer",
		"not-a-schema-error",
	})
	if len(paths) != 2 {
		t.Fatalf("expected 2 unique paths, got %d (%v)", len(paths), paths)
	}
	if paths[0] != "$.a.b" || paths[1] != "$.arr[0]" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestPatchReplacePreservesOrderedKeyOutput(t *testing.T) {
	// Simulate a package.json with exports in wrong order (default before types).
	doc, err := ParseJSONOrdered(`{
	  "exports": {
	    ".": {
	      "default": "./dist/index.js",
	      "import": "./dist/index.js",
	      "types": "./dist/index.d.ts"
	    }
	  }
	}`)
	if err != nil {
		t.Fatalf("ParseJSONOrdered: %v", err)
	}

	// Replace the "." entry with a reordered value.
	// The value arrives as map[string]interface{} from json.Unmarshal (tool args),
	// which loses the original key order. convertToOrderedValue sorts alphabetically,
	// so the output is deterministic (alphabetical) rather than random.
	patchValue := map[string]interface{}{
		"types":   "./dist/index.d.ts",
		"import":  "./dist/index.js",
		"default": "./dist/index.js",
	}
	ops := []jsonPatchOperation{
		{Op: "replace", Path: "/exports/.", Value: patchValue},
	}

	result := interface{}(doc)
	for _, op := range ops {
		result, err = applyPatchOperation(result, op)
		if err != nil {
			t.Fatalf("applyPatchOperation failed: %v", err)
		}
	}

	// Serialize back to JSON and verify keys are in deterministic order.
	serialized, err := SerializeJSONOrdered(result)
	if err != nil {
		t.Fatalf("SerializeJSONOrdered: %v", err)
	}

	// Keys should be alphabetically sorted: default < import < types.
	expected := `{
  "exports": {
    ".": {
      "default": "./dist/index.js",
      "import": "./dist/index.js",
      "types": "./dist/index.d.ts"
    }
  }
}`
	if serialized != expected {
		t.Fatalf("expected alphabetically ordered keys:\n%s\n\ngot:\n%s", expected, serialized)
	}
}
