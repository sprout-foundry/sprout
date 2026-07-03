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

	"github.com/sprout-foundry/sprout/pkg/events"
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
		Description: "Read the contents of a file. Supports text files and PDFs. For large files, use view_range to read specific line ranges.",
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
	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Parse view_range
	var startLine, endLine int
	if vr, exists := args["view_range"]; exists && vr != nil {
		arr := vr.([]any)
		startLine = toIntArg(arr[0])
		endLine = toIntArg(arr[1])
	}

	// Publish tool start event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool": "read_file",
			"path": path,
		})
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

	// Use existing read logic
	var content string
	if startLine > 0 || endLine > 0 {
		content, err = ReadFileWithRange(ctx, path, startLine, endLine)
	} else {
		content, err = ReadFile(ctx, path)
	}

	// NOTE: Security approvals for path traversal are handled by the legacy
	// dispatch path (tool_definitions.go). In the new ToolHandler path,
	// errors from SafeResolvePathWithBypass are returned directly.
	// To enable cross-directory reads, use workspace configuration or unsafe mode.

	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read file %q: %w", path, err)
	}

	// Publish tool end event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":   "read_file",
			"path":   path,
			"bytes":  len(content),
			"tokens": estimateTokenUsage(content),
		})
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

func (h *readFileHandler) Aliases() []string         { return nil }
func (h *readFileHandler) Timeout() time.Duration    { return 0 }
func (h *readFileHandler) MaxResultSize() int        { return 0 }
func (h *readFileHandler) SafeForParallel() bool     { return false }
func (h *readFileHandler) Interactive() bool         { return false }

// handlePDF processes a PDF file and returns it as base64 data URI for vision-capable models.
func (h *readFileHandler) handlePDF(ctx context.Context, env ToolEnv, path string) (ToolResult, error) {
	// Resolve path securely
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

	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":   "read_file",
			"path":   path,
			"bytes":  len(data),
			"images": 1,
		})
	}

	return ToolResult{
		Output:     textContent,
		Images:     []ImageData{{URI: dataURI, MIMEType: mimeType}},
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
