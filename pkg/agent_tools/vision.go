package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
)

// Global variables for vision model tracking and caching
var lastVisionUsage *VisionUsageInfo
var visionCache = make(map[string]string)                // cache key -> result
var visionCacheUsage = make(map[string]*VisionUsageInfo) // cache key -> usage info

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

// VisionProcessor handles image analysis using vision-capable models
type VisionProcessor struct {
	visionClient api.ClientInterface
	debug        bool
}

// NewVisionProcessorWithMode creates a vision processor optimized for specific analysis mode
func NewVisionProcessorWithMode(debug bool, mode string) (*VisionProcessor, error) {
	var client api.ClientInterface
	var err error

	// Check if PDF OCR is configured - use glm-ocr from Ollama if available
	configManager, configErr := configuration.NewManager()
	if configErr == nil {
		config := configManager.GetConfig()
		if config.PDFOCREnabled && config.PDFOCRProvider == "ollama" {
			// Use the configured OCR model from Ollama
			model := config.PDFOCRModel
			if model != "" {
				client, err = createOllamaClient(model)
				if err == nil {
					return &VisionProcessor{
						visionClient: client,
						debug:        debug,
					}, nil
				}
			}
		}
	}

	// Choose optimal model based on analysis mode
	switch strings.ToLower(mode) {
	case "frontend", "design", "ui", "html", "css":
		// Use gemma-3-27b-it for comprehensive frontend analysis
		client, err = createVisionClientWithModel("google/gemma-3-27b-it")
	case "general", "text", "content", "extract", "analyze":
		// Try Ollama with glm-ocr first, then fall back to remote
		client, err = createOllamaClient("glm-ocr:latest")
		if err != nil {
			client, err = createVisionClientWithModel("google/gemma-3-27b-it")
		}
	default:
		// Default to balanced approach (current implementation)
		client, err = createVisionClient()
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
	client, err := createVisionClientWithProvider(providerType)
	if err != nil {
		return nil, fmt.Errorf("failed to create vision client for provider %s: %w", providerType, err)
	}

	return &VisionProcessor{
		visionClient: client,
		debug:        debug,
	}, nil
}

// createVisionClientWithProvider creates a vision client using the specified provider
func createVisionClientWithProvider(providerType api.ClientType) (api.ClientInterface, error) {
	// Get the vision model for this provider
	visionModel := GetVisionModelForProvider(providerType)
	if visionModel == "" {
		return nil, fmt.Errorf("provider %s does not support vision models", providerType)
	}

	// Create client with the vision model
	client, err := factory.CreateProviderClient(providerType, visionModel)
	if err != nil {
		return nil, fmt.Errorf("failed to create vision client for provider %s: %w", providerType, err)
	}

	return client, nil
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
				return ensureOllamaModelTag(config.PDFOCRModel)
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

	// Try to create a provider to get its vision model
	// Use the default model for this provider
	model := getDefaultModelForProvider(providerType)
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

// getDefaultModelForProvider returns the default model for a given provider type
func getDefaultModelForProvider(providerType api.ClientType) string {
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

// createVisionClient creates a client capable of vision analysis
func createVisionClient() (api.ClientInterface, error) {
	// Priority: Local Ollama -> DeepInfra -> OpenRouter -> OpenAI -> Mistral -> ZAI
	providers := []api.ClientType{
		api.OllamaClientType,
		api.DeepInfraClientType,
		api.OpenRouterClientType,
		api.OpenAIClientType,
		api.MistralClientType,
		api.ZAIClientType,
		api.DeepSeekClientType,
	}

	for _, providerType := range providers {
		// Get API key env var for this provider
		envVar := getAPIKeyEnvVar(providerType)
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

	return nil, fmt.Errorf("no vision-capable providers available - please set up DEEPINFRA_API_KEY, OPENROUTER_API_KEY, or OPENAI_API_KEY for vision capabilities")
}

// getAPIKeyEnvVar returns the environment variable name for the provider's API key
func getAPIKeyEnvVar(providerType api.ClientType) string {
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

// createVisionClientWithModel creates a vision client using a specific model
func createVisionClientWithModel(modelName string) (api.ClientInterface, error) {
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
	return createVisionClient()
}

// generatePromptForMode creates appropriate prompts based on analysis mode
func generatePromptForMode(mode string) string {
	switch strings.ToLower(mode) {
	case "frontend", "design", "ui", "html", "css":
		return `You are an expert frontend engineer specializing in converting UI designs and screenshots into pixel-perfect responsive web layouts. Analyze this UI screenshot and provide:

1. **Colors**: Extract primary colors with hex values for:
   - Primary brand colors
   - Secondary/accent colors  
   - Text colors (primary, secondary, muted)
   - Background colors
   - Border colors

2. **Layout Structure**: Describe the overall layout pattern:
   - Grid/flexbox structure
   - Component hierarchy
   - Responsive breakpoint considerations

3. **Typography**: Identify font details:
   - Font families used
   - Font sizes and weights
   - Text hierarchy

4. **CSS Implementation**: Provide key CSS properties:
   - Exact spacing and padding values
   - Border-radius and shadows
   - Responsive design patterns

5. **Design Tokens**: Create a design system:
   - Color palette
   - Spacing scale
   - Typography scale

Focus on accuracy and detail that would be useful for a developer implementing this design.`

	case "general", "text", "content", "extract", "analyze":
		return `Analyze this image and provide:

1. **Content Description**: What does this image show?
2. **Text Extraction**: Any visible text, code, or written content
3. **Technical Details**: Code, interfaces, diagrams, or technical elements
4. **Context**: How this relates to the user's query or task
5. **Key Information**: Important details for understanding or implementation

Be thorough but concise, focusing on actionable information.`

	default:
		return "Analyze this image for software development purposes. Describe what you see, identify any UI elements, code, diagrams, or design patterns. Provide structured information that would be useful for a developer."
	}
}

// ProcessImagesInText detects images in text and processes them with vision models
func (vp *VisionProcessor) ProcessImagesInText(text string) (string, []VisionAnalysis, error) {
	if vp.debug {
		fmt.Println("🔍 Scanning text for image references...")
	}

	// Find image references in the text
	images := vp.extractImageReferences(text)
	if len(images) == 0 {
		return text, nil, nil
	}

	if vp.debug {
		fmt.Printf("📸 Found %d image references\n", len(images))
	}

	var analyses []VisionAnalysis
	enhancedText := text

	// Process each image
	for i, imgPath := range images {
		if vp.debug {
			fmt.Printf("🔍 Analyzing image %d: %s\n", i+1, imgPath)
		}

		analysis, err := vp.analyzeImage(imgPath)
		if err != nil {
			if vp.debug {
				fmt.Printf("⚠️  Failed to analyze %s: %v\n", imgPath, err)
			}
			continue
		}

		analyses = append(analyses, analysis)

		// Replace image reference with detailed analysis
		enhancedText = vp.enhanceTextWithAnalysis(enhancedText, imgPath, analysis)
	}

	if vp.debug && len(analyses) > 0 {
		fmt.Printf("✅ Successfully analyzed %d images\n", len(analyses))
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

// analyzeImage processes a single image with the vision model
func (vp *VisionProcessor) analyzeImage(imagePath string) (VisionAnalysis, error) {
	// Download or read the image
	imageData, err := vp.getImageData(imagePath)
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("failed to get image data: %w", err)
	}

	// Create vision analysis prompt
	prompt := vp.createVisionPrompt(imagePath)

	// Create message with image
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
			Images:  []api.ImageData{{Base64: imageData, Type: "image/png"}},
		},
	}

	// Get vision analysis using the vision-enabled method
	response, err := vp.visionClient.SendVisionRequest(messages, nil, "")
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("vision request failed: %w", err)
	}

	// Store usage information for cost tracking
	if response.Usage.TotalTokens > 0 {
		lastVisionUsage = &VisionUsageInfo{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
		}
	}

	// Extract response content
	if len(response.Choices) == 0 {
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

// analyzeImageWithPrompt analyzes an image with a custom prompt
func (vp *VisionProcessor) analyzeImageWithPrompt(imagePath string, customPrompt string) (VisionAnalysis, error) {
	// Download or read the image
	imageData, err := vp.getImageData(imagePath)
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("failed to get image data: %w", err)
	}

	// Use custom prompt or default
	prompt := customPrompt
	if prompt == "" {
		prompt = vp.createVisionPrompt(imagePath)
	}

	// Create messages for the vision model
	// Detect image type from the file extension or content
	imageType := "image/png" // default to png since most images we process are png
	lowerPath := strings.ToLower(imagePath)
	if strings.HasSuffix(lowerPath, ".png") {
		imageType = "image/png"
	} else if strings.HasSuffix(lowerPath, ".gif") {
		imageType = "image/gif"
	} else if strings.HasSuffix(lowerPath, ".webp") {
		imageType = "image/webp"
	} else if strings.HasSuffix(lowerPath, ".avif") {
		imageType = "image/avif"
	}

	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
			Images:  []api.ImageData{{Base64: imageData, Type: imageType}},
		},
	}

	// Get vision analysis using the vision-enabled method
	response, err := vp.visionClient.SendVisionRequest(messages, nil, "")
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("vision request failed: %w", err)
	}

	// Store usage information for cost tracking
	if response.Usage.TotalTokens > 0 {
		lastVisionUsage = &VisionUsageInfo{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
		}
	}

	// Extract response content
	if len(response.Choices) == 0 {
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

// getImageData reads image data from file or URL
func (vp *VisionProcessor) getImageData(imagePath string) (string, error) {
	var data []byte
	var err error

	if strings.HasPrefix(imagePath, "http") {
		// Download from URL
		data, err = vp.downloadImage(imagePath)
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

// downloadImage downloads an image from URL
func (vp *VisionProcessor) downloadImage(url string) ([]byte, error) {
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

// createVisionPrompt creates an appropriate prompt based on image context
func (vp *VisionProcessor) createVisionPrompt(imagePath string) string {
	filename := filepath.Base(imagePath)

	// Customize prompt based on likely image type
	if strings.Contains(strings.ToLower(filename), "ui") ||
		strings.Contains(strings.ToLower(filename), "screen") ||
		strings.Contains(strings.ToLower(filename), "mockup") {
		return `Analyze this UI screenshot or mockup in detail. Please provide:

1. **Overall Description**: What type of interface is this?
2. **UI Elements**: List all visible elements (buttons, inputs, text, navigation, etc.) with their positions
3. **Layout & Design**: Describe the layout, colors, typography, spacing
4. **Implementation Guidance**: Suggest HTML structure, CSS classes, or component architecture that would be needed

Format your response clearly with sections. Focus on details that would help a developer implement or modify this interface.`
	}

	if strings.Contains(strings.ToLower(filename), "error") ||
		strings.Contains(strings.ToLower(filename), "bug") {
		return `Analyze this error screenshot or bug report image. Please provide:

1. **Error Description**: What error or issue is shown?
2. **Context**: What application, browser, or environment is this?
3. **Symptoms**: Describe exactly what's wrong or unexpected
4. **Potential Causes**: What might be causing this issue?
5. **Investigation Steps**: How would you debug this problem?
6. **Fix Suggestions**: What changes might resolve this issue?

Be specific and technical in your analysis.`
	}

	// General image analysis
	return `Analyze this image in the context of software development. Please provide:

1. **Content Description**: What does this image show?
2. **Technical Details**: Any code, interfaces, diagrams, or technical content
3. **Context**: How this relates to software development or implementation
4. **Key Information**: Important details a developer should know
5. **Implementation Notes**: If applicable, how to implement or recreate what's shown

Focus on providing actionable information for software development tasks.`
}

// looksLikeUI determines if the description suggests a UI interface
func (vp *VisionProcessor) looksLikeUI(description string) bool {
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

// extractUIElements attempts to extract structured UI elements from the description
func (vp *VisionProcessor) extractUIElements(description string) []UIElement {
	// This is a simplified extraction - could be enhanced with more sophisticated parsing
	var elements []UIElement

	// Look for common UI element mentions
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		if element := vp.parseUIElementFromLine(line); element.Type != "" {
			elements = append(elements, element)
		}
	}

	return elements
}

// parseUIElementFromLine attempts to extract a UI element from a description line
func (vp *VisionProcessor) parseUIElementFromLine(line string) UIElement {
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
				Position:    vp.extractPosition(line),
			}
		}
	}

	return UIElement{}
}

// extractPosition attempts to extract position information from a description
func (vp *VisionProcessor) extractPosition(line string) string {
	positionKeywords := []string{"top", "bottom", "left", "right", "center", "upper", "lower", "corner"}
	lowerLine := strings.ToLower(line)

	for _, keyword := range positionKeywords {
		if strings.Contains(lowerLine, keyword) {
			return keyword
		}
	}

	return "unknown"
}

// enhanceTextWithAnalysis replaces image references with detailed analysis
func (vp *VisionProcessor) enhanceTextWithAnalysis(text, imagePath string, analysis VisionAnalysis) string {
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

// HasVisionCapability checks if vision processing is available
func HasVisionCapability() bool {
	// Check if any provider with vision capability is available
	// Priority: local providers first, then remote providers.
	providers := []api.ClientType{
		api.OllamaClientType,
		api.OllamaLocalClientType,
		api.DeepInfraClientType,
		api.OpenRouterClientType,
		api.OpenAIClientType,
		api.MistralClientType,
		api.DeepSeekClientType,
		api.ZAIClientType,
	}

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

// VisionUsageInfo contains token usage and cost information from vision model calls
type VisionUsageInfo struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

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

	ext := strings.ToLower(filepath.Ext(imagePath))
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
		fmt.Printf("🔄 Using cached vision analysis for %s [%s]\n", filepath.Base(imagePath), analysisMode)

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
		prompt = generatePromptForMode(analysisMode)
	}

	response.InputResolved = true
	response.OCRAttempted = true

	analysis, err := processor.analyzeImageWithPrompt(imagePath, prompt)
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

// ProcessPDFWithVision processes a PDF file using Ollama with glm-ocr model
func ProcessPDFWithVision(pdfPath string) (string, error) {
	// Load config to check PDF OCR settings
	configManager, err := configuration.NewManager()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	config := configManager.GetConfig()

	// Check if PDF OCR is enabled
	if !config.PDFOCREnabled {
		return "", fmt.Errorf("PDF OCR is not enabled. Please enable PDF OCR in config with provider 'ollama' and model 'glm-ocr'")
	}

	// Use the configured provider and model
	provider := config.PDFOCRProvider
	model := config.PDFOCRModel

	// Add :latest suffix if not present (Ollama convention)
	if model != "" && !strings.Contains(model, ":") {
		model = model + ":latest"
	}

	if provider == "" || model == "" {
		return "", fmt.Errorf("PDF OCR provider and model must be configured")
	}

	// Process PDF with the configured provider
	text, err := processPDFWithProvider(pdfPath, provider, model)
	if err != nil {
		return "", fmt.Errorf("PDF OCR failed: %w", err)
	}

	return text, nil
}

// processPDFWithProvider processes a PDF using the specified provider and model
// Works cross-platform without system dependencies (poppler, tesseract, etc.)
func processPDFWithProvider(pdfPath, provider, model string) (string, error) {
	// Check file size
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat PDF file: %w", err)
	}

	maxSize := int64(50 * 1024 * 1024) // 50MB for PDF OCR
	if fileInfo.Size() > maxSize {
		return "", fmt.Errorf("PDF file too large (%d MB), maximum size is %d MB", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}

	// First try to extract text using pypdf (works for text-based PDFs, cross-platform)
	text, hasText, err := extractTextWithPypdf(pdfPath)
	if err == nil && hasText && len(strings.TrimSpace(text)) > 0 {
		return text, nil
	}

	// If no text found, try OCR by extracting images from PDF
	if !hasText || len(strings.TrimSpace(text)) == 0 {
		// Try OCR by extracting images from PDF
		ocrText, ocrErr := processPDFWithOCR(pdfPath, provider, model)
		if ocrErr == nil && len(strings.TrimSpace(ocrText)) > 0 {
			return ocrText, nil
		}
		// If image extraction OCR failed, try sending PDF directly
		if ocrErr != nil {
			ocrText2, ocrErr2 := processPDFWithVisionModel(pdfPath, provider, model)
			if ocrErr2 == nil && len(strings.TrimSpace(ocrText2)) > 0 {
				return ocrText2, nil
			}
			// Return original error if all fail
			return "", fmt.Errorf("PDF has no extractable text and OCR failed: %w", ocrErr)
		}
	}

	return text, nil
}

// extractTextWithPypdf extracts text from PDF using pypdf
func extractTextWithPypdf(pdfPath string) (string, bool, error) {
	cmd := exec.Command("python3", "-c", fmt.Sprintf(`
import sys
try:
    from pypdf import PdfReader
    reader = PdfReader('%s')
    text = ''
    for page in reader.pages:
        page_text = page.extract_text()
        if page_text:
            text += page_text + '\\n'
    print(text[:5000])  # Limit output
    if text.strip():
        sys.exit(0)
    else:
        sys.exit(1)  # No text found
except Exception as e:
    print(f'Error: {e}', file=sys.stderr)
    sys.exit(2)
`, pdfPath))

	output, err := cmd.CombinedOutput()
	exitCode := cmd.ProcessState.ExitCode()

	if err == nil && exitCode == 0 {
		return string(output), true, nil
	}

	return "", false, fmt.Errorf("pypdf extraction failed: %s", string(output))
}

// processPDFWithOCR extracts images from PDF and uses vision model for OCR
// Cross-platform solution using pypdf for image extraction (BSD licensed)
func processPDFWithOCR(pdfPath, provider, model string) (string, error) {
	// Extract images from PDF using pypdf (cross-platform, no external deps)
	images, err := extractImagesFromPDF(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract images from PDF: %w", err)
	}

	if len(images) == 0 {
		return "", fmt.Errorf("no images found in PDF (scanned PDF may be single raster image)")
	}

	// Create client
	var client api.ClientInterface
	switch provider {
	case "ollama":
		client, err = createOllamaClient(model)
		if err != nil {
			return "", fmt.Errorf("failed to create Ollama client: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported PDF OCR provider: %s", provider)
	}

	// Process each extracted image
	var allText strings.Builder
	for i, imgData := range images {
		imgBase64 := base64.StdEncoding.EncodeToString(imgData)

		// Determine image type from first bytes
		imgType := "image/png"
		if len(imgData) >= 4 {
			if imgData[0] == 0xFF && imgData[1] == 0xD8 {
				imgType = "image/jpeg"
			} else if imgData[0] == 0x89 && string(imgData[1:4]) == "PNG" {
				imgType = "image/png"
			}
		}

		// Create prompt for OCR
		prompt := "Extract all text from this image. Return only the extracted text."

		// Create message with image
		messages := []api.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []api.ImageData{{Base64: imgBase64, Type: imgType}},
			},
		}

		// Send request
		response, err := client.SendChatRequest(messages, nil, "")
		if err != nil {
			continue // Try next image
		}

		if len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
			if allText.Len() > 0 {
				allText.WriteString("\n\n--- Image ")
				allText.WriteString(fmt.Sprintf("%d", i+1))
				allText.WriteString(" ---\n\n")
			}
			allText.WriteString(response.Choices[0].Message.Content)
		}
	}

	if allText.Len() == 0 {
		return "", fmt.Errorf("OCR failed for all extracted images")
	}

	return allText.String(), nil
}

// extractImagesFromPDF extracts all images from a PDF using pypdf and Pillow
// Returns properly formatted PNG images for OCR
func extractImagesFromPDF(pdfPath string) ([][]byte, error) {
	cmd := exec.Command("python3", "-c", fmt.Sprintf(`
import sys
import base64
try:
    from pypdf import PdfReader
    from PIL import Image
    import io
    
    reader = PdfReader('%s')
    images = []
    for page_num, page in enumerate(reader.pages):
        if '/XObject' in page['/Resources']:
            xobjects = page['/Resources']['/XObject'].get_object()
            for obj in xobjects:
                if xobjects[obj]['/Subtype'] == '/Image':
                    try:
                        data = xobjects[obj].get_data()
                        filter_type = str(xobjects[obj].get('/Filter', ''))
                        
                        # Handle different filter types
                        if 'DCTDecode' in filter_type:
                            # JPEG encoded - decode directly with PIL
                            img = Image.open(io.BytesIO(data))
                        elif 'JPXDecode' in filter_type:
                            # JPEG2000 - try to handle
                            img = Image.open(io.BytesIO(data))
                        else:
                            # Raw/FlateDecode - need to get dimensions
                            width = xobjects[obj]['/Width']
                            height = xobjects[obj]['/Height']
                            color_mode = 'L'
                            if '/ColorSpace' in xobjects[obj]:
                                cs = str(xobjects[obj]['/ColorSpace'])
                                if 'RGB' in cs:
                                    color_mode = 'RGB'
                            img = Image.frombytes(color_mode, (width, height), data)
                        
                        # Convert to PNG
                        png_io = io.BytesIO()
                        img.save(png_io, 'PNG')
                        png_data = png_io.getvalue()
                        images.append(base64.b64encode(png_data).decode('ascii'))
                    except Exception as e:
                        print(f'Error extracting image: {{e}}', file=sys.stderr)
    print('|'.join(images))
except Exception as e:
    print(f'Error: {{e}}', file=sys.stderr)
    sys.exit(1)
`, pdfPath))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pypdf image extraction failed: %s", string(output))
	}

	// Parse base64-encoded images
	var images [][]byte
	if len(output) > 0 {
		encoded := strings.TrimSpace(string(output))
		if encoded != "" {
			for _, enc := range strings.Split(encoded, "|") {
				if enc != "" {
					data, err := base64.StdEncoding.DecodeString(enc)
					if err == nil {
						images = append(images, data)
					}
				}
			}
		}
	}

	return images, nil
}

// processPDFWithVisionModel sends PDF directly to glm-ocr model for OCR
// This is cross-platform and doesn't require poppler or tesseract
func processPDFWithVisionModel(pdfPath, provider, model string) (string, error) {
	// Read PDF file
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF: %w", err)
	}

	// Convert to base64
	pdfBase64 := base64.StdEncoding.EncodeToString(data)

	// Create client
	var client api.ClientInterface
	switch provider {
	case "ollama":
		client, err = createOllamaClient(model)
		if err != nil {
			return "", fmt.Errorf("failed to create Ollama client: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported PDF OCR provider: %s", provider)
	}

	// Create prompt for OCR
	prompt := "Extract all text from this PDF document. Return only the extracted text, preserving the structure."

	// Create message with PDF - glm-ocr supports PDF natively
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
			Images:  []api.ImageData{{Base64: pdfBase64, Type: "application/pdf"}},
		},
	}

	// Send request to Ollama
	response, err := client.SendChatRequest(messages, nil, "")
	if err != nil {
		return "", fmt.Errorf("OCR request failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from OCR model")
	}

	return response.Choices[0].Message.Content, nil
}

