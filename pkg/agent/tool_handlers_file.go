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

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// Tool handler implementations for file operations

func handleReadFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Get file path - supports both "path" (new) and "file_path" (legacy)
	path, err := getFilePath(args)
	if err != nil {
		return "", fmt.Errorf("failed to get file path: %w", err)
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
		a.Logger().Debug("Reading file: %s (lines %d-%d)\n", path, startLine, endLine)
		result, err := tools.ReadFileWithRange(ctx, path, startLine, endLine)

		if err != nil {
			if ctx2, approved := handleFileSecurityError(ctx, a, "read_file", path, err); approved {
				result, err = tools.ReadFileWithRange(ctx2, path, startLine, endLine)
			}
		}

		a.Logger().Debug("Read file result: %s, error: %v\n", result, err)

		if err == nil {
			a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s (lines %d-%d)", path, startLine, endLine), path)
			// SP-046 §7: record the read so the staleness rule lets a
			// subsequent write_file through. Range-read still counts —
			// the agent knows enough of the file to write coherently.
			a.RecordFileReadThisTurn(path)
		}

		if err != nil {
			return result, fmt.Errorf("read file %q: %w", path, err)
		}
		// Inject semantic context if embedding is enabled
		result = injectSemanticContext(ctx, a, path, result)
		return result, nil
	}

	a.Logger().Debug("Reading file: %s\n", path)
	result, err := tools.ReadFile(ctx, path)

	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "read_file", path, err); approved {
			result, err = tools.ReadFile(ctx2, path)
		}
	}

	a.Logger().Debug("Read file result: %s, error: %v\n", result, err)

	if err == nil {
		a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s", path), path)
		// SP-046 §7: record the read so a subsequent write_file passes
		// the staleness check.
		a.RecordFileReadThisTurn(path)
	}

	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	// Inject semantic context if embedding is enabled
	result = injectSemanticContext(ctx, a, path, result)
	return result, nil
}

// injectSemanticContext appends semantically related function references to
// the read_file result, giving the agent awareness of related code in other files.
// This is the input-side of the embedding system — proactive context, not warnings.
func injectSemanticContext(ctx context.Context, a *Agent, filePath string, content string) string {
	if !shouldInjectContext(a) {
		return content
	}

	// Only inject for code files with extractable units
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".go" && ext != ".ts" && ext != ".tsx" && ext != ".py" {
		return content
	}

	em := a.GetEmbeddingManager()
	if em == nil || !em.IsInitialized() {
		return content
	}

	// Use the first ~500 chars as a representative query
	query := content
	if len(query) > 500 {
		query = query[:500]
	}

	results, err := em.QuerySimilar(ctx, query, 5, 0.85)
	if err != nil || len(results) == 0 {
		return content
	}

	// Filter out results from the same file (agent already has that context)
	var external []embedding.QueryResult
	workspaceRoot := a.GetWorkspaceRoot()
	for _, r := range results {
		if embedding.NormalizePathToWorkspace(workspaceRoot, r.Record.File) != embedding.NormalizePathToWorkspace(workspaceRoot, filePath) {
			external = append(external, r)
		}
	}

	if len(external) == 0 {
		return content
	}

	var sb strings.Builder
	sb.WriteString("\n\n--- Related code (semantic search) ---\n")
	for _, r := range external {
		sb.WriteString(fmt.Sprintf("• %s (similarity: %.2f)\n  %s [%d-%d]\n",
			r.Record.ID, r.Similarity, r.Record.Signature, r.Record.StartLine, r.Record.EndLine))
	}
	sb.WriteString("--- End related code ---\n")

	return content + sb.String()
}

