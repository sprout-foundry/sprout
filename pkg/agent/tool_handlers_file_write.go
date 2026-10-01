package agent

// tool_handlers_file_write.go — the write_file handler and its JSON /
// structured-content parsing + error-formatting helpers, split out of
// tool_handlers_file.go.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Tool handler implementations for file operations
func handleWriteFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
	}

	content, err := getRequiredString(args, "content")
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get content parameter")
	}

	// JSON writes are transparently routed through structured serialization/validation.
	if strings.EqualFold(filepath.Ext(path), ".json") {
		parsed, parseErr := parseStructuredJSONContent(content, "write_file")
		if parseErr != nil {
			return "", agenterrors.NewTool("write_file", "write_file JSON forwarding failed", parseErr).WithDetail("path", path)
		}
		return handleWriteStructuredFile(ctx, a, map[string]interface{}{
			"path":   path,
			"format": "json",
			"data":   parsed,
		})
	}

	return writeFileContent(ctx, a, path, content, "write_file", false)
}

func parseStructuredJSONContent(content string, callerTool string) (interface{}, error) {
	// Use ParseJSONOrderedAny to preserve key insertion order from the source
	// text. Standard json.Unmarshal loses ordering, which causes edit_file's
	// JSON normalization step to rewrite files with scrambled key order.
	parsed, err := ParseJSONOrderedAny(content)
	if err != nil {
		return nil, formatJSONParseError(content, err, callerTool)
	}
	switch parsed.(type) {
	case *OrderedMap, []interface{}:
		return parsed, nil
	default:
		return nil, agenterrors.NewInvalidInputError("top-level JSON must be an object or array", nil)
	}
}

func formatJSONParseError(content string, err error, callerTool string) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError

	offset := int64(-1)
	switch {
	case errors.As(err, &syntaxErr):
		offset = syntaxErr.Offset
	case errors.As(err, &typeErr):
		offset = typeErr.Offset
	}

	if offset <= 0 {
		return agenterrors.Wrapf(err, "invalid JSON; next_step=%s", sameToolJSONFixHint(callerTool))
	}

	line, col := lineColFromOffset(content, offset)
	snippet := snippetAtLine(content, line)
	if snippet == "" {
		return agenterrors.Wrapf(err, "invalid JSON at line=%d col=%d; next_step=%s", line, col, sameToolJSONFixHint(callerTool))
	}

	return agenterrors.Wrapf(err, "invalid JSON at line=%d col=%d; snippet=%q; next_step=%s", line, col, snippet, sameToolJSONFixHint(callerTool))
}

func sameToolJSONFixHint(callerTool string) string {
	switch strings.TrimSpace(callerTool) {
	case "edit_file":
		return "fix JSON syntax and retry edit_file so resulting file is valid JSON"
	default:
		return "fix JSON syntax and retry write_file with valid JSON object/array content"
	}
}

func lineColFromOffset(content string, offset int64) (line int, col int) {
	if offset < 1 {
		return 1, 1
	}
	line = 1
	col = 1
	max := int64(len(content))
	if offset > max+1 {
		offset = max + 1
	}
	for i := int64(0); i < offset-1 && i < max; i++ {
		if content[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func snippetAtLine(content string, line int) string {
	if line < 1 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if line > len(lines) {
		return ""
	}
	snippet := strings.TrimSpace(lines[line-1])
	if len(snippet) > 120 {
		return snippet[:120] + "..."
	}
	return snippet
}

func writeFileContent(ctx context.Context, a *Agent, path, content, toolName string, allowStructured bool) (string, error) {
	if !allowStructured {
		if err := disallowRawStructuredWrite(path, toolName); err != nil {
			return "", agenterrors.Wrap(err, "failed to validate structured write")
		}
	}

	// Staleness rule: refuse the write if the agent hasn't read
	// the file this turn, or if it was modified after the last read.
	// Returning the error before any side effects lets the agent react
	// (re-read, then retry write) without leaving partial state.
	if err := a.checkWriteStaleness(path); err != nil {
		return "", err
	}

	// Route through diff-approval gate when enabled.
	if a.ShouldGateEdit(path) {
		original, readErr := tools.ReadFile(ctx, path)
		if readErr != nil && !os.IsNotExist(readErr) {
			a.Logger().Debug("edit-approval: could not read original for %s: %v\n", path, readErr)
		} else {
			proposal := EditProposal{
				Path: path, Original: original, Proposed: content,
			}
			approved, summary, appErr := a.RequestEditApproval(ctx, proposal)
			if appErr != nil {
				return "", agenterrors.NewApproval("edit-approval failed", map[string]any{"path": path}).WithDetail("cause", appErr.Error())
			}
			content = approved
			a.Logger().Debug("edit-approval: %s\n", summary)
		}
	}

	if err := swallowedCommentError(content, path); err != nil {
		return "", err
	}

	if warning := validateJSONContent(content, path); warning != "" {
		a.Logger().Debug("%s\n", warning)
	}

	a.Logger().Debug("Writing file: %s\n", path)

	// Pre-write read: the tracker stores this as OriginalCode for
	// recovery. Must happen before tools.WriteFile mutates the file.
	var preWriteOriginal string
	if preData, preErr := os.ReadFile(path); preErr == nil {
		preWriteOriginal = string(preData)
	}
	if trackErr := a.TrackFileWrite(path, preWriteOriginal, content); trackErr != nil {
		a.Logger().Debug("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(ctx, path, content)

	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "write_file", path, "", err); approved {
			result, err = tools.WriteFile(ctx2, path, content)
		}
	}

	a.Logger().Debug("Write file result: %s, error: %v\n", result, err)

	// Invalidate cached file metadata when file is successfully written
	// This prevents stale line counts from misleading the model
	if err == nil && a.state.GetOptimizer() != nil {
		a.state.GetOptimizer().InvalidateFile(path)
	}

	// Publish file change event for web UI auto-sync
	if err == nil {
		a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "write", content))
		a.Logger().Debug("Published file_changed event: %s (write)\n", path)

		// Publish workspace_patch for real-time browser sync
		seq := nextPatchSeq()
		conflict, theirsPath := a.CheckPatchConflict(path)
		if conflict {
			a.publishEvent(events.EventTypeWorkspacePatch, events.WorkspacePatchEvent(path, content, "write", seq, events.PatchConflictInfo{Conflict: true, TheirsPath: theirsPath}))
			a.Logger().Debug("Published workspace_patch event with conflict: %s (seq=%d, theirs=%s)\n", path, seq, theirsPath)
		} else {
			a.publishEvent(events.EventTypeWorkspacePatch, events.WorkspacePatchEvent(path, content, "write", seq))
			a.Logger().Debug("Published workspace_patch event: %s (seq=%d)\n", path, seq)
		}

		// Check for security concerns in the written content
		a.CheckFileContentSecurity(path, content)
	}

	// Start async validation (fire-and-forget)
	if a.validator != nil {
		a.validator.RunAsyncValidation(ctx, path, content)
	}

	if err != nil {
		return "", agenterrors.NewTool("write_file", "failed to write file", err).WithDetail("path", path)
	}
	return result, nil
}
