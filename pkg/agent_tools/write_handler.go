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

// writeFileHandler implements ToolHandler for the write_file tool.
type writeFileHandler struct{}

func (h *writeFileHandler) Name() string {
	return "write_file"
}

func (h *writeFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "write_file",
		Description: "Write content to a file",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the file to write",
			},
			{
				Name:        "content",
				Type:        "string",
				Required:    true,
				Description: "Content to write to the file",
			},
		},
		Required: []string{"path", "content"},
	}
}

func (h *writeFileHandler) Validate(args map[string]any) error {
	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return agenterrors.NewValidation("parameter 'path' must not be empty", nil)
	}

	content, err := extractString(args, "content")
	if err != nil {
		return err
	}
	_ = content // content is validated by the write function itself

	return nil
}

func (h *writeFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// Wire the agent's filesystem gate into ctx so WriteFile's resolve
	// step surfaces the approve / session-allow / elevate dialog on
	// off-workspace paths. See FilesystemGate in handler.go and
	// withFilesystemApproval in filesystem_gate.go.
	ctx = WithFilesystemGateFromEnv(ctx, env)

	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	content, err := extractString(args, "content")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-127 M2: Gate 1 precheck. Consult the classifier before the
	// resolve so Deny paths return a typed error immediately and Allow
	// paths bypass withFilesystemApproval entirely (the path is already
	// workspace/tmp/allowlisted).
	_, decision, _ := PrecheckFileAccess(env.FileAccessClassifier, "write_file", path)
	if decision == "deny" {
		return ToolResult{Output: fmt.Sprintf("write blocked: %s is declared read_only in the active workflow's allowed_paths", path), IsError: true},
			fmt.Errorf("write blocked: %s is declared read_only", path)
	}
	if decision == "allow" {
		// Path is workspace/tmp/allowlisted — bypass the gate and resolve directly.
		ctx = filesystem.WithSecurityBypass(ctx)
	}

	// SP-046-2: Check staleness before writing
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	result, err := WriteFile(ctx, path, content)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, agenterrors.NewTool("write_file", fmt.Sprintf("write file %q: %v", path, err), err)
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

func (h *writeFileHandler) Aliases() []string      { return nil }
func (h *writeFileHandler) Timeout() time.Duration { return 0 }
func (h *writeFileHandler) MaxResultSize() int     { return 0 }
func (h *writeFileHandler) SafeForParallel() bool  { return false }
func (h *writeFileHandler) Interactive() bool      { return false }
