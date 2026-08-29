package providers

import (
	"encoding/json"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// toolSchemaCompat.go — defense-in-depth schema normalization for strict
// function-calling validators.
//
// Google Gemini 3.x (reached via OpenRouter as google/gemini-3-*) enforces
// strict JSON-schema validation on tool definitions: any array-typed property
// without an "items" key causes the entire request to be rejected (HTTP 400,
// "array should have required properties at ... items"). Gemini 2.5 tolerated
// array properties without items. Native sprout tool definitions declare items
// explicitly (enforced by the schema lint test in pkg/agent_tools), but the
// seed tool registry's wire schema (core.ToolParameter has no Items field)
// and third-party/MCP tool schemas may still lack them. When a provider opts
// in via message_conversion.fill_missing_array_items, buildChatRequest walks
// every tool schema and fills missing items with a permissive fallback so the
// request is always well-formed for strict validators.

// defaultArrayItemsFallback is the most permissive single-type schema: any
// object passes. "string" is also supported for providers where array items
// are known to be textual.
const defaultArrayItemsFallback = "object"

// fillMissingArrayItems returns a copy of tools in which every array-typed
// property lacking an "items" key has one added. The input slice and its
// elements are not mutated. Parameters of any type (seed's ToolParameters
// struct, raw maps, or nil) are normalized via a JSON round-trip so the
// result is always a plain map that marshals to the correct wire shape.
func fillMissingArrayItems(tools []api.Tool, fallback string) []api.Tool {
	fallback = normalizeArrayItemsFallback(fallback)
	patched := make([]api.Tool, len(tools))
	copy(patched, tools)
	for i := range patched {
		m, ok := paramsToMap(patched[i].Function.Parameters)
		if !ok {
			continue
		}
		walkAndFillItems(m, fallback)
		patched[i].Function.Parameters = m
	}
	return patched
}

// normalizeArrayItemsFallback validates the configured fallback type, falling
// back to "object" for anything unrecognized.
func normalizeArrayItemsFallback(fallback string) string {
	switch fallback {
	case "string", "object":
		return fallback
	default:
		return defaultArrayItemsFallback
	}
}

// paramsToMap normalizes a Function.Parameters value to a map. Returns false
// when the value is nil or cannot be represented as a JSON object.
func paramsToMap(params interface{}) (map[string]interface{}, bool) {
	if params == nil {
		return nil, false
	}
	if m, ok := params.(map[string]interface{}); ok {
		return m, true
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}

// walkAndFillItems recursively fills missing "items" keys on every
// array-typed property in a JSON-schema node, in place.
func walkAndFillItems(node map[string]interface{}, fallback string) {
	if node == nil {
		return
	}
	// Fill this node if it is an array without items.
	if t, ok := node["type"].(string); ok && t == "array" {
		if items, present := node["items"]; !present || items == nil {
			node["items"] = map[string]interface{}{"type": fallback}
		}
	}
	// Recurse into object properties.
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, p := range props {
			if pm, ok := p.(map[string]interface{}); ok {
				walkAndFillItems(pm, fallback)
			}
		}
	}
	// Recurse into the items schema itself (nested arrays, array of arrays).
	if items, ok := node["items"].(map[string]interface{}); ok {
		walkAndFillItems(items, fallback)
	}
	// Recurse into combinators.
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := node[key].([]interface{}); ok {
			for _, a := range arr {
				if am, ok := a.(map[string]interface{}); ok {
					walkAndFillItems(am, fallback)
				}
			}
		}
	}
}
