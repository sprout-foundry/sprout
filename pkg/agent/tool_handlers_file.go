package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/events"
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

	if hasRange {
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

	// JSON writes are transparently routed through structured serialization/validation.
	if strings.EqualFold(filepath.Ext(path), ".json") {
		parsed, ok := parseStructuredJSONContent(content)
		if !ok {
			return "", fmt.Errorf("invalid JSON content for %s", path)
		}
		return handleWriteStructuredFile(ctx, a, map[string]interface{}{
			"path":   path,
			"format": "json",
			"data":   parsed,
		})
	}

	return writeFileContent(ctx, a, path, content, "write_file", false)
}

func parseStructuredJSONContent(content string) (interface{}, bool) {
	var parsed interface{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, false
	}
	switch parsed.(type) {
	case map[string]interface{}, []interface{}:
		return parsed, true
	default:
		return nil, false
	}
}

func writeFileContent(ctx context.Context, a *Agent, path, content, toolName string, allowStructured bool) (string, error) {
	if !allowStructured {
		if err := disallowRawStructuredWrite(path, toolName); err != nil {
			return "", err
		}
	}

	if warning := validateJSONContent(content, path); warning != "" {
		a.debugLog("%s\n", warning)
	}

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

	if warning := validateJSONContent(newStr, path); warning != "" {
		a.debugLog("%s\n", warning)
	}

	// Read original for diff
	originalContent, err := tools.ReadFile(ctx, path)
	if err != nil {
		return "", fmt.Errorf("failed to read original file for diff: %w", err)
	}

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

	// JSON edits are transparently validated and normalized through structured writes.
	if err == nil && strings.EqualFold(filepath.Ext(path), ".json") {
		editedContent, readErr := tools.ReadFile(ctx, path)
		if readErr != nil {
			return "", fmt.Errorf("json edit succeeded but failed to read edited file: %w", readErr)
		}
		parsed, ok := parseStructuredJSONContent(editedContent)
		if !ok {
			restoreErr := func() error {
				_, werr := tools.WriteFile(ctx, path, originalContent)
				return werr
			}()
			if restoreErr != nil {
				return "", fmt.Errorf("edit would produce invalid JSON in %s (and restore failed: %v)", path, restoreErr)
			}
			return "", fmt.Errorf("edit would produce invalid JSON in %s", path)
		}
		if _, werr := handleWriteStructuredFile(ctx, a, map[string]interface{}{
			"path":   path,
			"format": "json",
			"data":   parsed,
		}); werr != nil {
			return "", fmt.Errorf("json edit normalization failed: %w", werr)
		}
	}

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

func disallowRawStructuredWrite(path, toolName string) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json", ".yaml", ".yml":
		return fmt.Errorf("%s is not allowed for structured files (%s). Use write_structured_file or patch_structured_file", toolName, ext)
	default:
		return nil
	}
}
