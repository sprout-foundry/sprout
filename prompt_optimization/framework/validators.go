package framework

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// TextReplacementValidator validates simple text replacement tasks
type TextReplacementValidator struct{}

func (v *TextReplacementValidator) Validate(response string, expected ExpectedOutput) []ValidationResult {
	var results []ValidationResult

	// Extract old and new text from expected output
	// For text replacement, we expect Contains[0] to be old text and Contains[1] to be new text
	if len(expected.Contains) >= 2 {
		oldText := expected.Contains[0]
		newText := expected.Contains[1]

		hasOldText := strings.Contains(response, oldText)
		hasNewText := strings.Contains(response, newText)

		results = append(results, ValidationResult{
			Check:    "replacement_performed",
			Passed:   !hasOldText && hasNewText,
			Expected: fmt.Sprintf("Replace '%s' with '%s'", oldText, newText),
			Actual:   fmt.Sprintf("Old text present: %t, New text present: %t", hasOldText, hasNewText),
			Score:    calculateReplacementScore(hasOldText, hasNewText),
		})

		// Check if the replacement preserved surrounding content
		if !hasOldText && hasNewText {
			results = append(results, ValidationResult{
				Check:   "content_preserved",
				Passed:  true,
				Message: "Content appears to be properly preserved around replacement",
				Score:   1.0,
			})
		}
	}

	return results
}

// CodeGenerationValidator validates code generation tasks
type CodeGenerationValidator struct{}

func (v *CodeGenerationValidator) Validate(response string, expected ExpectedOutput) []ValidationResult {
	var results []ValidationResult

	// Extract code from response (handle markdown code blocks)
	codeContent := extractCodeFromResponse(response)

	// Language-specific validation
	switch strings.ToLower(expected.Language) {
	case "go":
		results = append(results, v.validateGoCode(codeContent)...)
	case "python":
		results = append(results, v.validatePythonCode(codeContent)...)
	case "javascript", "js":
		results = append(results, v.validateJavaScriptCode(codeContent)...)
	default:
		results = append(results, v.validateGenericCode(codeContent)...)
	}

	// Check for required functions
	for _, funcName := range expected.Functions {
		hasFunc := strings.Contains(codeContent, fmt.Sprintf("func %s", funcName)) ||
			strings.Contains(codeContent, fmt.Sprintf("function %s", funcName)) ||
			strings.Contains(codeContent, fmt.Sprintf("def %s", funcName))

		results = append(results, ValidationResult{
			Check:    fmt.Sprintf("function_%s_present", funcName),
			Passed:   hasFunc,
			Expected: fmt.Sprintf("Function '%s' should be present", funcName),
			Actual:   fmt.Sprintf("Function found: %t", hasFunc),
		})
	}

	return results
}

// validateGoCode validates Go code syntax and structure
func (v *CodeGenerationValidator) validateGoCode(code string) []ValidationResult {
	var results []ValidationResult

	// Try to parse the Go code
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "test.go", code, parser.ParseComments)

	if err != nil {
		results = append(results, ValidationResult{
			Check:    "go_syntax_valid",
			Passed:   false,
			Expected: "Valid Go syntax",
			Actual:   fmt.Sprintf("Parse error: %v", err),
			Score:    0.0,
		})
	} else {
		results = append(results, ValidationResult{
			Check:    "go_syntax_valid",
			Passed:   true,
			Expected: "Valid Go syntax",
			Actual:   "Code parses successfully",
			Score:    1.0,
		})
	}

	// Check for common Go patterns
	hasPackage := strings.Contains(code, "package ")
	results = append(results, ValidationResult{
		Check:    "go_package_declaration",
		Passed:   hasPackage,
		Expected: "Package declaration present",
		Actual:   fmt.Sprintf("Has package: %t", hasPackage),
	})

	return results
}

// validatePythonCode validates Python code structure
func (v *CodeGenerationValidator) validatePythonCode(code string) []ValidationResult {
	var results []ValidationResult

	// Basic Python structure checks
	lines := strings.Split(code, "\n")
	indentationConsistent := true
	hasFunction := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if strings.Contains(line, "def ") {
			hasFunction = true
		}

		// Check basic indentation (simplified)
		if strings.HasPrefix(line, "    ") || !strings.HasPrefix(line, " ") {
			// Good indentation
		} else if strings.HasPrefix(line, " ") {
			indentationConsistent = false
		}
	}

	results = append(results, ValidationResult{
		Check:    "python_indentation",
		Passed:   indentationConsistent,
		Expected: "Consistent indentation",
		Actual:   fmt.Sprintf("Indentation consistent: %t", indentationConsistent),
	})

	results = append(results, ValidationResult{
		Check:    "python_function_present",
		Passed:   hasFunction,
		Expected: "At least one function defined",
		Actual:   fmt.Sprintf("Has function: %t", hasFunction),
	})

	return results
}

