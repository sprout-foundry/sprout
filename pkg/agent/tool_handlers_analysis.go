package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
)

// Tool handler implementations for analysis operations

func handleAnalyzeUIScreenshot(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_ui_screenshot tool")
	}

	imagePath := args["image_path"].(string)
	a.debugLog("Analyzing UI screenshot: %s\n", imagePath)

	// Extract optional parameters
	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}

	viewportWidth := 1280
	if v, ok := args["viewport_width"].(int); ok && v > 0 {
		viewportWidth = v
	}

	viewportHeight := 720
	if v, ok := args["viewport_height"].(int); ok && v > 0 {
		viewportHeight = v
	}

	// Detect HTML content and render via headless browser before vision analysis.
	// This avoids passing raw HTML markup to the vision API and also avoids
	// a redundant HEAD request pattern by checking once at the handler level.
	effectiveImagePath := imagePath
	fallbackCleanup := func() {}
	defer fallbackCleanup()
	if resolvedPath, cleanup, usedFallback := resolveVisionToolInputPath(a, imagePath); usedFallback {
		a.debugLog("[img] Falling back to attached image for UI screenshot analysis: %s -> %s\n", imagePath, resolvedPath)
		effectiveImagePath = resolvedPath
		fallbackCleanup = cleanup
	}
	if isLocalHTMLFile(imagePath) || tools.IsHTMLInput(imagePath) {
		a.debugLog("HTML content detected, rendering via headless browser: %s\n", imagePath)
		screenshotPath, err := renderHTMLContent(ctx, a, imagePath, viewportWidth, viewportHeight)
		if err != nil {
			return "", fmt.Errorf("failed to render HTML content: %w", err)
		}
		defer os.Remove(screenshotPath)
		effectiveImagePath = screenshotPath
	}

	result, err := tools.AnalyzeImage(effectiveImagePath, analysisPrompt, "frontend")
	a.debugLog("Analyze UI screenshot error: %v\n", err)
	if err != nil {
		return result, fmt.Errorf("failed to analyze UI screenshot %s: %w", imagePath, err)
	}
	// Capture using the original path the user provided, not the temp screenshot
	a.captureVisionInputAndOutput(imagePath, result)

	normalized, normalizeErr := normalizeVisionToolOutput(result, true)
	if normalizeErr != nil {
		return "", normalizeErr
	}
	return normalized, nil
}

// isLocalHTMLFile checks if the given path looks like a local HTML file
// (not a URL) with extension .html or .htm.
func isLocalHTMLFile(path string) bool {
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	ext := tools.GetFileExtension(path)
	return ext == ".html" || ext == ".htm"
}

