package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildContextIgnoresLeditDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Change to the temp directory since BuildBaseContextJSON uses detectRepoRoot()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	err := os.Chdir(tempDir)
	require.NoError(t, err)

	// Create .ledit directory with some files
	leditDir := filepath.Join(tempDir, ".ledit")
	err = os.MkdirAll(leditDir, 0755)
	require.NoError(t, err)

	// Create some files in .ledit directory
	leditFiles := []string{
		"config.json",
		"api_keys.json",
		"changelog.md",
		"session.log",
	}

	for _, file := range leditFiles {
		filePath := filepath.Join(leditDir, file)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	// Create a legitimate project file to detect project type
	err = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module test"), 0644)
	require.NoError(t, err)

	// Create a main.go file
	err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644)
	require.NoError(t, err)

	// Test BuildBaseContextJSON
	result := BuildBaseContextJSON()
	require.NotEmpty(t, result)

	// Critical: Should NOT contain any .ledit files - this is the main regression test
	assert.NotContains(t, result, ".ledit", "Should not contain .ledit directory")
	assert.NotContains(t, result, "config.json", "Should not contain .ledit/config.json")
	assert.NotContains(t, result, "api_keys.json", "Should not contain .ledit/api_keys.json")
	assert.NotContains(t, result, "changelog.md", "Should not contain .ledit/changelog.md")
	assert.NotContains(t, result, "session.log", "Should not contain .ledit/session.log")

	// The most important test: ensure .ledit files are not exposed in the context
	// This prevents leaking sensitive configuration and API keys
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, ".ledit") {
			t.Errorf("Found .ledit reference in context: %s", line)
		}
	}
}

func TestBuildContextIgnoresLeditDirectoryComprehensive(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Change to the temp directory since BuildBaseContextJSON uses detectRepoRoot()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	err := os.Chdir(tempDir)
	require.NoError(t, err)

	// Create .ledit directory with some files
	leditDir := filepath.Join(tempDir, ".ledit")
	err = os.MkdirAll(leditDir, 0755)
	require.NoError(t, err)

	// Create some files in .ledit directory
	leditFiles := []string{
		"config.json",
		"api_keys.json",
		"changelog.md",
		"session.log",
	}

	for _, file := range leditFiles {
		filePath := filepath.Join(leditDir, file)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	// Create some legitimate project files (only root level files will be scanned)
	projectFiles := []string{
		"main.go",
		"README.md",
		"go.mod",
	}

	for _, file := range projectFiles {
		filePath := filepath.Join(tempDir, file)
		err := os.WriteFile(filePath, []byte("package main"), 0644)
		require.NoError(t, err)
	}

	// Create some other files that should be ignored
	ignoreFiles := []string{
		".DS_Store",
		"node_modules/package.json",
		"vendor/github.com/lib/lib.go",
		"build/binary",
	}

	for _, file := range ignoreFiles {
		filePath := filepath.Join(tempDir, file)
		err = os.MkdirAll(filepath.Dir(filePath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(filePath, []byte("ignore me"), 0644)
		require.NoError(t, err)
	}

	// Test BuildBaseContextJSON
	result := BuildBaseContextJSON()
	require.NotEmpty(t, result)

	// Critical: Should NOT contain any .ledit files - this is the main security test
	assert.NotContains(t, result, ".ledit", "Should not contain .ledit directory")
	assert.NotContains(t, result, "config.json", "Should not contain .ledit/config.json")
	assert.NotContains(t, result, "api_keys.json", "Should not contain .ledit/api_keys.json")
	assert.NotContains(t, result, "changelog.md", "Should not contain .ledit/changelog.md")
	assert.NotContains(t, result, "session.log", "Should not contain .ledit/session.log")

	// Should also not contain other ignored files
	assert.NotContains(t, result, ".DS_Store", "Should not contain .DS_Store")
	assert.NotContains(t, result, "node_modules", "Should not contain node_modules")
	assert.NotContains(t, result, "vendor", "Should not contain vendor")
	assert.NotContains(t, result, "build", "Should not contain build")
}

func TestBuildContextHandlesEmptyDirectory(t *testing.T) {
	// Create an empty temporary directory
	tempDir := t.TempDir()

	// Change to the temp directory since BuildBaseContextJSON uses detectRepoRoot()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	err := os.Chdir(tempDir)
	require.NoError(t, err)

	result := BuildBaseContextJSON()

	// Should return valid JSON even for empty directory
	assert.NotEmpty(t, result)

	// Should not contain any .ledit references
	assert.NotContains(t, result, ".ledit")
}

func TestBuildContextRespectsGitignore(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Change to the temp directory since BuildBaseContextJSON uses detectRepoRoot()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	err := os.Chdir(tempDir)
	require.NoError(t, err)

	// Create a .gitignore file with custom patterns
	gitignoreContent := `# Custom ignore patterns
*.log
temp/
secrets.txt
.ledit/
`
	err = os.WriteFile(filepath.Join(tempDir, ".gitignore"), []byte(gitignoreContent), 0644)
	require.NoError(t, err)

	// Create files that should be ignored due to .gitignore
	ignoredFiles := []string{
		"debug.log",
		"temp/cache.tmp",
		"secrets.txt",
	}

	for _, file := range ignoredFiles {
		filePath := filepath.Join(tempDir, file)
		err = os.MkdirAll(filepath.Dir(filePath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(filePath, []byte("ignore me"), 0644)
		require.NoError(t, err)
	}

	// Create files that should be included
	includedFiles := []string{
		"main.go",
		"README.md",
		"debug.go", // .go files should not be ignored by *.log pattern
	}

	for _, file := range includedFiles {
		filePath := filepath.Join(tempDir, file)
		err = os.WriteFile(filePath, []byte("include me"), 0644)
		require.NoError(t, err)
	}

	result := BuildBaseContextJSON()

	// Should NOT include ignored files - this is the critical security test
	assert.NotContains(t, result, "debug.log")
	assert.NotContains(t, result, "temp/")
	assert.NotContains(t, result, "secrets.txt")
	assert.NotContains(t, result, ".ledit")
}
