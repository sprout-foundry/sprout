package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// NOTE: This handler is part of the SP-038-4 migration. The old implementation
// lives in pkg/agent/tool_handlers_structured.go:handleWriteStructuredFile().
// Once the migration is complete, the old implementation can be removed.

// writeStructuredFileHandler implements ToolHandler for the write_structured_file tool.
type writeStructuredFileHandler struct{}

func (h *writeStructuredFileHandler) Name() string {
	return "write_structured_file"
}

func (h *writeStructuredFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "write_structured_file",
		Description: "Write schema-validated structured data to JSON/YAML with deterministic formatting. Key insertion order is preserved — fields appear in the file in the order you provide them, producing predictable diffs.",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the structured file to write",
			},
			{
				Name:        "format",
				Type:        "string",
				Required:    false,
				Description: "Optional format override: json or yaml (otherwise inferred from extension)",
			},
			{
				Name:        "data",
				Type:        "object",
				Required:    true,
				Description: "Structured data object/array to serialize",
			},
			{
				Name:        "schema",
				Type:        "object",
				Required:    false,
				Description: "Optional JSON Schema subset used to validate data before writing",
			},
		},
		Required: []string{"path", "data"},
	}
}

func (h *writeStructuredFileHandler) Validate(args map[string]any) error {
	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return agenterrors.NewValidation("parameter 'path' must not be empty", nil)
	}

	// Check that 'data' exists (required)
	if _, exists := args["data"]; !exists {
		return agenterrors.NewValidation("parameter 'data' is required", nil)
	}

	// Validate format if provided
	if fmtRaw, exists := args["format"]; exists && fmtRaw != nil {
		if fmtStr, ok := fmtRaw.(string); ok {
			if strings.TrimSpace(fmtStr) == "" {
				return agenterrors.NewValidation("parameter 'format' must not be empty if provided", nil)
			}
		}
	}

	return nil
}

func (h *writeStructuredFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return ToolResult{Output: "unsupported structured format: use json or yaml", IsError: true},
			agenterrors.NewValidation("unsupported structured format: use json or yaml", nil)
	}

	data, exists := args["data"]
	if !exists {
		return ToolResult{Output: "parameter 'data' is required", IsError: true},
			agenterrors.NewValidation("parameter 'data' is required", nil)
	}

	// Validate against schema if provided
	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("failed to parse schema: %v", err), IsError: true}, err
		}
		if errs := validateDataAgainstSchema(data, schema, "$"); len(errs) > 0 {
			return ToolResult{Output: formatStructuredValidationError("write_structured_file", errs, "").Error(), IsError: true},
				formatStructuredValidationError("write_structured_file", errs, "")
		}
	}

	// SP-082-1: Preserve key insertion order from the LLM's original JSON.
	// When RawArgsJSON is available, parse the "data" sub-object directly from
	// the source text into a *yaml.Node (which preserves map key order), then
	// serialize that.  Fall back to converting args data through mapToYamlNode
	// when RawArgsJSON is absent (e.g., unit tests that construct args as Go maps).
	var content string
	if env.RawArgsJSON != "" {
		content, err = serializeWithOrder(env.RawArgsJSON, format)
	} else {
		node := mapToYamlNode(data)
		content, err = serializeYamlNode(format, node)
	}
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("failed to serialize structured content: %v", err), IsError: true}, err
	}

	// Publish tool start event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   "write_structured_file",
			"path":   path,
			"format": format,
		})
	}

	result, err := WriteFile(ctx, path, content)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, agenterrors.NewTool("write_structured_file", fmt.Sprintf("write structured file %q: %v", path, err), err)
	}

	// Publish tool end event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":   "write_structured_file",
			"path":   path,
			"bytes":  len(content),
			"tokens": estimateTokenUsage(result),
		})
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

func (h *writeStructuredFileHandler) Aliases() []string         { return nil }
func (h *writeStructuredFileHandler) Timeout() time.Duration    { return 0 }
func (h *writeStructuredFileHandler) MaxResultSize() int        { return 0 }
func (h *writeStructuredFileHandler) SafeForParallel() bool     { return false }
func (h *writeStructuredFileHandler) Interactive() bool         { return false }

// getOptionalString extracts an optional string from args, returning "" if not present.
func getOptionalString(args map[string]any, key string) string {
	if val, ok := args[key]; ok && val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
