package design

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DTCG document editing for the apply half (SP-140-5 §5b, TODO item 5.4).
//
// The apply half rewrites a DTCG token source file (design/tokens/*.tokens.json)
// to revalue an existing entry or add a renamed/new one. The edit is *surgical*:
// it rewrites the file structurally (decode → mutate → re-encode with a stable
// two-space indent and sorted keys) rather than text-substituting, so it works
// for any formatting and cannot corrupt JSON.
//
// Re-encoding deliberately normalises formatting (indent + key order). The
// token file is a source of truth whose values matter, not its whitespace, and
// a normalising write is what makes the apply idempotent and deterministic: two
// applies of the same delta produce byte-identical files, and a second apply
// after the first finds the entry already correct and is a no-op.

// rewriteDTCEntry replaces the $value of an existing dotted DTCG entry in a
// token document, preserving everything else. It returns (newBytes, true, nil)
// when the entry exists and was rewritten, and (_, false, nil) when the entry
// is not present (the caller then adds it). A malformed document is an error.
func rewriteDTCEntry(content []byte, tokenPath string, d SyncDelta) ([]byte, bool, error) {
	doc, err := decodeDTCDocument(content)
	if err != nil {
		return nil, false, err
	}
	segments := strings.Split(tokenPath, ".")
	node := doc
	for i, seg := range segments {
		child, ok := node[seg]
		if !ok {
			return nil, false, nil // entry absent: caller adds it
		}
		if i == len(segments)-1 {
			obj, ok := child.(map[string]any)
			if !ok {
				// The path names a group, not a leaf: no entry to revalue.
				return nil, false, nil
			}
			obj["$value"] = dtcgValueFor(d)
			out, err := encodeDTCDocument(doc)
			if err != nil {
				return nil, false, err
			}
			return out, true, nil
		}
		next, ok := child.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		node = next
	}
	return nil, false, nil
}

// addDTCEntry adds a new dotted DTCG entry (with $type/$value) to a token
// document, creating intermediate groups as needed. It refuses to overwrite an
// existing entry (the caller revalues those instead), so a genuine add is never
// a silent destructive rename.
func addDTCEntry(content []byte, tokenPath string, d SyncDelta) ([]byte, error) {
	doc, err := decodeDTCDocument(content)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(tokenPath, ".")
	node := doc
	for i, seg := range segments {
		if i == len(segments)-1 {
			if _, exists := node[seg]; exists {
				return nil, fmt.Errorf("entry %q already exists", tokenPath)
			}
			node[seg] = map[string]any{
				"$type":  dtcgTypeFor(d),
				"$value": dtcgValueFor(d),
			}
			break
		}
		child, ok := node[seg]
		if !ok {
			next := map[string]any{}
			node[seg] = next
			node = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("token path %q crosses non-object segment %q", tokenPath, seg)
		}
		node = next
	}
	return encodeDTCDocument(doc)
}

// createTokenDocument renders a new token document containing exactly one
// entry.
func createTokenDocument(tokenPath string, d SyncDelta) []byte {
	doc := map[string]any{}
	node := doc
	segments := strings.Split(tokenPath, ".")
	for i, seg := range segments {
		if i == len(segments)-1 {
			node[seg] = map[string]any{
				"$type":  dtcgTypeFor(d),
				"$value": dtcgValueFor(d),
			}
			break
		}
		next := map[string]any{}
		node[seg] = next
		node = next
	}
	out, _ := encodeDTCDocument(doc)
	return out
}

