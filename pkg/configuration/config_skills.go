package configuration

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/skills"
)

// defaultSkills derives the built-in skill registry from the embedded
// pkg/skills library. Adding a new skill is a one-step process:
// create pkg/skills/library/<id>/SKILL.md with valid frontmatter and
// the registry, the `list_skills` tool output, and the `sprout skill
// list` CLI all pick it up automatically.
//
// repo-onboarding is preserved as a legacy alias for project-planning
// because user configs and prompts from older versions reference the
// old ID; the two point at the same content.
func defaultSkills() map[string]Skill {
	builtins := skills.Builtins()
	out := make(map[string]Skill, len(builtins)+1)
	for id, b := range builtins {
		out[id] = Skill{
			ID:          b.ID,
			Name:        b.Name,
			Description: b.Description,
			Path:        b.Path,
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		}
	}
	if pp, ok := out["project-planning"]; ok {
		alias := pp
		alias.ID = "repo-onboarding"
		alias.Metadata = map[string]string{"version": "2.0"}
		out["repo-onboarding"] = alias
	}
	return out
}

func mergeMissingDefaultSkills(config *Config) {
	if config == nil {
		return
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	for id, skill := range defaultSkills() {
		if existing, exists := config.Skills[id]; exists {
			// Update built-in skills in-place so description/path/metadata
			// stay current across versions.  Preserve user-set Enabled flag.
			wasEnabled := existing.Enabled
			skill.Metadata["source"] = "builtin"
			skill.Enabled = wasEnabled
			config.Skills[id] = skill
		} else {
			if skill.Metadata == nil {
				skill.Metadata = make(map[string]string)
			}
			skill.Metadata["source"] = "builtin"
			config.Skills[id] = skill
		}
	}

	// Prune stale built-in skills that were registered by a prior version
	// of defaultSkills() but are no longer present.  Accept both the
	// current builtin prefix and the pre-refactor location so legacy
	// configs migrate cleanly — entries with the old path get either
	// updated in-place above (still in defaults) or pruned here (no
	// longer in defaults).
	defaults := defaultSkills()
	for id, skill := range config.Skills {
		path := skill.Path
		if path == "" {
			continue
		}
		isBuiltin := strings.HasPrefix(path, skills.LogicalPath+"/") ||
			strings.HasPrefix(path, skills.LegacyLogicalPath+"/")
		if !isBuiltin {
			continue
		}
		if _, stillDefault := defaults[id]; !stillDefault {
			delete(config.Skills, id)
		}
	}
}

// discoverUserSkills scans the ~/.config/sprout/skills/ directory for user-level skills
// and adds them to the config. This allows users to create custom skills that are
// available across all projects. User skills are discovered before project skills,
// so project skills can override them.
func discoverUserSkills(config *Config) []string {
	var discovered []string
	if config == nil {
		return discovered
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Get user config directory
	configDir, err := GetConfigDir()
	if err != nil {
		return discovered
	}

	// Check for skills directory
	skillsDir := filepath.Join(configDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return discovered // No user skills directory, that's fine
	}

	// Scan for skill directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillID := entry.Name()
		skillFile := filepath.Join(skillsDir, skillID, "SKILL.md")

		// Check if SKILL.md exists
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		// Read skill file to extract metadata
		content, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		// Parse front matter
		name, description := parseSkillFrontMatter(string(content))
		if name == "" {
			name = skillID
		}
		if description == "" {
			description = fmt.Sprintf("User-level skill: %s", skillID)
		}

		// Add to config (don't override if already exists)
		if _, exists := config.Skills[skillID]; !exists {
			config.Skills[skillID] = Skill{
				ID:          skillID,
				Name:        name,
				Description: description,
				Path:        filepath.Join("skills", skillID),
				Enabled:     true,
				Metadata:    map[string]string{"source": "user"},
			}
			discovered = append(discovered, name)
		}
	}

	return discovered
}

// discoverProjectSkills scans the .sprout/skills/ directory for project-specific skills
// and adds them to the config. This allows users to create custom skills without
// modifying the global config.
func discoverProjectSkills(config *Config) []string {
	var discovered []string
	if config == nil {
		return discovered
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return discovered
	}

	// Check for .sprout/skills directory
	skillsDir := filepath.Join(cwd, ".sprout", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return discovered // No project skills directory, that's fine
	}

	// Read the allowed_skills allowlist (if present)
	allowed := ReadAllowedSkills(cwd)

	// Scan for skill directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillID := entry.Name()
		skillFile := filepath.Join(skillsDir, skillID, "SKILL.md")

		// Check if SKILL.md exists
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		// Read skill file to extract metadata
		content, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		// Parse front matter
		name, description := parseSkillFrontMatter(string(content))
		if name == "" {
			name = skillID
		}
		if description == "" {
			description = fmt.Sprintf("Project-specific skill: %s", skillID)
		}

		// Add to config (don't override if already exists)
		if _, exists := config.Skills[skillID]; !exists {
			// If an allowlist exists and the skill is not listed, load it disabled.
			enabled := true
			if allowed != nil && !allowed[skillID] {
				enabled = false
			}
			config.Skills[skillID] = Skill{
				ID:          skillID,
				Name:        name,
				Description: description,
				Path:        filepath.Join(".sprout", "skills", skillID),
				Enabled:     enabled,
				Metadata:    map[string]string{"source": "project"},
			}
			discovered = append(discovered, name)
		}
	}

	return discovered
}

// parseSkillFrontMatter extracts name and description from SKILL.md front matter
func parseSkillFrontMatter(content string) (name, description string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontMatter := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			inFrontMatter = !inFrontMatter
			continue
		}
		if inFrontMatter {
			if strings.HasPrefix(line, "name:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			} else if strings.HasPrefix(line, "description:") {
				description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			}
		}
	}
	return name, description
}

// discoverSkills runs both user-level and project-level skill discovery
// and returns the combined list of discovered skill names. Callers can use
// this single function instead of invoking discoverUserSkills and
// discoverProjectSkills separately. The SPROUT_NO_USER_SKILLS and
// SPROUT_NO_PROJECT_SKILLS env guards are applied inside.
func (c *Config) discoverSkills() []string {
	var found []string
	if os.Getenv("SPROUT_NO_USER_SKILLS") != "1" {
		found = append(found, discoverUserSkills(c)...)
	}
	if os.Getenv("SPROUT_NO_PROJECT_SKILLS") != "1" {
		found = append(found, discoverProjectSkills(c)...)
	}
	return found
}

// GetSkill retrieves a skill configuration by ID
// Returns nil if the skill doesn't exist or is disabled
func (c *Config) GetSkill(id string) *Skill {
	if c.Skills == nil {
		return nil
	}
	if skill, exists := c.Skills[id]; exists && skill.Enabled {
		return &skill
	}
	return nil
}

// GetSkillPath returns the full path to a skill directory
func (c *Config) GetSkillPath(id string) string {
	skill := c.GetSkill(id)
	if skill == nil || skill.Path == "" {
		return ""
	}
	// Skill path is relative to sprout source root
	return skill.Path
}

// GetAllEnabledSkills returns all enabled skills
func (c *Config) GetAllEnabledSkills() map[string]Skill {
	if c.Skills == nil {
		return nil
	}
	result := make(map[string]Skill)
	for id, skill := range c.Skills {
		if skill.Enabled {
			result[id] = skill
		}
	}
	return result
}
