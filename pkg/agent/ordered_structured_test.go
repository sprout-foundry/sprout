package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1: JSON key order preserved through serialize/deserialize
// ---------------------------------------------------------------------------

func TestOrderedStructured_JSONKeyOrderPreserved(t *testing.T) {
	// Create an OrderedMap with keys in non-alphabetical order.
	om := NewOrderedMap()
	om.Set("zebra", 1)
	om.Set("alpha", 2)
	om.Set("middle", 3)

	// Serialize via the structured content pipeline.
	output, err := serializeStructuredContent("json", om)
	require.NoError(t, err, "serializeStructuredContent should succeed for JSON")

	// Verify key order in the serialized output string.
	idxZebra := strings.Index(output, `"zebra"`)
	idxAlpha := strings.Index(output, `"alpha"`)
	idxMiddle := strings.Index(output, `"middle"`)
	require.Positive(t, idxZebra, `"zebra" should appear in output`)
	require.Positive(t, idxAlpha, `"alpha" should appear in output`)
	require.Positive(t, idxMiddle, `"middle" should appear in output`)

	assert.Less(t, idxZebra, idxAlpha, `"zebra" should appear before "alpha" in JSON output`)
	assert.Less(t, idxAlpha, idxMiddle, `"alpha" should appear before "middle" in JSON output`)

	// Parse the output back and verify the *OrderedMap preserves the same order.
	result, err := deserializeStructuredContent("json", output)
	require.NoError(t, err, "deserializeStructuredContent should succeed for JSON")

	resultOM, ok := result.(*OrderedMap)
	require.True(t, ok, "result should be *OrderedMap")
	assert.Equal(t, []string{"zebra", "alpha", "middle"}, resultOM.Keys(),
		"round-tripped keys should preserve original order")
}

// ---------------------------------------------------------------------------
// Test 2: YAML key order preserved through serialize/deserialize
// ---------------------------------------------------------------------------

func TestOrderedStructured_YAMLKeyOrderPreserved(t *testing.T) {
	om := NewOrderedMap()
	om.Set("zebra", 1)
	om.Set("alpha", 2)
	om.Set("middle", 3)

	output, err := serializeStructuredContent("yaml", om)
	require.NoError(t, err, "serializeStructuredContent should succeed for YAML")

	// Verify key order in YAML output — keys should appear as top-level mapping keys.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	// Find line indices for each key.
	var idxZebra, idxAlpha, idxMiddle int = -1, -1, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "zebra:") {
			idxZebra = i
		}
		if strings.HasPrefix(trimmed, "alpha:") {
			idxAlpha = i
		}
		if strings.HasPrefix(trimmed, "middle:") {
			idxMiddle = i
		}
	}

	require.GreaterOrEqual(t, idxZebra, 0, "zebra key should appear in YAML output")
	require.GreaterOrEqual(t, idxAlpha, 0, "alpha key should appear in YAML output")
	require.GreaterOrEqual(t, idxMiddle, 0, "middle key should appear in YAML output")

	assert.Less(t, idxZebra, idxAlpha, "zebra should appear before alpha in YAML")
	assert.Less(t, idxAlpha, idxMiddle, "alpha should appear before middle in YAML")

	// Round-trip: deserialize and verify.
	result, err := deserializeStructuredContent("yaml", output)
	require.NoError(t, err, "deserializeStructuredContent should succeed for YAML")

	resultOM, ok := result.(*OrderedMap)
	require.True(t, ok, "result should be *OrderedMap")
	assert.Equal(t, []string{"zebra", "alpha", "middle"}, resultOM.Keys(),
		"round-tripped YAML keys should preserve original order")
}

// ---------------------------------------------------------------------------
// Test 3: Patch replace preserves existing key order (JSON)
// ---------------------------------------------------------------------------

