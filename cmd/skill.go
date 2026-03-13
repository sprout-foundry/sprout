package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage agent skills",
	Long: `Manage agent skills - instruction bundles that can be loaded into context.

Skills provide domain expertise for specific languages, frameworks, or project conventions.
Use 'skill add' to create a new project-specific skill.`,
}

var skillAddCmd = &cobra.Command{
	Use:   "add [skill-id]",
	Short: "Create a new project-specific skill",
	Long: `Create a new skill in the project's .ledit/skills/ directory.

The skill-id should be a short identifier like "myproject-conventions" or "api-patterns".
This will create a SKILL.md file with the correct structure that you can edit.

Example:
  ledit skill add myproject-conventions`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		skillID := args[0]
		
		// Validate skill ID
		if strings.Contains(skillID, " ") {
			fmt.Fprintf(os.Stderr, "Error: skill ID cannot contain spaces\n")
			os.Exit(1)
		}
		if strings.Contains(skillID, "/") || strings.Contains(skillID, "\\") {
			fmt.Fprintf(os.Stderr, "Error: skill ID cannot contain path separators\n")
			os.Exit(1)
		}
		
		// Get current working directory
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
			os.Exit(1)
		}
		
		// Create .ledit/skills directory
		skillsDir := filepath.Join(cwd, ".ledit", "skills")
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create skills directory: %v\n", err)
			os.Exit(1)
		}
		
		// Create skill directory
		skillDir := filepath.Join(skillsDir, skillID)
		skillFile := filepath.Join(skillDir, "SKILL.md")
		
		// Check if skill already exists
		if _, err := os.Stat(skillFile); err == nil {
			fmt.Fprintf(os.Stderr, "Error: skill '%s' already exists at %s\n", skillID, skillFile)
			os.Exit(1)
		}
		
		// Create skill directory
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create skill directory: %v\n", err)
			os.Exit(1)
		}
		
		// Generate skill content
		skillName := strings.ReplaceAll(skillID, "-", " ")
		skillName = cases.Title(language.English).String(skillName)
		
		content := fmt.Sprintf(`---
name: %s
description: Project-specific conventions for %s. Update this description.
---

# %s

<!-- Edit this file to add your project-specific conventions -->

## Key Patterns

<!-- Example: API patterns, data models, error handling -->

## Tools

<!-- Example: formatters, linters, test runners -->

## Common Gotchas

<!-- Example: things that frequently trip people up -->

## Examples

<!-- Brief code examples showing the patterns -->
`, skillID, skillName, skillName)
		
		// Write skill file
		if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create skill file: %v\n", err)
			os.Exit(1)
		}
		
		fmt.Printf("✓ Created skill '%s' at %s\n", skillID, skillFile)
		fmt.Printf("\nEdit the file to add your project-specific conventions.\n")
		fmt.Printf("The skill will be automatically discovered when running ledit in this project.\n")
	},
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available skills",
	Long: `List all available skills including built-in and project-specific skills.`,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
			os.Exit(1)
		}
		
		fmt.Println("## Built-in Skills")
		fmt.Println()
		builtins := []struct {
			id, name, desc string
		}{
			{"go-conventions", "Go Conventions", "Go coding conventions and best practices"},
			{"python-conventions", "Python Conventions", "Python 3.11+ coding patterns"},
			{"typescript-conventions", "TypeScript Conventions", "TypeScript/JavaScript ES2022+ patterns"},
			{"rust-conventions", "Rust Conventions", "Rust 2021 edition patterns"},
			{"test-writing", "Test Writing", "Guidelines for writing effective tests"},
			{"commit-msg", "Commit Message", "Conventional commits format"},
			{"bug-triage", "Bug Triage", "Debugging workflow"},
			{"safe-refactor", "Safe Refactor", "Behavior-preserving refactoring"},
			{"review-workflow", "Review Workflow", "Code review process"},
		}
		for _, s := range builtins {
			fmt.Printf("  %-25s %s\n", s.id, s.desc)
		}
		
		// Check for project skills
		skillsDir := filepath.Join(cwd, ".ledit", "skills")
		entries, err := os.ReadDir(skillsDir)
		if err == nil && len(entries) > 0 {
			fmt.Println()
			fmt.Println("## Project Skills")
			fmt.Println()
			for _, entry := range entries {
				if entry.IsDir() {
					skillFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
					if content, err := os.ReadFile(skillFile); err == nil {
						// Extract description from front matter
						desc := extractDescription(string(content))
						fmt.Printf("  %-25s %s\n", entry.Name(), desc)
					} else {
						fmt.Printf("  %-25s (project skill)\n", entry.Name())
					}
				}
			}
		}
		
		fmt.Println()
		fmt.Println("Use 'activate_skill <skill-id>' in an agent session to load a skill.")
	},
}

func extractDescription(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontMatter := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			inFrontMatter = !inFrontMatter
			continue
		}
		if inFrontMatter && strings.HasPrefix(line, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return "(no description)"
}

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillAddCmd)
	skillCmd.AddCommand(skillListCmd)
}
