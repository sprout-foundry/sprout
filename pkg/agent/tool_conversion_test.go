package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestConvertToString(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		param    string
		expected string
		wantErr  bool
	}{
		{
			name:     "string input",
			input:    "hello world",
			param:    "test",
			expected: "hello world",
			wantErr:  false,
		},
		{
			name:     "number input",
			input:    42,
			param:    "test",
			expected: "42",
			wantErr:  false,
		},
		{
			name:     "float input",
			input:    3.14,
			param:    "test",
			expected: "3.14",
			wantErr:  false,
		},
		{
			name:     "bool input",
			input:    true,
			param:    "test",
			expected: "true",
			wantErr:  false,
		},
		{
			name: "map input",
			input: map[string]interface{}{
				"key": "value",
				"num": 123,
			},
			param:    "test",
			expected: `{"key":"value","num":123}`,
			wantErr:  false,
		},
		{
			name:     "nil input",
			input:    nil,
			param:    "test",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertToString(tt.input, tt.param)

			if tt.wantErr {
				if err == nil {
					t.Errorf("convertToString() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("convertToString() unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("convertToString() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestToolParameterConversion(t *testing.T) {
	// Test the problematic case: content as map[string]interface{}
	args := map[string]interface{}{
		"file_path": "test.json",
		"content": map[string]interface{}{
			"key":    "value",
			"number": 42,
			"nested": map[string]interface{}{
				"inner": "data",
			},
		},
	}

	// Convert to JSON and back (simulating LLM tool call)
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("Failed to marshal args: %v", err)
	}

	var parsedArgs map[string]interface{}
	if err := json.Unmarshal(argsJSON, &parsedArgs); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}

	// Test conversion of each parameter
	filePath, err := convertToString(parsedArgs["file_path"], "file_path")
	if err != nil {
		t.Errorf("Failed to convert file_path: %v", err)
	}
	if filePath != "test.json" {
		t.Errorf("file_path = %v, want %v", filePath, "test.json")
	}

	content, err := convertToString(parsedArgs["content"], "content")
	if err != nil {
		t.Errorf("Failed to convert content: %v", err)
	}
	// Parse both to compare structurally since JSON key order may vary
	var expectedMap, actualMap map[string]interface{}
	if err := json.Unmarshal([]byte(`{"key":"value","number":42,"nested":{"inner":"data"}}`), &expectedMap); err != nil {
		t.Fatalf("Failed to parse expected JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(content), &actualMap); err != nil {
		t.Errorf("Failed to parse actual content: %v", err)
	} else if len(actualMap) != len(expectedMap) {
		t.Errorf("content map length = %v, want %v", len(actualMap), len(expectedMap))
	} else {
		// Check that all expected keys and values are present
		for key, expectedVal := range expectedMap {
			if actualVal, exists := actualMap[key]; !exists {
				t.Errorf("content missing key %s", key)
			} else if fmt.Sprintf("%v", actualVal) != fmt.Sprintf("%v", expectedVal) {
				t.Errorf("content[%s] = %v, want %v", key, actualVal, expectedVal)
			}
		}
	}

	t.Logf("✅ Successfully converted map content to JSON string: %s", content)
}