func TestOrderedStructured_PatchReplacePreservesOrder(t *testing.T) {
	input := `{"zebra": 1, "alpha": 2, "middle": 3}`

	doc, err := deserializeStructuredContent("json", input)
	require.NoError(t, err)

	docOM, ok := doc.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"zebra", "alpha", "middle"}, docOM.Keys())

	// Apply replace patch on /alpha.
	patched, err := applyPatchOperation(doc, jsonPatchOperation{
		Op:    "replace",
		Path:  "/alpha",
		Value: float64(99),
	})
	require.NoError(t, err, "replace patch should succeed")

	// Serialize back and verify order is preserved.
	output, err := serializeStructuredContent("json", patched)
	require.NoError(t, err)

	idxZebra := strings.Index(output, `"zebra"`)
	idxAlpha := strings.Index(output, `"alpha"`)
	idxMiddle := strings.Index(output, `"middle"`)
	assert.Less(t, idxZebra, idxAlpha, "zebra should still appear before alpha after patch")
	assert.Less(t, idxAlpha, idxMiddle, "alpha should still appear before middle after patch")

	// Verify alpha's value is 99.
	patchedOM, ok := patched.(*OrderedMap)
	require.True(t, ok)
	val, ok := patchedOM.Get("alpha")
	require.True(t, ok)
	assert.Equal(t, float64(99), val, "alpha should now be 99")
}

// ---------------------------------------------------------------------------
// Test 4: Patch add appends new key at end
// ---------------------------------------------------------------------------

func TestOrderedStructured_PatchAddAppendsKey(t *testing.T) {
	input := `{"first": 1, "last": 2}`

	doc, err := deserializeStructuredContent("json", input)
	require.NoError(t, err)

	docOM, ok := doc.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"first", "last"}, docOM.Keys())

	// Add a new key.
	patched, err := applyPatchOperation(doc, jsonPatchOperation{
		Op:    "add",
		Path:  "/middle",
		Value: float64(3),
	})
	require.NoError(t, err, "add patch should succeed")

	patchedOM, ok := patched.(*OrderedMap)
	require.True(t, ok)

	// New key should be appended at the end.
	assert.Equal(t, []string{"first", "last", "middle"}, patchedOM.Keys(),
		"added key should appear at end, original keys stay in place")

	// Verify via serialized output.
	output, err := serializeStructuredContent("json", patched)
	require.NoError(t, err)

	idxFirst := strings.Index(output, `"first"`)
	idxLast := strings.Index(output, `"last"`)
	idxMiddle := strings.Index(output, `"middle"`)
	assert.Less(t, idxFirst, idxLast, "first should appear before last")
	assert.Less(t, idxLast, idxMiddle, "last should appear before middle (appended)")
}

// ---------------------------------------------------------------------------
// Test 5: Patch remove preserves remaining order
// ---------------------------------------------------------------------------

func TestOrderedStructured_PatchRemovePreservesOrder(t *testing.T) {
	input := `{"zebra": 1, "alpha": 2, "middle": 3}`

	doc, err := deserializeStructuredContent("json", input)
	require.NoError(t, err)

	// Remove the middle key (alpha).
	patched, err := applyPatchOperation(doc, jsonPatchOperation{
		Op:   "remove",
		Path: "/alpha",
	})
	require.NoError(t, err, "remove patch should succeed")

	patchedOM, ok := patched.(*OrderedMap)
	require.True(t, ok)

	// Remaining keys should be zebra, middle — order preserved.
	assert.Equal(t, []string{"zebra", "middle"}, patchedOM.Keys(),
		"remaining keys should stay in original order")

	_, exists := patchedOM.Get("alpha")
	assert.False(t, exists, "alpha should be gone")

	// Verify serialized output order.
	output, err := serializeStructuredContent("json", patched)
	require.NoError(t, err)

	idxZebra := strings.Index(output, `"zebra"`)
	idxMiddle := strings.Index(output, `"middle"`)
	assert.NotContains(t, output, `"alpha"`, "alpha should not appear in output")
	assert.Less(t, idxZebra, idxMiddle, "zebra should appear before middle after removal")
}

// ---------------------------------------------------------------------------
// Test 6: Nested object key order preserved
// ---------------------------------------------------------------------------