// createOllamaClient creates an Ollama client with the specified model
func createOllamaClient(model string) (api.ClientInterface, error) {
	model = ensureOllamaModelTag(model)
	client, err := factory.CreateProviderClient(api.OllamaClientType, model)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func ensureOllamaModelTag(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}
	if strings.Contains(model, ":") {
		return model
	}
	return model + ":latest"
}

// SimplePDFInfo returns basic info about PDF file
func SimplePDFInfo(pdfPath string) (map[string]interface{}, error) {
	// Check file size before processing (limit to 20MB for safety)
	fileInfo, err := os.Stat(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat PDF file: %w", err)
	}

	maxSize := int64(20 * 1024 * 1024) // 20MB
	if fileInfo.Size() > maxSize {
		return nil, fmt.Errorf("PDF file too large (%d MB), maximum size is %d MB", fileInfo.Size()/1024/1024, maxSize/1024/1024)
	}

	f, r, err := pdf.Open(pdfPath)
	defer func() {
		_ = f.Close()
	}()
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}

	info := make(map[string]interface{})
	info["page_count"] = r.NumPage()
	info["has_text"] = false

	// Check if PDF has extractable text
	for pageNum := 1; pageNum <= r.NumPage(); pageNum++ {
		p := r.Page(pageNum)
		if p.V.IsNull() {
			continue
		}

		text, err := p.GetPlainText(nil)
		if err == nil && strings.TrimSpace(text) != "" {
			info["has_text"] = true
			break
		}
	}

	return info, nil
}

// ProcessPDFForVision processes PDF using Ollama with glm-ocr model
func (vp *VisionProcessor) ProcessPDFForVision(pdfPath string) (VisionAnalysis, error) {
	text, err := ProcessPDFWithVision(pdfPath)
	if err != nil {
		return VisionAnalysis{}, fmt.Errorf("PDF OCR failed: %w", err)
	}

	return VisionAnalysis{
		ImagePath:   pdfPath,
		Description: text,
	}, nil
}
