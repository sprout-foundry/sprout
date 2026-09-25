package tools

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LooksLikeUI
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ExtractPosition
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ParseUIElementFromLine
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ExtractUIElements
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// EnhanceTextWithAnalysis
// ---------------------------------------------------------------------------

func TestEnhanceTextWithAnalysis_DirectPath(t *testing.T) {
	vp := &VisionProcessor{}
	text := "Please look at /home/user/photo.png for details"
	analysis := VisionAnalysis{
		ImagePath:   "/home/user/photo.png",
		Description: "A cat sitting on a table",
	}

	result := vp.EnhanceTextWithAnalysis(text, "/home/user/photo.png", analysis)
	if !strings.Contains(result, "Image Analysis: photo.png") {
		t.Errorf("expected enhanced text to contain 'Image Analysis: photo.png', got: %s", result)
	}
	if !strings.Contains(result, "A cat sitting on a table") {
		t.Errorf("expected enhanced text to contain description, got: %s", result)
	}
	// Original reference should be replaced
	if strings.Contains(result, "/home/user/photo.png") {
		t.Errorf("original path should be replaced, got: %s", result)
	}
}

func TestEnhanceTextWithAnalysis_BaseNameMatch(t *testing.T) {
	vp := &VisionProcessor{}
	text := "Please look at photo.png for details"
	analysis := VisionAnalysis{
		ImagePath:   "/home/user/photo.png",
		Description: "A beautiful sunset",
	}

	result := vp.EnhanceTextWithAnalysis(text, "/home/user/photo.png", analysis)
	if !strings.Contains(result, "A beautiful sunset") {
		t.Errorf("expected enhanced text to contain description, got: %s", result)
	}
}

func TestEnhanceTextWithAnalysis_MarkdownSyntax(t *testing.T) {
	vp := &VisionProcessor{}
	text := "Here is ![photo.png](/home/user/photo.png) in the text"
	analysis := VisionAnalysis{
		ImagePath:   "/home/user/photo.png",
		Description: "A mountain view",
	}

	result := vp.EnhanceTextWithAnalysis(text, "/home/user/photo.png", analysis)
	if !strings.Contains(result, "A mountain view") {
		t.Errorf("expected enhanced text to contain description, got: %s", result)
	}
}

func TestEnhanceTextWithAnalysis_WithUIElements(t *testing.T) {
	vp := &VisionProcessor{}
	text := "Check screenshot.png"
	analysis := VisionAnalysis{
		ImagePath:   "screenshot.png",
		Description: "A login form",
		Elements: []UIElement{
			{Type: "button", Description: "Login button", Position: "center"},
			{Type: "input", Description: "Password field", Position: "top"},
		},
	}

	result := vp.EnhanceTextWithAnalysis(text, "screenshot.png", analysis)
	if !strings.Contains(result, "UI Elements Detected") {
		t.Errorf("expected UI Elements section, got: %s", result)
	}
	if !strings.Contains(result, "Login button") {
		t.Errorf("expected button description, got: %s", result)
	}
	if !strings.Contains(result, "Password field") {
		t.Errorf("expected input description, got: %s", result)
	}
}

func TestEnhanceTextWithAnalysis_NoMatch(t *testing.T) {
	vp := &VisionProcessor{}
	text := "This text has no image references"
	analysis := VisionAnalysis{
		ImagePath:   "photo.png",
		Description: "A cat",
	}

	result := vp.EnhanceTextWithAnalysis(text, "photo.png", analysis)
	// When no replacement is found, the original text is returned unchanged
	if result != text {
		t.Errorf("expected unchanged text when no match, got: %s", result)
	}
}
