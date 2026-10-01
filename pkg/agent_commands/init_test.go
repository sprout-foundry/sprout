package commands

import (
	"os"
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
