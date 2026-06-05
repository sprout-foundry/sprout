package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ParseYAMLOrdered — key ordering
// ---------------------------------------------------------------------------

func TestParseYAMLOrdered_SimpleKeyOrder(t *testing.T) {
	input := "z: 1\na: 2\nm: 3\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	keys := om.Keys()
	assert.Equal(t, []string{"z", "a", "m"}, keys, "keys should preserve source order")
}

func TestParseYAMLOrdered_NestedMapping(t *testing.T) {
	input := "outer:\n  z: 1\n  a: 2\n  m: 3\nother: true\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	// Top-level keys.
	assert.Equal(t, []string{"outer", "other"}, om.Keys())

	// Nested mapping should also be an *OrderedMap with preserved order.
	outerVal, ok := om.Get("outer")
	require.True(t, ok)
	outerMap, ok := outerVal.(*OrderedMap)
	require.True(t, ok, "nested value should be *OrderedMap")
	assert.Equal(t, []string{"z", "a", "m"}, outerMap.Keys())
}

func TestParseYAMLOrdered_NestedDeep(t *testing.T) {
	input := "a:\n  b:\n    z: 1\n    a: 2\nx: end\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	assert.Equal(t, []string{"a", "x"}, om.Keys())

	aVal, _ := om.Get("a")
	aMap := aVal.(*OrderedMap)
	assert.Equal(t, []string{"b"}, aMap.Keys())

	bVal, _ := aMap.Get("b")
	bMap := bVal.(*OrderedMap)
	assert.Equal(t, []string{"z", "a"}, bMap.Keys())
}

func TestParseYAMLOrdered_ArrayOfObjects(t *testing.T) {
	input := "items:\n  - z: 1\n    a: 2\n  - m: 3\n    b: 4\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	itemsVal, ok := om.Get("items")
	require.True(t, ok)
	items, ok := itemsVal.([]interface{})
	require.True(t, ok)
	require.Len(t, items, 2)

	// First array element.
	first := items[0].(*OrderedMap)
	assert.Equal(t, []string{"z", "a"}, first.Keys())

	// Second array element.
	second := items[1].(*OrderedMap)
	assert.Equal(t, []string{"m", "b"}, second.Keys())
}

func TestParseYAMLOrdered_ScalarTypes(t *testing.T) {
	input := "str: hello\nnum: 42\nfloat_val: 3.14\nbool_val: true\nnull_val: null\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	strVal, _ := om.Get("str")
	assert.Equal(t, "hello", strVal)

	numVal, _ := om.Get("num")
	assert.Equal(t, 42, numVal)

	floatVal, _ := om.Get("float_val")
	assert.InDelta(t, 3.14, floatVal, 0.001)

	boolVal, _ := om.Get("bool_val")
	assert.Equal(t, true, boolVal)

	nullVal, _ := om.Get("null_val")
	assert.Nil(t, nullVal)
}

func TestParseYAMLOrdered_AnchorAlias(t *testing.T) {
	// Basic anchor/alias — just verify it doesn't crash and the alias
	// resolves to the correct value.
	input := "defaults: &defaults\n  color: blue\n  size: large\nitem:\n  <<: *defaults\n  color: red\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	// The "defaults" key should have its mapping.
	defaultsVal, ok := om.Get("defaults")
	require.True(t, ok)
	defaultsMap, ok := defaultsVal.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"color", "size"}, defaultsMap.Keys())
}

func TestParseYAMLOrdered_ArrayWithScalars(t *testing.T) {
	input := "list:\n  - one\n  - two\n  - three\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)

	listVal, _ := om.Get("list")
	list := listVal.([]interface{})
	assert.Equal(t, []interface{}{"one", "two", "three"}, list)
}

func TestParseYAMLOrdered_EmptyMapping(t *testing.T) {
	input := "empty: {}\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)

	emptyVal, _ := om.Get("empty")
	emptyMap := emptyVal.(*OrderedMap)
	assert.Equal(t, 0, emptyMap.Len())
}

func TestParseYAMLOrdered_NonMappingTopLevel(t *testing.T) {
	// A scalar top-level should fail.
	_, err := ParseYAMLOrdered("just a string\n")
	assert.Error(t, err)
}

func TestParseYAMLOrdered_InvalidYAML(t *testing.T) {
	_, err := ParseYAMLOrdered(":\n  invalid: [unclosed\n")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// NormalizeYAMLOrdered
// ---------------------------------------------------------------------------

func TestNormalizeYAMLOrdered_InterfaceMapKeys(t *testing.T) {
	// map[interface{}]interface{} → *OrderedMap (sorted keys)
	input := map[interface{}]interface{}{
		"b": 2,
		"a": 1,
	}
	result := NormalizeYAMLOrdered(input)
	om, ok := result.(*OrderedMap)
	require.True(t, ok)
	// Keys should be sorted alphabetically.
	assert.Equal(t, []string{"a", "b"}, om.Keys())
}

func TestNormalizeYAMLOrdered_StringMapKeys(t *testing.T) {
	input := map[string]interface{}{
		"z": 1,
		"a": 2,
	}
	result := NormalizeYAMLOrdered(input)
	om, ok := result.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"a", "z"}, om.Keys())
}

