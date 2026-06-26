package tools

import (
	"strings"
	"testing"
)

// TestGeneratePromptForMode tests the GeneratePromptForMode function with various modes.
func TestGeneratePromptForMode(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		wantContains []string // Substrings that should be present in the result
	}{
		{
			name: "frontend mode",
			mode: "frontend",
			wantContains: []string{
				"Colors",
				"Layout Structure",
				"Typography",
				"CSS Implementation",
				"Design Tokens",
			},
		},
		{
			name: "design mode",
			mode: "design",
			wantContains: []string{
				"Colors",
				"Layout Structure",
				"CSS Implementation",
			},
		},
		{
			name: "ui mode",
			mode: "ui",
			wantContains: []string{
				"Colors",
				"Layout Structure",
				"CSS Implementation",
			},
		},
		{
			name: "html mode",
			mode: "html",
			wantContains: []string{
				"Colors",
				"Layout Structure",
				"CSS Implementation",
			},
		},
		{
			name: "css mode",
			mode: "css",
			wantContains: []string{
				"Colors",
				"Layout Structure",
				"CSS Implementation",
			},
		},
		{
			name: "general mode",
			mode: "general",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
				"Technical Details",
				"Context",
				"Key Information",
			},
		},
		{
			name: "text mode",
			mode: "text",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
			},
		},
		{
			name: "content mode",
			mode: "content",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
			},
		},
		{
			name: "extract mode",
			mode: "extract",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
			},
		},
		{
			name: "analyze mode",
			mode: "analyze",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
			},
		},
		{
			name: "default mode - unknown",
			mode: "unknown",
			wantContains: []string{
				"software development",
			},
		},
		{
			name: "default mode - empty",
			mode: "",
			wantContains: []string{
				"software development",
			},
		},
		{
			name: "case insensitive - FRONTEND",
			mode: "FRONTEND",
			wantContains: []string{
				"Colors",
				"Layout Structure",
				"CSS Implementation",
			},
		},
		{
			name: "case insensitive - General",
			mode: "General",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
			},
		},
		{
			name: "case insensitive - Ui",
			mode: "Ui",
			wantContains: []string{
				"Colors",
				"Layout Structure",
			},
		},
		{
			name: "case insensitive - DESIGN",
			mode: "DESIGN",
			wantContains: []string{
				"Colors",
				"Layout Structure",
			},
		},
		{
			name: "case insensitive - ANALYZE",
			mode: "ANALYZE",
			wantContains: []string{
				"Content Description",
				"Text Extraction",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GeneratePromptForMode(tt.mode)

			// Verify the prompt is not empty
			if got == "" {
				t.Errorf("GeneratePromptForMode(%q) returned empty string", tt.mode)
			}

			// Verify all expected substrings are present
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("GeneratePromptForMode(%q) does not contain expected substring %q\nGot: %s", tt.mode, want, got)
				}
			}
		})
	}
}

// TestGetUIElementPrompt tests the GetUIElementPrompt function.
func TestGetUIElementPrompt(t *testing.T) {
	got := GetUIElementPrompt()

	if got == "" {
		t.Error("GetUIElementPrompt() returned empty string")
	}

	expectedKeywords := []string{
		"UI elements",
		"button",
		"input",
		"text",
		"link",
		"image",
		"dropdown",
		"checkbox",
		"radio",
		"position",
	}

	for _, keyword := range expectedKeywords {
		if !strings.Contains(got, keyword) {
			t.Errorf("GetUIElementPrompt() does not contain expected keyword %q\nGot: %s", keyword, got)
		}
	}
}

// TestGetOCRPrompt tests the GetOCRPrompt function.
func TestGetOCRPrompt(t *testing.T) {
	got := GetOCRPrompt()

	if got == "" {
		t.Error("GetOCRPrompt() returned empty string")
	}

	expectedKeywords := []string{
		"Extract all text",
		"image",
	}

	for _, keyword := range expectedKeywords {
		if !strings.Contains(got, keyword) {
			t.Errorf("GetOCRPrompt() does not contain expected keyword %q\nGot: %s", keyword, got)
		}
	}
}

// TestCreateVisionPrompt tests the CreateVisionPrompt method of VisionProcessor.
func TestCreateVisionPrompt(t *testing.T) {
	vp := &VisionProcessor{}

	tests := []struct {
		name         string
		imagePath    string
		wantContains []string
	}{
		{
			name:      "UI screenshot filename",
			imagePath: "/path/to/ui-screenshot.png",
			wantContains: []string{
				"UI screenshot",
				"mockup",
				"UI Elements",
				"Implementation Guidance",
			},
		},
		{
			name:      "screen in filename",
			imagePath: "/home/user/screen-capture.jpg",
			wantContains: []string{
				"UI screenshot",
				"mockup",
			},
		},
		{
			name:      "mockup in filename",
			imagePath: "design-mockup.webp",
			wantContains: []string{
				"UI screenshot",
				"mockup",
			},
		},
		{
			name:      "error filename - but also contains 'screen' keyword",
			imagePath: "error-screenshot.png",
			wantContains: []string{
				"UI screenshot", // "screen" matches before "error"
			},
		},
		{
			name:      "bug filename",
			imagePath: "bug-report.jpg",
			wantContains: []string{
				"bug",
				"Error Description",
				"Fix Suggestions",
			},
		},
		{
			name:      "error-only filename",
			imagePath: "error-log.png",
			wantContains: []string{
				"error screenshot",
				"bug",
				"Error Description",
				"Potential Causes",
				"Fix Suggestions",
			},
		},
		{
			name:      "general filename",
			imagePath: "random-image.png",
			wantContains: []string{
				"software development",
				"Content Description",
				"Technical Details",
			},
		},
		{
			name:      "generic path",
			imagePath: "/home/user/pictures/photo.jpg",
			wantContains: []string{
				"software development",
				"Content Description",
			},
		},
		{
			name:      "case insensitive - UI-Screenshot.PNG",
			imagePath: "UI-Screenshot.PNG",
			wantContains: []string{
				"UI screenshot",
				"mockup",
			},
		},
		{
			name:      "case insensitive - ERROR.PNG",
			imagePath: "ERROR.PNG",
			wantContains: []string{
				"error screenshot",
				"bug",
			},
		},
		{
			name:      "case insensitive - Mockup.GIF",
			imagePath: "Mockup.GIF",
			wantContains: []string{
				"mockup",
				"UI screenshot",
			},
		},
		{
			name:      "mixed case - MyScreenShot.jpeg",
			imagePath: "MyScreenShot.jpeg",
			wantContains: []string{
				"UI screenshot",
			},
		},
		{
			name:      "empty path",
			imagePath: "",
			wantContains: []string{
				"software development",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vp.CreateVisionPrompt(tt.imagePath)

			// Verify the prompt is not empty
			if got == "" {
				t.Errorf("CreateVisionPrompt(%q) returned empty string", tt.imagePath)
			}

			// Verify all expected substrings are present
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("CreateVisionPrompt(%q) does not contain expected substring %q\nGot: %s", tt.imagePath, want, got)
				}
			}
		})
	}
}
