package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type patchStructuredFileHandler struct{}

func (h *patchStructuredFileHandler) Name() string { return "patch_structured_file" }

func (h *patchStructuredFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "patch_structured_file",
		Description: "Apply JSON Patch operations (add/replace/remove/test) to existing JSON/YAML then write it back",
		Required:    []string{"path"},
		Parameters: []ParameterDef{
			{Name: "path", Type: "string", Required: true, Description: "Path to the structured file to patch"},
			{Name: "patch_ops", Type: "array", Description: "JSON Patch operations array"},
			{Name: "data", Type: "object", Description: "Optional full-document structured payload; if provided without patch_ops, this call is treated as write_structured_file"},
			{Name: "format", Type: "string", Description: "Optional format override: json or yaml (otherwise inferred from extension)"},
			{Name: "schema", Type: "object", Description: "Optional JSON Schema subset used to validate document after patch"},
		},
	}
}

func (h *patchStructuredFileHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "path")
	return err
}

func (h *patchStructuredFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	path, _ := extractString(args, "path")
	format, _ := extractString(args, "format")

	// Infer format from file extension if not provided
	inferredFormat := inferStructuredFormat(path, format)
	if inferredFormat == "" {
		return ToolResult{Output: fmt.Sprintf("Unsupported file format for %s. Use .json, .yaml, or .yml extensions, or specify 'format' parameter", path), IsError: true}, nil
	}
	if format == "" {
		format = inferredFormat
	}

	// Extract optional patch_ops
	var patchOps []any
	if po, ok := args["patch_ops"]; ok && po != nil {
		if arr, ok := po.([]interface{}); ok {
			patchOps = arr
		}
	}

	// Extract optional data (for full-document write mode)
	var data map[string]any
	if d, ok := args["data"]; ok && d != nil {
		if m, ok := d.(map[string]interface{}); ok {
			data = make(map[string]any)
			for k, v := range m {
				data[k] = v
			}
		}
	}

	// If data is provided without patch_ops, treat as full-document write
	if data != nil && len(patchOps) == 0 {
		content, err := serializeStructuredContent(format, data)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to serialize data: %v", err), IsError: true}, nil
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to write file: %v", err), IsError: true}, nil
		}
		return ToolResult{Output: fmt.Sprintf("File %s written successfully", path)}, nil
	}

	// Read existing file
	fileContent, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to read file %s: %v", path, err), IsError: true}, nil
	}

	// Parse existing document
	doc, err := deserializeStructuredContent(format, string(fileContent))
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to parse file %s: %v", path, err), IsError: true}, nil
	}

	// Apply patch operations if provided
	if len(patchOps) > 0 {
		ops, err := parsePatchOperations(patchOps)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Invalid patch operations: %v", err), IsError: true}, nil
		}
		for _, op := range ops {
			doc, err = applyPatchOperation(doc, op)
			if err != nil {
				return ToolResult{Output: fmt.Sprintf("Failed to apply patch operation %s: %v", op.Op, err), IsError: true}, nil
			}
		}
	} else {
		return ToolResult{Output: fmt.Sprintf("Either 'patch_ops' or 'data' must be provided")}, nil
	}

	// Write back the modified document
	content, err := serializeStructuredContent(format, doc)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to serialize modified document: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to write file: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: fmt.Sprintf("Successfully patched %s with %d operation(s)", path, len(patchOps))}, nil
}
