package tools

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LooksLikeUI
// ---------------------------------------------------------------------------

func TestLooksLikeUI(t *testing.T) {
	vp := &VisionProcessor{}

	tests := []struct {
		name   string
		input  string
		wantUI bool
	}{
		{"two UI keywords", "a login form with a submit button", true},
		{"one UI keyword only (button + page both match)", "a button on the page", true},
		{"no UI keywords", "a beautiful landscape photo", false},
		{"many UI keywords", "navigation menu input form dropdown button checkbox", true},
		{"empty string", "", false},
		{"case insensitive", "A Big FORM WITH A BUTTON", true},
		{"interface and screen", "the interface shows a dashboard screen", true},
		{"page and component", "the page has a custom component", true},
		{"menu and navigation", "top menu navigation bar", true},
		{"just 'button'", "click the button", false},
		{"button + form in long text", "Lorem ipsum dolor form sit amet button", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vp.LooksLikeUI(tt.input)
			if got != tt.wantUI {
				t.Errorf("LooksLikeUI(%q) = %v, want %v", tt.input, got, tt.wantUI)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractPosition
// ---------------------------------------------------------------------------

func TestExtractPosition(t *testing.T) {
	vp := &VisionProcessor{}

	tests := []struct {
		name string
		line string
		want string
	}{
		{"top keyword", "There is a button at the top of the page", "top"},
		{"bottom keyword", "The footer is at the bottom", "bottom"},
		{"left keyword", "Menu on the left side", "left"},
		{"right keyword", "Navigation on the right", "right"},
		{"center keyword", "Centered content", "center"},
		{"upper keyword alone", "The upper section of the document", "upper"},
		{"lower keyword alone", "In the lower area", "lower"},
		{"corner keyword", "In the corner", "corner"},
		{"no keyword", "Just some random text", "unknown"},
		{"empty string", "", "unknown"},
		{"case insensitive TOP", "Place at the TOP", "top"},
		{"first matching keyword wins", "Top and bottom and center", "top"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vp.ExtractPosition(tt.line)
			if got != tt.want {
				t.Errorf("ExtractPosition(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseUIElementFromLine
// ---------------------------------------------------------------------------

func TestParseUIElementFromLine(t *testing.T) {
	vp := &VisionProcessor{}

	tests := []struct {
		name       string
		line       string
		wantType   string
		wantPos    string
		wantInDesc string
	}{
		{"button keyword", "Click the Submit button at the top", "button", "top", "Submit button"},
		{"btn keyword", "There is a btn at the bottom", "button", "bottom", "btn"},
		{"input keyword", "Enter your name in the input field", "input", "unknown", "input field"},
		{"field keyword", "The name field on the form", "input", "unknown", "field on the form"},
		{"label keyword", "A label above the text", "text", "unknown", "label above the text"},
		{"link keyword", "Click the link here", "link", "unknown", "link here"},
		{"image keyword", "An image showing a chart", "image", "unknown", "image showing a chart"},
		{"dropdown keyword", "Select from the dropdown", "dropdown", "unknown", "dropdown"},
		{"checkbox keyword", "Check the checkbox", "checkbox", "unknown", "checkbox"},
		{"radio keyword", "Choose a radio option", "radio", "unknown", "radio option"},
		{"no match", "Just some random stuff", "", "unknown", ""},
		{"empty line", "", "", "unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vp.ParseUIElementFromLine(tt.line)
			if tt.wantType == "" {
				// expect no match
				if got.Type != "" {
					t.Errorf("ParseUIElementFromLine(%q) = %+v, want empty element", tt.line, got)
				}
			} else {
				if got.Type != tt.wantType {
					t.Errorf("ParseUIElementFromLine(%q).Type = %q, want %q", tt.line, got.Type, tt.wantType)
				}
				if got.Position != tt.wantPos {
					t.Errorf("ParseUIElementFromLine(%q).Position = %q, want %q", tt.line, got.Position, tt.wantPos)
				}
				if !strings.Contains(got.Description, tt.wantInDesc) {
					t.Errorf("ParseUIElementFromLine(%q).Description = %q, want it to contain %q", tt.line, got.Description, tt.wantInDesc)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractUIElements
// ---------------------------------------------------------------------------

func TestExtractUIElements(t *testing.T) {
	vp := &VisionProcessor{}

	tests := []struct {
		name      string
		desc      string
		wantCount int
	}{
		{"empty description", "", 0},
		{"single button", "A submit button at the top", 1},
		{"multiple elements", "A button at the top\nAn input field\nA link on the left", 3},
		{"no UI elements", "Just regular stuff\nNothing special here\nPlain words", 0},
		{"mixed lines", "A random phrase\nSubmit button at top\nAnother phrase\nA checkbox below", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			elements := vp.ExtractUIElements(tt.desc)
			if len(elements) != tt.wantCount {
				t.Errorf("ExtractUIElements(%q) returned %d elements, want %d", tt.desc, len(elements), tt.wantCount)
			}
		})
	}
}

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