// screenshotHTMLFile serves the HTML file via a temporary localhost HTTP server,
// takes a screenshot with the headless browser, and returns the screenshot path.
func screenshotHTMLFile(ctx context.Context, a *Agent, htmlPath string, viewportWidth, viewportHeight int) (string, error) {
	// Resolve to absolute path
	absPath, err := filepath.Abs(htmlPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve HTML file path: %w", err)
	}

	// Verify the file exists
	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("failed to find HTML file: %s: %w", absPath, err)
	}

	// Serve only the single HTML file via a temp HTTP server.
	// Using http.ServeFile instead of http.FileServer on a directory
	// eliminates path traversal risks entirely.
	fileHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, absPath)
	})

	server := &http.Server{
		Handler:           fileHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       10 * time.Second,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to start temporary HTTP server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	go server.Serve(listener)
	defer func() {
		// Close listener first to unblock the server.Serve goroutine immediately,
		// then do a graceful shutdown with timeout.
		listener.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/%s", port, filepath.Base(absPath))
	a.debugLog("Serving HTML file at temporary URL: %s\n", url)

	screenshotPath, screenshotErr := captureScreenshot(ctx, a, url, viewportWidth, viewportHeight)
	if screenshotErr != nil {
		return "", screenshotErr
	}

	a.debugLog("HTML screenshot saved to: %s\n", screenshotPath)
	return screenshotPath, nil
}

// screenshotRemoteURL screenshots a remote URL using the headless browser.
func screenshotRemoteURL(ctx context.Context, a *Agent, targetURL string, viewportWidth, viewportHeight int) (string, error) {
	a.debugLog("Screenshotting remote URL: %s\n", targetURL)
	screenshotPath, err := captureScreenshot(ctx, a, targetURL, viewportWidth, viewportHeight)
	if err != nil {
		return "", fmt.Errorf("failed to screenshot URL %s: %w", targetURL, err)
	}

	a.debugLog("URL screenshot saved to: %s\n", screenshotPath)
	return screenshotPath, nil
}

// captureScreenshot creates a temp screenshot file and uses the headless browser to capture it.
func captureScreenshot(ctx context.Context, a *Agent, target string, viewportWidth, viewportHeight int) (string, error) {
	screenshotPath := filepath.Join(os.TempDir(), "ledit_examples", fmt.Sprintf("html_screenshot_%d.png", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(screenshotPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create screenshot directory: %w", err)
	}

	browser := webcontent.GetGlobalBrowser()
	if err := browser.Screenshot(ctx, target, screenshotPath, viewportWidth, viewportHeight, ""); err != nil {
		_ = os.Remove(screenshotPath)
		if strings.Contains(err.Error(), "browser rendering not available") {
			return "", fmt.Errorf("cannot analyze '%s': a headless browser is required to render HTML content. "+
				"Please rebuild with the 'browser' build tag (e.g., go build -tags browser ...)", target)
		}
		return "", fmt.Errorf("failed to screenshot '%s': %w", target, err)
	}

	return screenshotPath, nil
}

// renderHTMLContent renders HTML content to a screenshot, handling both local files and URLs.
func renderHTMLContent(ctx context.Context, a *Agent, htmlPath string, viewportWidth, viewportHeight int) (string, error) {
	lower := strings.ToLower(htmlPath)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return screenshotRemoteURL(ctx, a, htmlPath, viewportWidth, viewportHeight)
	}
	return screenshotHTMLFile(ctx, a, htmlPath, viewportWidth, viewportHeight)
}

func handleAnalyzeImageContent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_image_content tool")
	}

	imagePath := args["image_path"].(string)
	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}
	analysisMode := "general"
	if v, ok := args["analysis_mode"].(string); ok && strings.TrimSpace(v) != "" {
		analysisMode = v
	}

	a.debugLog("Analyzing image: %s (mode=%s)\n", imagePath, analysisMode)

	// Detect HTML content and render via headless browser before vision analysis.
	effectiveImagePath := imagePath
	fallbackCleanup := func() {}
	defer fallbackCleanup()
	if resolvedPath, cleanup, usedFallback := resolveVisionToolInputPath(a, imagePath); usedFallback {
		a.debugLog("[img] Falling back to attached image for image analysis: %s -> %s\n", imagePath, resolvedPath)
		effectiveImagePath = resolvedPath
		fallbackCleanup = cleanup
	}
	if isLocalHTMLFile(imagePath) || tools.IsHTMLInput(imagePath) {
		a.debugLog("HTML content detected, rendering via headless browser: %s\n", imagePath)
		screenshotPath, screenshotErr := renderHTMLContent(ctx, a, imagePath, 1280, 720)
		if screenshotErr != nil {
			return "", screenshotErr
		}
		defer os.Remove(screenshotPath)
		effectiveImagePath = screenshotPath
	}

	result, err := tools.AnalyzeImage(effectiveImagePath, analysisPrompt, analysisMode)
	a.debugLog("Analyze image content error: %v\n", err)

	// Check if model download is needed
	if err != nil && strings.Contains(err.Error(), tools.ErrModelDownloadNeeded) {
		// Inform user about simplified processing
		prompt := "PDF processing has been simplified and no longer requires model downloads. The PDF will be processed using the new approach."
		choices := []ChoiceOption{
			{Label: "Continue with simplified processing", Value: "yes"},
			{Label: "Skip", Value: "no"},
		}

		a.PrintLine(fmt.Sprintf("\n[*] %s\n", prompt))

		choice, promptErr := a.PromptChoice(prompt, choices)
		if promptErr != nil {
			a.PrintLine(fmt.Sprintf("[WARN] Could not prompt for choice: %v", promptErr))
			return result, fmt.Errorf("prompt choice failed: %w", promptErr)
		}

		if choice == "yes" {
			// The simplified PDF processing doesn't require model downloads
			a.PrintLine("[~] Processing PDF with simplified approach...")
			result, err = tools.AnalyzeImage(imagePath, analysisPrompt, analysisMode)
		}
	}

	if err != nil {
		return result, fmt.Errorf("image analysis failed: %w", err)
	}
	a.captureVisionInputAndOutput(imagePath, result)

	normalized, normalizeErr := normalizeVisionToolOutput(result, false)
	if normalizeErr != nil {
		return "", normalizeErr
	}
	return normalized, nil
}

