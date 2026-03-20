package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
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
	if isLocalHTMLFile(imagePath) || tools.IsHTMLInput(imagePath) {
		a.debugLog("HTML content detected, rendering via headless browser: %s\n", imagePath)
		screenshotPath, err := renderHTMLContent(ctx, a, imagePath, viewportWidth, viewportHeight)
		if err != nil {
			return "", err
		}
		defer os.Remove(screenshotPath)
		effectiveImagePath = screenshotPath
	}

	result, err := tools.AnalyzeImage(effectiveImagePath, analysisPrompt, "frontend")
	a.debugLog("Analyze UI screenshot error: %v\n", err)
	if err != nil {
		return result, err
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
		return "", fmt.Errorf("HTML file not found: %s: %w", absPath, err)
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
		return "", err
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

		a.PrintLine(fmt.Sprintf("\n✨ %s\n", prompt))

		choice, promptErr := a.PromptChoice(prompt, choices)
		if promptErr != nil {
			a.PrintLine(fmt.Sprintf("⚠️ Could not prompt for choice: %v", promptErr))
			return result, err
		}

		if choice == "yes" {
			// The simplified PDF processing doesn't require model downloads
			a.PrintLine("🔄 Processing PDF with simplified approach...")
			result, err = tools.AnalyzeImage(imagePath, analysisPrompt, analysisMode)
		}
	}

	if err != nil {
		return result, err
	}
	a.captureVisionInputAndOutput(imagePath, result)

	normalized, normalizeErr := normalizeVisionToolOutput(result, false)
	if normalizeErr != nil {
		return "", normalizeErr
	}
	return normalized, nil
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
