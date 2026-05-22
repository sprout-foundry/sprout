package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type activateSkillHandler struct{}

func (h *activateSkillHandler) Name() string { return "activate_skill" }

func (h *activateSkillHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "activate_skill",
		Description: "Activate a skill to load its instructions into the current session context. Use this when you need the skill's expertise for the current task.",
		Required: []string{"skill_id"},
		Parameters: []ParameterDef{
			{Name: "skill_id", Type: "string", Required: true, Description: "The ID of the skill to activate (e.g., 'project-planning', 'browse-debugging')"},
		},
	}
}

func (h *activateSkillHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "skill_id")
	return err
}

func (h *activateSkillHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": true,
			})
		}()
	}

	skillID, _ := extractString(args, "skill_id")

	// TODO: Full implementation requires *Agent access for skill manager,
	// GetAvailableSkills(), and LoadSkill(). This is a thin wrapper stub.
	return ToolResult{
		Output:  fmt.Sprintf("Skill activation requires full *Agent refactoring for complete functionality. Cannot activate skill %q without access to the Agent's skill manager. Please use the legacy interface or complete the migration.", skillID),
		IsError: true,
	}, fmt.Errorf("activate_skill requires full *Agent refactoring")
}
