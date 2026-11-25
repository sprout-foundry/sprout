// Assertion helpers for the conformance test runner.
package main

import (
	"fmt"
	"reflect"
	"strings"
)

// errorTypeMap maps errorType assertion values to substrings to match.
var errorTypeMap = map[string][]string{
	"no_provider":      {"no provider configured"},
	"no_executor":      {"no tool executor configured"},
	"interrupted":      {"interrupted"},
	"max_iterations":   {"maximum iterations exceeded"},
	"paused":           {"agent is paused"},
	"zero_choices":     {"zero choices"},
	"transient":        {"transient error"},
	"rate_limit":       {"rate limit", "too many requests"},
	"context_overflow": {"context window exceeded"},
	"auth":             {"authentication failed"},
	"client":           {"client error"},
	"content_filtered": {"content filter", "content_filtered", "content policy"},
	"blank_response":   {"blank", "repetitive"},
}

// valuesMatch compares two interface{} values for equality.
// JSON numbers come through as float64, so we handle numeric comparison.
func valuesMatch(expected, actual interface{}) bool {
	if expected == nil {
		return actual == nil
	}
	if actual == nil {
		return false
	}

	// Try numeric comparison first (JSON parses ints as float64).
	expFloat, expIsNum := toFloat(expected)
	actFloat, actIsNum := toFloat(actual)
	if expIsNum && actIsNum {
		return expFloat == actFloat
	}

	// String comparison.
	expStr, expIsStr := expected.(string)
	actStr, actIsStr := actual.(string)
	if expIsStr && actIsStr {
		return expStr == actStr
	}

	// Bool comparison.
	expBool, expIsBool := expected.(bool)
	actBool, actIsBool := actual.(bool)
	if expIsBool && actIsBool {
		return expBool == actBool
	}

	// Fallback to reflect.DeepEqual for complex types.
	return reflect.DeepEqual(expected, actual)
}

// toFloat converts a value to float64 if it's a numeric type.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

// navigatePath follows a dot-separated path into a nested map.
// Empty path returns the root value.
func navigatePath(m map[string]interface{}, path string) interface{} {
	if path == "" {
		return m
	}
	parts := strings.Split(path, ".")
	current := interface{}(m)
	for _, part := range parts {
		if mm, ok := current.(map[string]interface{}); ok {
			current = mm[part]
		} else {
			return nil
		}
	}
	return current
}

// containsInMap checks if any value in the map (recursively) contains the substring.
func containsInMap(m map[string]interface{}, substr string) bool {
	for _, v := range m {
		if containsValue(v, substr) {
			return true
		}
	}
	return false
}

// containsValue recursively checks if any value contains the substring.
func containsValue(v interface{}, substr string) bool {
	switch val := v.(type) {
	case string:
		return strings.Contains(val, substr)
	case map[string]interface{}:
		for _, vv := range val {
			if containsValue(vv, substr) {
				return true
			}
		}
	case []interface{}:
		for _, vv := range val {
			if containsValue(vv, substr) {
				return true
			}
		}
	}
	return false
}

// errorMessage extracts the error message string from a response's error map.
func errorMessage(errMap *map[string]interface{}) string {
	if errMap == nil {
		return ""
	}
	if v, ok := (*errMap)["message"]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// getResponseForAssertion returns the response for the given ID.
// When ID is 0 (default for assertions without an explicit id), it falls back
// to the response with the highest ID (the most recent response).
func getResponseForAssertion(id int, responses map[int]*CollectedResponse) *CollectedResponse {
	if id != 0 {
		return responses[id]
	}
	// Fallback: find the response with the highest ID
	maxID := -1
	for k := range responses {
		if k > maxID {
			maxID = k
		}
	}
	if maxID < 0 {
		return nil
	}
	return responses[maxID]
}
