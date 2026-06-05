package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SerializeJSONOrdered
// ---------------------------------------------------------------------------

func TestSerializeJSONOrdered_OrderedMapKeys(t *testing.T) {
	om := NewOrderedMap()
	om.Set("z", 1)
	om.Set("a", 2)
	om.Set("m", 3)

	result, err := SerializeJSONOrdered(om)
	require.NoError(t, err)

	// The JSON output should have keys in z, a, m order.
	// Verify order by checking positions in the string.
	zPos := strings.Index(result, `"z"`)
	aPos := strings.Index(result, `"a"`)
	mPos := strings.Index(result, `"m"`)
	assert.True(t, zPos < aPos, "z should come before a")
	assert.True(t, aPos < mPos, "a should come before m")
}

func TestSerializeJSONOrdered_NestedOrderedMap(t *testing.T) {
	inner := NewOrderedMap()
	inner.Set("inner_z", 1)
	inner.Set("inner_a", 2)

	outer := NewOrderedMap()
	outer.Set("outer_key", inner)
	outer.Set("top", true)

	result, err := SerializeJSONOrdered(outer)
	require.NoError(t, err)

	// Parse back and verify structure.
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	// Verify nested object exists.
	nested, ok := parsed["outer_key"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, nested, "inner_z")
	assert.Contains(t, nested, "inner_a")

	// Verify order in raw string: "outer_key" comes before "top".
	outerPos := strings.Index(result, `"outer_key"`)
	topPos := strings.Index(result, `"top"`)
	assert.True(t, outerPos < topPos)

	// Within nested, inner_z comes before inner_a.
	innerZPos := strings.Index(result, `"inner_z"`)
	innerAPos := strings.Index(result, `"inner_a"`)
	assert.True(t, innerZPos < innerAPos)
}

func TestSerializeJSONOrdered_RegularMap(t *testing.T) {
	// Regular map[string]interface{} should work (json.Marshal handles it).
	data := map[string]interface{}{
		"name":  "test",
		"count": 42,
	}

	result, err := SerializeJSONOrdered(data)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "test", parsed["name"])
}

func TestSerializeJSONOrdered_ArrayOfOrderedMaps(t *testing.T) {
	first := NewOrderedMap()
	first.Set("z", 1)
	first.Set("a", 2)

	second := NewOrderedMap()
	second.Set("m", 3)
	second.Set("b", 4)

	data := []interface{}{first, second}
	result, err := SerializeJSONOrdered(data)
	require.NoError(t, err)

	// Should be a JSON array.
	assert.True(t, strings.HasPrefix(result, "["))
	assert.True(t, strings.HasSuffix(result, "]"))

	// Parse and verify.
	var parsed []interface{}
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	require.Len(t, parsed, 2)
}

func TestSerializeJSONOrdered_NoTrailingNewline(t *testing.T) {
	om := NewOrderedMap()
	om.Set("key", "value")

	result, err := SerializeJSONOrdered(om)
	require.NoError(t, err)
	assert.False(t, strings.HasSuffix(result, "\n"), "should not have trailing newline")
}

func TestSerializeJSONOrdered_NoHTMLEscaping(t *testing.T) {
	om := NewOrderedMap()
	om.Set("html", "<b>bold</b>")

	result, err := SerializeJSONOrdered(om)
	require.NoError(t, err)
	assert.Contains(t, result, "<b>bold</b>", "HTML should not be escaped")
	assert.NotContains(t, result, "\\u003c", "should not have unicode escapes")
}

func TestSerializeJSONOrdered_TwoSpaceIndent(t *testing.T) {
	inner := NewOrderedMap()
	inner.Set("a", 1)

	outer := NewOrderedMap()
	outer.Set("nested", inner)

	result, err := SerializeJSONOrdered(outer)
	require.NoError(t, err)

	// Verify indentation.
	assert.Contains(t, result, "  \"nested\"", "should use 2-space indent")
	assert.Contains(t, result, "    \"a\"", "nested level should be 4 spaces")
}

func TestSerializeJSONOrdered_NilAndNullValues(t *testing.T) {
	om := NewOrderedMap()
	om.Set("null_val", nil)
	om.Set("str", "hello")

	result, err := SerializeJSONOrdered(om)
	require.NoError(t, err)
	assert.Contains(t, result, `"null_val": null`)
	assert.Contains(t, result, `"str": "hello"`)
}

func TestSerializeJSONOrdered_ScalarTypes(t *testing.T) {
	om := NewOrderedMap()
	om.Set("bool_val", true)
	om.Set("int_val", 42)
	om.Set("float_val", 3.14)
	om.Set("str_val", "hello")

	result, err := SerializeJSONOrdered(om)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, true, parsed["bool_val"])
	assert.InDelta(t, 42.0, parsed["int_val"], 0.001)
	assert.InDelta(t, 3.14, parsed["float_val"], 0.001)
	assert.Equal(t, "hello", parsed["str_val"])
}

// ---------------------------------------------------------------------------
// SerializeYAMLOrdered
// ---------------------------------------------------------------------------

func TestSerializeYAMLOrdered_OrderedMapKeys(t *testing.T) {
	om := NewOrderedMap()
	om.Set("z", 1)
	om.Set("a", 2)
	om.Set("m", 3)

	result, err := SerializeYAMLOrdered(om)
	require.NoError(t, err)

	// Keys should appear in z, a, m order in the output.
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "z: 1", lines[0])
	assert.Equal(t, "a: 2", lines[1])
	assert.Equal(t, "m: 3", lines[2])
}

