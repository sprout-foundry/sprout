package tools

// This file serves as the main entry point for the vision tool package.
// The implementation has been refactored into several logical units:
//
// - vision_types.go: Core types, error codes, and constants
// - vision_prompts.go: Prompt generation for different analysis modes
// - vision_analyze.go: VisionProcessor methods for image analysis
// - vision_pdf.go: PDF processing and OCR functionality
//
// The main exported functions are:
//
// - AnalyzeImage(imagePath, analysisPrompt, analysisMode) - Main entry point for image analysis
// - HasVisionCapability() - Check if vision is available
// - ProcessPDFWithVision(pdfPath) - Process PDF files with OCR
// - GetLastVisionUsage() - Get token usage from last vision call
// - GetVisionCacheStats() - Get cache statistics
//
// Example usage:
//
//	result, err := tools.AnalyzeImage("screenshot.png", "", "general")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result)
