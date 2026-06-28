package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// fakeSkillLoader is a test double for the SkillLoader interface.
// ---------------------------------------------------------------------------

type fakeSkillLoader struct {
	skills map[string]*SkillInfo
	err    error
}

func (f *fakeSkillLoader) LoadSkill(skillID string) (*SkillInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.skills[skillID], nil
}

// ---------------------------------------------------------------------------
// Helper: build a ToolEnv with a fake SkillLoader pre-loaded with test data.
// ---------------------------------------------------------------------------

func newTestEnvWithSkill(skillID, name, desc string) ToolEnv {
	return ToolEnv{
		SkillLoader: &fakeSkillLoader{
			skills: map[string]*SkillInfo{
				skillID: {
					ID:          skillID,
					Name:        name,
					Description: desc,
					Content:     "# Skill instructions\nDo the thing.",
					Source:      "builtin",
				},
			},
		},
	}
}

// =============================================================================
// Execute-level tests
// =============================================================================

func TestActivateSkillHandler_Success(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	env := newTestEnvWithSkill("project-planning", "Project Planning", "Strategic planning and alignment")

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "project-planning"},
	)
	requireNoError(t, err)

	if result.IsError {
		t.Fatalf("IsError should be false on success, output: %s", result.Output)
	}

	// Verify output format matches the expected template.
	expected := "Activated skill 'Project Planning' (project-planning).\n\nDescription: Strategic planning and alignment\n\nInstructions loaded into context."
	if result.Output != expected {
		t.Errorf("Output mismatch.\nGot:\n%s\n\nWant:\n%s", result.Output, expected)
	}

	// Individual substring checks (belt-and-suspenders).
	if !strings.Contains(result.Output, "Activated skill") {
		t.Error("Output should contain 'Activated skill'")
	}
	if !strings.Contains(result.Output, "Project Planning") {
		t.Error("Output should contain skill name")
	}
	if !strings.Contains(result.Output, "Description:") {
		t.Error("Output should contain 'Description:'")
	}
	if !strings.Contains(result.Output, "Strategic planning and alignment") {
		t.Error("Output should contain skill description")
	}
	if !strings.Contains(result.Output, "Instructions loaded into context") {
		t.Error("Output should contain 'Instructions loaded into context'")
	}
}

func TestActivateSkillHandler_NilSkillLoader(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	env := ToolEnv{} // SkillLoader is nil

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "project-planning"},
	)
	requireNoError(t, err)

	if !result.IsError {
		t.Fatalf("IsError should be true when SkillLoader is nil, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "skill loading not available") {
		t.Errorf("Output should mention 'skill loading not available', got: %s", result.Output)
	}
}

func TestActivateSkillHandler_NilSkillInfo(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	env := ToolEnv{
		SkillLoader: &fakeSkillLoader{skills: map[string]*SkillInfo{}}, // empty map → returns (nil, nil)
	}

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "nonexistent"},
	)
	requireNoError(t, err)
	requireTrue(t, result.IsError, "IsError should be true for nil skillInfo")
	if !strings.Contains(result.Output, "nonexistent") {
		t.Errorf("Output should mention the skill id, got: %s", result.Output)
	}
}

func TestActivateSkillHandler_LoadSkillError(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	loaderErr := fmt.Errorf("skill not found or disabled: nonexistent-skill")
	env := ToolEnv{
		SkillLoader: &fakeSkillLoader{err: loaderErr},
	}

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "nonexistent-skill"},
	)
	requireNoError(t, err)

	if !result.IsError {
		t.Fatalf("IsError should be true when LoadSkill fails, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "skill not found or disabled") {
		t.Errorf("Output should contain error message, got: %s", result.Output)
	}
}

