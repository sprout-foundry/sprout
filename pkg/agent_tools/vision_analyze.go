//go:build !js

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ============================================================================
// Image Analysis Methods
// ============================================================================

// ProcessImagesInText detects images in text and processes them with vision models
func (vp *VisionProcessor) ProcessImagesInText(ctx context.Context, text string) (string, []VisionAnalysis, error) {
	if vp.debug {
		fmt.Println("[search] Scanning text for image references...")
	}

	// Find image references in the text
	images := vp.extractImageReferences(text)
	if len(images) == 0 {
		return text, nil, nil
	}

	if vp.debug {
		fmt.Printf("[photo] Found %d image references\n", len(images))
	}

	var analyses []VisionAnalysis
	enhancedText := text

	// Process each image
	for i, imgPath := range images {
		if vp.debug {
			fmt.Printf("[search] Analyzing image %d: %s\n", i+1, imgPath)
		}

		analysis, err := vp.AnalyzeImage(ctx, imgPath)
		if err != nil {
			if vp.debug {
				console.GlyphWarning.Fprintf(os.Stdout, "Failed to analyze %s: %v", imgPath, err)
			}
			continue
		}

		analyses = append(analyses, analysis)

		// Replace image reference with detailed analysis
		enhancedText = vp.EnhanceTextWithAnalysis(enhancedText, imgPath, analysis)
	}

	if vp.debug && len(analyses) > 0 {
		console.GlyphSuccess.Fprintf(os.Stdout, "Successfully analyzed %d images", len(analyses))
	}

	return enhancedText, analyses, nil
}

