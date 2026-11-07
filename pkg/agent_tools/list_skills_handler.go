package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

type listSkillsHandler struct{}

func (h *listSkillsHandler) Name() string {
	return "list_skills"
}

func (h *listSkillsHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "list_skills",
		Description: "List all available skills that can be activated. Skills are instruction bundles that provide domain expertise.",
		Parameters:  []ParameterDef{},
		Required:    []string{},
	}
}

func (h *listSkillsHandler) Validate(args map[string]any) error {
	return nil
}

func (h *listSkillsHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// Get config - use manager if available, otherwise create default
	var config *configuration.Config
	if env.ConfigManager != nil {
		config = env.ConfigManager.GetConfig()
	} else {
		manager, err := configuration.NewManagerSilent()
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("Error getting configuration: %v", err),
				IsError: true,
			}, nil
		}
		config = manager.GetConfig()
	}

	skillMap := config.GetAllEnabledSkills()
	skills := make([]configuration.Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		// A persona that names its skills sees only those (SP-175).
		if !skillAllowed(env.AllowedSkills, skill.ID) {
			continue
		}
		skills = append(skills, skill)
	}

	return ToolResult{
		Output:  formatSkillList(skills),
		IsError: false,
	}, nil
}

// skillAllowed reports whether the active persona's skills list permits the
// named skill. A nil list means no restriction (a core persona, which never
// sees the skill tools through its own roster); an empty list denies every
// skill (a custom persona that declared no `skills` — "absent means none");
// "*" permits everything.
func skillAllowed(allowed []string, skillID string) bool {
	if allowed == nil {
		return true
	}
	if len(allowed) == 0 {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(skillID))
	for _, entry := range allowed {
		value := strings.ToLower(strings.TrimSpace(entry))
		if value == "" {
			continue
		}
		if value == "*" || value == needle {
			return true
		}
	}
	return false
}

func (h *listSkillsHandler) Aliases() []string      { return nil }
func (h *listSkillsHandler) Timeout() time.Duration { return 0 }
func (h *listSkillsHandler) MaxResultSize() int     { return 0 }
func (h *listSkillsHandler) SafeForParallel() bool  { return false }
func (h *listSkillsHandler) Interactive() bool      { return false }

func formatSkillList(skills []configuration.Skill) string {
	if len(skills) == 0 {
		return "No skills found."
	}

	// Sort by ID for consistent output
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].ID < skills[j].ID
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d skill(s):\n\n", len(skills)))
	sb.WriteString("| ID | Name | Description |\n")
	sb.WriteString("| ---- | ---- | ----------- |\n")
	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		// Truncate long descriptions
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", s.ID, s.Name, desc))
	}
	sb.WriteString("\nUse activate_skill to load a skill into your context.\n")
	return sb.String()
}