func TestOrderedStructured_NestedKeyOrderPreserved(t *testing.T) {
	input := `{"services": {"web": 1, "db": 2, "cache": 3}, "version": "1.0"}`

	doc, err := deserializeStructuredContent("json", input)
	require.NoError(t, err)

	docOM, ok := doc.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"services", "version"}, docOM.Keys())

	servicesVal, ok := docOM.Get("services")
	require.True(t, ok)
	servicesOM, ok := servicesVal.(*OrderedMap)
	require.True(t, ok, "nested object should be *OrderedMap")
	assert.Equal(t, []string{"web", "db", "cache"}, servicesOM.Keys(),
		"nested keys should preserve source order")

	// Round-trip: serialize and deserialize, verify nested order survives.
	output, err := serializeStructuredContent("json", doc)
	require.NoError(t, err)

	// Verify serialized output has services.web before services.db before services.cache.
	idxWeb := strings.Index(output, `"web"`)
	idxDb := strings.Index(output, `"db"`)
	idxCache := strings.Index(output, `"cache"`)
	assert.Less(t, idxWeb, idxDb, "web should appear before db in serialized output")
	assert.Less(t, idxDb, idxCache, "db should appear before cache in serialized output")

	// Parse again and verify nested order is still correct.
	roundTripped, err := deserializeStructuredContent("json", output)
	require.NoError(t, err)
	rtOM := roundTripped.(*OrderedMap)
	rtServices, ok := rtOM.Get("services")
	require.True(t, ok)
	rtServicesOM := rtServices.(*OrderedMap)
	assert.Equal(t, []string{"web", "db", "cache"}, rtServicesOM.Keys())
}

// ---------------------------------------------------------------------------
// Test 7: YAML round-trip with patch
// ---------------------------------------------------------------------------

func TestOrderedStructured_YAMLRoundTripWithPatch(t *testing.T) {
	input := `services:
  web: 8080
  database: 5432
  cache: 6379
version: "2.0"
`

	doc, err := deserializeStructuredContent("yaml", input)
	require.NoError(t, err)

	docOM, ok := doc.(*OrderedMap)
	require.True(t, ok)

	// Top-level keys: services before version (matching YAML source order).
	assert.Equal(t, []string{"services", "version"}, docOM.Keys(),
		"top-level YAML keys should preserve source order")

	// Nested services keys: web, database, cache.
	servicesVal, ok := docOM.Get("services")
	require.True(t, ok)
	servicesOM, ok := servicesVal.(*OrderedMap)
	require.True(t, ok)
	assert.Equal(t, []string{"web", "database", "cache"}, servicesOM.Keys(),
		"nested YAML keys should preserve source order")

	// Apply replace patch on /version.
	patched, err := applyPatchOperation(doc, jsonPatchOperation{
		Op:    "replace",
		Path:  "/version",
		Value: "3.0",
	})
	require.NoError(t, err)

	// Serialize back to YAML.
	output, err := serializeStructuredContent("yaml", patched)
	require.NoError(t, err)

	// Verify top-level key order in YAML output: services before version.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	var idxServices, idxVersion int = -1, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "services:") {
			idxServices = i
		}
		// Version key is at top level (no leading whitespace)
		if trimmed == "version:" || strings.HasPrefix(trimmed, "version:") {
			// Make sure it's not nested
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				idxVersion = i
			}
		}
	}
	require.GreaterOrEqual(t, idxServices, 0, "services key should appear in YAML output")
	require.GreaterOrEqual(t, idxVersion, 0, "version key should appear in YAML output")
	assert.Less(t, idxServices, idxVersion, "services should appear before version in YAML output")

	// Verify within services, keys are still web, database, cache.
	patchedOM := patched.(*OrderedMap)
	patchedServices, ok := patchedOM.Get("services")
	require.True(t, ok)
	patchedServicesOM := patchedServices.(*OrderedMap)
	assert.Equal(t, []string{"web", "database", "cache"}, patchedServicesOM.Keys(),
		"services key order should be preserved after patch")

	// Verify version value updated.
	versionVal, ok := patchedOM.Get("version")
	require.True(t, ok)
	assert.Equal(t, "3.0", versionVal, "version should be updated to 3.0")
}

// ---------------------------------------------------------------------------
// Test 8: Schema validation works with OrderedMap
// ---------------------------------------------------------------------------

func TestOrderedStructured_SchemaValidationWithOrderedMap(t *testing.T) {
	schema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"name"},
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"age":  map[string]interface{}{"type": "number"},
		},
	}

	// Valid OrderedMap.
	om := NewOrderedMap()
	om.Set("name", "Alice")
	om.Set("age", float64(30))

	errs := validateDataAgainstSchema(om, schema, "$")
	assert.Empty(t, errs, "OrderedMap with valid data should pass schema validation")

	// Missing required field — remove "name".
	om2 := NewOrderedMap()
	om2.Set("age", float64(25))

	errs = validateDataAgainstSchema(om2, schema, "$")
	assert.NotEmpty(t, errs, "OrderedMap missing required field should fail validation")

	// Verify the error mentions the missing required field.
	found := false
	for _, e := range errs {
		if strings.Contains(e, "name") && strings.Contains(e, "required") {
			found = true
			break
		}
	}
	assert.True(t, found, "validation error should mention missing required field 'name': %v", errs)
}