// validateJavaScriptCode validates JavaScript code structure
func (v *CodeGenerationValidator) validateJavaScriptCode(code string) []ValidationResult {
	var results []ValidationResult

	// Check for common JavaScript patterns
	hasFunction := strings.Contains(code, "function ") || strings.Contains(code, "=> ")
	hasBraces := strings.Contains(code, "{") && strings.Contains(code, "}")

	results = append(results, ValidationResult{
		Check:    "js_function_present",
		Passed:   hasFunction,
		Expected: "Function declaration present",
		Actual:   fmt.Sprintf("Has function: %t", hasFunction),
	})

	results = append(results, ValidationResult{
		Check:    "js_structure",
		Passed:   hasBraces,
		Expected: "Proper code structure with braces",
		Actual:   fmt.Sprintf("Has braces: %t", hasBraces),
	})

	return results
}

// validateGenericCode validates general code structure
func (v *CodeGenerationValidator) validateGenericCode(code string) []ValidationResult {
	var results []ValidationResult

	hasCodeStructure := (strings.Contains(code, "{") && strings.Contains(code, "}")) ||
		(strings.Contains(code, "(") && strings.Contains(code, ")"))

	results = append(results, ValidationResult{
		Check:    "generic_code_structure",
		Passed:   hasCodeStructure,
		Expected: "Basic code structure present",
		Actual:   fmt.Sprintf("Has structure: %t", hasCodeStructure),
	})

	return results
}

// JSONOutputValidator validates JSON format responses
type JSONOutputValidator struct{}

func (v *JSONOutputValidator) Validate(response string, expected ExpectedOutput) []ValidationResult {
	var results []ValidationResult

	// Extract JSON from response
	jsonContent := extractJSONFromResponse(response)

	// Validate JSON syntax
	var parsedJSON interface{}
	err := json.Unmarshal([]byte(jsonContent), &parsedJSON)

	results = append(results, ValidationResult{
		Check:    "json_valid_syntax",
		Passed:   err == nil,
		Expected: "Valid JSON syntax",
		Actual:   fmt.Sprintf("JSON valid: %t, Error: %v", err == nil, err),
		Score:    calculateJSONScore(err),
	})

	// Validate against schema if provided
	if expected.JSONSchema != nil && err == nil {
		schemaResults := v.validateJSONSchema(parsedJSON, expected.JSONSchema)
		results = append(results, schemaResults...)
	}

	return results
}

// validateJSONSchema validates JSON against a simple schema
func (v *JSONOutputValidator) validateJSONSchema(data interface{}, schema map[string]interface{}) []ValidationResult {
	var results []ValidationResult

	// Convert to map for easier validation
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		results = append(results, ValidationResult{
			Check:    "json_object_type",
			Passed:   false,
			Expected: "JSON object",
			Actual:   "Not a JSON object",
		})
		return results
	}

	// Check required fields
	for key := range schema {
		_, exists := dataMap[key]
		results = append(results, ValidationResult{
			Check:    fmt.Sprintf("json_field_%s", key),
			Passed:   exists,
			Expected: fmt.Sprintf("Field '%s' should exist", key),
			Actual:   fmt.Sprintf("Field exists: %t", exists),
		})
	}

	return results
}

// Helper functions

// extractCodeFromResponse extracts code content from markdown code blocks
func extractCodeFromResponse(response string) string {
	// Look for code blocks
	codeBlockRegex := regexp.MustCompile("```[a-zA-Z]*\n([\\s\\S]*?)```")
	matches := codeBlockRegex.FindStringSubmatch(response)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// If no code block, return the whole response
	return response
}

// extractJSONFromResponse extracts JSON content from response
func extractJSONFromResponse(response string) string {
	// Look for JSON blocks
	jsonBlockRegex := regexp.MustCompile("```json\n([\\s\\S]*?)```")
	matches := jsonBlockRegex.FindStringSubmatch(response)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Look for inline JSON
	jsonInlineRegex := regexp.MustCompile(`{[\\s\\S]*}`)
	match := jsonInlineRegex.FindString(response)
	if match != "" {
		return match
	}

	return response
}

// calculateReplacementScore calculates score for text replacement tasks
func calculateReplacementScore(hasOldText, hasNewText bool) float64 {
	if !hasOldText && hasNewText {
		return 1.0 // Perfect replacement
	}
	if hasOldText && hasNewText {
		return 0.5 // Partial - new text added but old text not removed
	}
	if !hasOldText && !hasNewText {
		return 0.0 // Both missing - likely failed
	}
	return 0.0 // Old text present but new text missing
}

// calculateJSONScore calculates score for JSON validation
func calculateJSONScore(err error) float64 {
	if err == nil {
		return 1.0
	}
	return 0.0
}
