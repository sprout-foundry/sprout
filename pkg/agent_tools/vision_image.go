package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// Image Fetching
// ============================================================================

// GetImageData reads image data from file or URL
func (vp *VisionProcessor) GetImageData(ctx context.Context, imagePath string) (string, string, error) {
	var data []byte
	var err error

	lowerPath := strings.ToLower(imagePath)
	if strings.HasPrefix(lowerPath, "http://") || strings.HasPrefix(lowerPath, "https://") {
		// Download from URL
		data, err = vp.DownloadImage(ctx, imagePath)
	} else {
		// Read local file
		data, err = os.ReadFile(imagePath)
	}

	if err != nil {
		return "", "", fmt.Errorf("read image file %q: %w", imagePath, err)
	}

	originalSize := len(data)

	if len(data) > visionMaxImageFileSizeBytes {
		return "", "", fmt.Errorf("image file too large (%d MB), maximum size is %d MB",
			len(data)/1024/1024, visionMaxImageFileSizeBytes/1024/1024)
	}

	// Warn user if image is approaching the limit
	if len(data) > visionOptimizeThresholdBytes {
		if vp.logger != nil {
			vp.logger.LogProcessStep(fmt.Sprintf("[WARN] Large image detected (%d MB), attempting optimization to stay under %d MB limit",
				originalSize/1024/1024, visionMaxImageFileSizeBytes/1024/1024))
		}
	}

	optimizedData, mimeType, optErr := OptimizeImageData(imagePath, data)
	if optErr != nil {
		// Log optimization error but continue with original data
		if vp.logger != nil {
			vp.logger.LogProcessStep(fmt.Sprintf("[WARN] Image optimization failed: %v, using original data", optErr))
		}
	} else if optimizedData != nil && len(optimizedData) > 0 {
		data = optimizedData
		if len(data) < originalSize && vp.logger != nil {
			vp.logger.LogProcessStep(fmt.Sprintf("[ok] Image optimized: %d MB → %d MB (%.1f%% reduction)",
				originalSize/1024/1024, len(data)/1024/1024, 100-float64(len(data))/float64(originalSize)*100))
		}
	}

	if mimeType == "" {
		mimeType = detectImageMimeType(imagePath)
	}

	// Final check: if still over limit, return error with clear message
	if len(data) > visionMaxImageFileSizeBytes {
		return "", "", fmt.Errorf("image still too large after optimization (%d MB), maximum size is %d MB. Try a smaller image.",
			len(data)/1024/1024, visionMaxImageFileSizeBytes/1024/1024)
	}

	// Convert to base64
	return base64.StdEncoding.EncodeToString(data), mimeType, nil
}

// DownloadImage downloads an image from URL
func (vp *VisionProcessor) DownloadImage(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var data []byte
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create image download request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download image: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Surface Retry-After for 429/503 and other 5xx.
			if resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode >= 500 {
				return &RetryableHTTPError{
					StatusCode: resp.StatusCode,
					Status:     resp.Status,
					Method:     req.Method,
					URL:        url,
					RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
				}
			}
			return fmt.Errorf("download image: status %d", resp.StatusCode)
		}

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read image data: %w", err)
		}
		return nil
	}, RetryOptions{OpName: "download_image"})
	if err != nil {
		return nil, err
	}

	return data, nil
}

