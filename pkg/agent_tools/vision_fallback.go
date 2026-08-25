//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ============================================================================
// SP-103-A8 — Graceful OCR fallback when vision model fails
//
// When the primary vision model exhausts its retries (DoVisionRetry gives up),
// this package provides a transparent fallback to the configured OCR model
// (PDFOCRModel) as a last resort. The fallback is:
//
//  1. Gated by VISION_FALLBACK_TO_OCR env var (default: true).
//  2. Requires PDFOCRModel to be configured.
//  3. A single-shot attempt (no further retries).
//  4. Always logged at INFO level for every transition.
// ============================================================================

// shouldFallbackToOCR determines whether a fallback attempt is warranted.
// Currently any non-nil error after DoVisionRetry is a fallback candidate.
// (Future refinement: only on selected error types like 503/timeout, not 4xx.)
func shouldFallbackToOCR(err error) bool {
	return err != nil
}

// isFallbackEnabled checks whether the OCR fallback feature is enabled.
// It reads VISION_FALLBACK_TO_OCR env var (SPROUT_ / SPROUT_ prefixes),
// defaulting to true.
func isFallbackEnabled() bool {
	// Check env var first (runtime override)
	if raw := configuration.GetEnvSimple("VISION_FALLBACK_TO_OCR"); raw != "" {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "0", "false", "no", "off":
			return false
		}
	}
	// Check persisted config (if available)
	cfgManager, err := configuration.NewManager()
	if err == nil {
		cfg := cfgManager.GetConfig()
		if cfg != nil && cfg.VisionFallbackToOCR {
			return true
		}
	}
	// Default: enabled
	return true
}

// getOCRModel returns the configured OCR model name for fallback use.
// Returns empty string if not configured.
func getOCRModel() string {
	cfgManager, err := configuration.NewManager()
	if err != nil {
		return ""
	}
	cfg := cfgManager.GetConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.PDFOCRModel)
}

// fallbackToOCR attempts a single OCR analysis as a last resort when the
// primary vision model has already failed after retries.
//
// It creates a one-off Ollama client targeting the configured OCR model,
// sends the image with an OCR prompt, and returns the result.
// No further retries are performed — this is the last line of defense.
// imageData is the base64-encoded image string (same format as returned by
// GetImageData).
func (vp *VisionProcessor) fallbackToOCR(
	ctx context.Context,
	imagePath, originalPrompt, imageType string,
	imageData string,
	lastErr error,
) (VisionAnalysis, error) {
	// Check if fallback is enabled
	if !isFallbackEnabled() {
		return VisionAnalysis{}, lastErr
	}

	// Check if OCR model is configured
	ocrModel := getOCRModel()
	if ocrModel == "" {
		vp.loggerInfo("OCR fallback skipped: VISION_FALLBACK_TO_OCR=true but PDFOCRModel not set")
		return VisionAnalysis{}, fmt.Errorf("vision request: %w (OCR fallback skipped: PDFOCRModel not set)", lastErr)
	}

	vp.loggerInfo("vision primary failed; falling back to OCR model",
		"image", imagePath, "ocr_model", ocrModel)

	// Create OCR prompt — use the OCR-specific prompt for text extraction
	ocrPrompt := GetOCRPrompt()

	// Build messages with the OCR prompt
	messages := []api.Message{
		{
			Role:    "user",
			Content: ocrPrompt,
			Images:  []api.ImageData{{Base64: imageData, Type: imageType}},
		},
	}

	// Create a one-off Ollama client for the OCR model
	ocrClient, err := CreateOllamaClient(ocrModel)
	if err != nil {
		vp.loggerInfo("OCR fallback client creation failed", "err", err.Error())
		return VisionAnalysis{}, fmt.Errorf("vision request: %w (OCR fallback client creation failed: %v)", lastErr, err)
	}

	// Send the OCR request — single shot, no retries
	response, ocrErr := ocrClient.SendVisionRequest(ctx, messages, nil, "", false)
	if ocrErr != nil {
		vp.loggerInfo("OCR fallback also failed", "err", ocrErr.Error())
		return VisionAnalysis{}, fmt.Errorf("vision request: %w (OCR fallback also failed: %v)", lastErr, ocrErr)
	}

	// Parse the OCR response
	vp.loggerInfo("OCR fallback succeeded", "image", imagePath, "model", ocrModel)
	analysis, parseErr := parseVisionResponse(response, imagePath)
	if parseErr != nil {
		vp.loggerInfo("OCR fallback response parse failed", "err", parseErr.Error())
		return VisionAnalysis{}, fmt.Errorf("vision request: %w (OCR fallback response parse failed: %v)", lastErr, parseErr)
	}

	return analysis, nil
}

// parseVisionResponse extracts a VisionAnalysis from a ChatResponse.
// Shared between AnalyzeImage and fallbackToOCR to avoid duplication.
func parseVisionResponse(response *api.ChatResponse, imagePath string) (VisionAnalysis, error) {
	if response == nil || len(response.Choices) == 0 {
		return VisionAnalysis{}, fmt.Errorf("no response from vision model")
	}

	resultText := response.Choices[0].Message.Content

	// Try to parse as JSON first, fall back to plain text
	var analysis VisionAnalysis
	if err := json.Unmarshal([]byte(resultText), &analysis); err != nil {
		// If JSON parsing fails, use as plain description
		analysis = VisionAnalysis{
			ImagePath:   imagePath,
			Description: resultText,
		}
	} else {
		// Ensure image path is set
		analysis.ImagePath = imagePath
	}

	return analysis, nil
}

// loggerInfo emits a structured INFO log line. Uses the VisionProcessor's
// logger if available, otherwise falls back to fmt.Println.
func (vp *VisionProcessor) loggerInfo(msg string, keysAndValues ...interface{}) {
	// Format key=value pairs appended to the message.
	formatted := msg
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		val := fmt.Sprintf("%v", keysAndValues[i+1])
		formatted += " " + key + "=" + val
	}
	if vp.logger != nil {
		vp.logger.Logf("[INFO] vision_fallback: %s", formatted)
		return
	}
	console.GlyphInfo.Fprintln(os.Stdout, fmt.Sprintf("vision_fallback: %s", formatted))
}
