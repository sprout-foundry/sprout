package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =====================================================================
// InitCommand Tests
// =====================================================================

func TestInitCommand_Name(t *testing.T) {
	i := &InitCommand{}
	assert.Equal(t, "init", i.Name())
}

func TestInitCommand_Description(t *testing.T) {
	i := &InitCommand{}
	assert.Equal(t, "Generate or improve AGENTS.md with intelligent codebase analysis", i.Description())
}

func TestInitCommand_DiscoverExistingContextFiles(t *testing.T) {
	i := &InitCommand{}

	t.Run("with AGENTS.md and README.md present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		os.WriteFile("AGENTS.md", []byte("test"), 0644)
		os.WriteFile("README.md", []byte("test"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 2)
		assert.Contains(t, result, "AGENTS.md")
		assert.Contains(t, result, "README.md")
	})

	t.Run("with no expected files", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		result := i.discoverExistingContextFiles()
		assert.Empty(t, result)
	})

	t.Run("with .cursorrules present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		os.WriteFile(".cursorrules", []byte("rules"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 1)
		assert.Contains(t, result, ".cursorrules")
	})

	t.Run("with CLAUDE.md present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		os.WriteFile("CLAUDE.md", []byte("claude"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 1)
		assert.Contains(t, result, "CLAUDE.md")
	})

	t.Run("with .claude/project.md present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		err := os.MkdirAll(".claude", 0755)
		assert.NoError(t, err)
		os.WriteFile(".claude/project.md", []byte("project"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 1)
		assert.Contains(t, result, ".claude/project.md")
	})

	t.Run("with .cursor/rules present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		err := os.MkdirAll(".cursor", 0755)
		assert.NoError(t, err)
		os.WriteFile(".cursor/rules", []byte("cursor rules"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 1)
		assert.Contains(t, result, ".cursor/rules")
	})

	t.Run("with .github/copilot-instructions.md present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		err := os.MkdirAll(".github", 0755)
		assert.NoError(t, err)
		os.WriteFile(".github/copilot-instructions.md", []byte("copilot"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 1)
		assert.Contains(t, result, ".github/copilot-instructions.md")
	})

	t.Run("with all context files present", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		os.WriteFile("AGENTS.md", []byte("agents"), 0644)
		os.WriteFile("CLAUDE.md", []byte("claude"), 0644)
		os.WriteFile("README.md", []byte("readme"), 0644)

		err := os.MkdirAll(".claude", 0755)
		assert.NoError(t, err)
		os.WriteFile(".claude/project.md", []byte("project"), 0644)

		err = os.MkdirAll(".cursor", 0755)
		assert.NoError(t, err)
		os.WriteFile(".cursor/rules", []byte("cursor"), 0644)

		os.WriteFile(".cursorrules", []byte("cursorrules"), 0644)

		err = os.MkdirAll(".github", 0755)
		assert.NoError(t, err)
		os.WriteFile(".github/copilot-instructions.md", []byte("copilot"), 0644)

		result := i.discoverExistingContextFiles()
		assert.Len(t, result, 7)
	})
}

func TestInitCommand_BuildInitPrompt_EmptyExistingFiles(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt(nil)

	assert.Contains(t, prompt, "Analyze this codebase and create or improve the AGENTS.md file")
	assert.Contains(t, prompt, "Build, Test, and Development Commands")
	assert.Contains(t, prompt, "High-Level Architecture")
	assert.Contains(t, prompt, "Project-Specific Conventions")
	assert.Contains(t, prompt, "Be concise")

	// Should NOT have "Existing Context Files" section when no files exist
	assert.NotContains(t, prompt, "Existing Context Files")

	// Should have "create a new AGENTS.md" path (no AGENTS.md exists)
	assert.Contains(t, prompt, "create a new AGENTS.md file")
	assert.Contains(t, prompt, "Explore the codebase to understand")
}

func TestInitCommand_BuildInitPrompt_WithAgentsMd(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"AGENTS.md"})

	assert.Contains(t, prompt, "Existing Context Files")
	assert.Contains(t, prompt, "`AGENTS.md`")

	// Should have improvement-focused task (AGENTS.md exists)
	assert.Contains(t, prompt, "suggest improvements")
	assert.Contains(t, prompt, "Read the existing AGENTS.md")
	assert.Contains(t, prompt, "Outdated information")
	assert.Contains(t, prompt, "Update AGENTS.md with your improvements")

	// Should NOT have "create a new AGENTS.md"
	assert.NotContains(t, prompt, "create a new AGENTS.md file")
}

