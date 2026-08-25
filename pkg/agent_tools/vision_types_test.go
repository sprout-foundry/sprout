package tools

import (
	"encoding/json"
	"math"
	"testing"
)

// TestGetVisionMaxReturnedTextChars tests the getVisionMaxReturnedTextChars function.
func TestGetVisionMaxReturnedTextChars(t *testing.T) {
	// Test default behavior (no env var override in typical test environment)
	t.Run("default value", func(t *testing.T) {
		got := getVisionMaxReturnedTextChars()
		want := 20000

		if got != want {
			t.Errorf("getVisionMaxReturnedTextChars() = %v, want %v", got, want)
		}
	})

	// Note: Environment variable testing is limited because configuration.GetEnvSimple
	// may have caching behavior or use a different mechanism than direct os.Getenv.
	// In a real test environment, you would need to test with the actual configuration
	// system set up or inject the value through the proper configuration layer.
}

// TestVisionUsage tests GetLastVisionUsage and ClearLastVisionUsage.
// Note: These tests cannot be parallel because they modify global state.
func TestVisionUsage(t *testing.T) {
	// Clear global state at the start
	ClearLastVisionUsage()

	t.Run("initially nil", func(t *testing.T) {
		got := GetLastVisionUsage()
		if got != nil {
			t.Errorf("GetLastVisionUsage() = %v, want nil", got)
		}
	})

	t.Run("set and retrieve", func(t *testing.T) {
		// Set via recordVisionUsage helper (no processor, only global mirror)
		recordVisionUsage(nil, &VisionUsageInfo{
			PromptTokens:     1000,
			CompletionTokens: 500,
			TotalTokens:      1500,
			EstimatedCost:    0.01,
		})

		got := GetLastVisionUsage()

		if got == nil {
			t.Error("GetLastVisionUsage() = nil, want non-nil")
		} else {
			if got.PromptTokens != 1000 {
				t.Errorf("PromptTokens = %v, want 1000", got.PromptTokens)
			}
			if got.CompletionTokens != 500 {
				t.Errorf("CompletionTokens = %v, want 500", got.CompletionTokens)
			}
			if got.TotalTokens != 1500 {
				t.Errorf("TotalTokens = %v, want 1500", got.TotalTokens)
			}
			if got.EstimatedCost != 0.01 {
				t.Errorf("EstimatedCost = %v, want 0.01", got.EstimatedCost)
			}
		}
	})

	t.Run("clear returns nil", func(t *testing.T) {
		ClearLastVisionUsage()

		got := GetLastVisionUsage()
		if got != nil {
			t.Errorf("GetLastVisionUsage() after ClearLastVisionUsage() = %v, want nil", got)
		}
	})

	// Reset for other tests
	ClearLastVisionUsage()
}

// TestVisionCacheStats tests the GetVisionCacheStats function.
// Note: This test cannot be parallel because it modifies global state.
func TestVisionCacheStats(t *testing.T) {
	// Clear global state at the start
	resetVisionCache()

	t.Run("empty cache", func(t *testing.T) {
		stats := GetVisionCacheStats()

		if stats["cached_results"] != 0 {
			t.Errorf("cached_results = %v, want 0", stats["cached_results"])
		}
		if stats["estimated_savings"] != 0.0 {
			t.Errorf("estimated_savings = %v, want 0.0", stats["estimated_savings"])
		}
	})

	t.Run("single cached result", func(t *testing.T) {
		visionLRU.Put("key1", "result1", &VisionUsageInfo{
			TotalTokens:   1000,
			EstimatedCost: 0.01,
		})

		stats := GetVisionCacheStats()

		if stats["cached_results"] != 1 {
			t.Errorf("cached_results = %v, want 1", stats["cached_results"])
		}
		if stats["estimated_savings"] != 0.01 {
			t.Errorf("estimated_savings = %v, want 0.01", stats["estimated_savings"])
		}
	})

	t.Run("multiple cached results", func(t *testing.T) {
		visionLRU.Put("key2", "result2", &VisionUsageInfo{
			TotalTokens:   2000,
			EstimatedCost: 0.02,
		})
		visionLRU.Put("key3", "result3", &VisionUsageInfo{
			TotalTokens:   1500,
			EstimatedCost: 0.015,
		})

		stats := GetVisionCacheStats()

		if stats["cached_results"] != 3 {
			t.Errorf("cached_results = %v, want 3", stats["cached_results"])
		}
		// 0.01 + 0.02 + 0.015 = 0.045
		got := stats["estimated_savings"].(float64)
		if math.Abs(got-0.045) > 1e-9 {
			t.Errorf("estimated_savings = %v, want 0.045", got)
		}
	})

	// Clean up global state for other tests
	resetVisionCache()
}

