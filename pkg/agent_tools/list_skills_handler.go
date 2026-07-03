package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
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
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()

		// Get config - use manager if available, otherwise create default
		var config *configuration.Config
		if env.ConfigManager != nil {
			config = env.ConfigManager.GetConfig()
		} else {
			manager, err := configuration.NewManager()
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
			skills = append(skills, skill)
		}

		return ToolResult{
			Output:  formatSkillList(skills),
			IsError: false,
		}, nil
	}

	return ToolResult{
		Output:  "No results",
		IsError: false,
	}, nil
}

func (h *listSkillsHandler) Aliases() []string         { return nil }
func (h *listSkillsHandler) Timeout() time.Duration    { return 0 }
func (h *listSkillsHandler) MaxResultSize() int        { return 0 }
func (h *listSkillsHandler) SafeForParallel() bool     { return false }
func (h *listSkillsHandler) Interactive() bool         { return false }

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
