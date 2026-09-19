package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// tokenNode is one node of the parsed DTCG token tree. Only JSON
// objects become nodes: groups are interior nodes, token leaves carry
// $value/$type metadata, and a structural key with a non-object value
// becomes a nonObject node for the structure rules to report on.
type tokenNode struct {
	key        string
	path       string
	line       int
	children   []*tokenNode
	child      map[string]*tokenNode
	nonObject  bool
	leaf       bool
	hasType    bool
	typeName   string
	typeIsStr  bool
	valueStr   string
	valueIsStr bool
	// rawValue is the leaf's $value as decoded JSON (string, float64, bool,
	// nil, map[string]any, or []any). The validator never needs it — only a
	// string $value is examined for aliases — but token export
	// (pkg/design/export.go, SP-140-5 §5a) resolves and renders every value
	// type, so the parser retains the decoded value here.
	rawValue any
	// rawPresent reports whether a $value was seen at all, distinguishing a
	// JSON null value from an absent one.
	rawPresent bool
}

// parseTokensDocument parses W3C DTCG token JSON (SP-140-1 §1a) into a
// tree preserving document order and 1-based source lines; it reads a
// json.Decoder token stream rather than a map unmarshal so those lines
// survive. The root must be a JSON object (key "" and path ""). Keys
// starting with '$' are DTCG metadata, never structural: $value of any
// type marks a leaf (only string $values are recorded, for alias
// resolution), $type records a type name, the rest is ignored.
// Duplicate keys resolve last-wins for both the child map and the node
// fields; the first position in children is kept.
//
// Findings carry only Rule, Line, and Message — the caller adds File
// and sorts. Any token-stream error yields one token_json_parse finding
// at the decoder's offset; a document that parses but is not an object
// yields one token_leaf_structure finding at line 1.
func parseTokensDocument(content []byte) (*tokenNode, []Finding) {
	dec := json.NewDecoder(bytes.NewReader(content))

	tok, err := dec.Token()
	if err != nil {
		return nil, []Finding{jsonParseFinding(content, dec, err)}
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, []Finding{{
			Rule:    ruleTokenLeafStructure,
			Line:    1,
			Message: "top level must be an object of token groups",
		}}
	}
	// The root object's own line is where its '{' sat; it anchors
	// findings on the root group itself (e.g. $type without $value).
	rootLine := lineOfOffset(content, int(dec.InputOffset()))
	root, err := parseTokenObject(dec, content, "", "", rootLine)
	if err != nil {
		return nil, []Finding{jsonParseFinding(content, dec, err)}
	}
	// A root carrying $value parsed as a leaf; "top level is groups"
	// makes that a structural violation, and reporting it here (rather
	// than walking) keeps every nested token from escaping validation
	// behind a well-typed leaf root — the same one-finding shape as the
	// non-object case above.
	if root.leaf {
		return nil, []Finding{{
			Rule:    ruleTokenLeafStructure,
			Line:    1,
			Message: "top level must be an object of token groups",
		}}
	}
	// Trailing data after the root object is invalid JSON (a JSON text
	// is a single value), but the decoder only objects to it on the
	// next read — and not always: a well-formed second value ("{}{}")
	// comes back as a plain token with no error, so the stray token
	// itself counts as trailing data; only a genuine read error falls
	// through to the offset-mapped parse finding.
	offset := dec.InputOffset()
	_, err = dec.Token()
	switch {
	case err == nil:
		return nil, []Finding{{
			Rule:    ruleTokenJSONParse,
			Line:    lineOfOffset(content, int(offset)),
			Message: "invalid JSON: trailing data after root object",
		}}
	case !errors.Is(err, io.EOF):
		return nil, []Finding{jsonParseFinding(content, dec, err)}
	}
	return root, nil
}

