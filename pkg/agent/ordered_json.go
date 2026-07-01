package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/errors"
)

// ParseJSONOrdered parses a JSON string into an *OrderedMap, preserving the
// key order from the source text. Only top-level objects are supported —
// passing a top-level array or scalar returns an error.
//
// Nested objects are recursively wrapped in *OrderedMap. Arrays become
// []interface{} slices where any contained objects are also *OrderedMap values.
func ParseJSONOrdered(content string) (*OrderedMap, error) {
	result, err := ParseJSONOrderedAny(content)
	if err != nil {
		return nil, err
	}
	om, ok := result.(*OrderedMap)
	if !ok {
		return nil, errors.NewTool("ordered", fmt.Sprintf("JSON content must be a top-level object, got %T", result), nil)
	}
	return om, nil
}

// ParseJSONOrderedAny parses a JSON string, preserving key order in objects.
// Returns *OrderedMap for objects and []interface{} for arrays (with nested
// objects also wrapped in *OrderedMap).
func ParseJSONOrderedAny(content string) (interface{}, error) {
	dec := json.NewDecoder(strings.NewReader(content))
	// Consume leading whitespace and peek at the first token.
	tok, err := dec.Token()
	if err != nil {
		// Use fmt.Errorf so the underlying *json.SyntaxError stays extractable
		// via errors.As for callers that need line/col diagnostics.
		return nil, fmt.Errorf("failed to read JSON token: %w", err)
	}

	result, err := parseJSONValueFromToken(dec, tok)
	if err != nil {
		return nil, err
	}

	// Verify there is no trailing content (except whitespace).
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.NewTool("ordered", "unexpected trailing content after JSON document", nil)
	}

	return result, nil
}

// parseJSONObject reads key-value pairs from dec (positioned after the
// opening '{' delimiter) until the closing '}' and returns an OrderedMap
// preserving the source key order.
func parseJSONObject(dec *json.Decoder) (*OrderedMap, error) {
	om := NewOrderedMap()

	for {
		// Read key or closing brace.
		tok, err := dec.Token()
		if err != nil {
			// Use fmt.Errorf so the underlying *json.SyntaxError stays extractable
			// via errors.As for callers that need line/col diagnostics.
			return nil, fmt.Errorf("failed to read JSON object key: %w", err)
		}

		if delim, ok := tok.(json.Delim); ok && delim == '}' {
			return om, nil
		}

		key, ok := tok.(string)
		if !ok {
			return nil, errors.NewTool("ordered", fmt.Sprintf("expected string key in JSON object, got %s", tokenDescription(tok)), nil)
		}

		// Read value.
		value, err := parseJSONValue(dec)
		if err != nil {
			return nil, errors.NewTool("ordered", fmt.Sprintf("failed to parse value for key %q", key), err)
		}

		om.Set(key, value)
	}
}

// parseJSONArray reads elements from dec (positioned after the opening '[')
// until ']' and returns a []interface{}.
func parseJSONArray(dec *json.Decoder) ([]interface{}, error) {
	var result []interface{}

	for {
		// Peek at the next token to check for closing bracket.
		tok, err := dec.Token()
		if err != nil {
			// Use fmt.Errorf so the underlying *json.SyntaxError stays extractable
			// via errors.As for callers that need line/col diagnostics.
			return nil, fmt.Errorf("failed to read JSON array element: %w", err)
		}

		if delim, ok := tok.(json.Delim); ok && delim == ']' {
			return result, nil
		}

		// tok is the first token of the value — dispatch based on it.
		value, err := parseJSONValueFromToken(dec, tok)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
}

// parseJSONValue reads the next complete JSON value from dec.
func parseJSONValue(dec *json.Decoder) (interface{}, error) {
	tok, err := dec.Token()
	if err != nil {
		// Use fmt.Errorf so the underlying *json.SyntaxError stays extractable
		// via errors.As for callers that need line/col diagnostics.
		return nil, fmt.Errorf("failed to read JSON value: %w", err)
	}
	return parseJSONValueFromToken(dec, tok)
}

// parseJSONValueFromToken processes a JSON value whose first token has already
// been consumed (and is passed as tok). For objects and arrays it delegates to
// the corresponding parser; for delimiters '}' and ']' it is an error (they
// should be handled by the caller).
func parseJSONValueFromToken(dec *json.Decoder, tok json.Token) (interface{}, error) {
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			obj, err := parseJSONObject(dec)
			if err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			return parseJSONArray(dec)
		case '}', ']':
			// These should be consumed by the parent object/array parser.
			return nil, errors.NewTool("ordered", fmt.Sprintf("unexpected delimiter '%c' while parsing value", v), nil)
		default:
			return nil, errors.NewTool("ordered", fmt.Sprintf("unexpected JSON delimiter '%c'", v), nil)
		}
	case string:
		return v, nil
	case float64:
		return v, nil
	case bool:
		return v, nil
	case nil:
		return nil, nil
	default:
		return nil, errors.NewTool("ordered", fmt.Sprintf("unexpected JSON token type: %T", tok), nil)
	}
}

// tokenDescription returns a human-friendly description of a JSON token for
// error messages.
func tokenDescription(tok json.Token) string {
	if tok == nil {
		return "null"
	}
	switch v := tok.(type) {
	case json.Delim:
		return fmt.Sprintf("delimiter '%c'", v)
	case string:
		return fmt.Sprintf("string %q", v)
	case float64:
		return fmt.Sprintf("number %v", v)
	case bool:
		return fmt.Sprintf("bool %v", v)
	default:
		return fmt.Sprintf("%T", tok)
	}
}
