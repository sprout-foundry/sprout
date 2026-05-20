package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// TestMCPToolWrapper_ValidateArgs_MissingRequiredField verifies that when an MCP tool
// call has invalid arguments (e.g., missing required fields), the MCPToolWrapper.Execute()
// returns early with a useful validation error message that the LLM can use to self-correct.
func TestMCPToolWrapper_ValidateArgs_MissingRequiredField(t *testing.T) {
	t.Parallel()

	// Create an MCP tool with a schema that requires "query" and "max_results"
	mcpTool := mcp.MCPTool{
		Name:        "search",
		Description: "Search for information",
		ServerName:  "test-server",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"required":             []interface{}{"query", "max_results"},
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
				"max_results": map[string]interface{}{
					"type":        "number",
					"description": "Maximum number of results to return",
				},
			},
		},
	}

	// No manager needed — validation fails before any server call
	wrapper := mcp.NewMCPToolWrapper(mcpTool, nil)

	// Call Execute with missing required fields
	ctx := context.Background()
	result, err := wrapper.Execute(ctx, mcp.Parameters{
		Kwargs: map[string]interface{}{},
	})

	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	// Verify the result indicates failure
	if result.Success {
		t.Fatal("Expected Success=false for invalid arguments, got true")
	}

	// Verify the output contains LLM-friendly formatted validation error
	outputStr, ok := result.Output.(string)
	if !ok {
		t.Fatalf("Expected Output to be a string, got %T", result.Output)
	}

	// Should mention validation failure
	if !strings.Contains(outputStr, "Validation failed") {
		t.Errorf("Output should mention 'Validation failed', got: %s", outputStr)
	}

	// Should mention the tool and server names
	if !strings.Contains(outputStr, "search") {
		t.Errorf("Output should mention tool name 'search', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "test-server") {
		t.Errorf("Output should mention server name 'test-server', got: %s", outputStr)
	}

	// Should mention the missing required fields
	if !strings.Contains(outputStr, "query") {
		t.Errorf("Output should mention missing field 'query', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "max_results") {
		t.Errorf("Output should mention missing field 'max_results', got: %s", outputStr)
	}

	// Should guide the LLM to correct the arguments
	if !strings.Contains(outputStr, "correct") && !strings.Contains(outputStr, "try again") {
		t.Errorf("Output should guide the LLM to fix the issue, got: %s", outputStr)
	}

	// Verify Errors slice contains the validation error
	if len(result.Errors) == 0 {
		t.Fatal("Expected non-empty Errors slice")
	}
	errorMsg := result.Errors[0]
	if !strings.Contains(errorMsg, "invalid arguments") {
		t.Errorf("Errors[0] should mention 'invalid arguments', got: %s", errorMsg)
	}

	// Verify metadata flags this as a validation error
	metadata := result.Metadata
	if validationError, ok := metadata["validation_error"].(bool); !ok || !validationError {
		t.Error("Expected metadata.validation_error to be true")
	}
}

// TestMCPToolWrapper_ValidateArgs_WrongType verifies that type mismatches are
// reported with field-specific messages.
func TestMCPToolWrapper_ValidateArgs_WrongType(t *testing.T) {
	t.Parallel()

	mcpTool := mcp.MCPTool{
		Name:        "search",
		Description: "Search for information",
		ServerName:  "test-server",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"required":             []interface{}{"query", "max_results"},
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type": "string",
				},
				"max_results": map[string]interface{}{
					"type": "number",
				},
			},
		},
	}

	wrapper := mcp.NewMCPToolWrapper(mcpTool, nil)

	// Provide query as a number (should be string) and max_results as a string (should be number)
	ctx := context.Background()
	result, err := wrapper.Execute(ctx, mcp.Parameters{
		Kwargs: map[string]interface{}{
			"query":       42,
			"max_results": "ten",
		},
	})

	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	if result.Success {
		t.Fatal("Expected Success=false for wrong argument types, got true")
	}

	outputStr, ok := result.Output.(string)
	if !ok {
		t.Fatalf("Expected Output to be a string, got %T", result.Output)
	}

	// Should report type errors for both fields
	if !strings.Contains(outputStr, "Validation failed") {
		t.Errorf("Output should mention 'Validation failed', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "query") {
		t.Errorf("Output should mention field 'query', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "max_results") {
		t.Errorf("Output should mention field 'max_results', got: %s", outputStr)
	}

	// Should contain type-related language
	hasTypeMention := strings.Contains(outputStr, "string") ||
		strings.Contains(outputStr, "number") ||
		strings.Contains(outputStr, "type")
	if !hasTypeMention {
		t.Errorf("Output should mention type mismatch, got: %s", outputStr)
	}
}

// TestMCPToolWrapper_ValidateArgs_NoSchema verifies that when no
// schema is present, validation passes and does not produce a
// validation error.  This test uses a context timeout to avoid
// blocking on the nil manager call.
func TestMCPToolWrapper_ValidateArgs_NoSchema(t *testing.T) {
	t.Parallel()

	mcpTool := mcp.MCPTool{
		Name:        "simple_tool",
		Description: "A tool with no schema",
		ServerName:  "test-server",
		// No InputSchema
	}

	wrapper := mcp.NewMCPToolWrapper(mcpTool, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// With no manager, CallTool will panic on nil receiver.
	// Recovery is needed, but the key assertion is that
	// no validation_error is in the metadata.
	didPanic := false
	var result *mcp.Result
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		result, err = wrapper.Execute(ctx, mcp.Parameters{
			Kwargs: map[string]interface{}{"foo": "bar"},
		})
	}()

	// A panic on nil manager is expected; the key point is that
	// validation did NOT add validation_error to metadata.
	if didPanic {
		// Nil manager caused panic — that's expected, validation passed.
		return
	}

	// If no panic (shouldn't happen with nil manager), verify no validation_error
	if result != nil && result.Metadata != nil {
		if val, exists := result.Metadata["validation_error"]; exists && val == true {
			t.Error("No schema should not produce a validation_error in metadata")
		}
	}

	// If there was an error (not a panic), ensure it's not validation-related
	if err != nil {
		if strings.Contains(err.Error(), "validation") {
			t.Errorf("Error should not be validation-related when no schema: %v", err)
		}
	}
}

// TestMCPToolWrapper_ValidateArgs_OutputUsableAsToolResult verifies that the
// validation error output is usable as a tool-result message that the LLM can
// self-correct from.
func TestMCPToolWrapper_ValidateArgs_OutputUsableAsToolResult(t *testing.T) {
	t.Parallel()

	mcpTool := mcp.MCPTool{
		Name:        "file_read",
		Description: "Read a file from disk",
		ServerName:  "fs",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"required":             []interface{}{"path"},
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File path to read",
				},
			},
		},
	}

	wrapper := mcp.NewMCPToolWrapper(mcpTool, nil)

	ctx := context.Background()
	result, err := wrapper.Execute(ctx, mcp.Parameters{
		Kwargs: map[string]interface{}{},
	})

	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}

	if result.Success {
		t.Fatal("Expected failure for missing required field")
	}

	outputStr, ok := result.Output.(string)
	if !ok {
		t.Fatalf("Expected Output to be a string, got %T", result.Output)
	}

	// The output should be a complete, self-contained message suitable for the LLM
	// to understand what went wrong without needing to look at the schema.
	if len(outputStr) < 20 {
		t.Errorf("Output too short to be useful: %q", outputStr)
	}

	// Should clearly identify the failing field
	if !strings.Contains(outputStr, "path") {
		t.Errorf("Output should clearly identify the failing field 'path': %s", outputStr)
	}

	// Should indicate that this is a validation issue (not a runtime error)
	if !strings.Contains(outputStr, "validation") && !strings.Contains(outputStr, "Validation") {
		t.Errorf("Output should indicate this is a validation issue: %s", outputStr)
	}

	// Print the full output for human review during development
	t.Logf("Tool result output:\n%s", outputStr)
}
