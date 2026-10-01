package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

func handleWriteStructuredFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return "", agenterrors.NewInvalidInputError("unsupported structured format: use json or yaml", nil)
	}

	data, exists := args["data"]
	if !exists {
		return "", agenterrors.NewInvalidInputError("parameter 'data' is required", nil)
	}

	// Convert data to *OrderedMap for ordered serialization.
	// If the caller already provides an *OrderedMap (future-proofing), use it directly.
	// Otherwise, convert map[string]interface{} → OrderedMapFromMap (alphabetical order).
	switch d := data.(type) {
	case *OrderedMap:
		// Already ordered — use as-is.
	case map[string]interface{}:
		data = OrderedMapFromMap(d)
	default:
		// For non-object types (arrays, scalars), pass through — the
		// ordered serializers handle these via their fallback paths.
	}

	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return "", agenterrors.NewTool("structured", "failed to parse schema", err)
		}
		if errs := validateDataAgainstSchema(data, schema, "$"); len(errs) > 0 {
			return "", formatStructuredValidationError("write_structured_file", errs, "")
		}
	}

	content, err := serializeStructuredContent(format, data)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to serialize structured content", err)
	}

	result, err := writeFileContent(ctx, a, path, content, "write_structured_file", true)
	if err != nil {
		return "", agenterrors.NewTool("structured", fmt.Sprintf("failed to write structured file %s", path), err)
	}
	return result, nil
}

func handlePatchStructuredFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
	}

	opsRaw, ok := args["patch_ops"]
	if !ok {
		// Compatibility path: some models call patch_structured_file with full `data`
		// instead of patch operations. Treat this as a structured write.
		if data, hasData := args["data"]; hasData {
			writeArgs := map[string]interface{}{
				"path": path,
				"data": data,
			}
			if format, hasFormat := args["format"]; hasFormat {
				writeArgs["format"] = format
			}
			if schema, hasSchema := args["schema"]; hasSchema {
				writeArgs["schema"] = schema
			}
			return handleWriteStructuredFile(ctx, a, writeArgs)
		}
		return "", agenterrors.NewInvalidInputError("parameter 'patch_ops' is required (or provide 'data' for full write)", nil)
	}

	format := inferStructuredFormat(path, getOptionalString(args, "format"))
	if format == "" {
		return "", agenterrors.NewInvalidInputError("unsupported structured format: use json or yaml", nil)
	}

	resolvedPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "patch_structured_file", path, "", err); approved {
			resolvedPath, err = filesystem.SafeResolvePathWithBypass(ctx2, path)
		}
		if err != nil {
			return "", agenterrors.NewTool("structured", "failed to resolve file path", err)
		}
	}
	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to read structured file", err)
	}

	doc, err := deserializeStructuredContent(format, string(contentBytes))
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to parse structured content", err)
	}

	ops, err := parsePatchOperations(opsRaw)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to parse patch operations", err)
	}

	applied := 0
	for i, op := range ops {
		doc, err = applyPatchOperation(doc, op)
		if err != nil {
			return "", agenterrors.Wrapf(err, "patch operation failed: tool=patch_structured_file index=%d op=%s path=%s applied=%d/%d",
				i, op.Op, op.Path, applied, len(ops))
		}
		applied++
	}

	if schemaRaw, ok := args["schema"]; ok && schemaRaw != nil {
		schema, err := toSchemaMap(schemaRaw)
		if err != nil {
			return "", agenterrors.NewTool("structured", "failed to parse schema", err)
		}
		if errs := validateDataAgainstSchema(doc, schema, "$"); len(errs) > 0 {
			context := fmt.Sprintf("applied=%d/%d", applied, len(ops))
			return "", formatStructuredValidationError("patch_structured_file", errs, context)
		}
	}

	updated, err := serializeStructuredContent(format, doc)
	if err != nil {
		return "", agenterrors.NewTool("structured", "failed to serialize updated content", err)
	}

	result, err := writeFileContent(ctx, a, path, updated, "patch_structured_file", true)
	if err != nil {
		return "", agenterrors.NewTool("structured", fmt.Sprintf("failed to write patched file %s", path), err)
	}
	return result, nil
}

func inferStructuredFormat(path, provided string) string {
	format := strings.ToLower(strings.TrimSpace(provided))
	switch format {
	case "json", "yaml", "yml":
		if format == "yml" {
			return "yaml"
		}
		return format
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return ""
	}
}

func serializeStructuredContent(format string, data interface{}) (string, error) {
	switch format {
	case "json":
		return SerializeJSONOrdered(data)
	case "yaml":
		return SerializeYAMLOrdered(data)
	default:
		return "", agenterrors.NewInvalidInputError("unsupported format: "+format, nil)
	}
}

func deserializeStructuredContent(format, content string) (interface{}, error) {
	switch format {
	case "json":
		return ParseJSONOrdered(content)
	case "yaml":
		return ParseYAMLOrdered(content)
	default:
		return nil, agenterrors.NewInvalidInputError("unsupported format: "+format, nil)
	}
}

func toSchemaMap(v interface{}) (map[string]interface{}, error) {
	schema, ok := v.(map[string]interface{})
	if !ok {
		return nil, agenterrors.NewInvalidInputError("parameter 'schema' must be an object", nil)
	}
	return schema, nil
}

func getOptionalString(args map[string]interface{}, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return str
}
