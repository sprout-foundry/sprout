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
	// SP-127 M2: Gate 1 precheck. Consult the classifier before the
	// resolve so Deny paths return a typed error immediately and Allow
	// paths resolve directly with bypass. Prompt paths fall through and will
	// fail with the raw filesystem error.

	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-046-2: Check staleness before read/write
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-127 M2: Gate 1 precheck. Consult the classifier before the
	// resolve so Deny paths return a typed error immediately and Allow
	// paths resolve directly with bypass.
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "patch_structured_file", path)
	if decision == "deny" {
		return ToolResult{Output: fmt.Sprintf("patch blocked: %s is declared read_only in the active workflow's allowed_paths", path), IsError: true},
			fmt.Errorf("patch blocked: %s is declared read_only", path)
	}
	if decision == "allow" {
		// Path is workspace/tmp/allowlisted — resolve directly and bypass the gate.
		resolvedPath, err = filesystem.SafeResolvePathForWriteWithBypass(ctx, path)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}, nil
		}
		// Proceed to the full-document write or patch path below with the
		// pre-resolved path; set resolvedPath to a sentinel that signals
		// "use preRes" to the rest of the function. We encode this as a
		// non-empty string that the gate-aware paths will treat specially.
		// Actually, simpler: just continue with resolvedPath already set
		// and let the individual branches handle it.
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
		var writePath string
		var filePerm os.FileMode
		if decision == "allow" {
			// Already pre-resolved above with bypass context.
			writePath = resolvedPath
			filePerm = os.ModePerm
			if fi, statErr := os.Stat(writePath); statErr == nil {
				filePerm = fi.Mode() & 0777
			}
		} else {
			writePath, filePerm, err = resolveForWriteWithoutGate(ctx, path)
			if err != nil {
				return ToolResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}, nil
			}
		}
		if err := os.WriteFile(writePath, []byte(content), filePerm); err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to write file: %v", err), IsError: true}, nil
		}
		return ToolResult{Output: fmt.Sprintf("File %s written successfully", path)}, nil
	}

	// Read existing file via a single resolve. The resolved path is
	// reused for the subsequent write to avoid TOCTOU between read and
	// write (a symlink swap in between would otherwise make read and
	// write target different files).
	var readPath string
	if decision == "allow" {
		// Already pre-resolved above with bypass context.
		readPath = resolvedPath
	} else {
		readPath, err = resolveForReadWithoutGate(ctx, path)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("Failed to resolve path: %v", err), IsError: true}, nil
		}
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

// resolveForReadWithoutGate resolves the path for reading without the gate.
// Returns the canonical path the caller should use to read.
func resolveForReadWithoutGate(ctx context.Context, path string) (string, error) {
	return filesystem.SafeResolvePathWithBypass(ctx, path)
}

// resolveForWriteWithoutGate resolves the path for writing without the gate.
// Returns the canonical path the caller should use plus the file mode the
// existing file uses (so the caller can preserve permissions).
func resolveForWriteWithoutGate(ctx context.Context, path string) (string, os.FileMode, error) {
	writePath, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, path)
	if err != nil {
		return "", 0, err
	}
	perm := os.FileMode(0644)
	if fi, statErr := os.Stat(writePath); statErr == nil {
		perm = fi.Mode() & 0777
	}
	return writePath, perm, nil
}

func (h *patchStructuredFileHandler) Aliases() []string      { return nil }
func (h *patchStructuredFileHandler) Timeout() time.Duration { return 0 }
func (h *patchStructuredFileHandler) MaxResultSize() int     { return 0 }
func (h *patchStructuredFileHandler) SafeForParallel() bool  { return false }
func (h *patchStructuredFileHandler) Interactive() bool      { return false }
