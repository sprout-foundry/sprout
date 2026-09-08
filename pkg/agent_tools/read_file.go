package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// readFileHandler implements ToolHandler for the read_file tool.
type readFileHandler struct{}

func (h *readFileHandler) Name() string {
	return "read_file"
}

func (h *readFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "read_file",
		Description: "Read the contents of a file. Supports text files, PDFs, and images (images are attached for visual analysis or OCR-extracted, never dumped as binary). For large files, use view_range to read specific line ranges.",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the file to read.",
			},
			{
				Name:        "view_range",
				Type:        "array",
				Required:    false,
				Description: "Optional line range as [start, end] array (1-based). Use this to read specific sections of large files.",
				Items:       map[string]any{"type": "integer"},
			},
		},
		Required: []string{"path"},
	}
}

func (h *readFileHandler) Validate(args map[string]any) error {
	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("parameter 'path' must not be empty")
	}

	// Validate view_range if provided
	if vr, exists := args["view_range"]; exists && vr != nil {
		arr, ok := vr.([]any)
		if !ok {
			return fmt.Errorf("parameter 'view_range' must be an array")
		}
		if len(arr) != 2 {
			return fmt.Errorf("parameter 'view_range' must have exactly 2 elements: [start, end]")
		}
		for i, v := range arr {
			switch v.(type) {
			case int, float64:
				// Valid numeric types (JSON numbers come as float64)
			default:
				return fmt.Errorf("parameter 'view_range' elements must be integers, got %T at index %d", v, i)
			}
		}
	}

	return nil
}

func (h *readFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// SP-127 M2: Consult Gate 1's path-tier classifier up-front so
	// Allow paths skip the gate entirely and Deny paths return a
	// typed error immediately — without waiting for SafeResolvePath
	// to fail first. Prompt paths fall through to the interactive
	// dialog.

	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Gate 1 precheck — resolves the path and classifies it.
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "read_file", path)
	if decision == "deny" {
		// A deny on read_file is not a read_only violation — reads are
		// always allowed under read_only grants. Surface a neutral message.
		return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", path), IsError: true},
			fmt.Errorf("read blocked: %s is not accessible", path)
	}
	// "allow"  → path is workspace/tmp/allowlisted; proceed directly.
	// "prompt" → interactive approval; on deny fall through to the raw
	// error. This is the pinned contract (file_access_prompt_test.go):
	// denied and nil-prompter reads surface the underlying fs error, not
	// a synthesized denial — the off-workspace block still holds because
	// the bypass ctx is only set on approval.
	if decision == "prompt" {
		if ctx2, approved := promptForOffWorkspacePath(ctx, env, "read_file", path, resolvedPath, "read"); approved {
			ctx = ctx2
		}
	}

	// Parse view_range (defensive — Validate() should have been called,
	// but we guard against panic if it wasn't or input is malformed)
	var startLine, endLine int
	if vr, exists := args["view_range"]; exists && vr != nil {
		if arr, ok := vr.([]any); ok && len(arr) == 2 {
			startLine = toIntArg(arr[0])
			endLine = toIntArg(arr[1])
		}
	}

	// SP-046-2: Record the read for staleness tracking (all code paths, including PDF)
	// Use a defer so this runs regardless of which branch handles the file.
	if tracker := GetGlobalTurnReadTracker(); tracker != nil {
		meta, _ := GetGlobalSyncState().GetMetadata(path)
		seq := int64(0)
		if meta != nil {
			seq = meta.BrowserSeq
		}
		tracker.RecordRead(path, seq)
	}

	// Check if this is a PDF file
	if strings.ToLower(filepath.Ext(path)) == ".pdf" {
		return h.handlePDF(ctx, env, path)
	}

	// Image files: never dump binary into the context (SP-137). Vision-
	// capable models get the image inline as multimodal content; others
	// get OCR-extracted text.
	if isImageExtension(path) {
		return h.handleImage(ctx, env, path)
	}

	// Use existing read logic. Off-workspace paths will fail
	// with the raw filesystem error since the interactive gate is gone.
	var content string
	if startLine > 0 || endLine > 0 {
		content, err = ReadFileWithRange(ctx, path, startLine, endLine)
	} else {
		content, err = ReadFile(ctx, path)
	}

	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read file %q: %w", path, err)
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, content)
	}

	return ToolResult{
		Output:     content,
		TokenUsage: int64(estimateTokenUsage(content)),
	}, nil
}

