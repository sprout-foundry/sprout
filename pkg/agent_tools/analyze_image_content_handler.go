//go:build !js

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type analyzeImageContentHandler struct{}

func (h *analyzeImageContentHandler) Name() string { return "analyze_image_content" }

func (h *analyzeImageContentHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "analyze_image_content",
		Description: "Analyze images/PDFs for text extraction (OCR), structured extraction, or general visual insights. Supports local paths and HTTP(S) URLs.",
		Required:    []string{"image_path"},
		Parameters: []ParameterDef{
			{Name: "image_path", Type: "string", Required: true, Description: "Path or URL to image/PDF"},
			{Name: "analysis_prompt", Type: "string", Description: "Custom vision prompt"},
			{Name: "analysis_mode", Type: "string", Description: "Mode: 'ocr' for text extraction, 'general' for description"},
		},
	}
}

func (h *analyzeImageContentHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "image_path")
	return err
}

func (h *analyzeImageContentHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	imagePath, err := extractString(args, "image_path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Gate 1 precheck — local filesystem paths only; http(s) URLs are
	// fetched over the network and skip the path-tier classifier.
	if !isHTTPURL(imagePath) {
		resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "analyze_image_content", imagePath)
		if decision == "deny" {
			return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", imagePath), IsError: true},
				fmt.Errorf("read blocked: %s is not accessible", imagePath)
		}
		if decision == "prompt" && env.FileAccessPrompter != nil {
			if ctx2, approved := promptForOffWorkspacePath(ctx, env, "analyze_image_content", imagePath, resolvedPath, "read"); approved {
				ctx = ctx2
			} else {
				return ToolResult{Output: fmt.Sprintf("read blocked: off-workspace access to %s was not approved", imagePath), IsError: true},
					fmt.Errorf("read blocked: off-workspace access to %s was not approved", imagePath)
			}
		}
	}

	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}

	analysisMode := ""
	if v, ok := args["analysis_mode"].(string); ok {
		analysisMode = v
	}

	result, err := AnalyzeImage(ctx, imagePath, analysisPrompt, analysisMode)
	if err != nil {
		return ToolResult{Output: result, IsError: true}, err
	}

	// Multimodal attachment (SP-137): when the request is a local image
	// file, attach it alongside the analysis so vision-capable primary
	// models see the raw pixels — the analyzed text alone loses spatial
	// detail. Seed strips Images for non-vision models, so this is safe
	// unconditionally. OCR-mode results already carry the extracted text
	// and skip attachment to avoid doubling payload for a pure-text ask.
	attachment := ToolResult{}
	if analysisMode != "ocr" && !isHTTPURL(imagePath) && isImageExtension(imagePath) {
		attachment = buildImageAttachment(ctx, imagePath)
	}

	attachment.Output = result
	return attachment, nil
}

// buildImageAttachment reads a local image file and returns a ToolResult
// carrying its inline data-URI (bounded by maxInlineImageBytes). Failure
// degrades to a text-only result — analysis output is still valuable.
func buildImageAttachment(ctx context.Context, imagePath string) ToolResult {
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, imagePath)
	if err != nil {
		return ToolResult{}
	}
	info, err := os.Stat(cleanPath)
	if err != nil || info.IsDir() || info.Size() > maxInlineImageBytes {
		return ToolResult{}
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{}
	}
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		mimeType = imageExtensions[strings.ToLower(filepath.Ext(cleanPath))]
	}
	if mimeType == "" {
		return ToolResult{}
	}
	return ToolResult{
		Images: []ImageData{{
			URI:      fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)),
			MIMEType: mimeType,
		}},
	}
}

func (h *analyzeImageContentHandler) Aliases() []string      { return nil }
func (h *analyzeImageContentHandler) Timeout() time.Duration { return 0 }
func (h *analyzeImageContentHandler) MaxResultSize() int     { return 0 }
func (h *analyzeImageContentHandler) SafeForParallel() bool  { return false }
func (h *analyzeImageContentHandler) Interactive() bool      { return false }