func TestInitCommand_BuildInitPrompt_WithMultipleContextFiles(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"README.md", "CLAUDE.md"})

	assert.Contains(t, prompt, "Existing Context Files")
	assert.Contains(t, prompt, "`README.md`")
	assert.Contains(t, prompt, "`CLAUDE.md`")

	// Should have "create a new AGENTS.md" path (no AGENTS.md in list)
	assert.Contains(t, prompt, "create a new AGENTS.md file")
	assert.Contains(t, prompt, "Explore the codebase to understand")

	// Should NOT have improvement-focused task (no AGENTS.md exists)
	assert.NotContains(t, prompt, "suggest improvements")
}

func TestInitCommand_BuildInitPrompt_WithAgentsMdAndOthers(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"AGENTS.md", "README.md", "CLAUDE.md"})

	assert.Contains(t, prompt, "Existing Context Files")
	assert.Contains(t, prompt, "`AGENTS.md`")
	assert.Contains(t, prompt, "`README.md`")
	assert.Contains(t, prompt, "`CLAUDE.md`")

	// Should have improvement-focused task (AGENTS.md exists)
	assert.Contains(t, prompt, "suggest improvements")

	// Should NOT have "create a new AGENTS.md"
	assert.NotContains(t, prompt, "create a new AGENTS.md file")
}

func TestInitCommand_BuildInitPrompt_OutputSection(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"README.md"})

	assert.Contains(t, prompt, "## Output")
	assert.Contains(t, prompt, "Write the final AGENTS.md file directly using the write_file tool")
	assert.Contains(t, prompt, "Start by reading key files to understand the project, then write AGENTS.md.")
}

func TestInitCommand_BuildInitPrompt_NoExistingFiles(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{})

	// Should NOT have "Existing Context Files" section
	assert.NotContains(t, prompt, "Existing Context Files")

	// Should have "create a new AGENTS.md" path
	assert.Contains(t, prompt, "create a new AGENTS.md file")
}

func TestInitCommand_BuildInitPrompt_AgentsMdOnly(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"AGENTS.md"})

	// Should have both Existing Context Files and the improvement task
	assert.Contains(t, prompt, "Existing Context Files")
	assert.Contains(t, prompt, "suggest improvements")
	assert.Contains(t, prompt, "Update AGENTS.md with your improvements")
}

func TestInitCommand_BuildInitPrompt_OutputSection_NoAgentsMd(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"README.md"})

	// Verify output section mentions write_file tool
	assert.Contains(t, prompt, "write_file tool")
	assert.Contains(t, prompt, "Do NOT show me the content")
}

func TestInitCommand_BuildInitPrompt_OutputSection_HasAgentsMd(t *testing.T) {
	i := &InitCommand{}

	prompt := i.buildInitPrompt([]string{"AGENTS.md"})

	assert.Contains(t, prompt, "## Output")
	assert.Contains(t, prompt, "Write the final AGENTS.md file directly using the write_file tool")
}

// =====================================================================
// detectProjectType Tests
// =====================================================================

