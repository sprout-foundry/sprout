package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ParseJSONOrdered — key ordering
// ---------------------------------------------------------------------------

func TestParseJSONOrdered_FlatObject(t *testing.T) {
	input := `{"z":1,"a":2,"m":3}`
	om, err := ParseJSONOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	keys := om.Keys()
	assert.Equal(t, []string{"z", "a", "m"}, keys, "keys should preserve source order")

	val, ok := om.Get("z")
	assert.True(t, ok)
	assert.Equal(t, float64(1), val)

	val, ok = om.Get("a")
	assert.True(t, ok)
	assert.Equal(t, float64(2), val)

	val, ok = om.Get("m")
	assert.True(t, ok)
	assert.Equal(t, float64(3), val)
}

func TestParseJSONOrdered_NestedObject(t *testing.T) {
	input := `{"outer":{"inner_b":2,"inner_a":1}}`
	om, err := ParseJSONOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	assert.Equal(t, []string{"outer"}, om.Keys())

	inner, ok := om.Get("outer")
	require.True(t, ok)
	innerOM, ok := inner.(*OrderedMap)
	require.True(t, ok, "nested object should be *OrderedMap")

	assert.Equal(t, []string{"inner_b", "inner_a"}, innerOM.Keys(), "nested keys should preserve source order")
}

func TestParseJSONOrdered_ArrayWithObjects(t *testing.T) {
	input := `{"items":[{"z":1},{"a":2}]}`
	om, err := ParseJSONOrdered(input)
	require.NoError(t, err)
	require.NotNil(t, om)

	items, ok := om.Get("items")
	require.True(t, ok)
	arr, ok := items.([]interface{})
	require.True(t, ok)
	require.Len(t, arr, 2)

	// First element: {"z":1}
	first, ok := arr[0].(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"z"}, first.Keys())

	// Second element: {"a":2}
	second, ok := arr[1].(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"a"}, second.Keys())
}

func TestParseJSONOrdered_DeeplyNested(t *testing.T) {
	input := `{"l1_c":{"l2_b":{"l3_z":true,"l3_a":false},"l2_a":42},"l1_a":"hello"}`
	om, err := ParseJSONOrdered(input)
	require.NoError(t, err)

	assert.Equal(t, []string{"l1_c", "l1_a"}, om.Keys())

	l1c, _ := om.Get("l1_c")
	l1cOM := l1c.(*OrderedMap)
	assert.Equal(t, []string{"l2_b", "l2_a"}, l1cOM.Keys())

	l2b, _ := l1cOM.Get("l2_b")
	l2bOM := l2b.(*OrderedMap)
	assert.Equal(t, []string{"l3_z", "l3_a"}, l2bOM.Keys())
}

// ---------------------------------------------------------------------------
// Round-trip: parse → ToMap → compare with standard json.Unmarshal
// ---------------------------------------------------------------------------

func TestParseJSONOrdered_RoundTrip(t *testing.T) {
	input := `{"zebra":1,"alpha":2,"mid":3,"nested":{"d":4,"a":5},"arr":[6,7]}`

	om, err := ParseJSONOrdered(input)
	require.NoError(t, err)

	// Verify key order preserved.
	assert.Equal(t, []string{"zebra", "alpha", "mid", "nested", "arr"}, om.Keys())

	// Convert to regular map and compare values with standard library.
	result := om.ToMap()

	var expected map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(input), &expected))

	assert.Equal(t, expected["zebra"], result["zebra"])
	assert.Equal(t, expected["alpha"], result["alpha"])
	assert.Equal(t, expected["mid"], result["mid"])
	assert.Equal(t, expected["arr"], result["arr"])
}

