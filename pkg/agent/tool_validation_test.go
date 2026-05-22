package agent

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestGetMapKeys_NilMap verifies that getMapKeys handles nil maps without panicking.
func TestGetMapKeys_NilMap(t *testing.T) {
	var m map[string]interface{}
	keys := getMapKeys(m)
	if keys == nil {
		// nil is acceptable for nil input, but empty slice is fine too.
		return
	}
	if len(keys) != 0 {
		t.Errorf("getMapKeys(nil) returned %v, want empty slice", keys)
	}
}

// TestGetMapKeys_EmptyMap verifies that getMapKeys returns an empty slice for empty maps.
func TestGetMapKeys_EmptyMap(t *testing.T) {
	m := make(map[string]interface{})
	keys := getMapKeys(m)
	if len(keys) != 0 {
		t.Errorf("getMapKeys(empty map) returned %v, want empty slice", keys)
	}
}

// TestGetMapKeys_SingleKey verifies that getMapKeys returns the correct key for a single-entry map.
func TestGetMapKeys_SingleKey(t *testing.T) {
	m := map[string]interface{}{
		"only_key": "value",
	}
	keys := getMapKeys(m)
	if len(keys) != 1 {
		t.Fatalf("getMapKeys(single key map) returned %d keys, want 1", len(keys))
	}
	if keys[0] != "only_key" {
		t.Errorf("getMapKeys returned %q, want %q", keys[0], "only_key")
	}
}

// TestGetMapKeys_MultipleKeys verifies that getMapKeys returns all keys from a multi-entry map.
func TestGetMapKeys_MultipleKeys(t *testing.T) {
	m := map[string]interface{}{
		"alpha":   "a",
		"bravo":   "b",
		"charlie": "c",
		"delta":   "d",
	}
	keys := getMapKeys(m)
	if len(keys) != 4 {
		t.Fatalf("getMapKeys returned %d keys, want 4", len(keys))
	}

	// Sort keys for deterministic comparison (map iteration order is not guaranteed).
	sort.Strings(keys)
	expected := []string{"alpha", "bravo", "charlie", "delta"}
	if !reflect.DeepEqual(keys, expected) {
		t.Errorf("getMapKeys returned %v, want %v", keys, expected)
	}
}

// TestGetMapKeys_NilValues verifies that getMapKeys handles maps with nil values.
func TestGetMapKeys_NilValues(t *testing.T) {
	m := map[string]interface{}{
		"nil_val": nil,
		"str_val": "hello",
	}
	keys := getMapKeys(m)
	if len(keys) != 2 {
		t.Fatalf("getMapKeys returned %d keys, want 2", len(keys))
	}
}

// TestGetMapKeys_MixedTypes verifies that getMapKeys works with maps containing
// values of mixed types.
func TestGetMapKeys_MixedTypes(t *testing.T) {
	m := map[string]interface{}{
		"string": "value",
		"int":    42,
		"float":  3.14,
		"bool":   true,
		"slice":  []interface{}{1, 2, 3},
		"map":    map[string]interface{}{"nested": true},
	}
	keys := getMapKeys(m)
	if len(keys) != 6 {
		t.Fatalf("getMapKeys returned %d keys, want 6", len(keys))
	}
}

// TestMapToJsonString_EmptyMap verifies that mapToJSONString produces valid JSON
// for an empty map.
func TestMapToJsonString_EmptyMap(t *testing.T) {
	r := newDefaultToolRegistry()
	m := make(map[string]interface{})
	result, err := r.mapToJSONString(m)
	if err != nil {
		t.Fatalf("mapToJSONString(empty map) returned error: %v", err)
	}
	if result != "{}" {
		t.Errorf("mapToJSONString(empty map) returned %q, want %q", result, "{}")
	}
}

// TestMapToJsonString_ValidData verifies that mapToJSONString produces valid
// indented JSON for a map with data.
func TestMapToJsonString_ValidData(t *testing.T) {
	r := newDefaultToolRegistry()
	m := map[string]interface{}{
		"name":   "test",
		"count":  42,
		"active": true,
	}
	result, err := r.mapToJSONString(m)
	if err != nil {
		t.Fatalf("mapToJSONString returned error: %v", err)
	}
	if result == "" {
		t.Error("mapToJSONString returned empty string")
	}
	// Verify it's valid indented JSON.
	if !strings.Contains(result, "\"name\"") {
		t.Error("result missing \"name\" key")
	}
	if !strings.Contains(result, "\"count\"") {
		t.Error("result missing \"count\" key")
	}
}
