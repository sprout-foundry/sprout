//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

	// Get vision analysis using the vision-enabled method. The parent
	// context is threaded through so the Stop button can abort in-flight
	// vision calls (SP-034-1c).
	var response *api.ChatResponse
	err = DoVisionRetry(ctx, func(ctx context.Context) error {
		var innerErr error
		response, innerErr = vp.visionClient.SendVisionRequest(ctx, messages, nil, "", false)
		return innerErr
	}, RetryOptions{OpName: "analyze_image"})

	if err != nil {
		// The delegated-vision path has no second-tier fallback by design:
		// an honest error lets the calling model retry or switch strategy.
		// Non-vision primaries already received native-OCR text upstream
		// (read_file / analyze_image_content); a failure here means the
		// delegated vision tier itself is down.
		return VisionAnalysis{}, fmt.Errorf("vision request: %w", err)
	}

	// Extract response content
	analysis, parseErr := parseVisionResponse(response, imagePath)
	if parseErr != nil {
		return VisionAnalysis{}, parseErr
	}

	return analysis, nil
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

// parseVisionResponse extracts a VisionAnalysis from a ChatResponse.
// Tries JSON first (the structured prompts request it); falls back to
// treating the whole content as the description.
func parseVisionResponse(response *api.ChatResponse, imagePath string) (VisionAnalysis, error) {
	if response == nil || len(response.Choices) == 0 {
		return VisionAnalysis{}, fmt.Errorf("no response from vision model")
	}

	resultText := response.Choices[0].Message.Content

	var analysis VisionAnalysis
	if err := json.Unmarshal([]byte(resultText), &analysis); err != nil {
		analysis = VisionAnalysis{
			ImagePath:   imagePath,
			Description: resultText,
		}
	} else {
		analysis.ImagePath = imagePath
	}

	return analysis, nil
}