// TestGetBaseName tests the GetBaseName function.
func TestGetBaseName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "simple filename",
			path: "file.txt",
			want: "file.txt",
		},
		{
			name: "relative path",
			path: "path/to/file.txt",
			want: "file.txt",
		},
		{
			name: "absolute path",
			path: "/home/user/file.txt",
			want: "file.txt",
		},
		{
			name: "trailing slash",
			path: "/path/to/directory/",
			want: "directory",
		},
		{
			name: "complex path",
			path: "/home/user/documents/work/project/file.go",
			want: "file.go",
		},
		{
			name: "dotfile",
			path: "/home/user/.bashrc",
			want: ".bashrc",
		},
		{
			name: "file with multiple extensions",
			path: "/path/to/archive.tar.gz",
			want: "archive.tar.gz",
		},
		{
			name: "just filename",
			path: "image.png",
			want: "image.png",
		},
		{
			name: "path with spaces",
			path: "/home/user/my folder/my file.txt",
			want: "my file.txt",
		},
		{
			name: "current directory",
			path: "./file.txt",
			want: "file.txt",
		},
		{
			name: "parent directory reference",
			path: "../file.txt",
			want: "file.txt",
		},
		{
			name: "Windows-style path (basic)",
			path: "C:\\Users\\file.txt",
			want: "C:\\Users\\file.txt", // filepath.Base on Unix doesn't handle backslashes as separators
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetBaseName(tt.path)
			if got != tt.want {
				t.Errorf("GetBaseName(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestImageAnalysisResponseSerialization tests JSON serialization of ImageAnalysisResponse.
func TestImageAnalysisResponseSerialization(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		resp := ImageAnalysisResponse{
			Success:         true,
			ToolInvoked:     true,
			InputResolved:   true,
			OCRAttempted:    true,
			InputType:       "local_file",
			InputPath:       "/path/to/image.png",
			ExtractedText:   "Sample text from image",
			OutputTruncated: false,
			OriginalChars:   100,
			ReturnedChars:   100,
			Analysis: &VisionAnalysis{
				ImagePath:   "/path/to/image.png",
				Description: "A sample image",
				Elements: []UIElement{
					{
						Type:        "button",
						Description: "Submit button",
						Position:    "center",
					},
				},
			},
			SupportedInput: ImageAnalysisSupported{
				RemoteURL:     true,
				LocalFile:     true,
				ImageFormats:  true,
				PDFSupport:    true,
				PDFWorkaround: "",
				MaxFileSizeMB: 20,
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("json.Marshal() failed: %v", err)
		}

		// Unmarshal back
		var decoded ImageAnalysisResponse
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() failed: %v", err)
		}

		// Verify key fields
		if !decoded.Success {
			t.Error("decoded.Success = false, want true")
		}
		if decoded.InputPath != "/path/to/image.png" {
			t.Errorf("decoded.InputPath = %v, want /path/to/image.png", decoded.InputPath)
		}
		if decoded.ExtractedText != "Sample text from image" {
			t.Errorf("decoded.ExtractedText = %v, want 'Sample text from image'", decoded.ExtractedText)
		}
		if decoded.Analysis == nil {
			t.Error("decoded.Analysis = nil, want non-nil")
		} else {
			if decoded.Analysis.Description != "A sample image" {
				t.Errorf("decoded.Analysis.Description = %v, want 'A sample image'", decoded.Analysis.Description)
			}
			if len(decoded.Analysis.Elements) != 1 {
				t.Errorf("len(decoded.Analysis.Elements) = %v, want 1", len(decoded.Analysis.Elements))
			}
		}
	})

	t.Run("error response", func(t *testing.T) {
		resp := ImageAnalysisResponse{
			Success:       false,
			ToolInvoked:   true,
			InputResolved: false,
			OCRAttempted:  false,
			InputType:     "local_file",
			InputPath:     "/path/to/missing.png",
			ErrorCode:     ErrCodeLocalFileNotFound,
			ErrorMessage:  "failed to read local file: no such file or directory",
			SupportedInput: ImageAnalysisSupported{
				RemoteURL:     true,
				LocalFile:     true,
				ImageFormats:  true,
				PDFSupport:    true,
				PDFWorkaround: "",
				MaxFileSizeMB: 20,
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("json.Marshal() failed: %v", err)
		}

		// Unmarshal back
		var decoded ImageAnalysisResponse
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() failed: %v", err)
		}

		// Verify key fields
		if decoded.Success {
			t.Error("decoded.Success = true, want false")
		}
		if decoded.ErrorCode != ErrCodeLocalFileNotFound {
			t.Errorf("decoded.ErrorCode = %v, want %v", decoded.ErrorCode, ErrCodeLocalFileNotFound)
		}
		if decoded.ErrorMessage == "" {
			t.Error("decoded.ErrorMessage is empty")
		}
	})

	t.Run("truncated response with full output path", func(t *testing.T) {
		resp := ImageAnalysisResponse{
			Success:         true,
			ToolInvoked:     true,
			InputResolved:   true,
			OCRAttempted:    true,
			InputType:       "local_file",
			InputPath:       "/path/to/large.pdf",
			ExtractedText:   "First 1000 chars...",
			OutputTruncated: true,
			OriginalChars:   25000,
			ReturnedChars:   1000,
			FullOutputPath:  "./.sprout_ocr_outputs/large_abc123_full.txt",
			SupportedInput: ImageAnalysisSupported{
				RemoteURL:     true,
				LocalFile:     true,
				ImageFormats:  true,
				PDFSupport:    true,
				PDFWorkaround: "",
				MaxFileSizeMB: 20,
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("json.Marshal() failed: %v", err)
		}

		// Unmarshal back
		var decoded ImageAnalysisResponse
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() failed: %v", err)
		}

		// Verify key fields
		if !decoded.OutputTruncated {
			t.Error("decoded.OutputTruncated = false, want true")
		}
		if decoded.OriginalChars != 25000 {
			t.Errorf("decoded.OriginalChars = %v, want 25000", decoded.OriginalChars)
		}
		if decoded.FullOutputPath == "" {
			t.Error("decoded.FullOutputPath is empty")
		}
	})
}

// TestImageAnalysisSupportedSerialization tests JSON serialization of ImageAnalysisSupported.
func TestImageAnalysisSupportedSerialization(t *testing.T) {
	supported := ImageAnalysisSupported{
		RemoteURL:     true,
		LocalFile:     true,
		ImageFormats:  true,
		PDFSupport:    true,
		PDFWorkaround: "Use PDF OCR with glm-ocr model",
		MaxFileSizeMB: 20,
	}

	// Marshal to JSON
	data, err := json.Marshal(supported)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}

	// Unmarshal back
	var decoded ImageAnalysisSupported
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}

	// Verify all fields
	if !decoded.RemoteURL {
		t.Error("decoded.RemoteURL = false, want true")
	}
	if !decoded.LocalFile {
		t.Error("decoded.LocalFile = false, want true")
	}
	if !decoded.ImageFormats {
		t.Error("decoded.ImageFormats = false, want true")
	}
	if !decoded.PDFSupport {
		t.Error("decoded.PDFSupport = false, want true")
	}
	if decoded.PDFWorkaround != "Use PDF OCR with glm-ocr model" {
		t.Errorf("decoded.PDFWorkaround = %v, want 'Use PDF OCR with glm-ocr model'", decoded.PDFWorkaround)
	}
	if decoded.MaxFileSizeMB != 20 {
		t.Errorf("decoded.MaxFileSizeMB = %v, want 20", decoded.MaxFileSizeMB)
	}
}