func OptimizeImageData(imagePath string, data []byte) ([]byte, string, error) {
	// Phase 0: Check and resize dimensions if image exceeds visionMaxDimension
	// This runs first, before any file size optimization
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Not all formats can be decoded with the stdlib (e.g., webp/avif). Keep original bytes.
		return data, detectImageMimeType(imagePath), nil
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	maxEdge := width
	if height > maxEdge {
		maxEdge = height
	}

	// Resize if dimensions exceed maximum
	if maxEdge > visionMaxDimension {
		// Calculate new dimensions while maintaining aspect ratio
		scale := float64(visionMaxDimension) / float64(maxEdge)
		newWidth := int(float64(width) * scale)
		newHeight := int(float64(height) * scale)

		// Ensure we don't go below minimum
		if newWidth < visionMinDimension {
			newWidth = visionMinDimension
		}
		if newHeight < visionMinDimension {
			newHeight = visionMinDimension
		}

		// Resize using nearest neighbor (fast and good enough for OCR/vision)
		resized := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
		for y := 0; y < newHeight; y++ {
			for x := 0; x < newWidth; x++ {
				srcX := x * width / newWidth
				srcY := y * height / newHeight
				if srcX >= width {
					srcX = width - 1
				}
				if srcY >= height {
					srcY = height - 1
				}
				resized.Set(x, y, img.At(srcX, srcY))
			}
		}

		img = resized
		width = newWidth
		height = newHeight
		fmt.Printf("[measure] Resized image from %dx%d to %dx%d (exceeded max dimension of %d)\n",
			bounds.Dx(), bounds.Dy(), width, height, visionMaxDimension)
	}

	// Early return if no file size optimization needed
	if len(data) <= visionOptimizeThresholdBytes {
		// Re-encode the potentially resized image to JPEG
		var buf bytes.Buffer
		if encodeErr := jpeg.Encode(&buf, img, &jpeg.Options{Quality: visionMaxOptimizedJPEGQuality}); encodeErr == nil {
			return buf.Bytes(), "image/jpeg", nil
		}
		return data, detectImageMimeType(imagePath), nil
	}

	// Phase 1: Quality reduction
	best := data
	bestMime := detectImageMimeType(imagePath)

	// If we resized in Phase 0, use the resized image; otherwise use original format
	if format != "" {
		bestMime = "image/jpeg"
	}

	for quality := visionMaxOptimizedJPEGQuality; quality >= visionMinOptimizedJPEGQuality; quality -= visionOptimizedJPEGQualityStep {
		var buf bytes.Buffer
		if encodeErr := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); encodeErr != nil {
			continue
		}

		candidate := buf.Bytes()
		if len(candidate) < len(best) {
			best = candidate
			bestMime = "image/jpeg"
		}
		// Target ~2MB instead of 8MB for vision APIs
		if len(candidate) <= visionTargetOptimizedSizeBytes {
			return candidate, "image/jpeg", nil
		}
	}

	// Phase 2: Dimension resizing if quality reduction wasn't enough
	// Start from current dimensions and progressively reduce
	currentWidth := width
	currentHeight := height

	for currentWidth > visionMinDimension && currentHeight > visionMinDimension {
		// Resize image
		newWidth := int(float64(currentWidth) * visionResizeStepPercent)
		newHeight := int(float64(currentHeight) * visionResizeStepPercent)

		resized := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
		// Use nearest neighbor for speed (good enough for OCR/vision analysis)
		for y := 0; y < newHeight; y++ {
			for x := 0; x < newWidth; x++ {
				srcX := x * currentWidth / newWidth
				srcY := y * currentHeight / newHeight
				if srcX >= currentWidth {
					srcX = currentWidth - 1
				}
				if srcY >= currentHeight {
					srcY = currentHeight - 1
				}
				resized.Set(x, y, img.At(srcX, srcY))
			}
		}

		// Try encoding with best quality first at new size
		var buf bytes.Buffer
		if encodeErr := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: visionMaxOptimizedJPEGQuality}); encodeErr != nil {
			currentWidth = newWidth
			currentHeight = newHeight
			continue
		}

		candidate := buf.Bytes()
		if len(candidate) < len(best) {
			best = candidate
			bestMime = "image/jpeg"
		}
		if len(candidate) <= visionTargetOptimizedSizeBytes {
			return candidate, "image/jpeg", nil
		}

		currentWidth = newWidth
		currentHeight = newHeight
	}

	return best, bestMime, nil
}

func detectImageMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// ============================================================================
// Main AnalyzeImage Function
// ============================================================================

// getCachedVisionResult checks the vision cache and returns the cached result if present.
// Returns (result, true, nil) when a cache hit occurs, (result, false, nil) otherwise.
func getCachedVisionResult(cacheKey, imagePath, analysisMode string) (string, bool, error) {
	cachedResult, cachedUsage, ok := visionLRU.Get(cacheKey)
	if !ok {
		return "", false, nil
	}

	fmt.Printf("[~] Using cached vision analysis for %s [%s]\n", GetBaseName(imagePath), analysisMode)

	if cachedUsage != nil {
		recordVisionUsage(nil, cachedUsage)
	}

	var cachedResp ImageAnalysisResponse
	if err := json.Unmarshal([]byte(cachedResult), &cachedResp); err == nil {
		cachedResp.Success = true
		respJSON, _ := json.Marshal(cachedResp)
		return string(respJSON), true, nil
	}

	return cachedResult, true, nil
}