// parseTokenObject consumes an object's members into a node; the caller
// has already read the opening '{'. Each value's input offset is
// captured before the value token is read, so node lines point at the
// value in the source.
func parseTokenObject(dec *json.Decoder, content []byte, key, path string, line int) (*tokenNode, error) {
	node := &tokenNode{
		key:   key,
		path:  path,
		line:  line,
		child: make(map[string]*tokenNode),
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '}' {
				return node, nil
			}
			// The decoder enforces key/value alternation, so any
			// other delimiter here is unreachable.
			return nil, fmt.Errorf("unexpected delimiter %q in token object", d)
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected token object key %v", tok)
		}

		valueOffset := dec.InputOffset()
		valTok, err := dec.Token()
		if err != nil {
			return nil, err
		}

		if strings.HasPrefix(name, "$") {
			if err := recordMeta(node, dec, name, valTok); err != nil {
				return nil, err
			}
			continue
		}

		if d, ok := valTok.(json.Delim); ok && d == '{' {
			child, err := parseTokenObject(dec, content, name, joinPath(path, name),
				lineOfOffset(content, int(valueOffset)))
			if err != nil {
				return nil, err
			}
			setChild(node, child)
			continue
		}

		if err := skipValue(dec, valTok); err != nil {
			return nil, err
		}
		setChild(node, &tokenNode{
			key:       name,
			path:      joinPath(path, name),
			line:      lineOfOffset(content, int(valueOffset)),
			nonObject: true,
			child:     make(map[string]*tokenNode),
		})
	}
}

// recordMeta absorbs one '$'-prefixed metadata value on node, skipping
// any object/array remainder. Metadata never becomes structural and is
// never descended into as a token group.
//
// A $value of any JSON type marks the node a leaf. Only a string $value
// is recorded (valueStr/valueIsStr) for alias resolution; object and
// array $values — the norm for color, border, and the other structured
// types — keep the node a leaf but carry no recorded value, so
// checkAliases' !valueIsStr guard never attempts an alias walk on them.
// A non-string $type likewise sets hasType without typeIsStr, which the
// structure rules report separately.
//
// rawValue additionally records the decoded JSON value of any scalar
// $value (string, number, bool, or null), which token export needs to
// render numeric and boolean tokens; structured object/array values keep
// rawValue nil (rawPresent true) and are re-read from the source by the
// exporter rather than retained here.
func recordMeta(node *tokenNode, dec *json.Decoder, key string, valTok json.Token) error {
	switch key {
	case "$value":
		node.leaf = true
		node.rawPresent = true
		switch v := valTok.(type) {
		case string:
			node.valueStr, node.valueIsStr = v, true
			node.rawValue = v
		case json.Delim:
			// Object or array $value: structured, not scalar. Retained as
			// nil here; export resolves structured values from the node's
			// parsed JSON subtree (see rawLeafValue in export.go).
		default:
			node.rawValue = v
		}
	case "$type":
		node.hasType = true
		if s, ok := valTok.(string); ok {
			node.typeName, node.typeIsStr = s, true
		}
	default: // $description, $extensions, and unknown $ keys: ignored.
		// $extensions' subtree is invisible to the validator (spec
		// §1a — sprout never requires anything there; no structure
		// findings, no alias collection, not an alias target).
	}
	return skipValue(dec, valTok)
}

// skipValue consumes the remainder of a value whose first token is
// already read; scalars end there, arrays and objects need their
// balanced remainder consumed so the stream stays positioned on the
// next key or closing brace.
func skipValue(dec *json.Decoder, first json.Token) error {
	if _, ok := first.(json.Delim); !ok {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok && (d == '{' || d == '[') {
			depth++
		} else if ok {
			depth--
		}
	}
	return nil
}

// joinPath builds a token's alias path: the bare key under the root
// group, parent.path + "." + key below it.
func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// setChild attaches child to node; a duplicate key replaces the prior
// subtree in place (last wins) while keeping its first position.
func setChild(node, child *tokenNode) {
	if existing, ok := node.child[child.key]; ok {
		*existing = *child
		return
	}
	node.child[child.key] = child
	node.children = append(node.children, child)
}

// jsonParseFinding builds the single finding every token-stream error
// maps to, located at the decoder's current input offset.
func jsonParseFinding(content []byte, dec *json.Decoder, err error) Finding {
	return Finding{
		Rule:    ruleTokenJSONParse,
		Line:    lineOfOffset(content, int(dec.InputOffset())),
		Message: fmt.Sprintf("invalid JSON: %v", err),
	}
}

// lineOfOffset maps a decoder input offset to a 1-based source line by
// counting the newlines before it; out-of-range offsets clamp to the
// nearest end so the result is never below 1.
func lineOfOffset(content []byte, offset int) int {
	offset = min(max(offset, 0), len(content))
	return 1 + bytes.Count(content[:offset], []byte{'\n'})
}