// decodeDTCDocument decodes a token document preserving numeric fidelity via
// json.Number (so re-encoding does not turn an integer into a float).
func decodeDTCDocument(content []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parsing token document: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// encodeDTCDocument renders a token document with a stable two-space indent and
// sorted keys, so re-encoding is deterministic and byte-identical across runs.
func encodeDTCDocument(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSONValue(&buf, doc, 0); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// writeJSONValue renders a decoded JSON value with a stable two-space indent
// and sorted object keys. Numbers are emitted verbatim (json.Number), strings
// are JSON-escaped, and maps/slices recurse.
func writeJSONValue(buf *bytes.Buffer, v any, depth int) error {
	indent := strings.Repeat("  ", depth)
	childIndent := strings.Repeat("  ", depth+1)
	switch val := v.(type) {
	case map[string]any:
		if len(val) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			buf.WriteString(childIndent)
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(keyBytes)
			buf.WriteString(": ")
			if err := writeJSONValue(buf, val[k], depth+1); err != nil {
				return err
			}
			if i < len(keys)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent)
		buf.WriteByte('}')
	case []any:
		if len(val) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, item := range val {
			buf.WriteString(childIndent)
			if err := writeJSONValue(buf, item, depth+1); err != nil {
				return err
			}
			if i < len(val)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent)
		buf.WriteByte(']')
	case json.Number:
		buf.WriteString(val.String())
	case string:
		b, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf.Write(b)
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case nil:
		buf.WriteString("null")
	default:
		// Fallback for any value the decoder produced that is not one of the
		// above (should not happen with encoding/json).
		b, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

// dtcgTypeFor names the DTCG $type a new/updated entry carries. A token delta
// that the analyzer could not type is treated as a color when its value looks
// like a colour and a dimension otherwise — the two families the v1 vocabulary
// detects (SP-140-5 §5b literal/inferred passes).
func dtcgTypeFor(d SyncDelta) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.Token)), "color.") {
		return "color"
	}
	value := dtcgValueString(d)
	if strings.HasPrefix(value, "#") {
		return "color"
	}
	if strings.HasSuffix(value, "px") || strings.HasSuffix(value, "rem") || strings.HasSuffix(value, "em") {
		return "dimension"
	}
	return "color"
}

// hasLiteralTokenValue reports whether a token delta carries a concrete value
// the apply half can mechanically write: a value parsed from the delta's
// evidence (a declaration/revalue), or a proposed value. A bare token reference
// with neither is a proposal, not a write.
func hasLiteralTokenValue(d SyncDelta) bool {
	if _, ok := literalValueFromEvidence(d.Evidence); ok {
		return true
	}
	return strings.TrimSpace(d.Proposed) != ""
}

// dtcgValueFor returns the JSON value a rewritten/added entry carries: the
// literal value the code used (parsed from the delta's evidence), or, when the
// evidence carries no extractable value, the delta's proposed value.
func dtcgValueFor(d SyncDelta) any {
	if v, ok := literalValueFromEvidence(d.Evidence); ok {
		return v
	}
	if d.Proposed != "" {
		return d.Proposed
	}
	return dtcgValueString(d)
}

// dtcgValueString renders the best-effort literal value a delta carries, for
// type inference and as a last-resort value.
func dtcgValueString(d SyncDelta) string {
	if v, ok := literalValueFromEvidence(d.Evidence); ok {
		if s, isStr := v.(string); isStr {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return d.Proposed
}

// literalValueFromEvidence extracts the literal value from a delta's evidence
// string. The literal pass writes evidence as:
//
//	--color-brand-primary: #ff0000; (line 3)   (a var declaration/revalue)
//	{color.brand.primary} (line 7)             (a token reference)
//
// A declaration's value after the colon is the new literal; a reference has no
// value of its own, so it yields nothing (the delta then adds/keeps the entry
// unchanged). The trailing "(line N)" suffix is stripped first.
func literalValueFromEvidence(evidence string) (any, bool) {
	text := strings.TrimSpace(evidence)
	if text == "" {
		return nil, false
	}
	// Strip a trailing "(line N)".
	if idx := strings.LastIndex(text, "(line "); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	colon := strings.Index(text, ":")
	if colon < 0 {
		return nil, false
	}
	value := strings.TrimSpace(text[colon+1:])
	value = strings.TrimSuffix(value, ";")
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	return coerceDTCGValue(value), true
}

// coerceDTCGValue renders a literal text value as the JSON scalar a DTCG entry
// carries: a bare number becomes a number, a quoted/other string stays a
// string. Colours, dimensions, and font names all stay strings.
func coerceDTCGValue(value string) any {
	// A pure integer/decimal literal (a DTCG number, e.g. a font weight) is
	// stored as a number; anything with a unit or a leading '#' stays a string.
	if n, ok := parseBareNumber(value); ok {
		return n
	}
	return value
}

// parseBareNumber parses a numeric literal (no unit, no colour marker) into a
// json.Number, returning ok=false otherwise.
func parseBareNumber(value string) (json.Number, bool) {
	if value == "" {
		return "", false
	}
	for i, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '+':
			if i == 0 && r == '+' {
				return "", false
			}
		case r == 'e' || r == 'E':
			// exponent: allow
		default:
			return "", false
		}
	}
	return json.Number(value), true
}
