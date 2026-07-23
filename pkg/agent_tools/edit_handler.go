package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// editFileHandler implements ToolHandler for the edit_file tool.
type editFileHandler struct{}

func (h *editFileHandler) Name() string {
	return "edit_file"
}

func (h *editFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "edit_file",
		Description: "Edit a file by replacing old string with new string",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the file to edit",
			},
			{
				Name:        "old_str",
				Type:        "string",
				Required:    true,
				Description: "String to replace",
			},
			{
				Name:        "new_str",
				Type:        "string",
				Required:    true,
				Description: "Replacement string",
			},
		},
		Required: []string{"path", "old_str", "new_str"},
	}
}

func (h *editFileHandler) Validate(args map[string]any) error {
	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return agenterrors.NewValidation("parameter 'path' must not be empty", nil)
	}

	oldStr, err := extractString(args, "old_str")
	if err != nil {
		return err
	}
	if strings.TrimSpace(oldStr) == "" {
		return agenterrors.NewValidation("parameter 'old_str' must not be empty", nil)
	}

	newStr, err := extractString(args, "new_str")
	if err != nil {
		return err
	}
	_ = newStr // new_str can be empty (replacing with nothing)

	return nil
}

func (h *editFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// SP-127 M2: Gate 1 precheck. Consult the classifier before the
	// resolve so Deny paths return a typed error immediately and Allow
	// paths bypass the gate entirely. Prompt paths fall through and will
	// fail with the raw filesystem error.

	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	oldStr, err := extractString(args, "old_str")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	newStr, err := extractString(args, "new_str")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-127 M2: Gate 1 precheck. Consult the classifier before the
	// resolve so Deny paths return a typed error immediately and Allow
	// paths bypass the gate entirely.
	_, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "edit_file", path)
	if decision == "deny" {
		return ToolResult{Output: fmt.Sprintf("edit blocked: %s is declared read_only in the active workflow's allowed_paths", path), IsError: true},
			fmt.Errorf("edit blocked: %s is declared read_only", path)
	}
	if decision == "allow" {
		// Path is workspace/tmp/allowlisted — bypass the gate and resolve directly.
		ctx = filesystem.WithSecurityBypass(ctx)
	}

	// SP-046-2: Check staleness before editing
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	result, err := EditFile(ctx, path, oldStr, newStr)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, agenterrors.NewTool("edit_file", fmt.Sprintf("edit file %q: %v", path, err), err)
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

func (h *editFileHandler) Aliases() []string      { return nil }
func (h *editFileHandler) Timeout() time.Duration { return 0 }
func (h *editFileHandler) MaxResultSize() int     { return 0 }
func (h *editFileHandler) SafeForParallel() bool  { return false }
func (h *editFileHandler) Interactive() bool      { return false }