// handleAnalyzeImageContentWithImages is the image-capable analyze_image_content handler.
// When the primary model supports vision, it sends image data directly as multimodal content
// instead of routing through a separate OCR model. PDFs still use the OCR pipeline.
func handleAnalyzeImageContentWithImages(ctx context.Context, a *Agent, args map[string]interface{}) ([]api.ImageData, string, error) {
	if a == nil {
		result, err := handleAnalyzeImageContent(ctx, a, args)
		return nil, result, utils.WrapError(err, "analyze image content")
	}

	imagePath, ok := args["image_path"].(string)
	if !ok || imagePath == "" {
		result, err := handleAnalyzeImageContent(ctx, a, args)
		return nil, result, utils.WrapError(err, "analyze image content")
	}

	// Only use multimodal path when primary model supports vision
	if a.client == nil || !a.client.SupportsVision() {
		result, err := handleAnalyzeImageContent(ctx, a, args)
		return nil, result, utils.WrapError(err, "analyze image content")
	}

	// For PDFs, use multimodal pipeline
	lowerPath := strings.ToLower(imagePath)
	if strings.HasSuffix(lowerPath, ".pdf") {
		a.debugLog("[doc] PDF detected, processing via multimodal pipeline\n")
		return handleAnalyzePDFWithImages(ctx, a, imagePath, args)
	}

	// Handle HTML URLs via screenshot first
	effectiveImagePath := imagePath
	fallbackCleanup := func() {}
	defer fallbackCleanup()
	if resolvedPath, cleanup, usedFallback := resolveVisionToolInputPath(a, imagePath); usedFallback {
		a.debugLog("[img] Falling back to attached image for multimodal analysis: %s -> %s\n", imagePath, resolvedPath)
		effectiveImagePath = resolvedPath
		fallbackCleanup = cleanup
	}
	if isLocalHTMLFile(effectiveImagePath) || tools.IsHTMLInput(effectiveImagePath) {
		a.debugLog("HTML content detected, rendering via headless browser: %s\n", effectiveImagePath)
		screenshotPath, screenshotErr := renderHTMLContent(ctx, a, effectiveImagePath, 1280, 720)
		if screenshotErr != nil {
			// Fall back to standard OCR pipeline
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
		defer os.Remove(screenshotPath)
		effectiveImagePath = screenshotPath
	}

	// Read image data
	var data []byte
	var resolvedPath string

	if strings.HasPrefix(strings.ToLower(effectiveImagePath), "http://") || strings.HasPrefix(strings.ToLower(effectiveImagePath), "https://") {
		// Download remote image
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Get(effectiveImagePath)
		if err != nil {
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
		// Limit download size to 20MB
		data, err = io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
		if err != nil {
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
		resolvedPath = effectiveImagePath
	} else {
		// Local file
		var err error
		resolvedPath, err = filesystem.SafeResolvePathWithBypass(ctx, effectiveImagePath)
		if err != nil {
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
		data, err = os.ReadFile(resolvedPath)
		if err != nil {
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
	}

	// Validate image via magic bytes
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		a.debugLog("[WARN] File is not a valid image, falling back to OCR pipeline: %s\n", imagePath)
		result, err := handleAnalyzeImageContent(ctx, a, args)
		return nil, result, utils.WrapError(err, "analyze image content")
	}

	// Optimize/resize if needed
	optimizedData, optimizedMIME, optErr := tools.OptimizeImageData(resolvedPath, data)
	if optErr != nil {
		a.debugLog("[WARN] Image optimization failed: %v, using original\n", optErr)
	} else if len(optimizedData) > 0 {
		data = optimizedData
		if optimizedMIME != "" {
			mimeType = optimizedMIME
		}
	}

	// Check size after optimization
	if len(data) > console.MaxPastedImageSize {
		a.debugLog("[WARN] Optimized image still too large (%d bytes), falling back to OCR\n", len(data))
		result, err := handleAnalyzeImageContent(ctx, a, args)
		return nil, result, utils.WrapError(err, "analyze image content")
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(data)

	// Build descriptive text result
	prompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		prompt = v
	}
	textResult := fmt.Sprintf("[Image analyzed: %s (%s, %d bytes)]", imagePath, mimeType, len(data))
	if prompt != "" {
		textResult += "\nAnalysis prompt: " + prompt
	}

	images := []api.ImageData{{
		Base64: encoded,
		Type:   mimeType,
	}}

	return images, textResult, nil
}

func resolveVisionToolInputPath(a *Agent, imagePath string) (string, func(), bool) {
	trimmed := strings.TrimSpace(imagePath)
	if trimmed == "" {
		return imagePath, func() {}, false
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return imagePath, func() {}, false
	}

	if localFileExists(trimmed) {
		return imagePath, func() {}, false
	}

	img, ok := latestAttachedUserImage(a)
	if !ok {
		return imagePath, func() {}, false
	}

	resolvedPath, cleanup, err := materializeImageDataForVisionTool(img)
	if err != nil {
		if a != nil {
			a.debugLog("[WARN] Failed to materialize attached image fallback for %s: %v\n", imagePath, err)
		}
		return imagePath, func() {}, false
	}

	return resolvedPath, cleanup, true
}

func localFileExists(path string) bool {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir()
	}

	if filepath.IsAbs(path) {
		return false
	}

	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		return false
	}
	info, err = os.Stat(absPath)
	return err == nil && !info.IsDir()
}

func latestAttachedUserImage(a *Agent) (api.ImageData, bool) {
	if a == nil {
		return api.ImageData{}, false
	}

	for i := len(a.messages) - 1; i >= 0; i-- {
		msg := a.messages[i]
		if msg.Role != "user" || len(msg.Images) == 0 {
			continue
		}
		return msg.Images[0], true
	}

	return api.ImageData{}, false
}

func materializeImageDataForVisionTool(img api.ImageData) (string, func(), error) {
	if url := strings.TrimSpace(img.URL); url != "" {
		return url, func() {}, nil
	}

	encoded := strings.TrimSpace(img.Base64)
	if encoded == "" {
		return "", nil, fmt.Errorf("attached image has no URL or base64 data")
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode attached image data: %w", err)
	}

	ext, _ := console.DetectImageMagic(data)
	if ext == "" {
		ext = extensionForImageMIME(img.Type)
	}
	if ext == "" {
		ext = ".png"
	}

	tempFile, err := os.CreateTemp("", "ledit-attached-image-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp image file: %w", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)
		return "", nil, fmt.Errorf("failed to write temp image file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", nil, fmt.Errorf("failed to close temp image file: %w", err)
	}

	return tempPath, func() {
		_ = os.Remove(tempPath)
	}, nil
}

func extensionForImageMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/avif":
		return ".avif"
	default:
		return ""
	}
}

