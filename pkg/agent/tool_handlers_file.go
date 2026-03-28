package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/console"
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

// isImageExtension returns true for common image file extensions
func isImageExtension(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".avif":
		return true
	default:
		return false
	}
}

// isPDFExtension returns true for PDF file extensions
func isPDFExtension(filePath string) bool {
	return strings.ToLower(filepath.Ext(filePath)) == ".pdf"
}

// handleReadFileWithImages is the image-capable read_file handler.
// When the primary model supports vision and the file is an image or PDF, it returns
// the content as multimodal data. Otherwise falls back to the text handler.
func handleReadFileWithImages(ctx context.Context, a *Agent, args map[string]interface{}) ([]api.ImageData, string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return nil, "", err
	}

	// Handle PDFs — either via multimodal pipeline or OCR text extraction
	if isPDFExtension(path) {
		cleanPath, resolveErr := filesystem.SafeResolvePathWithBypass(ctx, path)
		if resolveErr != nil {
			return nil, "", resolveErr
		}

		if a != nil && a.client != nil && a.client.SupportsVision() {
			return handleReadPDFFileMultimodal(ctx, a, cleanPath)
		}

		// Non-multimodal: extract text via OCR
		result, ocrErr := tools.ProcessPDFForTextOnly(cleanPath)
		if ocrErr != nil {
			return nil, "", fmt.Errorf("failed to read PDF %s: %w", path, ocrErr)
		}
		return nil, preparePDFTextResult(path, result), nil
	}

	// Only use image path for files with image extensions and when model supports vision
	if !isImageExtension(path) || a == nil || a.client == nil || !a.client.SupportsVision() {
		result, err := handleReadFile(ctx, a, args)
		return nil, result, err
	}

	return handleReadImageFileMultimodal(ctx, a, path)
}

// handleReadImageFileMultimodal reads an image file and returns it as multimodal content
func handleReadImageFileMultimodal(ctx context.Context, a *Agent, filePath string) ([]api.ImageData, string, error) {
	// Resolve path securely
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, filePath)
	if err != nil {
		return nil, "", err
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to access file %s: %w", cleanPath, err)
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}

	// Read file data
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
	}

	// Validate it's actually an image via magic bytes
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		// Not a valid image — fall back to text handler error
		return nil, "", fmt.Errorf("only text content files can be read. %s appears to be a non-text or unsupported image file", cleanPath)
	}

	// Check size limit
	if len(data) > console.MaxPastedImageSize {
		return nil, "", fmt.Errorf("image file too large (%d bytes, max %d bytes): %s", len(data), console.MaxPastedImageSize, cleanPath)
	}

	// Optimize/resize if needed (using existing vision_types.go function)
	optimizedData, optimizedMIME, optErr := tools.OptimizeImageData(cleanPath, data)
	if optErr != nil {
		a.debugLog("[WARN] Image optimization failed for %s: %v, using original data\n", cleanPath, optErr)
		// Use original data if optimization fails
	} else if optimizedData != nil && len(optimizedData) > 0 {
		data = optimizedData
		if optimizedMIME != "" {
			mimeType = optimizedMIME
		}
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(data)

	// Build descriptive text for the tool result
	textResult := fmt.Sprintf("[Image file: %s (%s, %d bytes)]", cleanPath, mimeType, len(data))

	images := []api.ImageData{{
		Base64: encoded,
		Type:   mimeType,
	}}

	return images, textResult, nil
}

// handleReadPDFFileMultimodal processes a PDF file for multimodal consumption.
// When the PDF contains extractable text, returns it directly. Otherwise renders
// pages as images so the model can visually analyze them.
func handleReadPDFFileMultimodal(ctx context.Context, a *Agent, filePath string) ([]api.ImageData, string, error) {
	a.debugLog("[doc] PDF detected, processing via multimodal pipeline: %s\n", filePath)

	result, err := tools.ProcessPDFForMultimodal(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to process PDF %s: %w", filePath, err)
	}

	if len(result.Images) > 0 {
		textResult := fmt.Sprintf("[PDF file: %s (%d pages rendered as images for visual analysis)]", filePath, len(result.Images))
		return result.Images, textResult, nil
	}

	// Text was extractable via pypdf — return as text only (no images needed)
	textResult := fmt.Sprintf("[PDF content: %s (extracted as text)]\n\n%s", filePath, result.Text)
	return nil, textResult, nil
}

// preparePDFTextResult formats OCR-extracted PDF text for display.
func preparePDFTextResult(filePath, text string) string {
	return fmt.Sprintf("[PDF content: %s (converted to text via OCR)]\n\n%s", filePath, text)
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
		parsed, parseErr := parseStructuredJSONContent(content, "write_file")
		if parseErr != nil {
			return "", fmt.Errorf("write_file JSON forwarding failed for %s: %w", path, parseErr)
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
	var parsed interface{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, formatJSONParseError(content, err, callerTool)
	}
	switch parsed.(type) {
	case map[string]interface{}, []interface{}:
		return parsed, nil
	default:
		return nil, fmt.Errorf("top-level JSON must be an object or array")
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
		return fmt.Errorf("invalid JSON: %v; next_step=%s", err, sameToolJSONFixHint(callerTool))
	}

	line, col := lineColFromOffset(content, offset)
	snippet := snippetAtLine(content, line)
	if snippet == "" {
		return fmt.Errorf("invalid JSON at line=%d col=%d: %v; next_step=%s", line, col, err, sameToolJSONFixHint(callerTool))
	}

	return fmt.Errorf(
		"invalid JSON at line=%d col=%d: %v; snippet=%q; next_step=%s",
		line, col, err, snippet, sameToolJSONFixHint(callerTool),
	)
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

	// Pre-write validation: check syntax before writing
	if a.validator != nil && a.configManager.GetConfig().EnablePreWriteValidation {
		if err := a.validator.ValidateSyntax(ctx, path, content); err != nil {
			a.debugLog("Pre-write validation failed: %v\n", err)
			// Don't fail the write - let it through but log the warning
			a.debugLog("Proceeding with write despite validation warning\n")
		}
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
	if err == nil {
		a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "write", content))
		a.debugLog("Published file_changed event: %s (write)\n", path)
	}

	// Start async validation (fire-and-forget)
	if a.validator != nil {
		a.validator.RunAsyncValidation(ctx, path, content)
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
		parsed, parseErr := parseStructuredJSONContent(editedContent, "edit_file")
		if parseErr != nil {
			restoreErr := func() error {
				_, werr := tools.WriteFile(ctx, path, originalContent)
				return werr
			}()
			if restoreErr != nil {
				return "", fmt.Errorf("edit would produce invalid JSON in %s (%v) and restore failed: %v", path, parseErr, restoreErr)
			}
			return "", fmt.Errorf("edit would produce invalid JSON in %s: %v", path, parseErr)
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
	if err == nil {
		var eventContent string
		if eventContent, err = tools.ReadFile(ctx, path); err == nil {
			a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", eventContent))
			a.debugLog("Published file_changed event: %s (edit)\n", path)
		} else {
			a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", ""))
			a.debugLog("Published file_changed event: %s (edit, no content)\n", path)
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