func TestSerializeYAMLOrdered_NestedOrderedMap(t *testing.T) {
	inner := NewOrderedMap()
	inner.Set("inner_z", 1)
	inner.Set("inner_a", 2)

	outer := NewOrderedMap()
	outer.Set("outer_key", inner)
	outer.Set("top", true)

	result, err := SerializeYAMLOrdered(outer)
	require.NoError(t, err)

	// Verify top-level order: outer_key before top.
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	assert.True(t, strings.HasPrefix(lines[0], "outer_key:"), "outer_key should be first")
	assert.True(t, strings.HasPrefix(lines[len(lines)-1], "top:"), "top should be last")
}

func TestSerializeYAMLOrdered_TrailingNewline(t *testing.T) {
	om := NewOrderedMap()
	om.Set("key", "value")

	result, err := SerializeYAMLOrdered(om)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(result, "\n"), "should have trailing newline")
}

func TestSerializeYAMLOrdered_RegularMap(t *testing.T) {
	data := map[string]interface{}{
		"name":  "test",
		"count": 42,
	}

	result, err := SerializeYAMLOrdered(data)
	require.NoError(t, err)
	assert.Contains(t, result, "name: test")
	assert.Contains(t, result, "count: 42")
}

func TestSerializeYAMLOrdered_ArrayOfMaps(t *testing.T) {
	first := NewOrderedMap()
	first.Set("z", 1)
	first.Set("a", 2)

	data := []interface{}{first}
	result, err := SerializeYAMLOrdered(data)
	require.NoError(t, err)
	assert.Contains(t, result, "- z: 1")
	assert.Contains(t, result, "  a: 2")
}

func TestSerializeYAMLOrdered_ScalarTypes(t *testing.T) {
	om := NewOrderedMap()
	om.Set("str", "hello")
	om.Set("num", 42)
	om.Set("float_val", 3.14)
	om.Set("bool_val", true)
	om.Set("null_val", nil)

	result, err := SerializeYAMLOrdered(om)
	require.NoError(t, err)
	assert.Contains(t, result, "str: hello")
	assert.Contains(t, result, "bool_val: true")
}

func TestSerializeYAMLOrdered_NilOrderedMap(t *testing.T) {
	var om *OrderedMap
	result, err := SerializeYAMLOrdered(om)
	require.NoError(t, err)
	assert.Equal(t, "null\n", result)
}

// ---------------------------------------------------------------------------
// Round-trip tests
// ---------------------------------------------------------------------------

func TestRoundTrip_JSON(t *testing.T) {
	original := `{"z":1,"a":{"nested_z":2,"nested_a":3},"m":[4,5]}`
	om, err := ParseJSONOrdered(original)
	require.NoError(t, err)

	serialized, err := SerializeJSONOrdered(om)
	require.NoError(t, err)

	// Parse again.
	om2, err := ParseJSONOrdered(serialized)
	require.NoError(t, err)

	// Verify same order.
	assert.Equal(t, om.Keys(), om2.Keys())

	nested1, _ := om.Get("a")
	nested2, _ := om2.Get("a")
	inner1 := nested1.(*OrderedMap)
	inner2 := nested2.(*OrderedMap)
	assert.Equal(t, inner1.Keys(), inner2.Keys())
}

func TestRoundTrip_YAML(t *testing.T) {
	original := "z: 1\na:\n  nested_z: 2\n  nested_a: 3\nm:\n  - 4\n  - 5\n"
	om, err := ParseYAMLOrdered(original)
	require.NoError(t, err)

	serialized, err := SerializeYAMLOrdered(om)
	require.NoError(t, err)

	// Parse again.
	om2, err := ParseYAMLOrdered(serialized)
	require.NoError(t, err)

	// Verify same order.
	assert.Equal(t, om.Keys(), om2.Keys())

	nested1, _ := om.Get("a")
	nested2, _ := om2.Get("a")
	inner1 := nested1.(*OrderedMap)
	inner2 := nested2.(*OrderedMap)
	assert.Equal(t, inner1.Keys(), inner2.Keys())
}

func TestRoundTrip_JSON_FlatOrder(t *testing.T) {
	// Create an OrderedMap with specific key order, serialize, parse, verify.
	om := NewOrderedMap()
	om.Set("zebra", "last_alpha")
	om.Set("apple", "first_alpha")
	om.Set("mango", "middle")

	serialized, err := SerializeJSONOrdered(om)
	require.NoError(t, err)

	om2, err := ParseJSONOrdered(serialized)
	require.NoError(t, err)

	assert.Equal(t, []string{"zebra", "apple", "mango"}, om2.Keys())
}

func TestRoundTrip_YAML_FlatOrder(t *testing.T) {
	om := NewOrderedMap()
	om.Set("zebra", "last_alpha")
	om.Set("apple", "first_alpha")
	om.Set("mango", "middle")

	serialized, err := SerializeYAMLOrdered(om)
	require.NoError(t, err)

	om2, err := ParseYAMLOrdered(serialized)
	require.NoError(t, err)

	assert.Equal(t, []string{"zebra", "apple", "mango"}, om2.Keys())
}