// ---------------------------------------------------------------------------
// Test 9: OrderedMapFromMap produces alphabetical keys
// ---------------------------------------------------------------------------

func TestOrderedStructured_OrderedMapFromMapAlphabetical(t *testing.T) {
	// Simulate what handleWriteStructuredFile receives: a plain map.
	m := map[string]interface{}{
		"z": 1,
		"a": 2,
		"m": 3,
	}

	om := OrderedMapFromMap(m)

	// Keys should be sorted alphabetically.
	assert.Equal(t, []string{"a", "m", "z"}, om.Keys(),
		"OrderedMapFromMap should sort keys alphabetically")

	// Serialize to JSON and verify it's valid.
	output, err := serializeStructuredContent("json", om)
	require.NoError(t, err)

	// Parse back and verify content.
	result, err := deserializeStructuredContent("json", output)
	require.NoError(t, err)
	resultOM := result.(*OrderedMap)

	// After round-trip, keys maintain the order they had in the input to
	// ParseJSONOrdered (which is the serialized order: alphabetical).
	assert.Equal(t, []string{"a", "m", "z"}, resultOM.Keys())

	// Values preserved.
	val, ok := resultOM.Get("z")
	require.True(t, ok)
	assert.Equal(t, float64(1), val)

	val, ok = resultOM.Get("a")
	require.True(t, ok)
	assert.Equal(t, float64(2), val)

	val, ok = resultOM.Get("m")
	require.True(t, ok)
	assert.Equal(t, float64(3), val)
}

// ---------------------------------------------------------------------------
// Test 10: Patch "test" operation works with nested OrderedMap objects
// ---------------------------------------------------------------------------

func TestOrderedStructured_PatchTestWithNestedObjects(t *testing.T) {
	// Input with nested objects — deserialization produces *OrderedMap.
	input := `{"config": {"host": "localhost", "port": 8080}, "name": "app"}`

	doc, err := deserializeStructuredContent("json", input)
	require.NoError(t, err)

	// The "test" op's value comes from JSON tool args as map[string]interface{}.
	// Without the fix, reflect.DeepEqual fails because actual is *OrderedMap
	// but op.Value is map[string]interface{}.
	_, err = applyPatchOperation(doc, jsonPatchOperation{
		Op:   "test",
		Path: "/config",
		Value: map[string]interface{}{
			"host": "localhost",
			"port": float64(8080),
		},
	})
	require.NoError(t, err, "test patch should succeed when comparing *OrderedMap with map[string]interface{}")

	// Verify it fails for wrong values.
	_, err = applyPatchOperation(doc, jsonPatchOperation{
		Op:   "test",
		Path: "/config",
		Value: map[string]interface{}{
			"host": "wrong",
			"port": float64(8080),
		},
	})
	assert.Error(t, err, "test patch should fail for mismatched values")
	assert.Contains(t, err.Error(), "patch test failed")
}

// ---------------------------------------------------------------------------
// Test 11: Enum validation works with OrderedMap objects
// ---------------------------------------------------------------------------

func TestOrderedStructured_EnumValidationWithOrderedMap(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"color": map[string]interface{}{
				"type": "string",
				"enum": []interface{}{"red", "green", "blue"},
			},
		},
	}

	// Valid enum value in an OrderedMap.
	om := NewOrderedMap()
	om.Set("color", "red")
	errs := validateDataAgainstSchema(om, schema, "$")
	assert.Empty(t, errs, "OrderedMap with valid enum value should pass")

	// Invalid enum value.
	om2 := NewOrderedMap()
	om2.Set("color", "purple")
	errs = validateDataAgainstSchema(om2, schema, "$")
	assert.NotEmpty(t, errs, "OrderedMap with invalid enum value should fail")
	found := false
	for _, e := range errs {
		if strings.Contains(e, "not in enum") {
			found = true
			break
		}
	}
	assert.True(t, found, "validation error should mention enum: %v", errs)
}
