package main

import (
	"bytes"
	"fmt"
	"math"

	"gopkg.in/yaml.v3"
)

// unmarshalDoc parses an OpenAPI YAML document into a generic map. The result
// is suitable for merging and re-marshaling; free-form YAML comments are not
// preserved (the generated output carries its own generated-header instead).
func unmarshalDoc(data []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// mergeDocs merges the Huma-generated document (humaDoc) into the hand-written
// seed (base). The generated sections — paths and components — are unioned so
// the hand-written families and the Huma operations coexist, with the Huma
// value winning a name conflict (a migrated family is defined by its handler,
// not the seed). Every other top-level section (info, tags, openapi, servers,
// …) keeps the hand-written seed; the Huma document only adds a section the
// seed lacks.
func mergeDocs(base, humaDoc map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(humaDoc))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range humaDoc {
		baseVal, baseOK := out[k]
		switch k {
		case "paths", "components":
			if bm, ok := baseVal.(map[string]any); ok {
				if hm, ok := v.(map[string]any); ok {
					out[k] = mergeMaps(bm, hm)
					continue
				}
			}
			// One side is not a map (unexpected); the generated section wins.
			out[k] = v
		default:
			if !baseOK {
				out[k] = v
			}
		}
	}
	return out
}

// mergeMaps unions a (the seed) and b (the generated overlay) into a new map.
// When a key is present in both and both values are maps, they are merged
// recursively; otherwise b wins.
func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if existing, ok := out[k]; ok {
			if am, aIsMap := existing.(map[string]any); aIsMap {
				if bm, bIsMap := v.(map[string]any); bIsMap {
					out[k] = mergeMaps(am, bm)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// normalizeNumbers coerces every number in the tree to a stable representation
// (int64 for whole numbers, float64 otherwise) so the rendered YAML does not
// depend on the platform's int width. yaml.v3 already yields int for whole
// values on 64-bit, but the pass keeps the guarantee explicit and portable.
func normalizeNumbers(v any) any {
	switch n := v.(type) {
	case map[string]any:
		for k, val := range n {
			n[k] = normalizeNumbers(val)
		}
		return n
	case []any:
		for i, val := range n {
			n[i] = normalizeNumbers(val)
		}
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		// yaml.v3 only produces these unsigned widths for in-range whole
		// values (OpenAPI doc integers are far below math.MaxInt64), so the
		// bound always holds; the guard is defensive and avoids a flagged
		// unchecked conversion.
		if n <= math.MaxInt64 { // #nosec G115 -- guarded by the bound above
			return int64(n)
		}
		return float64(n)
	case uint32:
		return int64(n)
	case uint64:
		if n <= math.MaxInt64 { // #nosec G115 -- guarded by the bound above
			return int64(n)
		}
		return float64(n)
	case float64:
		if n == math.Trunc(n) && n >= -1e15 && n <= 1e15 {
			return int64(n)
		}
		return n
	default:
		return v
	}
}

// marshalDoc renders the merged document to YAML. Map keys are emitted in
// sorted order and the 2-space indentation matches the hand-written seed, so
// the output is deterministic and stable for a byte-compare staleness test.
func marshalDoc(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}
	return buf.Bytes(), nil
}
