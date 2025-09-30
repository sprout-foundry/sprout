package components

import (
	"fmt"
	"testing"
)

func TestJSONFormatter(t *testing.T) {
	formatter := NewJSONFormatter()

	// Test data
	testCases := []struct {
		name string
		data interface{}
	}{
		{
			name: "Simple Object",
			data: map[string]interface{}{
				"name":    "John Doe",
				"age":     30,
				"active":  true,
				"balance": nil,
			},
		},
		{
			name: "Nested Object",
			data: map[string]interface{}{
				"user": map[string]interface{}{
					"name": "Jane Smith",
					"preferences": map[string]interface{}{
						"theme":         "dark",
						"notifications": true,
					},
				},
				"tags": []interface{}{"developer", "golang", "json"},
			},
		},
		{
			name: "Array",
			data: []interface{}{
				"string item",
				42,
				true,
				nil,
				map[string]interface{}{"key": "value"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			formatted, err := formatter.FormatJSON(tc.data)
			if err != nil {
				t.Fatalf("FormatJSON failed: %v", err)
			}

			// Print for visual inspection
			fmt.Printf("\n=== %s ===\n%s\n", tc.name, formatted)

			// Basic validation - formatted string should contain color codes
			if !containsColorCodes(formatted) {
				t.Error("Formatted JSON should contain ANSI color codes")
			}
		})
	}
}

func TestJSONFormatterWithString(t *testing.T) {
	formatter := NewJSONFormatter()

	jsonString := `{
		"name": "Test User",
		"data": {
			"values": [1, 2, 3],
			"enabled": true,
			"config": null
		}
	}`

	formatted, err := formatter.FormatJSON(jsonString)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	fmt.Printf("\n=== JSON String Test ===\n%s\n", formatted)

	if !containsColorCodes(formatted) {
		t.Error("Formatted JSON should contain ANSI color codes")
	}
}

func TestDetectAndFormatJSON(t *testing.T) {
	formatter := NewJSONFormatter()

	text := `Here's some JSON data: {"status": "success", "count": 42} and more text.`

	formatted := formatter.DetectAndFormatJSON(text)
	fmt.Printf("\n=== Detect and Format Test ===\n%s\n", formatted)

	if !containsColorCodes(formatted) {
		t.Error("Formatted text should contain ANSI color codes for JSON parts")
	}
}

func TestFormatModelResponse(t *testing.T) {
	formatter := NewJSONFormatter()

	response := `Here's the analysis result:

{
  "findings": [
    "Issue 1: Memory leak detected",
    "Issue 2: Unused variable"
  ],
  "score": 85,
  "recommendations": {
    "priority": "high",
    "actions": ["fix memory leak", "remove unused code"]
  }
}

Please review these findings.`

	formatted := formatter.FormatModelResponse(response)
	fmt.Printf("\n=== Model Response Test ===\n%s\n", formatted)

	if !containsColorCodes(formatted) {
		t.Error("Formatted response should contain ANSI color codes")
	}
}

func TestCompactFormat(t *testing.T) {
	formatter := NewJSONFormatter()

	data := map[string]interface{}{
		"deeply": map[string]interface{}{
			"nested": map[string]interface{}{
				"object": map[string]interface{}{
					"with": "values",
				},
			},
		},
	}

	compact, err := formatter.FormatCompact(data)
	if err != nil {
		t.Fatalf("FormatCompact failed: %v", err)
	}

	regular, err := formatter.FormatJSON(data)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	fmt.Printf("\n=== Regular Format ===\n%s\n", regular)
	fmt.Printf("\n=== Compact Format ===\n%s\n", compact)

	// Compact should be shorter (fewer spaces)
	if len(compact) >= len(regular) {
		t.Error("Compact format should be shorter than regular format")
	}
}

func TestStripColors(t *testing.T) {
	formatter := NewJSONFormatter()

	data := map[string]interface{}{"test": "value"}
	formatted, err := formatter.FormatJSON(data)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	stripped := formatter.StripColors(formatted)

	if containsColorCodes(stripped) {
		t.Error("Stripped text should not contain color codes")
	}

	if !containsColorCodes(formatted) {
		t.Error("Original formatted text should contain color codes")
	}
}

// Helper function to check if text contains ANSI color codes
func containsColorCodes(text string) bool {
	return len(text) > 0 && (fmt.Sprintf("%s", text) != formatter.StripColors(text))
}

// Create a formatter instance for the helper function
var formatter = NewJSONFormatter()

// Example function to demonstrate usage
func ExampleJSONFormatter() {
	formatter := NewJSONFormatter()

	data := map[string]interface{}{
		"message": "Hello, World!",
		"status":  "success",
		"data": map[string]interface{}{
			"items": []interface{}{1, 2, 3},
			"total": 3,
		},
	}

	formatted, _ := formatter.FormatJSON(data)
	fmt.Println(formatted)
}
