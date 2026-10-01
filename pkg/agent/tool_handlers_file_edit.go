package agent

// tool_handlers_file_edit.go — the edit_file handler and the shared
// arg-extraction / JSON-validation helpers, split out of
// tool_handlers_file.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// Tool handler implementations for file operations
func handleEditFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
	}

	oldStr, err := getRequiredString(args, "old_str")
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get old_str parameter")
	}

	newStr, err := getRequiredString(args, "new_str")
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get new_str parameter")
	}

	if err := swallowedCommentError(newStr, path); err != nil {
		return "", err
	}

	if warning := validateJSONContent(newStr, path); warning != "" {
		a.Logger().Debug("%s\n", warning)
	}

	// Read original for diff, handling filesystem security errors
	originalContent, err := tools.ReadFile(ctx, path)
	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "edit_file", path, "", err); approved {
			ctx = ctx2 // reuse bypassed context for subsequent operations
			originalContent, err = tools.ReadFile(ctx, path)
		}
	}
	if err != nil {
		return "", agenterrors.NewTool("edit_file", "failed to read original file for diff", err).WithDetail("path", path)
	}

	a.Logger().Debug("Editing file: %s\n", path)
	a.Logger().Debug("Old string: %s\n", oldStr)
	a.Logger().Debug("New string: %s\n", newStr)

	// Route through diff-approval gate when enabled.
	if a.ShouldGateEdit(path) {
		proposedContent := strings.Replace(originalContent, oldStr, newStr, 1)
		proposal := EditProposal{Path: path, Original: originalContent, Proposed: proposedContent}
		approved, summary, appErr := a.RequestEditApproval(ctx, proposal)
		if appErr != nil {
			return "", agenterrors.NewApproval("edit-approval failed", map[string]any{"path": path}).WithDetail("cause", appErr.Error())
		}
		if approved != proposedContent {
			a.Logger().Debug("edit-approval: %s\n", summary)
			a.Logger().Debug("edit-approval modified content for %s: %s\n", path, summary)
			if trackErr := a.TrackFileWrite(path, originalContent, approved); trackErr != nil {
				a.Logger().Debug("Warning: Failed to track approved write: %v\n", trackErr)
			}
			writeResult, writeErr := tools.WriteFile(ctx, path, approved)
			if writeErr != nil {
				return "", agenterrors.NewTool("edit_file", "failed to write approved content", writeErr).WithDetail("path", path)
			}
			a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", approved))
			if a.state.GetOptimizer() != nil {
				a.state.GetOptimizer().InvalidateFile(path)
			}
			return writeResult, nil
		}
		a.Logger().Debug("edit-approval: %s\n", summary)
	}

	// TrackFileEdit stores FULL file content (not fragments) so
	// recovery/rollback restores the complete file rather than a single
	// edit fragment. originalContent is the full file read above; the
	// proposed content is the single-occurrence replacement matching
	// tools.EditFile's first-match behaviour.
	proposedContent := strings.Replace(originalContent, oldStr, newStr, 1)
	if trackErr := a.TrackFileEdit(path, originalContent, proposedContent); trackErr != nil {
		a.Logger().Debug("Warning: Failed to track file edit: %v\n", trackErr)
	}

	result, err := tools.EditFile(ctx, path, oldStr, newStr)

	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "edit_file", path, "", err); approved {
			ctx = ctx2
			originalContent, err = tools.ReadFile(ctx, path)
			if err != nil {
				return "", agenterrors.NewTool("edit_file", "failed to read original file for diff", err).WithDetail("path", path)
			}
			result, err = tools.EditFile(ctx, path, oldStr, newStr)
		}
	}

	a.Logger().Debug("Edit file result: %s, error: %v\n", result, err)

	// Check for security concerns in the edited content
	if err == nil {
		a.CheckFileContentSecurity(path, newStr)
	}

	// JSON edits are transparently validated and normalized through structured writes.
	if err == nil && strings.EqualFold(filepath.Ext(path), ".json") {
		editedContent, readErr := tools.ReadFile(ctx, path)
		if readErr != nil {
			return "", agenterrors.NewTool("edit_file", "json edit succeeded but failed to read edited file", readErr).WithDetail("path", path)
		}
		// Record the re-read so the staleness check in handleWriteStructuredFile
		// sees an up-to-date readAt that is >= the edit's ModTime. Without this,
		// the JSON normalization write triggers a false-positive "file modified
		// after your last read_file" because the edit we just applied updated
		// ModTime to be newer than the read_file recorded at the start of this
		// turn.
		a.RecordFileReadThisTurn(path)
		parsed, parseErr := parseStructuredJSONContent(editedContent, "edit_file")
		if parseErr != nil {
			restoreErr := func() error {
				_, werr := tools.WriteFile(ctx, path, originalContent)
				return werr
			}()
			if restoreErr != nil {
				// Note: parseErr is included with %v for context but not wrapped - only restoreErr is the primary error
				return "", agenterrors.NewTool("edit_file", "edit would produce invalid JSON and restore failed", restoreErr).WithDetail("path", path).WithDetail("parse_error", parseErr.Error())
			}
			return "", agenterrors.NewTool("edit_file", "edit would produce invalid JSON", parseErr).WithDetail("path", path)
		}
		if _, werr := handleWriteStructuredFile(ctx, a, map[string]interface{}{
			"path":   path,
			"format": "json",
			"data":   parsed,
		}); werr != nil {
			return "", agenterrors.NewTool("edit_file", "json edit normalization failed", werr)
		}
	}

	// Invalidate cached file metadata when file is successfully edited
	// This prevents stale line counts from misleading the model
	if err == nil && a.state.GetOptimizer() != nil {
		a.state.GetOptimizer().InvalidateFile(path)
	}

	// Publish file change event for web UI auto-sync
	if err == nil {
		var eventContent string
		if eventContent, err = tools.ReadFile(ctx, path); err == nil {
			a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", eventContent))
			a.Logger().Debug("Published file_changed event: %s (edit)\n", path)

			// Publish workspace_patch for real-time browser sync
			seq := nextPatchSeq()
			conflict, theirsPath := a.CheckPatchConflict(path)
			if conflict {
				a.publishEvent(events.EventTypeWorkspacePatch, events.WorkspacePatchEvent(path, eventContent, "edit", seq, events.PatchConflictInfo{Conflict: true, TheirsPath: theirsPath}))
				a.Logger().Debug("Published workspace_patch event with conflict: %s (seq=%d, theirs=%s)\n", path, seq, theirsPath)
			} else {
				a.publishEvent(events.EventTypeWorkspacePatch, events.WorkspacePatchEvent(path, eventContent, "edit", seq))
				a.Logger().Debug("Published workspace_patch event: %s (seq=%d)\n", path, seq)
			}
		} else {
			a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", ""))
			a.Logger().Debug("Published file_changed event: %s (edit, no content)\n", path)

			// Publish workspace_patch for real-time browser sync (empty content since
			// the post-edit read failed).
			seq := nextPatchSeq()
			conflict, theirsPath := a.CheckPatchConflict(path)
			if conflict {
				a.publishEvent(events.EventTypeWorkspacePatch, events.WorkspacePatchEvent(path, "", "edit", seq, events.PatchConflictInfo{Conflict: true, TheirsPath: theirsPath}))
				a.Logger().Debug("Published workspace_patch event with conflict: %s (seq=%d, theirs=%s, empty content)\n", path, seq, theirsPath)
			} else {
				a.publishEvent(events.EventTypeWorkspacePatch, events.WorkspacePatchEvent(path, "", "edit", seq))
				a.Logger().Debug("Published workspace_patch event: %s (seq=%d, empty content)\n", path, seq)
			}
		}

		// Start async validation (fire-and-forget)
		if a.validator != nil {
			if content, readErr := tools.ReadFile(ctx, path); readErr == nil {
				a.validator.RunAsyncValidation(ctx, path, content)
			}
		}
	}

	// Display diff if successful
	if err == nil {
		newContent, readErr := tools.ReadFile(ctx, path)
		if readErr == nil {
			a.ShowColoredDiff(originalContent, newContent, 50)
		}
	}

	if err != nil {
		return "", agenterrors.NewTool("edit_file", "failed to edit file", err).WithDetail("path", path)
	}
	return result, nil
}

