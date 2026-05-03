package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewThemeManager(t *testing.T) {
	tm := NewThemeManager()
	if tm == nil {
		t.Fatal("NewThemeManager() returned nil")
	}
}

func TestThemeManagerDefaultColors(t *testing.T) {
	tm := NewThemeManager()

	tests := []struct {
		name     string
		expected string
	}{
		{"success", "green"},
		{"warning", "yellow"},
		{"error", "red"},
		{"info", "blue"},
		{"primary", "cyan"},
		{"secondary", "magenta"},
		{"accent", "white"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tm.GetColor(tt.name)
			if got != tt.expected {
				t.Errorf("GetColor(%q) = %q, want %q", tt.name, got, tt.expected)
			}
		})
	}
}

func TestThemeManagerGetColorUnknown(t *testing.T) {
	tm := NewThemeManager()
	got := tm.GetColor("unknown")
	if got != "white" {
		t.Errorf("GetColor(\"unknown\") = %q, want %q", got, "white")
	}
}

func TestThemeManagerGetTheme(t *testing.T) {
	tm := NewThemeManager()
	theme := tm.GetTheme()

	if theme.Name != "default" {
		t.Errorf("GetTheme().Name = %q, want %q", theme.Name, "default")
	}
	if theme.Description != "Default theme" {
		t.Errorf("GetTheme().Description = %q, want %q", theme.Description, "Default theme")
	}
	if theme.Colors.Success != "green" {
		t.Errorf("GetTheme().Colors.Success = %q, want %q", theme.Colors.Success, "green")
	}
}

func TestThemeManagerLoadThemeFromFileNonexistent(t *testing.T) {
	tm := NewThemeManager()
	err := tm.LoadThemeFromFile("/nonexistent/path/theme.json")
	if err != nil {
		t.Errorf("LoadThemeFromFile(nonexistent) = %v, want nil", err)
	}
	// Should fall back to default theme
	theme := tm.GetTheme()
	if theme.Name != "default" {
		t.Errorf("After nonexistent file, Name = %q, want %q", theme.Name, "default")
	}
}

func TestThemeManagerLoadThemeFromFileInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.json")
	err := os.WriteFile(path, []byte("{invalid json}"), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	tm := NewThemeManager()
	err = tm.LoadThemeFromFile(path)
	if err == nil {
		t.Error("LoadThemeFromFile(invalid JSON) = nil, want error")
	}
}

func TestThemeManagerLoadThemeFromFileValid(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "custom.json")
	content := `{
		"name": "custom",
		"description": "Custom theme",
		"colors": {
			"success": "orange",
			"warning": "purple",
			"error": "pink",
			"info": "gray",
			"primary": "lime",
			"secondary": "teal",
			"accent": "navy"
		}
	}`
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	tm := NewThemeManager()
	err = tm.LoadThemeFromFile(path)
	if err != nil {
		t.Fatalf("LoadThemeFromFile(valid) = %v, want nil", err)
	}

	theme := tm.GetTheme()
	if theme.Name != "custom" {
		t.Errorf("Name = %q, want %q", theme.Name, "custom")
	}
	if theme.Description != "Custom theme" {
		t.Errorf("Description = %q, want %q", theme.Description, "Custom theme")
	}
	if tm.GetColor("success") != "orange" {
		t.Errorf("GetColor(\"success\") = %q, want %q", tm.GetColor("success"), "orange")
	}
	if tm.GetColor("warning") != "purple" {
		t.Errorf("GetColor(\"warning\") = %q, want %q", tm.GetColor("warning"), "purple")
	}
}
