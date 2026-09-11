package tools

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
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
	// SP-127 M2: Gate 1 precheck. Consult the classifier before the
	// resolve so Deny paths return a typed error immediately and Allow
	// paths resolve directly with bypass (the path is already
	// workspace/tmp/allowlisted). Prompt paths fall through and will
	// fail with the raw filesystem error.

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
	// paths bypass the gate entirely (the path is already
	// workspace/tmp/allowlisted).
	resolvedWrite, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "write_file", path)
	if decision == "deny" {
		return ToolResult{Output: fmt.Sprintf("write blocked: %s is declared read_only in the active workflow's allowed_paths", path), IsError: true},
			agenterrors.NewPermission(fmt.Sprintf("write blocked: %s is declared read_only", path), nil)
	}
	if decision == "allow" {
		// Path is workspace/tmp/allowlisted — bypass the gate and resolve directly.
		ctx = filesystem.WithSecurityBypass(ctx)
	}
	// "prompt" → interactive approval; on deny fall through to the raw error.
	if decision == "prompt" {
		if ctx2, approved := promptForOffWorkspacePath(ctx, env, "write_file", path, resolvedWrite, "write"); approved {
			ctx = ctx2
		}
	}

	// SP-046-2: Check staleness before writing
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Capture pre-write content for change tracking. Must happen BEFORE
	// WriteFile mutates the file: reading afterwards would store the
	// post-write bytes as the "original" and make recovery a no-op.
	// Only read when a tracker is present. A read miss (file does not
	// exist yet) is the create case: original stays empty.
	var preWriteOriginal string
	if env.ResolveToolFuncs().TrackFileWrite != nil {
		if data, readErr := os.ReadFile(path); readErr == nil {
			preWriteOriginal = string(data)
		}
	}

	result, err := WriteFile(ctx, path, content)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, agenterrors.NewTool("write_file", fmt.Sprintf("write file %q: %v", path, err), err)
	}

	// Session change tracking: record the mutation with the agent's
	// ChangeTracker (powers the Agent Changes panel, /api/changes/*, and
	// revert tooling). Best-effort — a tracking failure must not fail the
	// write itself. Nil func = no tracker (standalone handler use).
	if fn := env.ResolveToolFuncs().TrackFileWrite; fn != nil {
		if trackErr := fn(path, preWriteOriginal, content); trackErr != nil {
			log.Printf("[write_file] change tracking failed for %q: %v", path, trackErr)
		}
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