// extractImageReferences finds image file paths or URLs in text
func (vp *VisionProcessor) extractImageReferences(text string) []string {
	var images []string

	// Common image file patterns
	imagePatterns := []string{
		// File paths
		`[^\s]+\.(?i:png|jpg|jpeg|gif|bmp|webp|avif|svg)`,
		// URLs
		`https?://[^\s]+\.(?i:png|jpg|jpeg|gif|bmp|webp|avif|svg)`,
		// Markdown image syntax
		`!\[[^\]]*\]\(([^)]+\.(?i:png|jpg|jpeg|gif|bmp|webp|avif|svg))\)`,
	}

	for _, pattern := range imagePatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllString(text, -1)
		for _, match := range matches {
			// For markdown syntax, extract URL from parentheses
			if strings.Contains(match, "](") {
				if markdownRe := regexp.MustCompile(`\(([^)]+)\)`); markdownRe.MatchString(match) {
					url := markdownRe.FindStringSubmatch(match)[1]
					images = append(images, url)
				}
			} else {
				images = append(images, match)
			}
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var unique []string
	for _, img := range images {
		if !seen[img] {
			seen[img] = true
			unique = append(unique, img)
		}
	}

	return unique
}

// AnalyzeImage processes a single image with the vision model.
// If optionalPrompt is provided and non-empty, it is used as the prompt;
// otherwise the default vision prompt for imagePath is created.
func (vp *VisionProcessor) AnalyzeImage(ctx context.Context, imagePath string, optionalPrompt ...string) (VisionAnalysis, error) {
	// Download or read the image
	imageData, imageType, err := vp.GetImageData(ctx, imagePath)
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("get image data: %w", err)
	}

	// Use optional prompt if provided and non-empty, otherwise create default
	var prompt string
	if len(optionalPrompt) > 0 && optionalPrompt[0] != "" {
		prompt = optionalPrompt[0]
	} else {
		prompt = vp.CreateVisionPrompt(imagePath)
	}

	// Create message with image
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
			Images:  []api.ImageData{{Base64: imageData, Type: imageType}},
		},
	}

	// VISION-5: per-call retry stats collector.
	var retryStats RetryStats

	// Get vision analysis using the vision-enabled method. The parent
	// context is threaded through so the Stop button can abort in-flight
	// vision calls (SP-034-1c).
	var response *api.ChatResponse
	reqStart := time.Now()
	err = DoVisionRetry(ctx, func(ctx context.Context) error {
		var innerErr error
		response, innerErr = vp.visionClient.SendVisionRequest(ctx, messages, nil, "", false)
		return innerErr
	}, RetryOptions{OpName: "analyze_image", Stats: &retryStats})
	reqDuration := time.Since(reqStart)
	AddVisionLatencyRequest(reqDuration)

	usedFallback := false
	ocrFallbackSuccess := false
	var fallbackDuration time.Duration

	if err != nil {
		// SP-103-A8: attempt OCR fallback when primary vision model fails
		if shouldFallbackToOCR(err) {
			usedFallback = true
			IncVisionFallbackTotal()
			fbStart := time.Now()
			analysis, fbErr := vp.fallbackToOCR(ctx, imagePath, prompt, imageType, imageData, err)
			fallbackDuration = time.Since(fbStart)
			AddVisionLatencyFallback(fallbackDuration)
			if fbErr == nil {
				ocrFallbackSuccess = true
				IncVisionFallbackSuccess()

				// VISION-5: emit per-call record for successful fallback.
				AppendVisionRecord(VisionMetricsRecord{
					OpName:             "analyze_image",
					ImageCount:         1,
					Success:            true,
					RetryCount:         retryStats.RetryCount,
					UsedOCRFallback:    true,
					OCRFallbackSuccess: true,
					LatencyRequestMS:   reqDuration.Milliseconds(),
					LatencyRetrySleepMS: retryStats.SleepDuration.Milliseconds(),
					LatencyFallbackMS:  fallbackDuration.Milliseconds(),
				})

				return analysis, nil
			}
			// Fallback exhausted; return the composed error.
			// VISION-5: emit per-call record for failed fallback.
			AppendVisionRecord(VisionMetricsRecord{
				OpName:             "analyze_image",
				ImageCount:         1,
				Success:            false,
				FailureReason:      classifyVisionError(fbErr),
				RetryCount:         retryStats.RetryCount,
				UsedOCRFallback:    true,
				OCRFallbackSuccess: false,
				LatencyRequestMS:   reqDuration.Milliseconds(),
				LatencyRetrySleepMS: retryStats.SleepDuration.Milliseconds(),
				LatencyFallbackMS:  fallbackDuration.Milliseconds(),
			})
			return VisionAnalysis{}, fbErr
		}
		// VISION-5: emit per-call record for non-fallback failure.
		AppendVisionRecord(VisionMetricsRecord{
			OpName:             "analyze_image",
			ImageCount:         1,
			Success:            false,
			FailureReason:      classifyVisionError(err),
			RetryCount:         retryStats.RetryCount,
			LatencyRequestMS:   reqDuration.Milliseconds(),
			LatencyRetrySleepMS: retryStats.SleepDuration.Milliseconds(),
		})
		return VisionAnalysis{}, fmt.Errorf("vision request: %w", err)
	}

	// Store usage information for cost tracking (per-session + global mirror)
	var imageTokens, imageTokensCached int
	if response.Usage.TotalTokens > 0 {
		recordVisionUsage(vp, &VisionUsageInfo{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
		})
		// SP-103-C4: emit vision_image_tokens metric. Cached reads show
		// up as input tokens too but cost less — we record the count
		// (not the dollar value) so callers can compute costs separately.
		imageTokens = response.Usage.PromptTokens
		imageTokensCached = response.Usage.CachedTokens
		IncVisionImageTokens(imageTokens, imageTokensCached)
	}
	// SP-103-C4: count this as an OCR call (analyze_image_content path).
	IncVisionOCRCall()

	// Extract response content
	parseStart := time.Now()
	analysis, parseErr := parseVisionResponse(response, imagePath)
	parseDuration := time.Since(parseStart)
	AddVisionLatencyParse(parseDuration)
	if parseErr != nil {
		return VisionAnalysis{}, parseErr
	}

	// VISION-5: emit per-call record for successful primary path.
	AppendVisionRecord(VisionMetricsRecord{
		OpName:             "analyze_image",
		ImageCount:         1,
		Success:            true,
		RetryCount:         retryStats.RetryCount,
		UsedOCRFallback:    usedFallback,
		OCRFallbackSuccess: ocrFallbackSuccess,
		LatencyRequestMS:   reqDuration.Milliseconds(),
		LatencyRetrySleepMS: retryStats.SleepDuration.Milliseconds(),
		LatencyFallbackMS:  fallbackDuration.Milliseconds(),
		LatencyParseMS:     parseDuration.Milliseconds(),
		ImageTokens:        imageTokens,
		ImageTokensCached:  imageTokensCached,
	})

	return analysis, nil
}

