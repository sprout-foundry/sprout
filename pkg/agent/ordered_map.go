package agent

import (
	"fmt"
	"sort"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// OrderedMap wraps orderedmap.OrderedMap[string, interface{}] to preserve key
// insertion order throughout the structured file pipeline. All nested
// map[string]interface{} values are recursively converted so that ordering
// is maintained at every depth.
type OrderedMap struct {
	inner *orderedmap.OrderedMap[string, interface{}]
}

// NewOrderedMap creates an empty OrderedMap.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{inner: orderedmap.New[string, interface{}]()}
}

// OrderedMapFromMap converts a regular map[string]interface{} into an
// OrderedMap. Keys are sorted alphabetically to provide a deterministic
// (though not original-source) order. Nested maps and slices are converted
// recursively. This is intended as a fallback when source order is
// unavailable.
func OrderedMapFromMap(m map[string]interface{}) *OrderedMap {
	om := NewOrderedMap()

	// Sort keys for deterministic ordering.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		om.inner.Set(k, convertToOrderedValue(m[k]))
	}
	return om
}

// Get retrieves the value for the given key. The second return value indicates
// whether the key was present.
func (om *OrderedMap) Get(key string) (interface{}, bool) {
	if om == nil || om.inner == nil {
		return nil, false
	}
	return om.inner.Get(key)
}

// Set stores the key-value pair. If the key already exists its value is
// replaced but the original insertion position is preserved (matching the
// underlying library semantics).
func (om *OrderedMap) Set(key string, value interface{}) {
	if om == nil || om.inner == nil {
		return
	}
	om.inner.Set(key, value)
}

// Delete removes the key from the map.
func (om *OrderedMap) Delete(key string) {
	if om == nil || om.inner == nil {
		return
	}
	om.inner.Delete(key)
}

// Keys returns all keys in insertion order.
func (om *OrderedMap) Keys() []string {
	if om == nil || om.inner == nil {
		return nil
	}
	keys := make([]string, 0, om.inner.Len())
	for pair := om.inner.Oldest(); pair != nil; pair = pair.Next() {
		keys = append(keys, pair.Key)
	}
	return keys
}

// ToMap converts the OrderedMap to a standard map[string]interface{}.
// Nested OrderedMap values are recursively converted back. This is useful for
// compatibility with existing code that expects regular maps.
func (om *OrderedMap) ToMap() map[string]interface{} {
	if om == nil || om.inner == nil {
		return nil
	}
	m := make(map[string]interface{}, om.inner.Len())
	for pair := om.inner.Oldest(); pair != nil; pair = pair.Next() {
		m[pair.Key] = convertFromOrderedValue(pair.Value)
	}
	return m
}

// Len returns the number of key-value pairs.
func (om *OrderedMap) Len() int {
	if om == nil || om.inner == nil {
		return 0
	}
	return om.inner.Len()
}

// InOrder returns all pairs in insertion order.
func (om *OrderedMap) InOrder() []orderedmap.Pair[string, interface{}] {
	if om == nil || om.inner == nil {
		return nil
	}
	pairs := make([]orderedmap.Pair[string, interface{}], 0, om.inner.Len())
	for pair := om.inner.Oldest(); pair != nil; pair = pair.Next() {
		pairs = append(pairs, *pair)
	}
	return pairs
}

// convertToOrderedValue recursively wraps nested maps in OrderedMap. Slices
// are walked element-by-element so that any contained maps are also converted.
func convertToOrderedValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return OrderedMapFromMap(typed)
	case *OrderedMap:
		return typed
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, elem := range typed {
			out[i] = convertToOrderedValue(elem)
		}
		return out
	default:
		return v
	}
}

// convertFromOrderedValue reverses the conversion: OrderedMap values become
// map[string]interface{}; slices are walked recursively.
func convertFromOrderedValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case *OrderedMap:
		return typed.ToMap()
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, elem := range typed {
			out[i] = convertFromOrderedValue(elem)
		}
		return out
	default:
		return v
	}
}

// String returns a human-readable representation useful for debugging.
func (om *OrderedMap) String() string {
	if om == nil || om.inner == nil {
		return "OrderedMap(nil)"
	}
	return fmt.Sprintf("OrderedMap%v", om.ToMap())
}
