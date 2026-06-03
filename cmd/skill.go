//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/skills"
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
	Long: `Create a new skill in the project's .sprout/skills/ directory.

The skill-id should be a short identifier like "myproject-conventions" or "api-patterns".
This will create a SKILL.md file with the correct structure that you can edit.

Example:
  sprout skill add myproject-conventions`,
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
		
		// Create .sprout/skills directory
		skillsDir := filepath.Join(cwd, ".sprout", "skills")
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
		
		console.GlyphSuccess.Printf("Created skill '%s' at %s", skillID, skillFile)
		fmt.Printf("\nEdit the file to add your project-specific conventions.\n")
		fmt.Printf("The skill will be automatically discovered when running sprout in this project.\n")
	},
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available skills",
	Long:  `List all available skills including built-in, user-level, and project-specific skills.`,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("## Built-in Skills")
		fmt.Println()
		// Derive the list from pkg/skills so a new skill added under
		// pkg/skills/library/<id>/SKILL.md shows up here automatically.
		// IDs() returns alphabetical order so the output is stable.
		builtins := skills.Builtins()
		for _, id := range skills.IDs() {
			fmt.Printf("  %-25s %s\n", id, builtins[id].Description)
		}

		// User-level skills (~/.config/sprout/skills/). Listed BEFORE
		// project skills because that mirrors the merge order in
		// configuration.Load() — project skills override user skills
		// when both define the same ID. envutil.GetConfigDir resolves
		// SPROUT_CONFIG / XDG_CONFIG_HOME / $HOME, matching the same
		// path discoverUserSkills uses at agent startup.
		if userConfigDir, err := envutil.GetConfigDir(); err == nil {
			printSkillsSection("User Skills", filepath.Join(userConfigDir, "skills"), "user skill")
		}

		// Project skills (./.sprout/skills/) — the override layer.
		printSkillsSection("Project Skills", filepath.Join(cwd, ".sprout", "skills"), "project skill")

		fmt.Println()
		fmt.Println("Use 'activate_skill <skill-id>' in an agent session to load a skill.")
	},
}

// printSkillsSection lists skills found in dir under the given header.
// Silent no-op when the directory is missing or empty — keeps the
// output clean when a user has only project skills (or vice versa).
// fallbackTag is shown in parentheses when a SKILL.md exists but the
// frontmatter description can't be read.
func printSkillsSection(header, dir, fallbackTag string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return
	}

	// Pre-scan for valid skill directories so we can skip the header
	// when the directory has only stray files. Without this the user
	// would see an empty section if (for example) the skills dir
	// contains a README and no actual skill subdirs.
	type skillRow struct {
		id, desc string
	}
	var rows []skillRow
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		content, err := os.ReadFile(skillFile)
		if err != nil {
			rows = append(rows, skillRow{id: entry.Name(), desc: "(" + fallbackTag + ")"})
			continue
		}
		desc := extractDescription(string(content))
		if desc == "" {
			desc = "(" + fallbackTag + ")"
		}
		rows = append(rows, skillRow{id: entry.Name(), desc: desc})
	}
	if len(rows) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("## " + header)
	fmt.Println()
	for _, row := range rows {
		fmt.Printf("  %-25s %s\n", row.id, row.desc)
	}
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
