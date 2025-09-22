package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_providers"
	"github.com/alantheprice/ledit/pkg/factory"
)

// Global variables for vision model tracking and caching
var lastVisionUsage *VisionUsageInfo
var visionCache = make(map[string]string)                // cache key -> result
var visionCacheUsage = make(map[string]*VisionUsageInfo) // cache key -> usage info

// VisionAnalysis represents the result of vision model analysis
type VisionAnalysis struct {
	ImagePath   string      `json:"image_path"`
	Description string      `json:"description"`
	Elements    []UIElement `json:"elements,omitempty"`
	Issues      []string    `json:"issues,omitempty"`
	Suggestions []string    `json:"suggestions,omitempty"`
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

// NewVisionProcessor creates a new vision processor
func NewVisionProcessor(debug bool) (*VisionProcessor, error) {
	// Try to create a vision-capable client (GPT-4V via OpenRouter)
	client, err := createVisionClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create vision client: %w", err)
	}

	return &VisionProcessor{
		visionClient: client,
		debug:        debug,
	}, nil
}

// NewVisionProcessorWithMode creates a vision processor optimized for specific analysis mode
func NewVisionProcessorWithMode(debug bool, mode string) (*VisionProcessor, error) {
	var client api.ClientInterface
	var err error

	// Choose optimal model based on analysis mode
	switch strings.ToLower(mode) {
	case "frontend", "design", "ui", "html", "css":
		// Use gemma-3-27b-it for comprehensive frontend analysis
		client, err = createVisionClientWithModel("google/gemma-3-27b-it")
	case "general", "text", "content", "extract", "analyze":
		// Use DeepSeek for fast general analysis
		client, err = createVisionClientWithModel("deepseek-ai/DeepSeek-V3.1")
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

// createVisionClient creates a client capable of vision analysis
func createVisionClient() (api.ClientInterface, error) {
	// List of providers to try, in order of preference
	providers := []struct {
		clientType api.ClientType
		envVar     string
	}{
		{api.OpenRouterClientType, "OPENROUTER_API_KEY"},
		{api.DeepInfraClientType, "DEEPINFRA_API_KEY"},

		{api.OllamaClientType, ""}, // Ollama doesn't need API key
	}

	for _, provider := range providers {
		// Check if provider is available
		if provider.envVar != "" && os.Getenv(provider.envVar) == "" {
			continue // Skip if API key not set
		}

		// For now, skip providers that don't have known vision support
		// TODO: Query provider directly for vision-capable models
		var visionModel string
		switch provider.clientType {
		case api.OpenAIClientType:
			visionModel = "gpt-5"
		case api.OpenRouterClientType:
			visionModel = "gpt-5"
		default:
			continue // Skip if no vision model known
		}

		// Try to create client with vision model
		client, err := factory.CreateProviderClient(provider.clientType, visionModel)
		if err != nil {
			continue // Try next provider
		}

		// Verify the client supports vision
		if !client.SupportsVision() {
			continue // Try next provider
		}

		return client, nil
	}

	return nil, fmt.Errorf("no vision-capable providers available - please set up OPENROUTER_API_KEY, DEEPINFRA_API_KEY, GROQ_API_KEY, or install Ollama with a vision model")
}

// createVisionClientWithModel creates a vision client using a specific model
func createVisionClientWithModel(modelName string) (api.ClientInterface, error) {
	// Determine which provider supports this model
	if strings.HasPrefix(modelName, "google/") || strings.HasPrefix(modelName, "meta-llama/") {
		// DeepInfra model
		if apiKey := os.Getenv("DEEPINFRA_API_KEY"); apiKey != "" {
			wrapper, err := providers.NewDeepInfraProviderWithModel(modelName)
			if err != nil {
				return nil, fmt.Errorf("failed to create DeepInfra client: %w", err)
			}
			return wrapper, nil
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
		`[^\s]+\.(?i:png|jpg|jpeg|gif|bmp|webp|svg)`,
		// URLs
		`https?://[^\s]+\.(?i:png|jpg|jpeg|gif|bmp|webp|svg)`,
		// Markdown image syntax
		`!\[[^\]]*\]\(([^)]+\.(?i:png|jpg|jpeg|gif|bmp|webp|svg))\)`,
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
			Images:  []api.ImageData{{Base64: imageData, Type: "image/jpeg"}},
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
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
			Images:  []api.ImageData{{Base64: imageData, Type: "image/jpeg"}},
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

// AnalyzeImageFile is a convenience function to analyze a single image file
func AnalyzeImageFile(imagePath string, debug bool) (*VisionAnalysis, error) {
	processor, err := NewVisionProcessor(debug)
	if err != nil {
		return nil, err
	}

	analysis, err := processor.analyzeImage(imagePath)
	if err != nil {
		return nil, err
	}

	return &analysis, nil
}

// HasVisionCapability checks if vision processing is available
func HasVisionCapability() bool {
	// Check if any provider with vision capability is available
	providers := []struct {
		clientType api.ClientType
		envVar     string
	}{
		{api.OpenRouterClientType, "OPENROUTER_API_KEY"},
		{api.DeepInfraClientType, "DEEPINFRA_API_KEY"},

		{api.OllamaClientType, ""}, // Ollama doesn't need API key
	}

	for _, provider := range providers {
		// For now, only OpenAI and OpenRouter have known vision support
		// TODO: Query provider directly for vision-capable models
		hasVisionSupport := false
		switch provider.clientType {
		case api.OpenAIClientType, api.OpenRouterClientType:
			hasVisionSupport = true
		default:
			continue // Skip if no vision support
		}

		if !hasVisionSupport {
			continue
		}

		// Check if provider is available
		if provider.envVar != "" && os.Getenv(provider.envVar) == "" {
			continue // Skip if API key not set
		}

		// Provider has vision support and is available
		return true
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
func AnalyzeImage(imagePath string, analysisPrompt string, analysisMode string) (string, error) {
	if !HasVisionCapability() {
		return "", fmt.Errorf("vision analysis not available - please set up OPENROUTER_API_KEY, DEEPINFRA_API_KEY, GROQ_API_KEY, or install Ollama with a vision model")
	}

	// Create cache key based on image path, mode, and prompt
	cacheKey := fmt.Sprintf("%s|%s|%s", imagePath, analysisMode, analysisPrompt)

	// Check cache first
	if cachedResult, exists := visionCache[cacheKey]; exists {
		fmt.Printf("🔄 Using cached vision analysis for %s [%s]\n", filepath.Base(imagePath), analysisMode)

		// Restore cached usage info for cost tracking
		if cachedUsage, hasUsage := visionCacheUsage[cacheKey]; hasUsage {
			lastVisionUsage = cachedUsage
		}

		return cachedResult, nil
	}

	// Create vision processor with appropriate model based on mode
	processor, err := NewVisionProcessorWithMode(false, analysisMode) // debug = false
	if err != nil {
		return "", fmt.Errorf("failed to create vision processor: %w", err)
	}

	// Determine the appropriate prompt based on analysis mode
	var prompt string
	if analysisPrompt != "" {
		// User provided custom prompt - use it directly
		prompt = analysisPrompt
	} else {
		// Generate appropriate prompt based on mode
		prompt = generatePromptForMode(analysisMode)
	}

	analysis, err := processor.analyzeImageWithPrompt(imagePath, prompt)
	if err != nil {
		return "", fmt.Errorf("image analysis failed: %w", err)
	}

	// Format the response
	result := fmt.Sprintf("## Image Analysis: %s\n\n", filepath.Base(imagePath))
	result += fmt.Sprintf("**Description:** %s\n\n", analysis.Description)

	if len(analysis.Elements) > 0 {
		result += "**UI Elements:**\n"
		for _, element := range analysis.Elements {
			result += fmt.Sprintf("- %s (%s): %s\n", element.Type, element.Position, element.Description)
		}
		result += "\n"
	}

	if len(analysis.Issues) > 0 {
		result += "**Issues:**\n"
		for _, issue := range analysis.Issues {
			result += fmt.Sprintf("- %s\n", issue)
		}
		result += "\n"
	}

	if len(analysis.Suggestions) > 0 {
		result += "**Suggestions:**\n"
		for _, suggestion := range analysis.Suggestions {
			result += fmt.Sprintf("- %s\n", suggestion)
		}
	}

	// Cache the result and usage info for future calls
	visionCache[cacheKey] = result
	if lastVisionUsage != nil {
		visionCacheUsage[cacheKey] = lastVisionUsage
	}

	return result, nil
}
