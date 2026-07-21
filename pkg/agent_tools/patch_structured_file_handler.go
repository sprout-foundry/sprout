package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type patchStructuredFileHandler struct{}

func (h *patchStructuredFileHandler) Name() string { return "patch_structured_file" }

func (h *patchStructuredFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "patch_structured_file",
		Description: "Apply JSON Patch operations (add/replace/remove/test) to JSON/YAML then write it back. Single-field patches produce minimal diffs with key order preservation.",
		Required:    []string{"path"},
		Parameters: []ParameterDef{
			{Name: "path", Type: "string", Required: true, Description: "Path to the structured file"},
			{Name: "patch_ops", Type: "array", Description: "JSON Patch operations array"},
			{Name: "data", Type: "object", Description: "Full-document payload (treated as write if no patch_ops)"},
			{Name: "format", Type: "string", Description: "Format override: json or yaml (inferred from extension)"},
			{Name: "schema", Type: "object", Description: "JSON Schema subset for validation after patch"},
		},
	}
}

func (h *patchStructuredFileHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "path")
	return err
}

func (h *patchStructuredFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// patch_structured_file previously bypassed the FilesystemGate
	// entirely (raw os.ReadFile / os.WriteFile), so off-workspace
	// patches hard-errored on the live seed dispatch path even
	// though write_file / edit_file / read_file prompted. Wire the
	// env's gate into ctx so both the read (resolve-existing) and
	// the write (resolve-target) flows consult it.
	ctx = WithFilesystemGateFromEnv(ctx, env)

	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-046-2: Check staleness before patching
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

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

	// If data is provided without patch_ops, treat as full-document write.
	// A single approval covers both read-existing-perms (for permission
	// preservation) and write — the gate is consulted exactly once and
	// the canonical path is reused for both ops, eliminating the
	// time-of-check/time-of-use gap where the read sees one symlink
	// target and the write sees another.
	if data != nil && len(patchOps) == 0 {
		content, err := serializeStructuredContent(format, data)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to serialize data: %v", err), IsError: true}, nil
		}
		writePath, filePerm, err := resolveForWriteWithGate(ctx, path)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}, nil
		}
		if err := os.WriteFile(writePath, []byte(content), filePerm); err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to write file: %v", err), IsError: true}, nil
		}
		return ToolResult{Output: fmt.Sprintf("File %s written successfully", path)}, nil
	}

	// Read existing file via a single gate-aware resolve. The
	// resolved path is reused for the subsequent write to avoid
	// TOCTOU between read and write (a symlink swap in between
	// would otherwise make read and write target different files).
	readPath, err := resolveForReadWithGate(ctx, path)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}, nil
	}
	fileBytes, err := os.ReadFile(readPath)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to read file %s: %v", path, err), IsError: true}, nil
	}
	fileContent := string(fileBytes)

	// Parse existing document into yaml.Node to preserve key order
	docNode, err := parseToYamlNode(format, fileContent)
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
			docNode, err = applyPatchOperationNode(docNode, op)
			if err != nil {
				return ToolResult{Output: fmt.Sprintf("Failed to apply patch operation %s: %v", op.Op, err), IsError: true}, nil
			}
		}
	} else {
		return ToolResult{Output: fmt.Sprintf("Either 'patch_ops' or 'data' must be provided")}, nil
	}

	// Write back the modified document at the SAME canonical path
	// already resolved for reading. Preserves existing file
	// permissions when the file already exists (matches WriteFile's
	// behavior); falls back to 0644 only for genuinely new files.
	content, err := serializeYamlNode(format, docNode)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to serialize modified document: %v", err), IsError: true}, nil
	}
	filePerm := os.FileMode(0644)
	if fi, statErr := os.Stat(readPath); statErr == nil {
		filePerm = fi.Mode() & 0777
	}
	if err := os.WriteFile(readPath, []byte(content), filePerm); err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to write file: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: fmt.Sprintf("Successfully patched %s with %d operation(s)", path, len(patchOps))}, nil
}

// resolveForReadWithGate wraps SafeResolvePathWithBypass in the
// approval gate so a single approval covers the read. Returns the
// canonical path the caller should use to read.
func resolveForReadWithGate(ctx context.Context, path string) (string, error) {
	return withFilesystemApproval(ctx, FilesystemGateFromContext(ctx), "patch_structured_file", path,
		func(ctx context.Context) (string, error) {
			return filesystem.SafeResolvePathWithBypass(ctx, path)
		},
	)
}

// resolveForWriteWithGate wraps SafeResolvePathForWriteWithBypass in
// the approval gate so a single approval covers the write. Returns
// the canonical path the caller should use plus the file mode the
// existing file uses (so the caller can preserve permissions
// without an additional stat that would race with the gate
// approval).
func resolveForWriteWithGate(ctx context.Context, path string) (string, os.FileMode, error) {
	type result struct {
		path string
		perm os.FileMode
	}
	res, err := withFilesystemApproval(ctx, FilesystemGateFromContext(ctx), "patch_structured_file", path,
		func(ctx context.Context) (result, error) {
			writePath, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, path)
			if err != nil {
				return result{}, err
			}
			perm := os.FileMode(0644)
			if fi, statErr := os.Stat(writePath); statErr == nil {
				perm = fi.Mode() & 0777
			}
			return result{path: writePath, perm: perm}, nil
		},
	)
	if err != nil {
		return "", 0, err
	}
	return res.path, res.perm, nil
}

func (h *patchStructuredFileHandler) Aliases() []string      { return nil }
func (h *patchStructuredFileHandler) Timeout() time.Duration { return 0 }
func (h *patchStructuredFileHandler) MaxResultSize() int     { return 0 }
func (h *patchStructuredFileHandler) SafeForParallel() bool  { return false }
func (h *patchStructuredFileHandler) Interactive() bool      { return false }
