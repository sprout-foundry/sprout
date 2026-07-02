//go:build !js

// vision_batch.go — VISION-4: multimodal batched vision analysis.
//
// When to use batching vs. parallel:
//
//   - processOCRImagesParallel (vision_parallel.go) — parallel worker pool for
//     per-image OCR. Each image is processed independently by a separate
//     goroutine. Use this for PDF pages or standalone text extraction.
//
//   - AnalyzeImagesBatched (this file) — true multimodal joint analysis where
//     the model sees ALL images in a single provider call. The model can
//     compare, contrast, and reason about the images collectively. Use this
//     when the user presents multiple images together (e.g., "compare these
//     screenshots", "analyze this diagram and its reference").
//
// The batched path sends ONE provider request with N images embedded, then
// parses the response into N per-image analyses. Failed per-image sections
// fall back to single-image processing. Results are cached by content hash.

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/errors"
)

// BatchVisionRequest holds the inputs for a batched vision analysis call.
// Images are raw bytes (not base64); they are encoded internally.
// Prompts can be one per image (len(Prompts) == len(Images)) or a single
// shared prompt (len(Prompts) == 1) that applies to all images.
type BatchVisionRequest struct {
	Images  [][]byte
	Prompts []string
	Mode    string // prompt template mode; ignored if Prompts is non-empty
}

// BatchVisionResult holds the per-image analyses from a batched call.
// Results[i] corresponds to the i-th image in the request.
type BatchVisionResult struct {
	Results       []VisionAnalysis
	CombinedUsage *VisionUsageInfo
}

const (
	batchCachePrefix = "batch:"
	batchSectionSep  = "\x00S\x00"
)

// AnalyzeImagesBatched sends ONE provider request containing all images,
// parses the response into N per-image analyses, and caches the result.
//
// Cache key: image hashes (in original order) + prompt hash, prefixed with "batch:".
// On per-image failure (missing/empty section in response), falls back to
// single-image processing for that image only.
//
// Returns a TypedError if the client is nil with error code "validation".
func AnalyzeImagesBatched(ctx context.Context, client api.ClientInterface, req BatchVisionRequest) (*BatchVisionResult, error) {
	if client == nil {
		return nil, &errors.TypedError{
			Code:     errors.CodeValidation,
			Severity: errors.SeverityError,
			Message:  "batch vision client is nil",
		}
	}

	n := len(req.Images)
	if n == 0 {
		return &BatchVisionResult{Results: []VisionAnalysis{}, CombinedUsage: nil}, nil
	}

	// Validate that no image bytes are empty
	for i := range req.Images {
		if len(req.Images[i]) == 0 {
			return nil, &errors.TypedError{
				Code:     errors.CodeValidation,
				Severity: errors.SeverityError,
				Message:  fmt.Sprintf("batch image at index %d is empty", i),
			}
		}
	}

	IncVisionBatchAttempt()

	prompts := resolveBatchPrompts(req)
	cacheKey := buildBatchCacheKey(req.Images, prompts)

	// Check cache
	if cached, usage, ok := visionLRU.Get(cacheKey); ok {
		IncVisionBatchHit()
		// Track cached usage (mirrors AnalyzeImage's cache-hit path)
		if usage != nil {
			recordVisionUsage(nil, usage)
		}
		return parseBatchCacheResult(cached, n, usage), nil
	}
	IncVisionBatchMiss()

	// Encode images to base64
	imageDataList := make([]api.ImageData, n)
	for i, raw := range req.Images {
		imageDataList[i] = api.ImageData{
			Base64: base64.StdEncoding.EncodeToString(raw),
			Type:   detectImageMIME(raw),
		}
	}

	// Build and send ONE provider call
	batchPrompt := buildBatchPrompt(prompts, n)
	msg := api.Message{Role: "user", Content: batchPrompt, Images: imageDataList}

	response, err := client.SendVisionRequest(ctx, []api.Message{msg}, nil, "", false)
	if err != nil {
		return nil, &errors.TypedError{
			Code:     errors.CodeNetwork,
			Severity: errors.SeverityError,
			Message:  "batch vision request failed",
			Cause:    err,
		}
	}

	responseText := ""
	if len(response.Choices) > 0 {
		responseText = response.Choices[0].Message.Content
	}

	// Parse response into N per-image sections
	sections := splitResponseIntoSections(responseText, n)

	// Build results, falling back for empty sections
	results := make([]VisionAnalysis, n)
	hadPartialFailure := false

	for i := 0; i < n; i++ {
		if sections[i] == "" {
			results[i], err = analyzeImageSingle(ctx, client, req.Images[i], prompts[i])
			if err != nil {
				results[i] = VisionAnalysis{
					Description: fmt.Sprintf("[batch fallback failed for image %d: %v]", i+1, err),
				}
			}
			hadPartialFailure = true
		} else {
			results[i] = VisionAnalysis{Description: strings.TrimSpace(sections[i])}
		}
	}

	if hadPartialFailure {
		IncVisionBatchPartialFailure()
	}

	// Build usage info
	var usage *VisionUsageInfo
	if response.Usage.TotalTokens > 0 {
		usage = &VisionUsageInfo{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
		}
	}

	// Track usage for batch calls (mirrors what AnalyzeImage does)
	if usage != nil {
		recordVisionUsage(nil, usage)
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			IncVisionImageTokens(usage.PromptTokens+usage.CompletionTokens, 0)
		}
	}

	// Cache the result
	visionLRU.Put(cacheKey, serializeBatchResults(results), usage)

	return &BatchVisionResult{Results: results, CombinedUsage: usage}, nil
}