func (h *readFileHandler) Aliases() []string      { return nil }
func (h *readFileHandler) Timeout() time.Duration { return 0 }
func (h *readFileHandler) MaxResultSize() int     { return 0 }
func (h *readFileHandler) SafeForParallel() bool  { return false }
func (h *readFileHandler) Interactive() bool      { return false }

// handlePDF processes a PDF file and returns it as base64 data URI for vision-capable models.
func (h *readFileHandler) handlePDF(ctx context.Context, env ToolEnv, path string) (ToolResult, error) {
	// Resolve path securely. Off-workspace paths will fail with the raw
	// filesystem error since the interactive gate is gone.
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("resolve PDF path: %w", err)
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("access PDF file: %w", err)
	}
	if info.IsDir() {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read PDF file: %w", err)
	}

	// Build data URI
	mimeType := mime.TypeByExtension(".pdf")
	if mimeType == "" {
		mimeType = "application/pdf"
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	// Try to extract text via the existing PDF pipeline as supplementary content
	var textContent string
	result, pipelineErr := ProcessPDFForMultimodal(ctx, cleanPath)
	if pipelineErr == nil && result != nil && result.Text != "" {
		textContent = fmt.Sprintf("[PDF content extracted from %s]\n\n%s", cleanPath, result.Text)
	} else if pipelineErr == nil && result != nil && len(result.Images) > 0 {
		textContent = fmt.Sprintf("[PDF file: %s (%d pages rendered as images for visual analysis)]", cleanPath, len(result.Images))
	} else {
		textContent = fmt.Sprintf("[PDF file: %s (%d bytes). Text extraction unavailable: %v. Base64 data URI provided for vision-capable models.]", cleanPath, len(data), pipelineErr)
	}

	return ToolResult{
		Output:     textContent,
		Images:     []ImageData{{URI: dataURI, MIMEType: mimeType}},
		TokenUsage: int64(estimateTokenUsage(textContent)),
	}, nil
}

// imageExtensions are the formats the paste pipeline recognizes
// (console.DetectImageMagic). Keep in sync with it.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".avif": "image/avif",
}

// isImageExtension reports whether path has a known image extension.
func isImageExtension(path string) bool {
	_, ok := imageExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// maxInlineImageBytes caps read_file's inline image payload (10 MB,
// matching the pasted-image budget).
const maxInlineImageBytes = 10 * 1024 * 1024

// handleImage serves an image file without dumping binary into the model
// context. Vision-capable primary models receive the image inline (the
// seed registry attaches ToolResult.Images to the tool message, and seed's
// prepareMessages strips them for non-vision models). Non-vision models
// get OCR-extracted text via the analyze pipeline.
func (h *readFileHandler) handleImage(ctx context.Context, env ToolEnv, path string) (ToolResult, error) {
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("resolve image path: %w", err)
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("access image file: %w", err)
	}
	if info.IsDir() {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}
	if info.Size() > maxInlineImageBytes {
		return ToolResult{
			Output:  fmt.Sprintf("[image file %s is %d bytes, over the %d MB inline cap; use analyze_image_content]", filepath.Base(path), info.Size(), maxInlineImageBytes/1024/1024),
			IsError: true,
		}, fmt.Errorf("image too large to read inline: %s", path)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read image file: %w", err)
	}

	// Magic-byte verification is authoritative: an image extension on
	// non-image bytes is an error (never serve garbage as base64).
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("unrecognized image format: %s", path)
	}

	// Always attach the image data for vision-capable models (seed strips
	// it for non-vision providers) and try to give everyone some text:
	// native OCR when available, else a stub note.
	var textContent string
	images := []ImageData{{
		URI:      fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)),
		MIMEType: mimeType,
	}}

	if nativeOCRAvailable() {
		if ocrText, ocrErr := nativeOCR(ctx, cleanPath); ocrErr == nil {
			trimmed, truncated, _ := limitVisionOutputText(strings.TrimSpace(ocrText))
			if truncated {
				trimmed += "\n[truncated]"
			}
			if trimmed != "" {
				textContent = fmt.Sprintf("[image: %s (%d bytes, %s)] OCR text:\n%s",
					filepath.Base(path), info.Size(), mimeType, trimmed)
			}
		}
	}
	if textContent == "" {
		textContent = fmt.Sprintf("[image: %s (%d bytes, %s) attached for visual analysis]",
			filepath.Base(path), info.Size(), mimeType)
	}

	return ToolResult{
		Output:     textContent,
		Images:     images,
		TokenUsage: int64(estimateTokenUsage(textContent)),
	}, nil
}

// toIntArg converts an interface{} to int, handling float64 from JSON.
func toIntArg(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