// ============================================================================
// UI Element Extraction
// ============================================================================

// LooksLikeUI determines if the description suggests a UI interface
func (vp *VisionProcessor) LooksLikeUI(description string) bool {
	uiKeywords := []string{"button", "input", "form", "menu", "navigation", "interface", "screen", "page", "component"}
	lowerDesc := strings.ToLower(description)

	count := 0
	for _, keyword := range uiKeywords {
		if strings.Contains(lowerDesc, keyword) {
			count++
		}
	}

	return count >= 2 // If we find 2+ UI-related keywords, it's likely a UI
}

// ExtractUIElements attempts to extract structured UI elements from the description
func (vp *VisionProcessor) ExtractUIElements(description string) []UIElement {
	// This is a simplified extraction - could be enhanced with more sophisticated parsing
	var elements []UIElement

	// Look for common UI element mentions
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		if element := vp.ParseUIElementFromLine(line); element.Type != "" {
			elements = append(elements, element)
		}
	}

	return elements
}

// ParseUIElementFromLine attempts to extract a UI element from a description line
func (vp *VisionProcessor) ParseUIElementFromLine(line string) UIElement {
	lowerLine := strings.ToLower(line)

	// Simple pattern matching for UI elements
	patterns := map[string]string{
		"button":   `(?i)(button|btn)`,
		"input":    `(?i)(input|field|textbox)`,
		"text":     `(?i)(text|label|heading)`,
		"link":     `(?i)(link|anchor)`,
		"image":    `(?i)(image|img|icon)`,
		"dropdown": `(?i)(dropdown|select)`,
		"checkbox": `(?i)(checkbox|check)`,
		"radio":    `(?i)(radio)`,
	}

	for elementType, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, lowerLine); matched {
			return UIElement{
				Type:        elementType,
				Description: strings.TrimSpace(line),
				Position:    vp.ExtractPosition(line),
			}
		}
	}

	return UIElement{}
}

// ExtractPosition attempts to extract position information from a description
func (vp *VisionProcessor) ExtractPosition(line string) string {
	positionKeywords := []string{"top", "bottom", "left", "right", "center", "upper", "lower", "corner"}
	lowerLine := strings.ToLower(line)

	for _, keyword := range positionKeywords {
		if strings.Contains(lowerLine, keyword) {
			return keyword
		}
	}

	return "unknown"
}

// EnhanceTextWithAnalysis replaces image references with detailed analysis
func (vp *VisionProcessor) EnhanceTextWithAnalysis(text, imagePath string, analysis VisionAnalysis) string {
	// Create enhanced description
	enhancement := fmt.Sprintf(`

## Image Analysis: %s

**Visual Description:**
%s

`, filepath.Base(imagePath), analysis.Description)

	// Add UI elements if detected
	if len(analysis.Elements) > 0 {
		enhancement += "**UI Elements Detected:**\n"
		for _, element := range analysis.Elements {
			enhancement += fmt.Sprintf("- **%s** (%s): %s\n",
				strings.Title(element.Type),
				element.Position,
				element.Description)
		}
		enhancement += "\n"
	}

	// Replace image reference with enhanced description
	// Try multiple replacement strategies
	replacements := []string{
		imagePath,                // Direct path
		filepath.Base(imagePath), // Just filename
		fmt.Sprintf("![%s](%s)", filepath.Base(imagePath), imagePath), // Markdown format
	}

	for _, replacement := range replacements {
		if strings.Contains(text, replacement) {
			text = strings.Replace(text, replacement, enhancement, 1)
			break
		}
	}

	return text
}

// ProcessPDFForVision processes PDF using the configured vision/OCR model
func (vp *VisionProcessor) ProcessPDFForVision(ctx context.Context, pdfPath string) (VisionAnalysis, error) {
	text, err := ProcessPDFWithVision(ctx, pdfPath)
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("PDF OCR: %w", err)
	}

	return VisionAnalysis{
		ImagePath:   pdfPath,
		Description: text,
	}, nil
}