func TestDetectProjectType(t *testing.T) {
	tests := []struct {
		name     string
		marker   string
		expected string
	}{
		{"go.mod", "go.mod", "Go project"},
		{"package.json", "package.json", "Node.js project"},
		{"requirements.txt", "requirements.txt", "Python project"},
		{"setup.py", "setup.py", "Python project"},
		{"pyproject.toml", "pyproject.toml", "Python project"},
		{"Cargo.toml", "Cargo.toml", "Rust project"},
		{"Gemfile", "Gemfile", "Ruby project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			// Create the marker file
			os.WriteFile(tt.marker, []byte("test content"), 0644)

			result := detectProjectType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectProjectType_NoMarker(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	result := detectProjectType()
	assert.Equal(t, "", result)
}

func TestDetectProjectType_Priority(t *testing.T) {
	// go.mod should be checked first; if it exists, others are ignored
	dir := t.TempDir()
	t.Chdir(dir)

	os.WriteFile("go.mod", []byte("module test"), 0644)
	os.WriteFile("package.json", []byte("{}"), 0644)
	os.WriteFile("requirements.txt", []byte("flask"), 0644)

	result := detectProjectType()
	assert.Equal(t, "Go project", result)
}

// =====================================================================
// extractKeyCommentsFromDiff Tests
// =====================================================================

func TestExtractKeyCommentsFromDiff(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want string
	}{
		{
			name: "empty diff",
			diff: "",
			want: "",
		},
		{
			name: "diff with TODO comment",
			diff: `diff --git a/main.go b/main.go
index abc123..def456 100644
--- a/main.go
+++ b/main.go
 package main
+// TODO: implement caching
 func main() {}
`,
			want: "- main.go: // TODO: implement caching",
		},
		{
			name: "diff with FIXME comment",
			diff: `diff --git a/utils.go b/utils.go
--- a/utils.go
+++ b/utils.go
+// FIXME: this is a temporary workaround
 func Process() {}
`,
			want: "- utils.go: // FIXME: this is a temporary workaround",
		},
		{
			name: "diff with plain non-important comment",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
+// do something
 func main() {}
`,
			want: "",
		},
		{
			name: "diff with no comment lines",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
 package main
 func main() {}
`,
			want: "",
		},
		{
			name: "hash-style comments",
			diff: `diff --git a/script.py b/script.py
--- a/script.py
+++ b/script.py
+# TODO: implement this
 def run():
`,
			want: "- script.py: # TODO: implement this",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractKeyCommentsFromDiff(tt.diff)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestExtractKeyCommentsFromDiff_CappedAt10(t *testing.T) {
	// Build a diff with 15 TODO comments
	var diff strings.Builder
	diff.WriteString("diff --git a/test.go b/test.go\n")
	for i := 0; i < 15; i++ {
		diff.WriteString(fmt.Sprintf("+// TODO: item %d\n", i))
	}

	result := extractKeyCommentsFromDiff(diff.String())

	// Result should be capped at 10 comments
	lines := strings.Split(result, "\n")
	assert.Equal(t, 10, len(lines))
}

func TestExtractKeyCommentsFromDiff_MultipleFiles(t *testing.T) {
	diff := `diff --git a/file1.go b/file1.go
--- a/file1.go
+++ b/file1.go
+// TODO: fix file1
 func A() {}

diff --git b/file2.go b/file2.go
--- a/file2.go
+++ b/file2.go
+// FIXME: fix file2
 func B() {}
`

	result := extractKeyCommentsFromDiff(diff)

	assert.Contains(t, result, "file1.go: // TODO: fix file1")
	assert.Contains(t, result, "file2.go: // FIXME: fix file2")
}

// =====================================================================
// categorizeChanges Tests
// =====================================================================

func TestCategorizeChanges(t *testing.T) {
	tests := []struct {
		name         string
		diff         string
		wantNil      bool
		wantContains []string
	}{
		{
			name:    "empty diff",
			diff:    "",
			wantNil: true,
		},
		{
			name: "security changes",
			diff: `diff --git a/security.go b/security.go
--- a/security.go
+++ b/security.go
+// SECURITY: validate input
 func Validate() {}
`,
			wantNil:      false,
			wantContains: []string{"Security fixes/improvements"},
		},
		{
			name: "error handling",
			diff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
+if err != nil {
+    return err
}`,
			wantNil:      false,
			wantContains: []string{"Error handling"},
		},
		{
			name: "test changes",
			diff: `diff --git a/main_test.go b/main_test.go
--- a/main_test.go
+++ b/main_test.go
+func TestSomething(t *testing.T) {
}`,
			wantNil:      false,
			wantContains: []string{"Test changes"},
		},
		{
			name: "dependency updates",
			diff: `diff --git a/go.mod b/go.mod
--- a/go.mod
+++ b/go.mod
+require (
+    github.com/some/pkg v1.0.0
)`,
			wantNil:      false,
			wantContains: []string{"Dependency updates"},
		},
		{
			name: "code removal",
			diff: `diff --git a/old.go b/old.go
--- a/old.go
+++ b/old.go
-func OldFunction() {
-    return 42
}`,
			wantNil:      false,
			wantContains: []string{"Code removal/refactoring"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categorizeChanges(tt.diff)
			if tt.wantNil {
				assert.Equal(t, "", result)
			} else {
				assert.NotEqual(t, "", result)
				for _, want := range tt.wantContains {
					assert.Contains(t, result, want)
				}
			}
		})
	}
}

func TestCategorizeChanges_Mixed(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
+// SECURITY: validate
+if err != nil { return err }
+func TestMain(t *testing.T) {}
-old code
`

	result := categorizeChanges(diff)

	assert.Contains(t, result, "Security fixes/improvements")
	assert.Contains(t, result, "Error handling")
	assert.Contains(t, result, "Test changes")
	assert.Contains(t, result, "Code removal/refactoring")
}

func TestCategorizeChanges_IndexLineIgnored(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc123..def456 100644
--- a/main.go
+++ b/main.go
+new code
`

	result := categorizeChanges(diff)

	// "index" line should be ignored (not counted as removal)
	assert.NotContains(t, result, "Code removal/refactoring")
}

// =====================================================================
// isValidRepoFilePath Tests
// =====================================================================

func TestIsValidRepoFilePath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{
			name:     "path with ..",
			filePath: "../etc/passwd",
			want:     false,
		},
		{
			name:     "clean relative path",
			filePath: "pkg/main.go",
			want:     true,
		},
		{
			name:     "current dir relative",
			filePath: "main.go",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			result := isValidRepoFilePath(tt.filePath)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsValidRepoFilePath_AbsoluteOutsideCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Absolute path outside our temp dir
	result := isValidRepoFilePath("/etc/passwd")
	assert.False(t, result)
}

func TestIsValidRepoFilePath_NestedPathsWithDots(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	result := isValidRepoFilePath("../../etc/passwd")
	assert.False(t, result)
}

// =====================================================================
// extractFileContextForChanges Tests
// =====================================================================

func TestExtractFileContextForChanges_NonExistentFile(t *testing.T) {
	diff := `diff --git a/nonexistent.go b/nonexistent.go
--- a/nonexistent.go
+++ b/nonexistent.go
+new code
`

	result := extractFileContextForChanges(diff)
	assert.Equal(t, "", result)
}

func TestExtractFileContextForChanges_WithRealFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a real file
	os.WriteFile("main.go", []byte("package main\n\nfunc main() {}\n"), 0644)

	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
+new code
`

	result := extractFileContextForChanges(diff)

	assert.NotEqual(t, "", result)
	assert.Contains(t, result, "### main.go")
	assert.Contains(t, result, "package main")
	assert.Contains(t, result, "func main()")
}

func TestExtractFileContextForChanges_SkippedFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a .sum file (should be skipped)
	os.WriteFile("go.sum", []byte("golang.org/x/text v0.3.0\n"), 0644)

	diff := `diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
+new dep
`

	result := extractFileContextForChanges(diff)
	assert.Equal(t, "", result)
}

func TestExtractFileContextForChanges_LargeFile_TruncatedAt500(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a file with 600 lines
	var content strings.Builder
	content.WriteString("package main\n")
	for i := 0; i < 600; i++ {
		content.WriteString(fmt.Sprintf("line%d\n", i))
	}
	os.WriteFile("large.go", []byte(content.String()), 0644)

	diff := `diff --git a/large.go b/large.go
--- a/large.go
+++ b/large.go
+new code
`

	result := extractFileContextForChanges(diff)

	assert.NotEqual(t, "", result)
	assert.Contains(t, result, "### large.go")

	// Count lines in the output (excluding the ### and ``` markers)
	lines := strings.Split(result, "\n")
	// Should be: "### large.go" + "```go" + up to 500 content lines + "```"
	assert.LessOrEqual(t, len(lines), 503)
}

func TestExtractFileContextForChanges_PathTraversal(t *testing.T) {
	diff := `diff --git a/../../etc/passwd b/../../etc/passwd
--- a/../../etc/passwd
+++ b/../../etc/passwd
+new code
`

	result := extractFileContextForChanges(diff)
	assert.Equal(t, "", result)
}

func TestExtractFileContextForChanges_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	os.WriteFile("a.go", []byte("package a\n"), 0644)
	os.WriteFile("b.go", []byte("package b\n"), 0644)

	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
+new

diff --git b/b.go b/b.go
--- a/b.go
+++ b/b.go
+new
`

	result := extractFileContextForChanges(diff)

	assert.Contains(t, result, "### a.go")
	assert.Contains(t, result, "### b.go")
}