// resolveBatchPrompts ensures we have exactly N prompts (one per image).
func resolveBatchPrompts(req BatchVisionRequest) []string {
	n := len(req.Images)

	if len(req.Prompts) == 0 {
		prompt := GeneratePromptForMode(req.Mode)
		result := make([]string, n)
		for i := range result {
			result[i] = prompt
		}
		return result
	}

	if len(req.Prompts) == 1 {
		result := make([]string, n)
		for i := range result {
			result[i] = req.Prompts[0]
		}
		return result
	}

	if len(req.Prompts) > n {
		return req.Prompts[:n]
	}

	result := make([]string, n)
	copy(result, req.Prompts)
	last := req.Prompts[len(req.Prompts)-1]
	for i := len(req.Prompts); i < n; i++ {
		result[i] = last
	}
	return result
}

// buildBatchPrompt constructs a single prompt asking the model to analyze
// each image and label sections with markers for later splitting.
func buildBatchPrompt(prompts []string, n int) string {
	var sb strings.Builder
	sb.WriteString("You will receive multiple images. Analyze each one and provide your response using the following format:\n\n")

	for i := range prompts {
		sb.WriteString(fmt.Sprintf("===IMAGE_%d_START===", i+1))
		sb.WriteString(prompts[i])
		sb.WriteString(fmt.Sprintf("\n===IMAGE_%d_END===", i+1))
		if i < n-1 {
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("\n\nIMPORTANT: Use the exact markers ===IMAGE_N_START=== and ===IMAGE_N_END=== to delimit each image's analysis section.")
	return sb.String()
}

// splitResponseIntoSections splits the model's response into N sections
// using the ===IMAGE_N_START=== / ===IMAGE_N_END=== markers.
func splitResponseIntoSections(response string, n int) []string {
	sections := make([]string, n)

	for i := 0; i < n; i++ {
		startMarker := fmt.Sprintf("===IMAGE_%d_START===", i+1)
		endMarker := fmt.Sprintf("===IMAGE_%d_END===", i+1)

		startIdx := strings.Index(response, startMarker)
		endIdx := strings.Index(response, endMarker)

		if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
			sections[i] = strings.TrimSpace(response[startIdx+len(startMarker) : endIdx])
		}
	}

	return sections
}

// buildBatchCacheKey creates a deterministic cache key from image data and prompts.
// Format: "batch:" + image_hashes.join(":") + "|" + hash(prompts)
// Image order is preserved (NOT sorted) since results[i] maps to images[i].
func buildBatchCacheKey(images [][]byte, prompts []string) string {
	hashes := make([]string, len(images))
	for i, img := range images {
		h := sha256.Sum256(img)
		hashes[i] = hex.EncodeToString(h[:])
	}

	promptHash := sha256.Sum256([]byte(strings.Join(prompts, "|")))
	return fmt.Sprintf("%s%s|%s", batchCachePrefix, strings.Join(hashes, ":"), hex.EncodeToString(promptHash[:]))
}

// serializeBatchResults serializes VisionAnalysis results for cache storage.
func serializeBatchResults(results []VisionAnalysis) string {
	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString(batchSectionSep)
		}
		sb.WriteString(r.Description)
	}
	return sb.String()
}

// parseBatchCacheResult deserializes a cached batch result string.
func parseBatchCacheResult(cached string, n int, usage *VisionUsageInfo) *BatchVisionResult {
	results := make([]VisionAnalysis, n)
	parts := strings.Split(cached, batchSectionSep)

	for i := 0; i < n && i < len(parts); i++ {
		results[i] = VisionAnalysis{Description: strings.TrimSpace(parts[i])}
	}

	return &BatchVisionResult{Results: results, CombinedUsage: usage}
}

// analyzeImageSingle performs a single-image vision analysis as a fallback.
func analyzeImageSingle(ctx context.Context, client api.ClientInterface, imageData []byte, prompt string) (VisionAnalysis, error) {
	msg := api.Message{
		Role:    "user",
		Content: prompt,
		Images: []api.ImageData{{
			Base64: base64.StdEncoding.EncodeToString(imageData),
			Type:   detectImageMIME(imageData),
		}},
	}

	response, err := client.SendVisionRequest(ctx, []api.Message{msg}, nil, "", false)
	if err != nil {
		return VisionAnalysis{}, err
	}

	if len(response.Choices) == 0 {
		return VisionAnalysis{}, fmt.Errorf("no response from vision model")
	}

	// Track usage for fallback single-image calls (mirrors AnalyzeImage)
	if response.Usage.TotalTokens > 0 {
		usage := &VisionUsageInfo{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
		}
		recordVisionUsage(nil, usage)
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			IncVisionImageTokens(usage.PromptTokens+usage.CompletionTokens, 0)
		}
	}

	return VisionAnalysis{Description: response.Choices[0].Message.Content}, nil
}

// detectImageMIME returns a MIME type based on magic bytes in the image data.
func detectImageMIME(data []byte) string {
	if len(data) < 2 {
		return "image/png"
	}
	switch {
	case data[0] == 0xFF && data[1] == 0xD8:
		return "image/jpeg"
	case len(data) > 3 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return "image/png"
	case len(data) > 3 && data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46:
		return "image/gif"
	case len(data) > 3 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46:
		return "image/webp"
	default:
		return "image/png"
	}
}