// AnalyzeImage is the tool function called by the agent for image analysis
// Returns a structured JSON response with metadata for robust error handling
func AnalyzeImage(ctx context.Context, imagePath string, analysisPrompt string, analysisMode string) (string, error) {
	response := ImageAnalysisResponse{
		Success:     false,
		ToolInvoked: true,
		InputPath:   imagePath,
		SupportedInput: ImageAnalysisSupported{
			RemoteURL:     true,
			LocalFile:     true,
			ImageFormats:  true,
			PDFSupport:    true,
			PDFWorkaround: "",
			MaxFileSizeMB: 20,
		},
	}

	// Note: HTML input detection is handled at the tool-handler level
	// (handleAnalyzeUIScreenshot / handleAnalyzeImageContent) to avoid
	// redundant HTTP HEAD requests on every remote image URL call.

	if !HasVisionCapability() {
		response.Success = false
		response.InputResolved = false
		response.ErrorCode = ErrCodeVisionNotAvailable
		response.ErrorMessage = "vision analysis not available - please set up DEEPINFRA_API_KEY, OPENROUTER_API_KEY, or OPENAI_API_KEY for vision capabilities"
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	inputType := "unknown"
	lowerPath := strings.ToLower(imagePath)
	if strings.HasPrefix(lowerPath, "http://") || strings.HasPrefix(lowerPath, "https://") {
		inputType = "remote_url"
	} else if imagePath != "" {
		inputType = "local_file"
	}
	response.InputType = inputType

	ext := strings.ToLower(GetFileExtension(imagePath))
	if ext == ".pdf" {
		// Try simplified PDF processing
		pdfText, err := ProcessPDFWithVision(ctx, imagePath)
		if err != nil {
			response.Success = false
			response.InputResolved = true
			response.OCRAttempted = true
			response.ErrorCode = classifyPDFProcessingErrorCode(err)
			response.ErrorMessage = fmt.Sprintf("PDF processing: %v", err)
			respJSON, _ := json.Marshal(response)
			return string(respJSON), nil
		}

		// Success - return bounded PDF text to avoid blowing up model context.
		limitedText, truncated, originalCount := limitVisionOutputText(pdfText)
		response.Success = true
		response.InputResolved = true
		response.OCRAttempted = true
		response.ExtractedText = limitedText
		response.OutputTruncated = truncated
		response.OriginalChars = originalCount
		response.ReturnedChars = len(limitedText)
		if truncated {
			if fullPath, saveErr := persistVisionFullText(imagePath, strings.TrimSpace(pdfText)); saveErr == nil {
				response.FullOutputPath = fullPath
			}
		}
		response.Analysis = &VisionAnalysis{
			ImagePath:   imagePath,
			Description: limitedText,
		}
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	cacheKey := visionCacheKey(imagePath, analysisMode, analysisPrompt)

	if cached, hit, _ := getCachedVisionResult(cacheKey, imagePath, analysisMode); hit {
		return cached, nil
	}

	processor, err := NewVisionProcessorWithMode(false, analysisMode)
	if err != nil {
		response.Success = false
		response.InputResolved = false
		response.ErrorCode = ErrCodeVisionRequestFailed
		response.ErrorMessage = fmt.Sprintf("create vision processor: %v", err)
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	var prompt string
	if analysisPrompt != "" {
		prompt = analysisPrompt
	} else {
		prompt = GeneratePromptForMode(analysisMode)
	}

	response.InputResolved = true
	response.OCRAttempted = true

	analysis, err := processor.AnalyzeImage(ctx, imagePath, prompt)
	if err != nil {
		errMsg := err.Error()

		if strings.Contains(errMsg, "get image data") || strings.Contains(errMsg, "download image") {
			if inputType == "remote_url" {
				response.ErrorCode = ErrCodeRemoteFetchFailed
				response.ErrorMessage = fmt.Sprintf("fetch image from remote URL: %v", err)
			} else {
				response.ErrorCode = ErrCodeLocalFileNotFound
				response.ErrorMessage = fmt.Sprintf("read local file: %v", err)
			}
		} else if strings.Contains(errMsg, "no response from vision model") {
			response.ErrorCode = ErrCodeInvalidResponse
			response.ErrorMessage = "vision model returned empty response"
		} else {
			response.ErrorCode = ErrCodeVisionRequestFailed
			response.ErrorMessage = fmt.Sprintf("vision analysis: %v", err)
		}

		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	description := strings.TrimSpace(analysis.Description)
	if description == "" {
		description = "No text or content detected in the image"
		response.ErrorCode = ErrCodeOCRNoTextDetected
		response.ErrorMessage = description
		response.ExtractedText = ""
		response.Analysis = &analysis
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	limitedDescription, truncated, originalCount := limitVisionOutputText(description)
	response.Success = true
	response.ExtractedText = limitedDescription
	response.OutputTruncated = truncated
	response.OriginalChars = originalCount
	response.ReturnedChars = len(limitedDescription)
	if truncated {
		if fullPath, saveErr := persistVisionFullText(imagePath, strings.TrimSpace(description)); saveErr == nil {
			response.FullOutputPath = fullPath
		}
	}
	analysis.Description = limitedDescription
	response.Analysis = &analysis

	respJSON, err := json.Marshal(response)
	if err != nil {
		response.Success = false
		response.ErrorCode = ErrCodeInvalidResponse
		response.ErrorMessage = fmt.Sprintf("marshal response: %v", err)
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	visionLRU.Put(cacheKey, string(respJSON), GetLastVisionUsage())

	return string(respJSON), nil
}