func TestActivateSkillHandler_SkillFallback(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	env := newTestEnvWithSkill("project-planning", "Project Planning", "Strategic planning")

	// Use "skill" key instead of "skill_id" — should fall back correctly.
	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill": "project-planning"},
	)
	requireNoError(t, err)

	if result.IsError {
		t.Fatalf("IsError should be false when 'skill' fallback works, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Activated skill") {
		t.Errorf("Output should contain 'Activated skill', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Project Planning") {
		t.Errorf("Output should contain skill name, got: %s", result.Output)
	}
}

func TestActivateSkillHandler_MissingSkillID(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	env := newTestEnvWithSkill("project-planning", "Project Planning", "Strategic planning")

	tests := []struct {
		name string
		args map[string]any
	}{
		{"nil args", nil},
		{"empty args", map[string]any{}},
		{"unrelated key", map[string]any{"other": "value"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := h.Execute(context.Background(), env, tc.args)
			requireNoError(t, err)

			if !result.IsError {
				t.Fatalf("IsError should be true when skill_id is missing, output: %s", result.Output)
			}
			if !strings.Contains(result.Output, "skill_id is required") {
				t.Errorf("Output should mention 'skill_id is required', got: %s", result.Output)
			}
		})
	}
}

func TestActivateSkillHandler_EventBus_Success(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	bus := events.NewEventBus()
	env := newTestEnvWithSkill("browse-debugging", "Browse Debugging", "Debug web UIs")
	env.EventBus = bus

	ch := bus.Subscribe("test-events")

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "browse-debugging"},
	)
	requireNoError(t, err)
	requireFalse(t, result.IsError, "IsError")

	// Collect up to 2 events.
	var received []events.UIEvent
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			received = append(received, ev)
		default:
			break
		}
	}

	if len(received) < 2 {
		t.Fatalf("expected 2 events (tool_start + tool_end), got %d", len(received))
	}

	if received[0].Type != events.EventTypeToolStart {
		t.Errorf("first event type = %q, want %q", received[0].Type, events.EventTypeToolStart)
	}
	if received[1].Type != events.EventTypeToolEnd {
		t.Errorf("second event type = %q, want %q", received[1].Type, events.EventTypeToolEnd)
	}

	// Verify error flag is false on success.
	if data, ok := received[1].Data.(map[string]any); ok {
		if errVal, hasErr := data["error"]; hasErr {
			if errVal != false {
				t.Errorf("tool_end error field should be false on success, got %v", errVal)
			}
		}
	}
}

func TestActivateSkillHandler_EventBus_ErrorFlag(t *testing.T) {
	t.Parallel()
	h := &activateSkillHandler{}
	bus := events.NewEventBus()
	env := ToolEnv{EventBus: bus} // No SkillLoader — will produce an error path

	ch := bus.Subscribe("test-events")

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "anything"},
	)
	requireNoError(t, err)
	requireTrue(t, result.IsError, "IsError")

	// Collect up to 2 events.
	var received []events.UIEvent
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			received = append(received, ev)
		default:
			break
		}
	}

	if len(received) < 2 {
		t.Fatalf("expected 2 events (tool_start + tool_end), got %d", len(received))
	}

	// Verify error flag is true on failure.
	if data, ok := received[1].Data.(map[string]any); ok {
		if errVal, hasErr := data["error"]; hasErr {
			if errVal != true {
				t.Errorf("tool_end error field should be true on failure, got %v", errVal)
			}
		}
	}
}

func TestActivateSkillHandler_OutputFormatMatchesLegacy(t *testing.T) {
	t.Parallel()
	// The legacy handleActivateSkill returns:
	//   "Activated skill '%s' (%s).\n\nDescription: %s\n\nInstructions loaded into context."
	// with args: skillInfo.Name, skillID, skillInfo.Description
	//
	// The new handler should produce identical output format.
	h := &activateSkillHandler{}
	env := newTestEnvWithSkill(
		"project-planning",
		"Project Planning",
		"Strategic planning and alignment for new (greenfield) or existing (brownfield) projects...",
	)

	result, err := h.Execute(
		context.Background(),
		env,
		map[string]any{"skill_id": "project-planning"},
	)
	requireNoError(t, err)
	requireFalse(t, result.IsError, "IsError")

	// Build the expected output using the same format string as the legacy handler.
	expected := fmt.Sprintf(
		"Activated skill '%s' (%s).\n\nDescription: %s\n\nInstructions loaded into context.",
		"Project Planning",
		"project-planning",
		"Strategic planning and alignment for new (greenfield) or existing (brownfield) projects...",
	)

	if result.Output != expected {
		t.Errorf("Output format does not match legacy handler.\nGot:\n%q\n\nWant:\n%q", result.Output, expected)
	}
}