// TestImageAnalysisSerialization tests JSON serialization of VisionAnalysis.
func TestImageAnalysisSerialization(t *testing.T) {
	t.Run("minimal analysis", func(t *testing.T) {
		analysis := VisionAnalysis{
			ImagePath:   "/path/to/image.png",
			Description: "Simple image",
		}

		data, err := json.Marshal(analysis)
		if err != nil {
			t.Fatalf("json.Marshal() failed: %v", err)
		}

		var decoded VisionAnalysis
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() failed: %v", err)
		}

		if decoded.ImagePath != "/path/to/image.png" {
			t.Errorf("decoded.ImagePath = %v, want /path/to/image.png", decoded.ImagePath)
		}
		if decoded.Description != "Simple image" {
			t.Errorf("decoded.Description = %v, want 'Simple image'", decoded.Description)
		}
	})

	t.Run("full analysis with elements and suggestions", func(t *testing.T) {
		analysis := VisionAnalysis{
			ImagePath:   "/path/to/ui.png",
			Description: "A login form",
			Elements: []UIElement{
				{
					Type:        "input",
					Description: "Username field",
					Position:    "top",
				},
				{
					Type:        "input",
					Description: "Password field",
					Position:    "center",
				},
				{
					Type:        "button",
					Description: "Submit button",
					Position:    "bottom",
				},
			},
			Issues: []string{
				"Missing labels on input fields",
				"Low contrast colors",
			},
			Suggestions: []string{
				"Add visible labels above input fields",
				"Increase contrast for better accessibility",
			},
		}

		data, err := json.Marshal(analysis)
		if err != nil {
			t.Fatalf("json.Marshal() failed: %v", err)
		}

		var decoded VisionAnalysis
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() failed: %v", err)
		}

		if len(decoded.Elements) != 3 {
			t.Errorf("len(decoded.Elements) = %v, want 3", len(decoded.Elements))
		}
		if len(decoded.Issues) != 2 {
			t.Errorf("len(decoded.Issues) = %v, want 2", len(decoded.Issues))
		}
		if len(decoded.Suggestions) != 2 {
			t.Errorf("len(decoded.Suggestions) = %v, want 2", len(decoded.Suggestions))
		}

		// Verify specific element
		if decoded.Elements[2].Type != "button" {
			t.Errorf("decoded.Elements[2].Type = %v, want button", decoded.Elements[2].Type)
		}
	})
}

// TestUIElementSerialization tests JSON serialization of UIElement.
func TestUIElementSerialization(t *testing.T) {
	element := UIElement{
		Type:        "button",
		Description: "Primary action button",
		Position:    "top-right",
		Issues:      "Low contrast with background",
	}

	data, err := json.Marshal(element)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}

	var decoded UIElement
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}

	if decoded.Type != "button" {
		t.Errorf("decoded.Type = %v, want button", decoded.Type)
	}
	if decoded.Description != "Primary action button" {
		t.Errorf("decoded.Description = %v, want 'Primary action button'", decoded.Description)
	}
	if decoded.Position != "top-right" {
		t.Errorf("decoded.Position = %v, want top-right", decoded.Position)
	}
	if decoded.Issues != "Low contrast with background" {
		t.Errorf("decoded.Issues = %v, want 'Low contrast with background'", decoded.Issues)
	}
}
