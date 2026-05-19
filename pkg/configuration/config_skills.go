package configuration

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func defaultSkills() map[string]Skill {
	return map[string]Skill{
		"go-conventions": {
			ID:          "go-conventions",
			Name:        "Go Conventions",
			Description: "Go coding conventions, best practices, and style guidelines. Use when writing or reviewing Go code.",
			Path:        "pkg/agent/skills/go-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"test-writing": {
			ID:          "test-writing",
			Name:        "Test Writing",
			Description: "Guidelines for writing effective unit tests, integration tests, and test coverage. Use when creating tests.",
			Path:        "pkg/agent/skills/test-writing",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"commit-msg": {
			ID:          "commit-msg",
			Name:        "Commit Message",
			Description: "Conventional commits format and best practices for writing clear commit messages.",
			Path:        "pkg/agent/skills/commit-msg",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"repo-onboarding": {
			ID:          "repo-onboarding",
			Name:        "Project Planning",
			Description: "Strategic planning and alignment for new (greenfield) or existing (brownfield) projects. Use when starting a new project, onboarding to an unfamiliar repo, or aligning an existing codebase to a standardized workflow.",
			Path:        "pkg/agent/skills/project-planning",
			Enabled:     true,
			Metadata:    map[string]string{"version": "2.0"},
		},
		"bug-triage": {
			ID:          "bug-triage",
			Name:        "Bug Triage",
			Description: "Repro-first debugging workflow with root-cause validation and minimal-risk fix planning.",
			Path:        "pkg/agent/skills/bug-triage",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"safe-refactor": {
			ID:          "safe-refactor",
			Name:        "Safe Refactor",
			Description: "Behavior-preserving refactor workflow focused on small steps, verification gates, and low regression risk.",
			Path:        "pkg/agent/skills/safe-refactor",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"test-author": {
			ID:          "test-author",
			Name:        "Test Author",
			Description: "Process for adding targeted tests, edge cases, and regressions for changed behavior.",
			Path:        "pkg/agent/skills/test-author",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"release-preflight": {
			ID:          "release-preflight",
			Name:        "Release Preflight",
			Description: "Pre-release checklist for build, test, and risk validation with clear go/no-go output.",
			Path:        "pkg/agent/skills/release-preflight",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"docs-sync": {
			ID:          "docs-sync",
			Name:        "Docs Sync",
			Description: "Process to keep docs aligned with shipped behavior and command surface.",
			Path:        "pkg/agent/skills/docs-sync",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"review-workflow": {
			ID:          "review-workflow",
			Name:        "Review Workflow",
			Description: "Evidence-first review process for triaging findings, reducing false positives, and prioritizing must-fix risks.",
			Path:        "pkg/agent/skills/review-workflow",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"python-conventions": {
			ID:          "python-conventions",
			Name:        "Python Conventions",
			Description: "Python 3.11+ coding conventions, best practices, and style guidelines. Use when writing or reviewing Python code.",
			Path:        "pkg/agent/skills/python-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"typescript-conventions": {
			ID:          "typescript-conventions",
			Name:        "TypeScript Conventions",
			Description: "TypeScript 5.x and JavaScript ES2022+ coding conventions, best practices, and style guidelines. Use when writing or reviewing TypeScript/JavaScript code.",
			Path:        "pkg/agent/skills/typescript-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"rust-conventions": {
			ID:          "rust-conventions",
			Name:        "Rust Conventions",
			Description: "Rust 2021 edition coding conventions, best practices, and style guidelines. Use when writing or reviewing Rust code.",
			Path:        "pkg/agent/skills/rust-conventions",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
		"project-planning": {
			ID:          "project-planning",
			Name:        "Project Planning",
			Description: "Structured planning and project initialization workflow. Use when starting a new project, setting up a new codebase, or creating a project plan.",
			Path:        "pkg/agent/skills/project-planning",
			Enabled:     true,
			Metadata:    map[string]string{"version": "1.0"},
		},
	}
}

func mergeMissingDefaultSkills(config *Config) {
	if config == nil {
		return
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	for id, skill := range defaultSkills() {
		if _, exists := config.Skills[id]; !exists {
			config.Skills[id] = skill
		}
	}
}

// discoverProjectSkills scans the .sprout/skills/ directory for project-specific skills
// and adds them to the config. This allows users to create custom skills without
// modifying the global config.
func discoverProjectSkills(config *Config) {
	if config == nil {
		return
	}
	if config.Skills == nil {
		config.Skills = make(map[string]Skill)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	// Check for .sprout/skills directory
	skillsDir := filepath.Join(cwd, ".sprout", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return // No project skills directory, that's fine
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
			description = fmt.Sprintf("Project-specific skill: %s", skillID)
		}

		// Add to config (don't override if already exists)
		if _, exists := config.Skills[skillID]; !exists {
			config.Skills[skillID] = Skill{
				ID:          skillID,
				Name:        name,
				Description: description,
				Path:        filepath.Join(".sprout", "skills", skillID),
				Enabled:     true,
				Metadata:    map[string]string{"source": "project"},
			}
		}
	}
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
