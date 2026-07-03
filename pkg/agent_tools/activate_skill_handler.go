package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// activateSkillHandler activates a skill by loading its instructions via
// the SkillLoader dependency. It is a thin tool that returns formatted
// skill instructions — it does NOT mutate agent state directly.
//
// The agent-state side effects (adding to ActiveSkills, folding content
// into systemPrompt) remain in the legacy handleActivateSkill and will
// be migrated in a follow-up phase.
type activateSkillHandler struct{}

func (h *activateSkillHandler) Name() string { return "activate_skill" }

func (h *activateSkillHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "activate_skill",
		Description: "Activate a skill to load its instructions into the current session context. Use this when you need the skill's expertise for the current task.",
		Required:    []string{"skill_id"},
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
	var hadError bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	// Extract skill_id with fallback to "skill" (matching legacy handleActivateSkill)
	skillID, err := extractString(args, "skill_id")
	if err != nil {
		skillID, err = extractString(args, "skill")
		if err != nil {
			hadError = true
			return ToolResult{Output: "skill_id is required", IsError: true}, nil
		}
	}

	// Check if SkillLoader is available
	if env.SkillLoader == nil {
		hadError = true
		return ToolResult{Output: "skill loading not available: SkillLoader is not configured", IsError: true}, nil
	}

	// Load the skill
	skillInfo, err := env.SkillLoader.LoadSkill(skillID)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	if skillInfo == nil {
		hadError = true
		return ToolResult{Output: fmt.Sprintf("skill '%s' returned no data", skillID), IsError: true}, nil
	}

	return ToolResult{
		Output: fmt.Sprintf("Activated skill '%s' (%s).\n\nDescription: %s\n\nInstructions loaded into context.",
			skillInfo.Name, skillInfo.ID, skillInfo.Description),
	}, nil
}

func (h *activateSkillHandler) Aliases() []string         { return nil }
func (h *activateSkillHandler) Timeout() time.Duration    { return 0 }
func (h *activateSkillHandler) MaxResultSize() int        { return 0 }
func (h *activateSkillHandler) SafeForParallel() bool     { return false }
func (h *activateSkillHandler) Interactive() bool         { return false }
