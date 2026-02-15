package agent

import (
	"context"
	"fmt"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/filesystem"
)

// Tool handler implementations for file operations

func handleReadFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Get file path - supports both "path" (new) and "file_path" (legacy)
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	// Parse view_range (Claude Code style: [start, end])
	var startLine, endLine int
	var hasRange bool

	if viewRange, exists := args["view_range"]; exists {
		if arr, ok := viewRange.([]interface{}); ok && len(arr) == 2 {
			if s, ok := toInt(arr[0]); ok {
				startLine = s
				if e, ok := toInt(arr[1]); ok {
					endLine = e
					hasRange = true
				}
			}
		}
	}

	// Log and execute
	if hasRange {
		a.ToolLog("reading file", fmt.Sprintf("%s (lines %d-%d)", path, startLine, endLine))
		a.debugLog("Reading file: %s (lines %d-%d)\n", path, startLine, endLine)
		result, err := tools.ReadFileWithRange(ctx, path, startLine, endLine)

		if err != nil {
			ctx2 := handleFileSecurityError(ctx, a, "read_file", path, err)
			if ctx2 != ctx {
				result, err = tools.ReadFileWithRange(ctx2, path, startLine, endLine)
			}
		}

		a.debugLog("Read file result: %s, error: %v\n", result, err)

		if err == nil {
			a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s (lines %d-%d)", path, startLine, endLine), path)
		}

		return result, err
	}

	a.ToolLog("reading file", path)
	a.debugLog("Reading file: %s\n", path)
	result, err := tools.ReadFile(ctx, path)

	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "read_file", path, err)
		if ctx2 != ctx {
			result, err = tools.ReadFile(ctx2, path)
		}
	}

	a.debugLog("Read file result: %s, error: %v\n", result, err)

	if err == nil {
		a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s", path), path)
	}

	return result, err
}

func handleWriteFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	content, err := getRequiredString(args, "content")
	if err != nil {
		return "", err
	}

	a.ToolLog("writing file", fmt.Sprintf("%s (%d bytes)", path, len(content)))
	a.debugLog("Writing file: %s\n", path)

	if trackErr := a.TrackFileWrite(path, content); trackErr != nil {
		a.debugLog("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(ctx, path, content)

	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "write_file", path, err)
		if ctx2 != ctx {
			result, err = tools.WriteFile(ctx2, path, content)
		}
	}

	a.debugLog("Write file result: %s, error: %v\n", result, err)

	// Invalidate cached file metadata when file is successfully written
	// This prevents stale line counts from misleading the model
	if err == nil && a.optimizer != nil {
		a.optimizer.InvalidateFile(path)
	}

	// Publish file change event for web UI auto-sync
	if err == nil && a.eventBus != nil {
		a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "write", content))
		a.debugLog("Published file_changed event: %s (write)\n", path)
	}

	return result, err
}

func handleEditFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	oldStr, err := getRequiredString(args, "old_str")
	if err != nil {
		return "", err
	}

	newStr, err := getRequiredString(args, "new_str")
	if err != nil {
		return "", err
	}

	// Read original for diff
	originalContent, err := tools.ReadFile(ctx, path)
	if err != nil {
		return "", fmt.Errorf("failed to read original file for diff: %w", err)
	}

	a.ToolLog("editing file", fmt.Sprintf("%s (replacing %d bytes → %d bytes)", path, len(oldStr), len(newStr)))
	a.debugLog("Editing file: %s\n", path)
	a.debugLog("Old string: %s\n", oldStr)
	a.debugLog("New string: %s\n", newStr)

	if trackErr := a.TrackFileEdit(path, oldStr, newStr); trackErr != nil {
		a.debugLog("Warning: Failed to track file edit: %v\n", trackErr)
	}

	result, err := tools.EditFile(ctx, path, oldStr, newStr)

	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "edit_file", path, err)
		if ctx2 != ctx {
			originalContent, err = tools.ReadFile(ctx2, path)
			if err != nil {
				return "", fmt.Errorf("failed to read original file for diff: %w", err)
			}
			result, err = tools.EditFile(ctx2, path, oldStr, newStr)
		}
	}

	a.debugLog("Edit file result: %s, error: %v\n", result, err)

	// Invalidate cached file metadata when file is successfully edited
	// This prevents stale line counts from misleading the model
	if err == nil && a.optimizer != nil {
		a.optimizer.InvalidateFile(path)
	}

	// Publish file change event for web UI auto-sync
	if err == nil && a.eventBus != nil {
		var eventContent string
		if eventContent, err = tools.ReadFile(ctx, path); err == nil {
			a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", eventContent))
			a.debugLog("Published file_changed event: %s (edit)\n", path)
		} else {
			a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", ""))
			a.debugLog("Published file_changed event: %s (edit, no content)\n", path)
		}
	}

	// Display diff if successful
	if err == nil {
		newContent, readErr := tools.ReadFile(ctx, path)
		if readErr == nil {
			a.ShowColoredDiff(originalContent, newContent, 50)
		}
	}

	return result, err
}

// handleCreate creates a new file with content. Fails if file already exists.
func handleCreate(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", err
	}

	fileText, err := getRequiredString(args, "file_text")
	if err != nil {
		return "", err
	}

	a.ToolLog("creating file", fmt.Sprintf("%s (%d bytes)", path, len(fileText)))
	a.debugLog("Creating file: %s\n", path)

	// Fail if file exists (safe create)
	if filesystem.FileExists(path) {
		return "", fmt.Errorf("file already exists: %s (use write_file to overwrite)", path)
	}

	result, err := tools.WriteFile(ctx, path, fileText)
	if err != nil {
		ctx2 := handleFileSecurityError(ctx, a, "create", path, err)
		if ctx2 != ctx {
			result, err = tools.WriteFile(ctx2, path, fileText)
		}
	}

	a.debugLog("Create file result: %s, error: %v\n", result, err)

	if err == nil {
		if trackErr := a.TrackFileWrite(path, fileText); trackErr != nil {
			a.debugLog("Warning: Failed to track file create: %v\n", trackErr)
		}
	}

	if err == nil && a.eventBus != nil {
		a.eventBus.Publish(events.EventTypeFileChanged, events.FileChangedEvent(path, "create", fileText))
		a.debugLog("Published file_changed event: %s (create)\n", path)
	}

	return result, err
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
	return "", fmt.Errorf("parameter 'path' is required")
}

// getRequiredString extracts a required string parameter
func getRequiredString(args map[string]interface{}, key string) (string, error) {
	val, exists := args[key]
	if !exists {
		return "", fmt.Errorf("parameter '%s' is required", key)
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
