package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// Tool handler implementations for analysis operations

func handleAnalyzeUIScreenshot(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_ui_screenshot tool")
	}

	imagePath := args["image_path"].(string)
	a.debugLog("Analyzing UI screenshot: %s\n", imagePath)

	result, err := tools.AnalyzeImage(imagePath, "", "frontend")
	a.debugLog("Analyze UI screenshot error: %v\n", err)
	if err != nil {
		return result, err
	}
	a.captureVisionInputAndOutput(imagePath, result)

	normalized, normalizeErr := normalizeVisionToolOutput(result, true)
	if normalizeErr != nil {
		return "", normalizeErr
	}
	return normalized, nil
}

func handleAnalyzeImageContent(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent context is required for analyze_image_content tool")
	}

	imagePath := args["image_path"].(string)
	analysisPrompt := ""
	if v, ok := args["analysis_prompt"].(string); ok {
		analysisPrompt = v
	}
	analysisMode := "general"
	if v, ok := args["analysis_mode"].(string); ok && strings.TrimSpace(v) != "" {
		analysisMode = v
	}

	a.debugLog("Analyzing image: %s (mode=%s)\n", imagePath, analysisMode)

	result, err := tools.AnalyzeImage(imagePath, analysisPrompt, analysisMode)
	a.debugLog("Analyze image content error: %v\n", err)

	// Check if model download is needed
	if err != nil && strings.Contains(err.Error(), tools.ErrModelDownloadNeeded) {
		// Inform user about simplified processing
		prompt := "PDF processing has been simplified and no longer requires model downloads. The PDF will be processed using the new approach."
		choices := []ChoiceOption{
			{Label: "Continue with simplified processing", Value: "yes"},
			{Label: "Skip", Value: "no"},
		}

		a.PrintLine(fmt.Sprintf("\n✨ %s\n", prompt))

		choice, promptErr := a.PromptChoice(prompt, choices)
		if promptErr != nil {
			a.PrintLine(fmt.Sprintf("⚠️ Could not prompt for choice: %v", promptErr))
			return result, err
		}

		if choice == "yes" {
			// The simplified PDF processing doesn't require model downloads
			a.PrintLine("🔄 Processing PDF with simplified approach...")
			result, err = tools.AnalyzeImage(imagePath, analysisPrompt, analysisMode)
		}
	}

	if err != nil {
		return result, err
	}
	a.captureVisionInputAndOutput(imagePath, result)

	normalized, normalizeErr := normalizeVisionToolOutput(result, false)
	if normalizeErr != nil {
		return "", normalizeErr
	}
	return normalized, nil
}

func normalizeVisionToolOutput(result string, preferPlainText bool) (string, error) {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return result, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return result, nil
	}

	var parsed tools.ImageAnalysisResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return result, nil
	}

	if !parsed.Success {
		code := strings.TrimSpace(parsed.ErrorCode)
		msg := strings.TrimSpace(parsed.ErrorMessage)
		if code == "" && msg == "" {
			return "", fmt.Errorf("vision analysis failed")
		}
		if code == "" {
			return "", fmt.Errorf("vision analysis failed: %s", msg)
		}
		if msg == "" {
			return "", fmt.Errorf("vision analysis failed (%s)", code)
		}
		return "", fmt.Errorf("vision analysis failed (%s): %s", code, msg)
	}

	if !preferPlainText {
		return result, nil
	}

	if text := strings.TrimSpace(parsed.ExtractedText); text != "" {
		return text, nil
	}
	if parsed.Analysis != nil {
		if desc := strings.TrimSpace(parsed.Analysis.Description); desc != "" {
			return desc, nil
		}
	}
	return result, nil
}
