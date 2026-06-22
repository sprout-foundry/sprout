//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage skill allowlist",
	Long: `Manage which skills are allowed for your project.

When an allowlist is configured, only the listed skills can be activated.
Without an allowlist, all skills are permitted by default.

Subcommands:
  allow   Add one or more skill IDs to the allowlist
  revoke  Remove one or more skill IDs from the allowlist
  list    Show currently allowlisted skills`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var skillsAllowCmd = &cobra.Command{
	Use:   "allow <id>...",
	Short: "Add one or more skill IDs to the allowlist",
	Long: `Add one or more skill IDs to the project's skill allowlist.

If no allowlist exists yet, one will be created. Skills that are already
allowed will be reported but not cause an error.

Examples:
  sprout skills allow project-planning
  sprout skills allow project-planning browse-debugging`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillsAllow(args)
	},
}

var skillsRevokeCmd = &cobra.Command{
	Use:   "revoke <id>...",
	Short: "Remove one or more skill IDs from the allowlist",
	Long: `Remove one or more skill IDs from the project's skill allowlist.

If no allowlist is configured, an error will be shown.

Examples:
  sprout skills revoke project-planning
  sprout skills revoke browse-debugging`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillsRevoke(args)
	},
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show currently allowlisted skills",
	Long: `Display the skills currently in the project's allowlist.

If no allowlist is configured, all skills are allowed by default.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillsList()
	},
}

func runSkillsAllow(ids []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	allowed := configuration.ReadAllowedSkills(cwd)
	if allowed == nil {
		allowed = make(map[string]bool)
	}

	added := make([]string, 0, len(ids))
	alreadyAllowed := make([]string, 0)

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if allowed[id] {
			alreadyAllowed = append(alreadyAllowed, id)
		} else {
			allowed[id] = true
			added = append(added, id)
		}
	}

	// Collect all IDs (existing + new) into a sorted slice for writing
	allIDs := make([]string, 0, len(allowed))
	for id := range allowed {
		allIDs = append(allIDs, id)
	}
	sort.Strings(allIDs)

	if err := configuration.WriteAllowedSkills(cwd, allIDs); err != nil {
		return fmt.Errorf("failed to write allowlist: %w", err)
	}

	path := configuration.AllowedSkillsPath(cwd)
	fmt.Printf("Skill allowlist updated: %s\n\n", path)

	if len(added) > 0 {
		fmt.Printf("Added %d skill(s):\n", len(added))
		for _, id := range added {
			fmt.Printf("  %s%s\n", console.GlyphSuccess.Prefix(), id)
		}
	}

	if len(alreadyAllowed) > 0 {
		fmt.Printf("\nAlready allowed (%d):\n", len(alreadyAllowed))
		for _, id := range alreadyAllowed {
			fmt.Printf("  %s%s\n", console.GlyphDim.Prefix(), id)
		}
	}

	if len(added) == 0 && len(alreadyAllowed) == 0 {
		fmt.Println("No skills were added (all inputs were empty).")
	}

	return nil
}

func runSkillsRevoke(ids []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	allowed := configuration.ReadAllowedSkills(cwd)
	if allowed == nil {
		return fmt.Errorf("no skill allowlist is configured at %s (all skills are currently allowed)", filepath.Join(cwd, ".sprout", "allowed_skills"))
	}

	removed := make([]string, 0, len(ids))
	notFound := make([]string, 0)

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if allowed[id] {
			delete(allowed, id)
			removed = append(removed, id)
		} else {
			notFound = append(notFound, id)
		}
	}

	// Collect remaining IDs into a sorted slice for writing
	allIDs := make([]string, 0, len(allowed))
	for id := range allowed {
		allIDs = append(allIDs, id)
	}
	sort.Strings(allIDs)

	if err := configuration.WriteAllowedSkills(cwd, allIDs); err != nil {
		return fmt.Errorf("failed to write allowlist: %w", err)
	}

	path := configuration.AllowedSkillsPath(cwd)
	fmt.Printf("Skill allowlist updated: %s\n\n", path)

	if len(removed) > 0 {
		fmt.Printf("Removed %d skill(s):\n", len(removed))
		for _, id := range removed {
			fmt.Printf("  [-] %s\n", id)
		}
	}

	if len(notFound) > 0 {
		fmt.Printf("\nNot found in allowlist (%d):\n", len(notFound))
		for _, id := range notFound {
			fmt.Printf("  [?] %s\n", id)
		}
	}

	if len(removed) == 0 && len(notFound) == 0 {
		fmt.Println("No skills were revoked (all inputs were empty).")
	}

	return nil
}

func runSkillsList() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	allowed := configuration.ReadAllowedSkills(cwd)
	if allowed == nil {
		fmt.Println("No skill allowlist configured (all skills are allowed by default).")
		fmt.Printf("Allowlist path: %s\n", configuration.AllowedSkillsPath(cwd))
		return nil
	}

	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	fmt.Printf("Skill allowlist: %s\n", configuration.AllowedSkillsPath(cwd))
	fmt.Printf("Allowed skills (%d):\n", len(ids))
	for _, id := range ids {
		fmt.Printf("  - %s\n", id)
	}

	return nil
}

func init() {
	skillsCmd.AddCommand(skillsAllowCmd)
	skillsCmd.AddCommand(skillsRevokeCmd)
	skillsCmd.AddCommand(skillsListCmd)
	rootCmd.AddCommand(skillsCmd)
}