func shouldInjectContext(a *Agent) bool {
	if a == nil {
		return false
	}
	cfg := a.GetConfig()
	if cfg == nil || cfg.EmbeddingIndex == nil || !cfg.EmbeddingIndex.Enabled {
		return false
	}
	return a.GetEmbeddingManager() != nil
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
		return nil, "", fmt.Errorf("failed to get file path: %w", err)
	}

	// Handle PDFs — either via multimodal pipeline or OCR text extraction
	if isPDFExtension(path) {
		cleanPath, resolveErr := filesystem.SafeResolvePathWithBypass(ctx, path)
		if resolveErr != nil {
			return nil, "", fmt.Errorf("failed to resolve PDF path %s: %w", path, resolveErr)
		}

		if a != nil {
			if c := a.getClient(); c != nil && c.SupportsVision() {
				images, text, err := handleReadPDFFileMultimodal(ctx, a, cleanPath)
				if err != nil {
					return nil, "", fmt.Errorf("failed to read PDF file %s: %w", path, err)
				}
				return images, text, nil
			}
		}

		// Non-multimodal: extract text via OCR
		result, ocrErr := tools.ProcessPDFForTextOnly(ctx, cleanPath)
		if ocrErr != nil {
			return nil, "", fmt.Errorf("failed to read PDF %s: %w", path, ocrErr)
		}
		return nil, preparePDFTextResult(path, result), nil
	}

	// Only use image path for files with image extensions and when model supports vision
	if !isImageExtension(path) || a == nil || a.client == nil || !a.client.SupportsVision() {
		result, err := handleReadFile(ctx, a, args)
		if err != nil {
			return nil, result, fmt.Errorf("handle read file for %q: %w", path, err)
		}
		return nil, result, nil
	}

	return handleReadImageFileMultimodal(ctx, a, path)
}

// handleReadImageFileMultimodal reads an image file and returns it as multimodal content
func handleReadImageFileMultimodal(ctx context.Context, a *Agent, filePath string) ([]api.ImageData, string, error) {
	// Resolve path securely
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve path: %w", err)
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
		return nil, "", fmt.Errorf("cannot read file %s: not a text file or unsupported image format", cleanPath)
	}

	// Check size limit
	if len(data) > console.MaxPastedImageSize {
		return nil, "", fmt.Errorf("image file too large (%d bytes, max %d bytes): %s", len(data), console.MaxPastedImageSize, cleanPath)
	}

	// Optimize/resize if needed (using existing vision_types.go function)
	optimizedData, optimizedMIME, optErr := tools.OptimizeImageData(cleanPath, data)
	if optErr != nil {
		a.Logger().Debug("[WARN] Image optimization failed for %s: %v, using original data\n", cleanPath, optErr)
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
	a.Logger().Debug("[doc] PDF detected, processing via multimodal pipeline: %s\n", filePath)

	result, err := tools.ProcessPDFForMultimodal(ctx, filePath)
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
		return "", fmt.Errorf("failed to get file path: %w", err)
	}

	content, err := getRequiredString(args, "content")
	if err != nil {
		return "", fmt.Errorf("failed to get content parameter: %w", err)
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
		return fmt.Errorf("invalid JSON: %w; next_step=%s", err, sameToolJSONFixHint(callerTool))
	}

	line, col := lineColFromOffset(content, offset)
	snippet := snippetAtLine(content, line)
	if snippet == "" {
		return fmt.Errorf("invalid JSON at line=%d col=%d: %w; next_step=%s", line, col, err, sameToolJSONFixHint(callerTool))
	}

	return fmt.Errorf("invalid JSON at line=%d col=%d: %w; snippet=%q; next_step=%s", line, col, err, snippet, sameToolJSONFixHint(callerTool))
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
			return "", fmt.Errorf("failed to validate structured write: %w", err)
		}
	}

	// SP-046 §7 staleness rule: refuse the write if the agent hasn't read
	// the file this turn, or if it was modified after the last read.
	// Returning the error before any side effects lets the agent react
	// (re-read, then retry write) without leaving partial state.
	if err := a.checkWriteStaleness(path); err != nil {
		return "", err
	}

	// SP-072: route through diff-approval gate when enabled.
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
				return "", fmt.Errorf("edit-approval failed for %s: %w", path, appErr)
			}
			content = approved
			a.Logger().Debug("edit-approval: %s\n", summary)
		}
	}

	if warning := validateJSONContent(content, path); warning != "" {
		a.Logger().Debug("%s\n", warning)
	}

	a.Logger().Debug("Writing file: %s\n", path)

	if trackErr := a.TrackFileWrite(path, content); trackErr != nil {
		a.Logger().Debug("Warning: Failed to track file write: %v\n", trackErr)
	}

	result, err := tools.WriteFile(ctx, path, content)

	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "write_file", path, err); approved {
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
		return "", fmt.Errorf("failed to write file %s: %w", path, err)
	}
	return result, nil
}

func handleEditFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	path, err := getFilePath(args)
	if err != nil {
		return "", fmt.Errorf("failed to get file path: %w", err)
	}

	oldStr, err := getRequiredString(args, "old_str")
	if err != nil {
		return "", fmt.Errorf("failed to get old_str parameter: %w", err)
	}

	newStr, err := getRequiredString(args, "new_str")
	if err != nil {
		return "", fmt.Errorf("failed to get new_str parameter: %w", err)
	}

	if warning := validateJSONContent(newStr, path); warning != "" {
		a.Logger().Debug("%s\n", warning)
	}

	// Read original for diff, handling filesystem security errors
	originalContent, err := tools.ReadFile(ctx, path)
	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "edit_file", path, err); approved {
			ctx = ctx2 // reuse bypassed context for subsequent operations
			originalContent, err = tools.ReadFile(ctx, path)
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to read original file for diff: %w", err)
	}

	a.Logger().Debug("Editing file: %s\n", path)
	a.Logger().Debug("Old string: %s\n", oldStr)
	a.Logger().Debug("New string: %s\n", newStr)

	// SP-072: route through diff-approval gate when enabled.
	if a.ShouldGateEdit(path) {
		proposedContent := strings.Replace(originalContent, oldStr, newStr, 1)
		proposal := EditProposal{Path: path, Original: originalContent, Proposed: proposedContent}
		approved, summary, appErr := a.RequestEditApproval(ctx, proposal)
		if appErr != nil {
			return "", fmt.Errorf("edit-approval failed for %s: %w", path, appErr)
		}
		if approved != proposedContent {
			a.Logger().Debug("edit-approval modified content for %s: %s\n", path, summary)
			if trackErr := a.TrackFileWrite(path, approved); trackErr != nil {
				a.Logger().Debug("Warning: Failed to track approved write: %v\n", trackErr)
			}
			writeResult, writeErr := tools.WriteFile(ctx, path, approved)
			if writeErr != nil {
				return "", fmt.Errorf("failed to write approved content to %s: %w", path, writeErr)
			}
			a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(path, "edit", approved))
			if a.state.GetOptimizer() != nil {
				a.state.GetOptimizer().InvalidateFile(path)
			}
			return writeResult, nil
		}
		a.Logger().Debug("edit-approval: %s\n", summary)
	}

	// SP-072: TrackFileEdit stores FULL file content (not fragments) so
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
		if ctx2, approved := handleFileSecurityError(ctx, a, "edit_file", path, err); approved {
			ctx = ctx2
			originalContent, err = tools.ReadFile(ctx, path)
			if err != nil {
				return "", fmt.Errorf("failed to read original file for diff: %w", err)
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
			return "", fmt.Errorf("json edit succeeded but failed to read edited file: %w", readErr)
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
				return "", fmt.Errorf("edit would produce invalid JSON in %s and restore failed: %w (original parse error: %v)", path, restoreErr, parseErr)
			}
			return "", fmt.Errorf("edit would produce invalid JSON in %s: %w", path, parseErr)
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
		return "", fmt.Errorf("failed to edit file %s: %w", path, err)
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
		return fmt.Errorf("%s is not allowed for structured files (%s); use write_structured_file or patch_structured_file instead", toolName, ext)
	default:
		return nil
	}
}
