package tools

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	visionMaxImageFileSizeBytes    = 20 * 1024 * 1024 // 20MB hard cap (API limit)
	visionOptimizeThresholdBytes   = 8 * 1024 * 1024  // Try optimization above 8MB
	visionTargetOptimizedSizeBytes = 2 * 1024 * 1024  // Target ~2MB for vision API calls
	visionMaxOptimizedJPEGQuality  = 85
	visionMinOptimizedJPEGQuality  = 35
	visionOptimizedJPEGQualityStep = 10
	visionResizeStepPercent        = 0.8  // Resize to 80% of original dimensions each iteration
	visionMinDimension             = 256  // Minimum dimension in pixels
	visionMaxDimension             = 4096 // Maximum pixels on longest edge for vision models
	visionMaxReturnedTextChars     = 20000 // Raised from 12000 to 20000 for better PDF/doc coverage
)

// getVisionMaxReturnedTextChars returns the max text chars limit from env or default
func getVisionMaxReturnedTextChars() int {
	if raw := os.Getenv("LEDIT_VISION_MAX_TEXT_CHARS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return visionMaxReturnedTextChars
}

// ============================================================================
// Error Codes
// ============================================================================

// Error codes for analyze_image_content tool
const (
	ErrCodeInputUnsupported    = "INPUT_UNSUPPORTED_TYPE"
	ErrCodeRemoteFetchFailed   = "REMOTE_FETCH_FAILED"
	ErrCodeLocalFileNotFound   = "LOCAL_FILE_NOT_FOUND"
	ErrCodeOCRNoTextDetected   = "OCR_NO_TEXT_DETECTED"
	ErrCodePDFProcessingFailed = "PDF_PROCESSING_FAILED"
	ErrCodeVisionNotAvailable  = "VISION_NOT_AVAILABLE"
	ErrCodeVisionRequestFailed = "VISION_REQUEST_FAILED"
	ErrCodeInvalidResponse     = "INVALID_RESPONSE"

	// Special error for model download needed
	ErrCodeModelDownloadNeeded = "MODEL_DOWNLOAD_NEEDED"
	ErrModelDownloadNeeded     = "PDF_OCR_MODEL_NEEDS_DOWNLOAD:"
)

// ============================================================================
// Data Structures
// ============================================================================

// VisionAnalysis represents the result of vision model analysis
type VisionAnalysis struct {
	ImagePath   string      `json:"image_path"`
	Description string      `json:"description"`
	Elements    []UIElement `json:"elements,omitempty"`
	Issues      []string    `json:"issues,omitempty"`
	Suggestions []string    `json:"suggestions,omitempty"`
}

// ImageAnalysisResponse represents a structured response for the analyze_image_content tool
type ImageAnalysisResponse struct {
	Success         bool                   `json:"success"`
	ToolInvoked     bool                   `json:"tool_invoked"`
	InputResolved   bool                   `json:"input_resolved"`
	OCRAttempted    bool                   `json:"ocr_attempted"`
	InputType       string                 `json:"input_type"` // "local_file", "remote_url", "unknown"
	InputPath       string                 `json:"input_path"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	ErrorMessage    string                 `json:"error_message,omitempty"`
	ExtractedText   string                 `json:"extracted_text,omitempty"`
	OutputTruncated bool                   `json:"output_truncated,omitempty"`
	OriginalChars   int                    `json:"original_chars,omitempty"`
	ReturnedChars   int                    `json:"returned_chars,omitempty"`
	FullOutputPath  string                 `json:"full_output_path,omitempty"` // Path to full OCR/analysis text when truncated
	Analysis        *VisionAnalysis        `json:"analysis,omitempty"`
	SupportedInput  ImageAnalysisSupported `json:"supported_input"`
}

// ImageAnalysisSupported describes what input types are supported
type ImageAnalysisSupported struct {
	RemoteURL     bool   `json:"remote_url"`
	LocalFile     bool   `json:"local_file"`
	ImageFormats  bool   `json:"image_formats"`  // jpg, png, gif, webp, etc.
	PDFSupport    bool   `json:"pdf_support"`    // PDF support status
	PDFWorkaround string `json:"pdf_workaround"` // Instructions for PDF handling
	MaxFileSizeMB int    `json:"max_file_size_mb"`
}

// UIElement represents a UI element detected in an image
type UIElement struct {
	Type        string `json:"type"`             // button, input, text, etc.
	Description string `json:"description"`      // what it looks like
	Position    string `json:"position"`         // approximate location
	Issues      string `json:"issues,omitempty"` // any problems noted
}

// VisionUsageInfo contains token usage and cost information from vision model calls
type VisionUsageInfo struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// VisionProcessor handles image analysis using vision-capable models
type VisionProcessor struct {
	visionClient api.ClientInterface
	logger       *utils.Logger
	debug        bool
}

// NewVisionProcessor creates a vision processor with the given client
func NewVisionProcessor(client api.ClientInterface, logger *utils.Logger, debug bool) *VisionProcessor {
	return &VisionProcessor{
		visionClient: client,
		logger:       logger,
		debug:        debug,
	}
}

// ============================================================================
// Caching and Usage Tracking
// ============================================================================

// Global variables for vision model tracking and caching
var lastVisionUsage *VisionUsageInfo
var visionCache = make(map[string]string)                // cache key -> result
var visionCacheUsage = make(map[string]*VisionUsageInfo) // cache key -> usage info

// GetLastVisionUsage returns the usage information from the last vision model call
func GetLastVisionUsage() *VisionUsageInfo {
	return lastVisionUsage
}

// ClearLastVisionUsage clears the stored vision usage information
func ClearLastVisionUsage() {
	lastVisionUsage = nil
}

// GetVisionCacheStats returns statistics about vision result caching
func GetVisionCacheStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["cached_results"] = len(visionCache)

	totalSavedCost := 0.0
	for _, usage := range visionCacheUsage {
		totalSavedCost += usage.EstimatedCost
	}
	stats["estimated_savings"] = totalSavedCost

	return stats
}

// ============================================================================
// Custom Provider Configuration Helpers
// ============================================================================

// GetCustomProviderConfig returns the custom provider configuration for a given type
func GetCustomProviderConfig(providerType api.ClientType) (configuration.CustomProviderConfig, bool) {
	configManager, err := configuration.NewManager()
	if err != nil {
		return configuration.CustomProviderConfig{}, false
	}
	config := configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return configuration.CustomProviderConfig{}, false
	}

	customConfig, exists := config.CustomProviders[string(providerType)]
	if !exists {
		return configuration.CustomProviderConfig{}, false
	}
	return customConfig, true
}

// GetCustomVisionProviders returns a list of custom providers that support vision
func GetCustomVisionProviders() []api.ClientType {
	configManager, err := configuration.NewManager()
	if err != nil {
		return nil
	}
	config := configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return nil
	}

	providers := make([]api.ClientType, 0, len(config.CustomProviders))
	for name, custom := range config.CustomProviders {
		if !custom.SupportsVision {
			continue
		}
		providers = append(providers, api.ClientType(name))
	}
	return providers
}

// GetCustomVisionFallback returns the fallback provider and model for vision
func GetCustomVisionFallback(providerType api.ClientType) (api.ClientType, string, bool) {
	customConfig, ok := GetCustomProviderConfig(providerType)
	if !ok {
		return "", "", false
	}

	fallbackProvider := strings.TrimSpace(customConfig.VisionFallbackProvider)
	if fallbackProvider == "" {
		return "", "", false
	}

	configManager, err := configuration.NewManager()
	if err != nil {
		return "", "", false
	}

	fallbackClientType, err := configManager.MapStringToClientType(fallbackProvider)
	if err != nil {
		return "", "", false
	}

	return fallbackClientType, strings.TrimSpace(customConfig.VisionFallbackModel), true
}

// EnsureOllamaModelTag ensures the model has a tag suffix
func EnsureOllamaModelTag(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}
	if strings.Contains(model, ":") {
		return model
	}
	return model + ":latest"
}

// CreateOllamaClient creates an Ollama client with the specified model
func CreateOllamaClient(model string) (api.ClientInterface, error) {
	model = EnsureOllamaModelTag(model)
	client, err := factory.CreateProviderClient(api.OllamaClientType, model)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// ============================================================================
// Vision Processor Creation
// ============================================================================

// NewVisionProcessorWithMode creates a vision processor for image/OCR workflows.
// Client selection is intentionally deterministic and does not vary by mode:
// provider-vision list first, local Ollama fallback last.
func NewVisionProcessorWithMode(debug bool, _ string) (*VisionProcessor, error) {
	client, err := CreateVisionClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create vision client: %w", err)
	}

	return &VisionProcessor{
		visionClient: client,
		logger:       nil,
		debug:        debug,
	}, nil
}

// NewVisionProcessorWithProvider creates a vision processor using the specified provider
func NewVisionProcessorWithProvider(debug bool, providerType api.ClientType) (*VisionProcessor, error) {
	client, err := CreateVisionClientWithProvider(providerType)
	if err != nil {
		return nil, fmt.Errorf("failed to create vision client for provider %s: %w", providerType, err)
	}

	return &VisionProcessor{
		visionClient: client,
		logger:       nil,
		debug:        debug,
	}, nil
}

// CreateVisionClientWithProvider creates a vision client using the specified provider
func CreateVisionClientWithProvider(providerType api.ClientType) (api.ClientInterface, error) {
	// Get the vision model for this provider
	visionModel := GetVisionModelForProvider(providerType)
	if visionModel != "" {
		// Create client with the vision model
		client, err := factory.CreateProviderClient(providerType, visionModel)
		if err == nil && client.SupportsVision() {
			return client, nil
		}
	}

	// For custom providers, support explicit vision fallback provider/model.
	fallbackProvider, fallbackModel, hasFallback := GetCustomVisionFallback(providerType)
	if hasFallback {
		if fallbackModel == "" {
			fallbackModel = GetVisionModelForProvider(fallbackProvider)
		}
		if fallbackModel != "" {
			client, err := factory.CreateProviderClient(fallbackProvider, fallbackModel)
			if err == nil && client.SupportsVision() {
				return client, nil
			}
		}
	}

	// Deterministic final fallback path shared across scenarios:
	// run the standard provider-first list with local Ollama last.
	globalClient, globalErr := CreateVisionClient()
	if globalErr == nil && globalClient != nil && globalClient.SupportsVision() {
		return globalClient, nil
	}

	return nil, fmt.Errorf("provider %s does not support vision models and no usable fallback is configured: %w", providerType, globalErr)
}

// GetVisionModelForProvider returns the appropriate vision model for a given provider
// Vision models are configured in the provider JSON config files in pkg/agent_providers/configs/
// This function creates a temporary provider client to get the configured vision model
func GetVisionModelForProvider(providerType api.ClientType) string {
	// Skip providers that don't use generic provider system
	switch providerType {
	case api.OpenAIClientType:
		// OpenAI uses built-in client with a fixed vision model.
		return "gpt-4o-mini"
	case api.OllamaClientType, api.OllamaLocalClientType:
		// Prefer configured Ollama OCR model when available.
		configManager, err := configuration.NewManager()
		if err == nil {
			config := configManager.GetConfig()
			if config.PDFOCREnabled && config.PDFOCRProvider == "ollama" && strings.TrimSpace(config.PDFOCRModel) != "" {
				return EnsureOllamaModelTag(config.PDFOCRModel)
			}
		}
		// Fallback to default local OCR/vision model.
		return "glm-ocr:latest"
	case api.OllamaTurboClientType:
		// Ollama turbo currently does not support vision.
		return ""
	case api.TestClientType:
		return ""
	}

	// Check custom provider config first for explicit vision settings.
	if customConfig, ok := GetCustomProviderConfig(providerType); ok {
		if !customConfig.SupportsVision {
			return ""
		}
		if strings.TrimSpace(customConfig.VisionModel) != "" {
			return strings.TrimSpace(customConfig.VisionModel)
		}
		return strings.TrimSpace(customConfig.ModelName)
	}

	// Try to create a provider to get its vision model
	// Use the default model for this provider
	model := GetDefaultModelForProvider(providerType)
	if model == "" {
		return ""
	}

	client, err := factory.CreateProviderClient(providerType, model)
	if err != nil {
		return ""
	}

	// Get vision model from the provider
	return client.GetVisionModel()
}

// GetDefaultModelForProvider returns the default model for a given provider type
func GetDefaultModelForProvider(providerType api.ClientType) string {
	switch providerType {
	case api.DeepInfraClientType:
		return "meta-llama/Llama-3.3-70B-Instruct"
	case api.OpenRouterClientType:
		return "openai/gpt-5"
	case api.MistralClientType:
		return "devstral-2512"
	case api.DeepSeekClientType:
		return "deepseek-ai/DeepSeek-V3"
	case api.ZAIClientType:
		return "glm-4.6"
	case api.LMStudioClientType:
		return "" // Depends on locally installed models
	case api.ChutesClientType:
		return "" // Depends on chutes service
	default:
		return ""
	}
}

// CreateVisionClient creates a client capable of vision analysis
func CreateVisionClient() (api.ClientInterface, error) {
	// Priority: configured provider vision models first, local Ollama last.
	providers := []api.ClientType{
		api.DeepInfraClientType,
		api.OpenRouterClientType,
		api.OpenAIClientType,
		api.MistralClientType,
		api.ZAIClientType,
		api.DeepSeekClientType,
	}
	providers = append(providers, GetCustomVisionProviders()...)
	providers = append(providers, api.OllamaClientType)

	for _, providerType := range providers {
		if !configuration.HasProviderCredential(string(providerType), nil) {
			continue // Skip if API key not set
		}

		// Get vision model from provider config
		visionModel := GetVisionModelForProvider(providerType)
		if visionModel == "" {
			continue // Skip if no vision model configured
		}

		// Try to create client with vision model
		client, err := factory.CreateProviderClient(providerType, visionModel)
		if err != nil {
			continue // Try next provider
		}

		// Verify the client supports vision
		if !client.SupportsVision() {
			continue // Try next provider
		}

		return client, nil
	}

	return nil, fmt.Errorf("no vision-capable providers available - please configure a provider vision model or local Ollama OCR model")
}

// CreateVisionClientWithModel creates a vision client using a specific model
func CreateVisionClientWithModel(modelName string) (api.ClientInterface, error) {
	// Determine which provider supports this model
	if strings.HasPrefix(modelName, "google/") || strings.HasPrefix(modelName, "meta-llama/") {
		// DeepInfra model - use new generic provider system
		if configuration.HasProviderCredential("deepinfra", nil) {
			provider, err := factory.CreateGenericProvider("deepinfra", modelName)
			if err != nil {
				return nil, fmt.Errorf("failed to create DeepInfra client: %w", err)
			}
			return provider, nil
		}
		return nil, fmt.Errorf("deepinfra credentials not configured for model %s", modelName)
	}

	// Fall back to default client creation
	return CreateVisionClient()
}

// HasVisionCapability checks if vision processing is available
func HasVisionCapability() bool {
	// Check if any provider with vision capability is available
	// Priority: provider vision models first, local providers last.
	providers := []api.ClientType{
		api.DeepInfraClientType,
		api.OpenRouterClientType,
		api.OpenAIClientType,
		api.MistralClientType,
		api.DeepSeekClientType,
		api.ZAIClientType,
	}
	providers = append(providers, GetCustomVisionProviders()...)
	providers = append(providers,
		api.OllamaClientType,
		api.OllamaLocalClientType,
	)

	for _, providerType := range providers {
		// Get the vision model for this provider
		visionModel := GetVisionModelForProvider(providerType)
		if visionModel == "" {
			continue // Skip providers without vision support
		}

		if !configuration.HasProviderCredential(string(providerType), nil) {
			switch providerType {
			case api.OllamaClientType, api.OllamaLocalClientType:
				// Local providers do not require API keys.
			default:
				continue
			}
		}

		// Try to create client with vision model
		client, err := factory.CreateProviderClient(providerType, visionModel)
		if err != nil {
			continue // Try next provider
		}

		// Verify the client actually supports vision
		if client.SupportsVision() {
			return true
		}
	}

	return false
}

// ============================================================================
// Image Fetching
// ============================================================================

// GetImageData reads image data from file or URL
func (vp *VisionProcessor) GetImageData(imagePath string) (string, string, error) {
	var data []byte
	var err error

	if strings.HasPrefix(imagePath, "http") {
		// Download from URL
		data, err = vp.DownloadImage(imagePath)
	} else {
		// Read local file
		data, err = os.ReadFile(imagePath)
	}

	if err != nil {
		return "", "", err
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
func (vp *VisionProcessor) DownloadImage(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
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
// Main AnalyzeImage Function (kept here for central access)
// ============================================================================

// AnalyzeImage is the tool function called by the agent for image analysis
// Returns a structured JSON response with metadata for robust error handling
func AnalyzeImage(imagePath string, analysisPrompt string, analysisMode string) (string, error) {
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
	if strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://") {
		inputType = "remote_url"
	} else if imagePath != "" {
		inputType = "local_file"
	}
	response.InputType = inputType

	ext := strings.ToLower(GetFileExtension(imagePath))
	if ext == ".pdf" {
		// Try simplified PDF processing
		pdfText, err := ProcessPDFWithVision(imagePath)
		if err != nil {
			response.Success = false
			response.InputResolved = true
			response.OCRAttempted = true
			response.ErrorCode = classifyPDFProcessingErrorCode(err)
			response.ErrorMessage = fmt.Sprintf("PDF processing failed: %v", err)
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

	cacheKey := fmt.Sprintf("%s|%s|%s", imagePath, analysisMode, analysisPrompt)

	if cachedResult, exists := visionCache[cacheKey]; exists {
		fmt.Printf("[~] Using cached vision analysis for %s [%s]\n", GetBaseName(imagePath), analysisMode)

		if cachedUsage, hasUsage := visionCacheUsage[cacheKey]; hasUsage {
			lastVisionUsage = cachedUsage
		}

		var cachedResp ImageAnalysisResponse
		if err := json.Unmarshal([]byte(cachedResult), &cachedResp); err == nil {
			cachedResp.Success = true
			respJSON, _ := json.Marshal(cachedResp)
			return string(respJSON), nil
		}

		return cachedResult, nil
	}

	processor, err := NewVisionProcessorWithMode(false, analysisMode)
	if err != nil {
		response.Success = false
		response.InputResolved = false
		response.ErrorCode = ErrCodeVisionRequestFailed
		response.ErrorMessage = fmt.Sprintf("failed to create vision processor: %v", err)
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

	analysis, err := processor.AnalyzeImageWithPrompt(imagePath, prompt)
	if err != nil {
		errMsg := err.Error()

		if strings.Contains(errMsg, "failed to get image data") || strings.Contains(errMsg, "failed to download image") {
			if inputType == "remote_url" {
				response.ErrorCode = ErrCodeRemoteFetchFailed
				response.ErrorMessage = fmt.Sprintf("failed to fetch image from remote URL: %v", err)
			} else {
				response.ErrorCode = ErrCodeLocalFileNotFound
				response.ErrorMessage = fmt.Sprintf("failed to read local file: %v", err)
			}
		} else if strings.Contains(errMsg, "no response from vision model") {
			response.ErrorCode = ErrCodeInvalidResponse
			response.ErrorMessage = "vision model returned empty response"
		} else {
			response.ErrorCode = ErrCodeVisionRequestFailed
			response.ErrorMessage = fmt.Sprintf("vision analysis failed: %v", err)
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
		response.ErrorMessage = fmt.Sprintf("failed to marshal response: %v", err)
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	visionCache[cacheKey] = string(respJSON)
	if lastVisionUsage != nil {
		visionCacheUsage[cacheKey] = lastVisionUsage
	}

	return string(respJSON), nil
}

func limitVisionOutputText(text string) (string, bool, int) {
	trimmed := strings.TrimSpace(text)
	original := len(trimmed)
	maxChars := getVisionMaxReturnedTextChars()
	if original <= maxChars {
		return trimmed, false, original
	}

	suffix := fmt.Sprintf("\n\n[TRUNCATED: returned first %d of %d characters]", maxChars, original)
	keep := maxChars - len(suffix)
	if keep < 0 {
		keep = maxChars
		suffix = ""
	}
	return strings.TrimSpace(trimmed[:keep]) + suffix, true, original
}

// persistVisionFullTextWithRoot persists full vision text to a file rooted at workspaceRoot.
// If workspaceRoot is empty, it falls back to os.Getwd() for the CWD-based output dir.
// The relative path for display also uses workspaceRoot (or os.Getwd() as fallback).
func persistVisionFullTextWithRoot(sourcePath, fullText, workspaceRoot string) (string, error) {
	fullText = strings.TrimSpace(fullText)
	if fullText == "" {
		return "", fmt.Errorf("full text is empty")
	}

	dir := resolveVisionOutputDirectoryWithRoot(workspaceRoot)
	if dir == "" {
		return "", fmt.Errorf("vision output directory unavailable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create vision output directory: %w", err)
	}

	base := sanitizeVisionFileComponent(strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)))
	if base == "" {
		base = "vision_output"
	}
	hash := sha1.Sum([]byte(strings.TrimSpace(sourcePath)))
	shortHash := hex.EncodeToString(hash[:])[:12]
	fileName := fmt.Sprintf("%s_%s_full.txt", base, shortHash)
	fullPath := filepath.Join(dir, fileName)

	if err := os.WriteFile(fullPath, []byte(fullText), 0o644); err != nil {
		return "", fmt.Errorf("failed to write full vision output: %w", err)
	}

	wd := workspaceRoot
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return fullPath, nil
		}
	}
	rel, err := filepath.Rel(wd, fullPath)
	if err != nil {
		return fullPath, nil
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return fullPath, nil
	}
	return "./" + rel, nil
}

// persistVisionFullText persists full vision text to a file using os.Getwd() for directory resolution.
// Kept for backward compatibility; new code should prefer persistVisionFullTextWithRoot.
func persistVisionFullText(sourcePath, fullText string) (string, error) {
	return persistVisionFullTextWithRoot(sourcePath, fullText, "")
}

// resolveVisionOutputDirectoryWithRoot resolves the vision output directory using the given
// workspace root. If workspaceRoot is empty, it falls back to os.Getwd().
func resolveVisionOutputDirectoryWithRoot(workspaceRoot string) string {
	raw := strings.TrimSpace(os.Getenv("LEDIT_RESOURCE_DIRECTORY"))
	if raw == "" {
		raw = ".ledit_ocr_outputs"
	}
	cleaned := filepath.Clean(raw)
	if filepath.IsAbs(cleaned) {
		if vol := filepath.VolumeName(cleaned); vol != "" {
			cleaned = strings.TrimPrefix(cleaned, vol)
		}
		cleaned = strings.TrimLeft(cleaned, `/\`)
	}
	wd := workspaceRoot
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(wd, cleaned)
}

// resolveVisionOutputDirectory resolves the vision output directory using os.Getwd().
// Kept for backward compatibility; new code should prefer resolveVisionOutputDirectoryWithRoot.
func resolveVisionOutputDirectory() string {
	return resolveVisionOutputDirectoryWithRoot("")
}

func sanitizeVisionFileComponent(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func classifyPDFProcessingErrorCode(err error) string {
	if err == nil {
		return ErrCodePDFProcessingFailed
	}
	msg := strings.ToLower(err.Error())

	// Input path / retrieval failures.
	if strings.Contains(msg, "failed to download pdf") ||
		strings.Contains(msg, "status 404") ||
		strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "status 401") {
		return ErrCodeRemoteFetchFailed
	}
	if strings.Contains(msg, "failed to stat pdf file") ||
		strings.Contains(msg, "no such file or directory") {
		return ErrCodeLocalFileNotFound
	}

	// Provider/inference transport failures: model call failed, not PDF support.
	if strings.Contains(msg, "ocr request failed") ||
		strings.Contains(msg, "http 5") ||
		strings.Contains(msg, "http 4") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "failed to create vision client") ||
		strings.Contains(msg, "no response from ocr model") {
		return ErrCodeVisionRequestFailed
	}

	// Invalid/unsupported PDF content.
	if strings.Contains(msg, "missing %pdf header") ||
		strings.Contains(msg, "not a valid pdf") {
		return ErrCodeInputUnsupported
	}

	// Conservative default for unknown PDF failures.
	return ErrCodePDFProcessingFailed
}

// GetFileExtension returns the file extension (with dot) in lowercase
func GetFileExtension(path string) string {
	ext := filepath.Ext(path)
	return strings.ToLower(ext)
}

// GetBaseName returns the base name of a file path
func GetBaseName(path string) string {
	return filepath.Base(path)
}

// IsHTMLInput checks if the input path appears to be HTML content.
// For URLs, it does a HEAD request to check Content-Type.
// For local files, it checks the file extension.
func IsHTMLInput(path string) bool {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Head(path)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
	}
	ext := strings.ToLower(GetFileExtension(path))
	return ext == ".html" || ext == ".htm"
}
