package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetIgnoreRulesAlwaysIncludesLeditPatterns(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "ledit-ignore-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test that even without any ignore files, essential ledit patterns are included
	rules := GetIgnoreRules(tempDir)

	// Check that .ledit directory is always ignored
	if !rules.MatchesPath(".ledit/workspace.json") {
		t.Error(".ledit/workspace.json should always be ignored")
	}

	if !rules.MatchesPath(".ledit/config.json") {
		t.Error(".ledit/config.json should always be ignored")
	}

	if !rules.MatchesPath("ledit") {
		t.Error("ledit binary should always be ignored")
	}

	// Check that common files are also ignored
	if !rules.MatchesPath("node_modules/package.json") {
		t.Error("node_modules should be ignored by fallback patterns")
	}

	if !rules.MatchesPath("build/output.exe") {
		t.Error("build directory should be ignored by fallback patterns")
	}
}

func TestGetIgnoreRulesCombinesGitignoreAndLeditignore(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "ledit-ignore-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a .gitignore file
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	gitignoreContent := "*.log\nbuild/\n"
	err = os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write .gitignore: %v", err)
	}

	// Create a .ledit/leditignore file
	leditDir := filepath.Join(tempDir, ".ledit")
	err = os.MkdirAll(leditDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create .ledit directory: %v", err)
	}

	leditignorePath := filepath.Join(leditDir, "leditignore")
	leditignoreContent := "temp/\ncache/\n"
	err = os.WriteFile(leditignorePath, []byte(leditignoreContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write .leditignore: %v", err)
	}

	// Get ignore rules
	rules := GetIgnoreRules(tempDir)

	// Check that .gitignore patterns are respected
	if !rules.MatchesPath("app.log") {
		t.Error("*.log from .gitignore should be ignored")
	}

	if !rules.MatchesPath("build/output.exe") {
		t.Error("build/ from .gitignore should be ignored")
	}

	// Check that .leditignore patterns are respected
	if !rules.MatchesPath("temp/file.txt") {
		t.Error("temp/ from .leditignore should be ignored")
	}

	if !rules.MatchesPath("cache/data.bin") {
		t.Error("cache/ from .leditignore should be ignored")
	}

	// Check that essential ledit patterns are still included
	if !rules.MatchesPath(".ledit/workspace.json") {
		t.Error(".ledit/workspace.json should always be ignored")
	}

	// Check that fallback patterns are still included
	if !rules.MatchesPath("node_modules/package.json") {
		t.Error("node_modules should be ignored by fallback patterns")
	}
}

func TestGetIgnoreRulesHandlesEmptyAndCommentLines(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "ledit-ignore-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a .gitignore file with empty lines and comments
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	gitignoreContent := "# This is a comment\n\n*.log\n\nbuild/\n# Another comment\n"
	err = os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write .gitignore: %v", err)
	}

	// Get ignore rules
	rules := GetIgnoreRules(tempDir)

	// Check that patterns are still respected despite empty lines and comments
	if !rules.MatchesPath("app.log") {
		t.Error("*.log should be ignored despite empty lines and comments")
	}

	if !rules.MatchesPath("build/output.exe") {
		t.Error("build/ should be ignored despite empty lines and comments")
	}
}

func TestEssentialLeditPatterns(t *testing.T) {
	patterns := getEssentialLeditPatterns()

	// Check that essential patterns are present
	essentialPatterns := []string{
		".ledit/",
		".ledit/*",
		"ledit",
		"testing/",
		"test_results.txt",
		"e2e_results.csv",
	}

	for _, pattern := range essentialPatterns {
		found := false
		for _, p := range patterns {
			if p == pattern {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Essential pattern '%s' not found in getEssentialLeditPatterns()", pattern)
		}
	}
}