func normalizeVisionToolOutput(result string, preferPlainText bool) (string, error) {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return result, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return result, nil
	}

	var parsed tools.ImageAnalysisResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return result, nil
	}

	if !parsed.Success {
		code := strings.TrimSpace(parsed.ErrorCode)
		msg := strings.TrimSpace(parsed.ErrorMessage)
		if code == "" && msg == "" {
			return "", fmt.Errorf("vision analysis failed")
		}
		if code == "" {
			return "", fmt.Errorf("vision analysis failed: %s", msg)
		}
		if msg == "" {
			return "", fmt.Errorf("vision analysis failed (%s)", code)
		}
		return "", fmt.Errorf("vision analysis failed (%s): %s", code, msg)
	}

	if !preferPlainText {
		return result, nil
	}

	if text := strings.TrimSpace(parsed.ExtractedText); text != "" {
		return text, nil
	}
	if parsed.Analysis != nil {
		if desc := strings.TrimSpace(parsed.Analysis.Description); desc != "" {
			return desc, nil
		}
	}
	return result, nil
}

// handleAnalyzePDFWithImages processes a PDF for multimodal consumption.
// For text-based PDFs, returns extracted text. For scanned/image PDFs, renders
// pages as images so the model can visually analyze them. Falls back to the
// full OCR pipeline if multimodal processing fails.
func handleAnalyzePDFWithImages(ctx context.Context, a *Agent, path string, args map[string]interface{}) ([]api.ImageData, string, error) {
	// Resolve path (handle remote URLs and local files)
	var effectivePath string
	var cleanup func()

	if strings.HasPrefix(strings.ToLower(path), "http://") || strings.HasPrefix(strings.ToLower(path), "https://") {
		resolvedPath, resolvedCleanup, resolveErr := tools.ResolvePDFInputPath(path)
		if resolveErr != nil || resolvedPath == "" {
			a.debugLog("[WARN] Failed to resolve remote PDF: %v\n", resolveErr)
			// Fall back to text-only pipeline
			result, err := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(err, "analyze image content")
		}
		effectivePath = resolvedPath
		cleanup = resolvedCleanup
	} else {
		var err error
		effectivePath, err = filesystem.SafeResolvePathWithBypass(ctx, path)
		if err != nil {
			result, ferr := handleAnalyzeImageContent(ctx, a, args)
			return nil, result, utils.WrapError(ferr, "analyze image content")
		}
	}

	result, err := tools.ProcessPDFForMultimodal(effectivePath)

	if cleanup != nil {
		cleanup()
	}

	if err != nil {
		// Fall back to full OCR pipeline (existing behavior)
		a.debugLog("[WARN] Multimodal PDF processing failed: %v, falling back to OCR pipeline\n", err)
		result, err := handleAnalyzeImageContent(ctx, a, args)
		return nil, result, utils.WrapError(err, "analyze image content")
	}

	// Extract optional analysis_prompt for text result
	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}

	if len(result.Images) > 0 {
		textResult := fmt.Sprintf("[PDF analyzed: %s (%d pages rendered as images)]", path, len(result.Images))
		if analysisPrompt != "" {
			textResult += "\nAnalysis prompt: " + analysisPrompt
		}
		return result.Images, textResult, nil
	}

	// Text was extractable via pypdf — return directly to model
	textResult := fmt.Sprintf("[PDF content: %s (extracted as text)]\n\n%s", path, result.Text)
	if analysisPrompt != "" {
		textResult += "\nAnalysis prompt: " + analysisPrompt
	}
	return nil, textResult, nil
}
