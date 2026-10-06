package agent

// tool_handlers_file.go — the read_file handler: the read path itself
// (text, image, and PDF multimodal reads). The write_file path lives in tool_handlers_file_write.go and
// the edit_file path + shared arg helpers in tool_handlers_file_edit.go.

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// Tool handler implementations for file operations
func handleReadFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Get file path - supports both "path" (new) and "file_path" (legacy)
	path, err := getFilePath(args)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get file path")
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
			if ctx2, approved := handleFileSecurityError(ctx, a, "read_file", path, "", err); approved {
				result, err = tools.ReadFileWithRange(ctx2, path, startLine, endLine)
			}
		}

		a.Logger().Debug("Read file result: %s, error: %v\n", result, err)

		if err == nil {
			a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s (lines %d-%d)", path, startLine, endLine), path)
			// Record the read so the staleness rule lets a
			// subsequent write_file through. Range-read still counts —
			// the agent knows enough of the file to write coherently.
			a.RecordFileReadThisTurn(path)
		}

		if err != nil {
			return result, agenterrors.NewTool("read_file", "read file", err).WithDetail("path", path)
		}
		return result, nil
	}

	a.Logger().Debug("Reading file: %s\n", path)
	result, err := tools.ReadFile(ctx, path)

	if err != nil {
		if ctx2, approved := handleFileSecurityError(ctx, a, "read_file", path, "", err); approved {
			result, err = tools.ReadFile(ctx2, path)
		}
	}

	a.Logger().Debug("Read file result: %s, error: %v\n", result, err)

	if err == nil {
		a.AddTaskAction("file_read", fmt.Sprintf("Read file: %s", path), path)
		// Record the read so a subsequent write_file passes
		// the staleness check.
		a.RecordFileReadThisTurn(path)
	}

	if err != nil {
		return "", agenterrors.NewTool("read_file", "failed to read file", err).WithDetail("path", path)
	}
	return result, nil
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
		return nil, "", agenterrors.Wrap(err, "failed to get file path")
	}

	// Handle PDFs — either via multimodal pipeline or OCR text extraction
	if isPDFExtension(path) {
		cleanPath, resolveErr := filesystem.SafeResolvePathWithBypass(ctx, path)
		if resolveErr != nil {
			return nil, "", agenterrors.NewTool("read_file", "failed to resolve PDF path", resolveErr).WithDetail("path", path)
		}

		if a != nil {
			if c := a.getClient(); c != nil && api.ResolveVisionCapability(c).AcceptsImages {
				images, text, err := handleReadPDFFileMultimodal(ctx, a, cleanPath)
				if err != nil {
					return nil, "", agenterrors.NewTool("read_file", "failed to read PDF file", err).WithDetail("path", path)
				}
				return images, text, nil
			}
		}

		// Non-multimodal: extract text via OCR
		result, ocrErr := tools.ProcessPDFForTextOnly(ctx, cleanPath)
		if ocrErr != nil {
			return nil, "", agenterrors.NewTool("read_file", "failed to read PDF", ocrErr).WithDetail("path", path)
		}
		return nil, preparePDFTextResult(path, result), nil
	}

	// Only use image path for files with image extensions and when model supports vision
	if !isImageExtension(path) || a == nil || a.client == nil || !api.ResolveVisionCapability(a.client).AcceptsImages {
		result, err := handleReadFile(ctx, a, args)
		if err != nil {
			return nil, result, agenterrors.NewTool("read_file", "handle read file", err).WithDetail("path", path)
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
		return nil, "", agenterrors.NewTool("read_file", "failed to resolve path", err)
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, "", agenterrors.NewTool("read_file", "failed to access file", err).WithDetail("path", cleanPath)
	}
	if info.IsDir() {
		return nil, "", agenterrors.NewInvalidInputError(fmt.Sprintf("path is a directory, not a file: %s", cleanPath), nil)
	}

	// Read file data
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, "", agenterrors.NewTool("read_file", "failed to read file", err).WithDetail("path", cleanPath)
	}

	// Validate it's actually an image via magic bytes
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		// Not a valid image — fall back to text handler error
		return nil, "", agenterrors.NewInvalidInputError(fmt.Sprintf("cannot read file %s: not a text file or unsupported image format", cleanPath), nil)
	}

	// Check size limit
	if len(data) > console.MaxPastedImageSize {
		return nil, "", agenterrors.NewInvalidInputError(fmt.Sprintf("image file too large (%d bytes, max %d bytes): %s", len(data), console.MaxPastedImageSize, cleanPath), nil)
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
		return nil, "", agenterrors.NewTool("read_file", "failed to process PDF", err).WithDetail("path", filePath)
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