// Helper functions for file handlers

// getFilePath extracts file path from args, supporting both "path" (new) and "file_path" (legacy)
func getFilePath(args map[string]interface{}) (string, error) {
	if path, exists := args["path"]; exists {
		return convertToString(path, "path")
	}
	if filePath, exists := args["file_path"]; exists {
		return convertToString(filePath, "file_path")
	}
	return "", agenterrors.NewInvalidInputError("parameter 'path' is required", nil)
}

// getRequiredString extracts a required string parameter
func getRequiredString(args map[string]interface{}, key string) (string, error) {
	val, exists := args[key]
	if !exists {
		return "", agenterrors.NewValidation("parameter '"+key+"' is required", nil)
	}
	return convertToString(val, key)
}

// toInt converts an interface{} to int, handling float64 from JSON
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func validateJSONContent(content, path string) string {
	if filepath.Ext(path) != ".json" {
		return ""
	}

	if len(content) == 0 {
		return ""
	}

	if err := json.Unmarshal([]byte(content), new(any)); err != nil {
		return fmt.Sprintf("Warning: invalid JSON in %s: %v", filepath.Base(path), err)
	}

	return ""
}

// doubleSlashCommentExts lists source file extensions where // is the line
// comment marker.
var doubleSlashCommentExts = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".mjs": true, ".ts": true, ".tsx": true,
	".java": true, ".c": true, ".h": true, ".cpp": true, ".cc": true, ".hpp": true,
	".rs": true, ".swift": true, ".kt": true, ".kts": true, ".cs": true,
	".php": true, ".scala": true, ".dart": true, ".groovy": true,
}

// swallowedCommentError checks for content that's almost certainly missing
// line breaks it needs: in every // line-comment language, a comment
// extends to the next newline, so with no newline anywhere in the content,
// the first // silently swallows everything after it. Local models
// occasionally emit multi-line code as one unbroken physical line under
// pressure (e.g. after several failed attempts); the resulting file is
// still syntactically legal (an empty declaration list is valid in most of
// these languages) but is missing every real declaration after the
// comment, which then sends the model into a long, confused debugging loop
// it has no way to diagnose — read_file/cat both show the content as
// "correct" because the missing newlines aren't visually obvious in that
// output. Requiring two or more // markers keeps this from firing on a
// single legitimate trailing comment (e.g. one containing a URL).
func swallowedCommentError(content, path string) error {
	if !doubleSlashCommentExts[strings.ToLower(filepath.Ext(path))] {
		return nil
	}
	if strings.Contains(content, "\n") {
		return nil
	}
	if strings.Count(content, "//") < 2 {
		return nil
	}
	return agenterrors.NewValidation(
		"this content has no line breaks but contains multiple // comments — "+
			"it looks like several lines got merged into one, and the first // "+
			"would silently swallow everything after it as a comment. Resubmit "+
			"with real newlines between statements/declarations.", nil)
}

func disallowRawStructuredWrite(path, toolName string) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json", ".yaml", ".yml":
		return agenterrors.NewValidation(toolName+" is not allowed for structured files ("+ext+"); use write_structured_file or patch_structured_file instead", nil)
	default:
		return nil
	}
}
