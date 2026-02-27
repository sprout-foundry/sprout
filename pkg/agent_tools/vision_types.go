package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
)

// ============================================================================
// Error Codes
// ============================================================================

// Error codes for analyze_image_content tool
const (
	ErrCodeInputUnsupported    = "INPUT_UNSUPPORTED_TYPE"
	ErrCodeRemoteFetchFailed   = "REMOTE_FETCH_FAILED"
	ErrCodeLocalFileNotFound   = "LOCAL_FILE_NOT_FOUND"
	ErrCodeOCRNoTextDetected   = "OCR_NO_TEXT_DETECTED"
	ErrCodePDFNotSupported     = "PDF_NOT_SUPPORTED"
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
	Success        bool                   `json:"success"`
	ToolInvoked    bool                   `json:"tool_invoked"`
	InputResolved  bool                   `json:"input_resolved"`
	OCRAttempted   bool                   `json:"ocr_attempted"`
	InputType      string                 `json:"input_type"` // "local_file", "remote_url", "unknown"
	InputPath      string                 `json:"input_path"`
	ErrorCode      string                 `json:"error_code,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	ExtractedText  string                 `json:"extracted_text,omitempty"`
	Analysis       *VisionAnalysis        `json:"analysis,omitempty"`
	SupportedInput ImageAnalysisSupported `json:"supported_input"`
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
	debug        bool
}

// NewVisionProcessor creates a vision processor with the given client
func NewVisionProcessor(client api.ClientInterface, debug bool) *VisionProcessor {
	return &VisionProcessor{
		visionClient: client,
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

// NewVisionProcessorWithMode creates a vision processor optimized for specific analysis mode
func NewVisionProcessorWithMode(debug bool, mode string) (*VisionProcessor, error) {
	var client api.ClientInterface
	var err error

	// Choose preferred provider/model by analysis mode, then fall back through provider vision setups.
	switch strings.ToLower(mode) {
	case "frontend", "design", "ui", "html", "css":
		// Prefer a high-quality frontend vision model when available.
		client, err = CreateVisionClientWithModel("google/gemma-3-27b-it")
		if err != nil {
			client, err = CreateVisionClient()
		}
	case "general", "text", "content", "extract", "analyze":
		// Prefer configured provider vision models first, then Ollama as last fallback.
		client, err = CreateVisionClient()
	default:
		// Default to provider-first selection with Ollama as fallback.
		client, err = CreateVisionClient()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create vision client: %w", err)
	}

	return &VisionProcessor{
		visionClient: client,
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

	return nil, fmt.Errorf("provider %s does not support vision models and no usable fallback is configured", providerType)
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
		// Get API key env var for this provider
		envVar := GetAPIKeyEnvVar(providerType)
		if envVar != "" && os.Getenv(envVar) == "" {
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

// GetAPIKeyEnvVar returns the environment variable name for the provider's API key
func GetAPIKeyEnvVar(providerType api.ClientType) string {
	switch providerType {
	case api.DeepInfraClientType:
		return "DEEPINFRA_API_KEY"
	case api.OpenRouterClientType:
		return "OPENROUTER_API_KEY"
	case api.OpenAIClientType:
		return "OPENAI_API_KEY"
	case api.MistralClientType:
		return "MISTRAL_API_KEY"
	case api.ZAIClientType:
		return "ZAI_API_KEY"
	case api.DeepSeekClientType:
		return "DEEPSEEK_API_KEY"
	default:
		return ""
	}
}

// CreateVisionClientWithModel creates a vision client using a specific model
func CreateVisionClientWithModel(modelName string) (api.ClientInterface, error) {
	// Determine which provider supports this model
	if strings.HasPrefix(modelName, "google/") || strings.HasPrefix(modelName, "meta-llama/") {
		// DeepInfra model - use new generic provider system
		if apiKey := os.Getenv("DEEPINFRA_API_KEY"); apiKey != "" {
			provider, err := factory.CreateGenericProvider("deepinfra", modelName)
			if err != nil {
				return nil, fmt.Errorf("failed to create DeepInfra client: %w", err)
			}
			return provider, nil
		}
		return nil, fmt.Errorf("DEEPINFRA_API_KEY not set for model %s", modelName)
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

		// Check if provider is available (has API key if required)
		switch providerType {
		case api.OpenRouterClientType:
			if os.Getenv("OPENROUTER_API_KEY") == "" {
				continue
			}
		case api.OpenAIClientType:
			if os.Getenv("OPENAI_API_KEY") == "" {
				continue
			}
		case api.DeepInfraClientType:
			if os.Getenv("DEEPINFRA_API_KEY") == "" {
				continue
			}
		case api.MistralClientType:
			if os.Getenv("MISTRAL_API_KEY") == "" {
				continue
			}
		case api.DeepSeekClientType:
			if os.Getenv("DEEPSEEK_API_KEY") == "" {
				continue
			}
		case api.ZAIClientType:
			if os.Getenv("ZAI_API_KEY") == "" {
				continue
			}
		case api.OllamaClientType, api.OllamaLocalClientType:
			// Local providers do not require API keys.
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
func (vp *VisionProcessor) GetImageData(imagePath string) (string, error) {
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
		return "", err
	}

	// Convert to base64
	return base64.StdEncoding.EncodeToString(data), nil
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
			response.ErrorCode = ErrCodePDFNotSupported
			response.ErrorMessage = fmt.Sprintf("PDF processing failed: %v", err)
			respJSON, _ := json.Marshal(response)
			return string(respJSON), nil
		}

		// Success - return the PDF text
		response.Success = true
		response.InputResolved = true
		response.OCRAttempted = true
		response.ExtractedText = pdfText
		response.Analysis = &VisionAnalysis{
			ImagePath:   imagePath,
			Description: pdfText,
		}
		respJSON, _ := json.Marshal(response)
		return string(respJSON), nil
	}

	cacheKey := fmt.Sprintf("%s|%s|%s", imagePath, analysisMode, analysisPrompt)

	if cachedResult, exists := visionCache[cacheKey]; exists {
		fmt.Printf("🔄 Using cached vision analysis for %s [%s]\n", GetBaseName(imagePath), analysisMode)

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

	response.Success = true
	response.ExtractedText = description
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

// GetFileExtension returns the file extension (with dot) in lowercase
func GetFileExtension(path string) string {
	ext := filepath.Ext(path)
	return strings.ToLower(ext)
}

// GetBaseName returns the base name of a file path
func GetBaseName(path string) string {
	return filepath.Base(path)
}
