package tools

import "strings"

// ============================================================================
// Prompt Generation
// ============================================================================

// GeneratePromptForMode creates appropriate prompts based on analysis mode
func GeneratePromptForMode(mode string) string {
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

// CreateVisionPrompt creates an appropriate prompt based on image context
func (vp *VisionProcessor) CreateVisionPrompt(imagePath string) string {
	filename := GetBaseName(imagePath)
	lowerFilename := strings.ToLower(filename)

	// Customize prompt based on likely image type
	if strings.Contains(lowerFilename, "ui") ||
		strings.Contains(lowerFilename, "screen") ||
		strings.Contains(lowerFilename, "mockup") {
		return `Analyze this UI screenshot or mockup in detail. Please provide:

1. **Overall Description**: What type of interface is this?
2. **UI Elements**: List all visible elements (buttons, inputs, text, navigation, etc.) with their positions
3. **Layout & Design**: Describe the layout, colors, typography, spacing
4. **Implementation Guidance**: Suggest HTML structure, CSS classes, or component architecture that would be needed

Format your response clearly with sections. Focus on details that would help a developer implement or modify this interface.`
	}

	if strings.Contains(lowerFilename, "error") ||
		strings.Contains(lowerFilename, "bug") {
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

// GetUIElementPrompt returns a prompt for extracting UI elements
func GetUIElementPrompt() string {
	return "Extract all UI elements from this image. For each element, identify its type (button, input, text, link, image, dropdown, checkbox, radio), description, and approximate position (top, bottom, left, right, center)."
}

// GetOCRPrompt returns a prompt for OCR text extraction
func GetOCRPrompt() string {
	return "Extract all text from this image. Return only the extracted text."
}

// GetPDFOCRPrompt returns a prompt for PDF OCR
func GetPDFOCRPrompt() string {
	return "Extract all text from this PDF document. Return only the extracted text, preserving the structure."
}
