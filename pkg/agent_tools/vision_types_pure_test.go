package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ============================================================================
// GetFileExtension tests
// ============================================================================

func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"simple file", "file.txt", ".txt"},
		{"path with extension", "/home/user/file.go", ".go"},
		{"uppercase extension", "/path/File.TXT", ".txt"},
		{"mixed case extension", "/path/File.Go", ".go"},
		{"no extension", "file", ""},
		{"hidden file", ".bashrc", ".bashrc"},
		{"multiple dots", "archive.tar.gz", ".gz"},
		{"complex path", "/home/user/docs/project/v2/file.html", ".html"},
		{"windows path", "C:\\Users\\file.txt", ".txt"},
		{"just dot", ".", "."},
		{"dot directory", "./test", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFileExtension(tt.path)
			if got != tt.expected {
				t.Errorf("GetFileExtension(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// IsHTMLInput tests
// ============================================================================

func TestIsHTMLInput_LocalFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"html extension", "/path/to/file.html", true},
		{"htm extension", "/path/to/file.htm", true},
		{"uppercase HTML", "/path/to/file.HTML", true},
		{"uppercase HTM", "/path/to/file.HTM", true},
		{"mixed case Html", "/path/to/file.Html", true},
		{"no extension", "file", false},
		{"jpg extension", "image.jpg", false},
		{"png extension", "screenshot.png", false},
		{"go file", "main.go", false},
		{"dotfile", ".bashrc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHTMLInput(tt.path)
			if got != tt.expected {
				t.Errorf("IsHTMLInput(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// detectImageMimeType tests
// ============================================================================

func TestDetectImageMimeType(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"png", "image.png", "image/png"},
		{"PNG uppercase", "image.PNG", "image/png"},
		{"jpg", "photo.jpg", "image/jpeg"},
		{"jpeg", "photo.jpeg", "image/jpeg"},
		{"JPEG uppercase", "photo.JPEG", "image/jpeg"},
		{"gif", "animation.gif", "image/gif"},
		{"webp", "photo.webp", "image/webp"},
		{"avif", "photo.avif", "image/avif"},
		{"bmp", "photo.bmp", "image/bmp"},
		{"svg", "icon.svg", "image/svg+xml"},
		{"unknown extension", "photo.xyz", "image/png"},
		{"no extension", "photo", "image/png"},
		{"path with png", "/home/user/screenshot.png", "image/png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectImageMimeType(tt.path)
			if got != tt.expected {
				t.Errorf("detectImageMimeType(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// EnsureOllamaModelTag tests
// ============================================================================

func TestEnsureOllamaModelTag(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"model without tag", "glm-ocr", "glm-ocr:latest"},
		{"model with tag", "glm-ocr:1.0", "glm-ocr:1.0"},
		{"model with latest tag", "glm-ocr:latest", "glm-ocr:latest"},
		{"model with spaces", "  glm-ocr  ", "glm-ocr:latest"},
		{"model with trailing colon", "glm-ocr:", "glm-ocr:"}, // already has colon
		{"complex model name", "bfl/llava-phi-3-mini", "bfl/llava-phi-3-mini:latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnsureOllamaModelTag(tt.model)
			if got != tt.expected {
				t.Errorf("EnsureOllamaModelTag(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// GetDefaultModelForProvider tests
// ============================================================================

func TestGetDefaultModelForProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider api.ClientType
		expected string
	}{
		{"DeepInfra", api.DeepInfraClientType, "meta-llama/Llama-3.3-70B-Instruct"},
		{"OpenRouter", api.OpenRouterClientType, "openai/gpt-5"},
		{"Mistral", api.MistralClientType, "devstral-2512"},
		{"DeepSeek", api.DeepSeekClientType, "deepseek-ai/DeepSeek-V3"},
		{"ZAI", api.ZAIClientType, "glm-4.6"},
		{"LMStudio", api.LMStudioClientType, ""},
		{"Chutes", api.ChutesClientType, ""},
		{"Unknown provider", api.ClientType("unknown"), ""},
		{"Ollama (not in switch)", api.OllamaClientType, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetDefaultModelForProvider(tt.provider)
			if got != tt.expected {
				t.Errorf("GetDefaultModelForProvider(%q) = %q, want %q", tt.provider, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// resolveVisionOutputDirectory tests
// ============================================================================

func TestResolveVisionOutputDirectory(t *testing.T) {
	// Save and restore cwd
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })

	t.Run("default directory", func(t *testing.T) {
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "")

		got := resolveVisionOutputDirectory()
		if !strings.HasSuffix(got, ".sprout_ocr_outputs") {
			t.Errorf("expected directory to end with '.sprout_ocr_outputs', got %q", got)
		}
	})

	t.Run("custom directory from env", func(t *testing.T) {
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "")
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "captures")

		got := resolveVisionOutputDirectory()
		// The directory should contain "captures" in the path
		if !strings.Contains(got, "captures") {
			t.Errorf("expected directory to contain 'captures', got %q", got)
		}
	})

	t.Run("absolute path in env is cleaned", func(t *testing.T) {
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "")
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "/absolute/custom/path")

		got := resolveVisionOutputDirectory()
		// Should strip the leading / and join with cwd
		if !strings.Contains(got, "custom") {
			t.Errorf("expected directory to contain 'custom', got %q", got)
		}
	})
}

// ============================================================================
// resolveVisionOutputDirectoryWithRoot tests
// ============================================================================

func TestResolveVisionOutputDirectoryWithRoot(t *testing.T) {
	t.Run("with workspace root", func(t *testing.T) {
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "")

		dir := t.TempDir()
		got := resolveVisionOutputDirectoryWithRoot(dir)

		expected := filepath.Join(dir, ".sprout_ocr_outputs")
		if got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("with custom env and workspace root", func(t *testing.T) {
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "")
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "my_outputs")

		dir := t.TempDir()
		got := resolveVisionOutputDirectoryWithRoot(dir)

		expected := filepath.Join(dir, "my_outputs")
		if got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("empty workspace root falls back to cwd", func(t *testing.T) {
		t.Setenv("SPROUT_RESOURCE_DIRECTORY", "")

		got := resolveVisionOutputDirectoryWithRoot("")

		if got == "" {
			t.Error("expected non-empty directory path")
		}
	})
}

// ============================================================================
// sanitizeVisionFileComponent tests
// ============================================================================

func TestSanitizeVisionFileComponent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple name", "hello.txt", "hello_txt"},
		{"already lowercase", "hello", "hello"},
		{"uppercase converted", "HELLO", "hello"},
		{"mixed case", "HelloWorld", "helloworld"},
		{"special chars replaced", "hello world!", "hello_world"},
		{"numbers preserved", "file123", "file123"},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"long name truncated", strings.Repeat("a", 100), strings.Repeat("a", 64)},
		{"dots replaced", "file.name.test", "file_name_test"},
		{"slashes replaced", "dir/file", "dir_file"},
		{"spaces replaced", "my file name", "my_file_name"},
		{"leading/trailing underscores trimmed", "  hello world  ", "hello_world"},
		{"only special chars", "!@#$%", ""},
		{"url-like input", "https://example.com/menu.pdf", "https___example_com_menu_pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeVisionFileComponent(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeVisionFileComponent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
