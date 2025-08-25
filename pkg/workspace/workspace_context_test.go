package workspace

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
)

// TestGetProgressiveWorkspaceContext tests the progressive context loading
func TestGetProgressiveWorkspaceContext(t *testing.T) {
	cfg := &config.Config{SkipPrompt: true}
	
	// Test with monorepo intent
	monorepoIntent := "Create a monorepo with backend and frontend"
	context := GetProgressiveWorkspaceContext(monorepoIntent, cfg)
	
	if context == "" {
		t.Error("Expected non-empty context")
	}
	
	// Progressive context should provide useful information either from workspace or intent-based
	if !strings.Contains(context, "backend") && !strings.Contains(context, "Project") {
		t.Errorf("Context should contain either monorepo suggestions or project info, got: %s", context)
	}
}

// TestGetDirectoryStructureContext tests directory structure context generation
func TestGetDirectoryStructureContext(t *testing.T) {
	// Create a temporary test directory
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	
	os.Chdir(tempDir)
	
	// Create test structure
	os.Mkdir("src", 0755)
	os.Mkdir("tests", 0755)
	os.Mkdir(".git", 0755) // Should be ignored
	os.WriteFile("main.go", []byte("package main"), 0644)
	os.WriteFile("README.md", []byte("# Test"), 0644)
	os.WriteFile(".gitignore", []byte("*.log"), 0644) // Should be ignored
	
	// Create nested structure
	os.Mkdir("src/handlers", 0755)
	os.WriteFile("src/handlers/user.go", []byte("package handlers"), 0644)
	
	context := getDirectoryStructureContext(".")
	
	if context == "" {
		t.Error("Expected non-empty directory context")
	}
	
	// Should include visible directories and files
	if !strings.Contains(context, "src/") {
		t.Error("Context should contain src directory")
	}
	
	if !strings.Contains(context, "main.go") {
		t.Error("Context should contain main.go file")
	}
	
	// Should exclude hidden files
	if strings.Contains(context, ".git") || strings.Contains(context, ".gitignore") {
		t.Error("Context should not contain hidden files")
	}
	
	// Should include nested structure (up to depth limit)
	if !strings.Contains(context, "handlers") {
		t.Error("Context should contain nested directories")
	}
}

// TestGenerateContextFromIntent tests intent-based context generation
func TestGenerateContextFromIntent(t *testing.T) {
	tests := []struct {
		intent           string
		expectedContains []string
		notContains      []string
	}{
		{
			intent:           "Create a monorepo with backend and frontend",
			expectedContains: []string{"backend/", "frontend/", "monorepo"},
			notContains:      []string{"django", "spring"},
		},
		{
			intent:           "Setup React application with Vite",
			expectedContains: []string{"package.json", "vite.config.js", "React"},
			notContains:      []string{"go.mod", "cargo.toml"},
		},
		{
			intent:           "Build Go backend with Echo framework",
			expectedContains: []string{"go.mod", "main.go", "Go backend"},
			notContains:      []string{"package.json", "requirements.txt"},
		},
		{
			intent:           "Setup SQLite database with migrations",
			expectedContains: []string{"migrations/", "database", "db config"},
			notContains:      []string{"redis", "mongodb"},
		},
		{
			intent:           "Simple file update task",
			expectedContains: []string{"Empty workspace"},
			notContains:      []string{"backend/", "frontend/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			context := generateContextFromIntent(tt.intent)
			
			for _, expected := range tt.expectedContains {
				if !strings.Contains(context, expected) {
					t.Errorf("Context should contain '%s', got: %s", expected, context)
				}
			}
			
			for _, notExpected := range tt.notContains {
				if strings.Contains(context, notExpected) {
					t.Errorf("Context should not contain '%s', got: %s", notExpected, context)
				}
			}
		})
	}
}

// TestContextFallbackChain tests the progressive fallback chain
func TestContextFallbackChain(t *testing.T) {
	cfg := &config.Config{SkipPrompt: true}
	
	// Test with intent that triggers fallback
	intent := "Create a new project with multiple components"
	context := GetProgressiveWorkspaceContext(intent, cfg)
	
	// Should not be empty even if minimal context fails
	if context == "" {
		t.Error("Progressive context should never return empty string")
	}
	
	// Should contain some useful information
	if len(context) < 10 {
		t.Error("Context should contain meaningful information")
	}
}