func TestParseJSONOrdered_ValueTypes(t *testing.T) {
	input := `{
		"str": "hello",
		"num": 42.5,
		"int": 7,
		"bool_true": true,
		"bool_false": false,
		"null_val": null,
		"empty_obj": {},
		"empty_arr": []
	}`

	om, err := ParseJSONOrdered(input)
	require.NoError(t, err)

	v, ok := om.Get("str")
	assert.True(t, ok)
	assert.Equal(t, "hello", v)

	v, ok = om.Get("num")
	assert.True(t, ok)
	assert.Equal(t, float64(42.5), v)

	v, ok = om.Get("int")
	assert.True(t, ok)
	assert.Equal(t, float64(7), v)

	v, ok = om.Get("bool_true")
	assert.True(t, ok)
	assert.Equal(t, true, v)

	v, ok = om.Get("bool_false")
	assert.True(t, ok)
	assert.Equal(t, false, v)

	v, ok = om.Get("null_val")
	assert.True(t, ok)
	assert.Nil(t, v)

	v, ok = om.Get("empty_obj")
	assert.True(t, ok)
	emptyOM, ok := v.(*OrderedMap)
	assert.True(t, ok)
	assert.Equal(t, 0, emptyOM.Len())

	v, ok = om.Get("empty_arr")
	assert.True(t, ok)
	arr, ok := v.([]interface{})
	assert.True(t, ok)
	assert.Len(t, arr, 0)
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestParseJSONOrdered_TopLevelArray(t *testing.T) {
	_, err := ParseJSONOrdered(`[1,2,3]`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "top-level object")
}

func TestParseJSONOrdered_TopLevelScalar(t *testing.T) {
	_, err := ParseJSONOrdered(`42`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "top-level object")
}

func TestParseJSONOrdered_TopLevelString(t *testing.T) {
	_, err := ParseJSONOrdered(`"hello"`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "top-level object")
}

func TestParseJSONOrdered_TopLevelNull(t *testing.T) {
	_, err := ParseJSONOrdered(`null`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "top-level object")
}

func TestParseJSONOrdered_InvalidJSON(t *testing.T) {
	_, err := ParseJSONOrdered(`{invalid}`)
	assert.Error(t, err)
}

func TestParseJSONOrdered_TrailingContent(t *testing.T) {
	_, err := ParseJSONOrdered(`{"a":1}{"b":2}`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trailing")
}

// ---------------------------------------------------------------------------
// OrderedMap helpers
// ---------------------------------------------------------------------------

func TestOrderedMapFromMap(t *testing.T) {
	m := map[string]interface{}{
		"zebra":  1,
		"alpha":  2,
		"middle": 3,
	}

	om := OrderedMapFromMap(m)
	// Keys should be sorted alphabetically.
	assert.Equal(t, []string{"alpha", "middle", "zebra"}, om.Keys())
	assert.Equal(t, 3, om.Len())
}

func TestOrderedMapFromMap_Nested(t *testing.T) {
	m := map[string]interface{}{
		"top": map[string]interface{}{
			"b_inner": 1,
			"a_inner": 2,
		},
	}

	om := OrderedMapFromMap(m)
	assert.Equal(t, []string{"top"}, om.Keys())

	top, ok := om.Get("top")
	require.True(t, ok)
	topOM, ok := top.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"a_inner", "b_inner"}, topOM.Keys())
}

func TestOrderedMap_SetAndGet(t *testing.T) {
	om := NewOrderedMap()

	_, ok := om.Get("missing")
	assert.False(t, ok)

	om.Set("first", 1)
	om.Set("second", 2)

	v, ok := om.Get("first")
	assert.True(t, ok)
	assert.Equal(t, 1, v)

	v, ok = om.Get("second")
	assert.True(t, ok)
	assert.Equal(t, 2, v)

	assert.Equal(t, []string{"first", "second"}, om.Keys())
}

func TestOrderedMap_SetReplacesValue(t *testing.T) {
	om := NewOrderedMap()
	om.Set("key", "old")
	om.Set("key", "new")

	v, ok := om.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "new", v)
	assert.Equal(t, []string{"key"}, om.Keys(), "replacing should not duplicate key")
}

func TestOrderedMap_Delete(t *testing.T) {
	om := NewOrderedMap()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	om.Delete("b")

	_, ok := om.Get("b")
	assert.False(t, ok)
	assert.Equal(t, []string{"a", "c"}, om.Keys())
	assert.Equal(t, 2, om.Len())
}

func TestOrderedMap_ToMap(t *testing.T) {
	om := NewOrderedMap()
	om.Set("x", 10)
	om.Set("y", "hello")
	om.Set("z", map[string]interface{}{"nested": true})

	m := om.ToMap()
	assert.Equal(t, 10, m["x"])
	assert.Equal(t, "hello", m["y"])

	nested, ok := m["z"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, nested["nested"])
}

func TestOrderedMap_InOrder(t *testing.T) {
	om := NewOrderedMap()
	om.Set("c", 3)
	om.Set("a", 1)
	om.Set("b", 2)

	pairs := om.InOrder()
	require.Len(t, pairs, 3)

	assert.Equal(t, "c", pairs[0].Key)
	assert.Equal(t, 3, pairs[0].Value)
	assert.Equal(t, "a", pairs[1].Key)
	assert.Equal(t, 1, pairs[1].Value)
	assert.Equal(t, "b", pairs[2].Key)
	assert.Equal(t, 2, pairs[2].Value)
}

func TestOrderedMap_NilReceivers(t *testing.T) {
	var om *OrderedMap

	assert.Equal(t, 0, om.Len())
	assert.Nil(t, om.Keys())
	assert.Nil(t, om.ToMap())
	assert.Nil(t, om.InOrder())

	_, ok := om.Get("anything")
	assert.False(t, ok)

	// These should not panic.
	om.Set("key", "val")
	om.Delete("key")
}

func TestOrderedMap_String(t *testing.T) {
	om := NewOrderedMap()
	om.Set("a", 1)
	s := om.String()
	assert.Contains(t, s, "OrderedMap")

	var nilOM *OrderedMap
	assert.Equal(t, "OrderedMap(nil)", nilOM.String())
}
