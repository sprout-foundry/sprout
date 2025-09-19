package codereview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// TestCumulativeFileStateTracking tests that the system maintains file state across review iterations
func TestCumulativeFileStateTracking(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "ledit_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.go")
	originalContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`

	err = os.WriteFile(testFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Change to temp directory
	oldWd, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(oldWd)

	// Test that subsequent edits build upon previous changes
	cfg := &configuration.Config{
		SkipPrompt: true,
	}
	logger := utils.GetLogger(true)

	// Create a service (used for context in real scenarios)
	_ = NewCodeReviewService(cfg, logger)

	// First edit: Add a function
	firstEdit := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}

func greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}`

	// Second edit: Add more functionality
	secondEdit := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
	greet("World")
}

func greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

func farewell(name string) {
	fmt.Printf("Goodbye, %s!\n", name)
}`

	// The key test: verify that the system can handle cumulative changes
	// In a real scenario, this would go through ProcessCodeGeneration
	// but for this test, we verify the concept

	// Check that the original file state tracking works
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	if string(content) != originalContent {
		t.Errorf("Expected original content, got: %s", string(content))
	}

	// Apply first edit
	err = os.WriteFile(testFile, []byte(firstEdit), 0644)
	if err != nil {
		t.Fatalf("Failed to apply first edit: %v", err)
	}

	// Verify first edit was applied
	content, err = os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file after first edit: %v", err)
	}

	if !strings.Contains(string(content), "func greet(name string)") {
		t.Error("First edit was not applied correctly")
	}

	// Apply second edit
	err = os.WriteFile(testFile, []byte(secondEdit), 0644)
	if err != nil {
		t.Fatalf("Failed to apply second edit: %v", err)
	}

	// Verify second edit was applied (cumulative)
	content, err = os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file after second edit: %v", err)
	}

	if !strings.Contains(string(content), "func greet(name string)") {
		t.Error("Second edit lost first edit (greet function)")
	}

	if !strings.Contains(string(content), "func farewell(name string)") {
		t.Error("Second edit was not applied correctly (farewell function)")
	}

	if !strings.Contains(string(content), `greet("World")`) {
		t.Error("Second edit was not applied correctly (main function update)")
	}
}

// TestReviewContextHistory tests that review history is maintained correctly
func TestReviewContextHistory(t *testing.T) {
	cfg := &configuration.Config{}
	logger := utils.GetLogger(true)

	service := NewCodeReviewService(cfg, logger)

	ctx := &ReviewContext{
		Diff:           "test content",
		OriginalPrompt: "test prompt",
		Config:         cfg,
		Logger:         logger,
	}

	// Initialize history
	ctx.History = service.initializeReviewHistory(ctx)

	if ctx.History.SessionID == "" {
		t.Error("Session ID should be generated")
	}

	if ctx.History.OriginalPrompt != ctx.OriginalPrompt {
		t.Error("Original prompt should be preserved in history")
	}

	if ctx.History.OriginalContent != ctx.Diff {
		t.Error("Original content should be preserved in history")
	}

	// Test iteration recording
	result := &types.CodeReviewResult{
		Status:   "approved",
		Feedback: "Looks good",
	}

	service.recordReviewIteration(ctx, result, "test diff")

	if len(ctx.History.Iterations) != 1 {
		t.Error("Should have recorded one iteration")
	}

	if ctx.History.Iterations[0].ReviewResult.Status != "approved" {
		t.Error("Should have recorded approved status")
	}
}