func TestNormalizeYAMLOrdered_Slice(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"b": 1, "a": 2},
		"scalar",
	}
	result := NormalizeYAMLOrdered(input)
	slice := result.([]interface{})
	require.Len(t, slice, 2)

	om := slice[0].(*OrderedMap)
	assert.Equal(t, []string{"a", "b"}, om.Keys())
	assert.Equal(t, "scalar", slice[1])
}

func TestNormalizeYAMLOrdered_NestedInterfaceMap(t *testing.T) {
	input := map[interface{}]interface{}{
		"outer": map[interface{}]interface{}{
			"inner_b": 2,
			"inner_a": 1,
		},
	}
	result := NormalizeYAMLOrdered(input)
	om := result.(*OrderedMap)
	outerVal, _ := om.Get("outer")
	innerMap := outerVal.(*OrderedMap)
	assert.Equal(t, []string{"inner_a", "inner_b"}, innerMap.Keys())
}

func TestNormalizeYAMLOrdered_PassThrough(t *testing.T) {
	assert.Equal(t, "hello", NormalizeYAMLOrdered("hello"))
	assert.Equal(t, 42, NormalizeYAMLOrdered(42))
	assert.Nil(t, NormalizeYAMLOrdered(nil))
	assert.Equal(t, true, NormalizeYAMLOrdered(true))
}

// ---------------------------------------------------------------------------
// YAML merge keys (<<)
// ---------------------------------------------------------------------------

func TestParseYAMLOrdered_MergeKeySingleAlias(t *testing.T) {
	input := "defaults: &defaults\n  color: blue\n  size: large\nitem:\n  <<: *defaults\n  color: red\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	// "item" should have merged keys from defaults, with its own "color" override.
	itemVal, ok := om.Get("item")
	require.True(t, ok)
	itemMap, ok := itemVal.(*OrderedMap)
	require.True(t, ok, "item should be *OrderedMap")

	// "color" overridden to "red".
	colorVal, _ := itemMap.Get("color")
	assert.Equal(t, "red", colorVal, "local color should override merged color")

	// "size" inherited from defaults.
	sizeVal, _ := itemMap.Get("size")
	assert.Equal(t, "large", sizeVal, "size should be inherited from merge")

	// Keys: merged base keys first, then local override preserves position.
	// The merge inserts "color" and "size" from defaults. Then the local
	// "color: red" overrides the merged value but keeps the key position
	// from the merge pass (since the key already exists, Set replaces the
	// value in-place).
	keys := itemMap.Keys()
	assert.Contains(t, keys, "color")
	assert.Contains(t, keys, "size")
	assert.Equal(t, 2, len(keys), "item should have exactly 2 keys")
}

func TestParseYAMLOrdered_MergeKeyMultipleAliases(t *testing.T) {
	input := "a: &a\n  x: 1\n  y: 2\nb: &b\n  y: 20\n  z: 30\nresult:\n  <<: [*a, *b]\n  z: 300\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	resultVal, ok := om.Get("result")
	require.True(t, ok)
	resultMap, ok := resultVal.(*OrderedMap)
	require.True(t, ok)

	// x from a, y from b (last merge wins), z from local override.
	xVal, _ := resultMap.Get("x")
	assert.Equal(t, 1, xVal, "x should come from merge of a")

	yVal, _ := resultMap.Get("y")
	assert.Equal(t, 20, yVal, "y should come from merge of b (last wins in sequence)")

	zVal, _ := resultMap.Get("z")
	assert.Equal(t, 300, zVal, "z should be overridden by local value")
}

func TestParseYAMLOrdered_MergeKeyInlineMapping(t *testing.T) {
	// Merge key pointing to an inline mapping (not an alias).
	input := "item:\n  <<: &base\n    name: widget\n    type: product\n  name: special_widget\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	itemVal, ok := om.Get("item")
	require.True(t, ok)
	itemMap, ok := itemVal.(*OrderedMap)
	require.True(t, ok)

	// "name" should be overridden by local value.
	nameVal, _ := itemMap.Get("name")
	assert.Equal(t, "special_widget", nameVal, "local name should override merged name")

	// "type" should be inherited.
	typeVal, _ := itemMap.Get("type")
	assert.Equal(t, "product", typeVal, "type should be inherited from merge")
}

func TestParseYAMLOrdered_MergeKeyNoOverride(t *testing.T) {
	// When there's no override, all merged values should be present.
	input := "base: &base\n  alpha: 1\n  beta: 2\nderived:\n  <<: *base\n  gamma: 3\n"
	om, err := ParseYAMLOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	derivedVal, ok := om.Get("derived")
	require.True(t, ok)
	derivedMap, ok := derivedVal.(*OrderedMap)
	require.True(t, ok)

	assert.Equal(t, 3, derivedMap.Len(), "derived should have 3 keys (alpha, beta, gamma)")

	alphaVal, _ := derivedMap.Get("alpha")
	assert.Equal(t, 1, alphaVal)

	betaVal, _ := derivedMap.Get("beta")
	assert.Equal(t, 2, betaVal)

	gammaVal, _ := derivedMap.Get("gamma")
	assert.Equal(t, 3, gammaVal)
}