// TestEmptyWorkspaceHandling tests handling of completely empty workspaces
func TestEmptyWorkspaceHandling(t *testing.T) {
	// Create completely empty temporary directory
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	
	os.Chdir(tempDir)
	
	cfg := &config.Config{SkipPrompt: true}
	
	// Test directory structure context with empty directory - should return empty for truly empty directory
	
	// Test progressive context falls back gracefully
	context := GetProgressiveWorkspaceContext("setup project", cfg)
	if context == "" {
		t.Error("Progressive context should provide fallback for empty workspace")
	}
}

// TestContextDepthLimit tests that directory traversal respects depth limits
func TestContextDepthLimit(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	
	os.Chdir(tempDir)
	
	// Create deep nested structure
	os.MkdirAll("level1/level2/level3/level4", 0755)
	os.WriteFile("level1/level2/level3/level4/deep.go", []byte("package deep"), 0644)
	
	context := getDirectoryStructureContext(".")
	
	// Should not include very deep files due to depth limit
	if strings.Contains(context, "level4") || strings.Contains(context, "deep.go") {
		t.Error("Context should respect depth limit and not include deeply nested files")
	}
	
	// Should include reasonable depth
	if !strings.Contains(context, "level1") {
		t.Error("Context should include first level directories")
	}
}

// TestContextSize tests that generated context is reasonably sized
func TestContextSize(t *testing.T) {
	cfg := &config.Config{SkipPrompt: true}
	
	// Test with various intents
	intents := []string{
		"Create monorepo",
		"Setup React app",
		"Build Go API",
		"Create database schema",
		"Simple task",
	}
	
	for _, intent := range intents {
		context := GetProgressiveWorkspaceContext(intent, cfg)
		
		// Context should be reasonable size (not too large, not too small)
		if len(context) < 5 {
			t.Errorf("Context too small for intent '%s': %d chars", intent, len(context))
		}
		
		if len(context) > 2000 {
			t.Errorf("Context too large for intent '%s': %d chars", intent, len(context))
		}
	}
}

// BenchmarkProgressiveContextLoading benchmarks the context loading performance
func BenchmarkProgressiveContextLoading(b *testing.B) {
	cfg := &config.Config{SkipPrompt: true}
	intent := "Create a monorepo with backend and frontend using Go and React"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetProgressiveWorkspaceContext(intent, cfg)
	}
}

// BenchmarkDirectoryStructureContext benchmarks directory traversal
func BenchmarkDirectoryStructureContext(b *testing.B) {
	tempDir := b.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	
	os.Chdir(tempDir)
	
	// Create moderate directory structure
	for i := 0; i < 10; i++ {
		dirname := fmt.Sprintf("dir%d", i)
		os.Mkdir(dirname, 0755)
		for j := 0; j < 5; j++ {
			filename := fmt.Sprintf("%s/file%d.go", dirname, j)
			os.WriteFile(filename, []byte("package main"), 0644)
		}
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		getDirectoryStructureContext(".")
	}
}

// TestIntegration_ProgressiveContextFlow tests the complete progressive context flow
func TestIntegration_ProgressiveContextFlow(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	
	os.Chdir(tempDir)
	cfg := &config.Config{SkipPrompt: true}
	
	// Test 1: Empty workspace should use intent-based context
	emptyContext := GetProgressiveWorkspaceContext("create monorepo", cfg)
	if !strings.Contains(emptyContext, "backend/") {
		t.Error("Empty workspace should generate intent-based context")
	}
	
	// Test 2: Add some files, should use directory context
	os.Mkdir("src", 0755)
	os.WriteFile("main.go", []byte("package main"), 0644)
	
	dirContext := GetProgressiveWorkspaceContext("update project", cfg)
	if !strings.Contains(dirContext, "main.go") {
		t.Error("Should use directory context when files present")
	}
	
	// Test 3: Different intents should generate appropriate context
	reactContext := GetProgressiveWorkspaceContext("setup React frontend", cfg)
	if !strings.Contains(reactContext, "package.json") && !strings.Contains(reactContext, "main.go") {
		t.Error("Should provide relevant context for React setup")
	}
}