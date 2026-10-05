package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/skills"
)

const designSkillMarker = "[Skill Activated: Design System"

// TestActivateSkillByID_FoldsOnceAndReportsAlreadyActive verifies the core
// contract: the skill is folded into the live prompt and marked active, and
// a repeat call reports it as active without folding it twice.
func TestActivateSkillByID_FoldsOnceAndReportsAlreadyActive(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	out, err := a.activateSkillByID(skills.SkillIDDesignSystem)
	if err != nil {
		t.Fatalf("activateSkillByID: %v", err)
	}
	if !strings.Contains(out, "Activated skill") {
		t.Errorf("first activation should report activation, got %q", out)
	}
	if !slices.Contains(a.state.GetActiveSkills(), skills.SkillIDDesignSystem) {
		t.Errorf("design-system must be marked active; got %v", a.state.GetActiveSkills())
	}

	out, err = a.activateSkillByID(skills.SkillIDDesignSystem)
	if err != nil {
		t.Fatalf("second activateSkillByID: %v", err)
	}
	if !strings.Contains(out, "already active") {
		t.Errorf("repeat activation should report already active, got %q", out)
	}
	if n := strings.Count(a.GetSystemPrompt(), designSkillMarker); n != 1 {
		t.Errorf("want exactly one fold, got %d", n)
	}
	if n := strings.Count(strings.Join(a.state.GetActiveSkills(), ","), skills.SkillIDDesignSystem); n != 1 {
		t.Errorf("active-skill set must not duplicate the ID; got %v", a.state.GetActiveSkills())
	}
}

// TestActivateSkillByID_RefoldsAfterPromptRebuild: a rebuilt prompt drops
// the fold while the active-skill set keeps the ID, and activation must
// restore the instructions rather than report the skill as loaded.
func TestActivateSkillByID_RefoldsAfterPromptRebuild(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if _, err := a.activateSkillByID(skills.SkillIDDesignSystem); err != nil {
		t.Fatalf("activateSkillByID: %v", err)
	}
	a.SetSystemPrompt("rebuilt prompt")

	out, err := handleActivateSkill(context.Background(), a, map[string]interface{}{"skill_id": skills.SkillIDDesignSystem})
	if err != nil {
		t.Fatalf("activate_skill: %v", err)
	}
	if !strings.Contains(out, "Activated skill") {
		t.Errorf("activate_skill must re-fold a dropped skill, got %q", out)
	}
	if n := strings.Count(a.GetSystemPrompt(), designSkillMarker); n != 1 {
		t.Errorf("want exactly one fold after re-activation, got %d", n)
	}
}

// TestActivateSkillByID_MissingSkillErrors: an unknown skill fails and is
// not marked active.
func TestActivateSkillByID_MissingSkillErrors(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if _, err := a.activateSkillByID("no-such-skill"); err == nil {
		t.Fatal("activating an unregistered skill must return an error")
	}
	if slices.Contains(a.state.GetActiveSkills(), "no-such-skill") {
		t.Error("a failed activation must not mark the skill active")
	}
}

// TestModeSkills_DesignModeActivatesDesignSystem pins the per-request mode
// contract: a query sent from Design mode starts with the design workflow
// in the prompt; Code mode and unknown modes add nothing.
func TestModeSkills_DesignModeActivatesDesignSystem(t *testing.T) {
	for _, mode := range []string{"", "code", "ship", "Unknown"} {
		a := newIsolatedTestAgent(t)
		a.SetWorkspaceMode(mode)
		a.autoActivateModeSkills()
		if strings.Contains(a.GetSystemPrompt(), designSkillMarker) {
			t.Errorf("mode %q must not activate the design-system skill", mode)
		}
		a.Shutdown()
	}

	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	a.SetWorkspaceMode(" Design ")
	a.autoActivateModeSkills()
	a.autoActivateModeSkills()
	if n := strings.Count(a.GetSystemPrompt(), designSkillMarker); n != 1 {
		t.Errorf("design mode must fold the skill exactly once across turns, got %d", n)
	}

	// A prompt rebuilt between turns gets the skill back on the next turn.
	a.SetSystemPrompt("rebuilt prompt")
	a.autoActivateModeSkills()
	if !strings.Contains(a.GetSystemPrompt(), designSkillMarker) {
		t.Error("design mode must re-fold the skill after a prompt rebuild")
	}
}

// TestDesignerSubagentActivatesSkill pins that a designer subagent is
// spawned with the design-system skill already in its prompt.
func TestDesignerSubagentActivatesSkill(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 200_000)
	defer parent.Shutdown()

	opts := subagentProfileOpts()
	opts.Persona = "designer"

	sub, err := runner.createSubagent(opts, context.Background())
	if err != nil {
		t.Fatalf("createSubagent(designer) failed: %v", err)
	}
	defer sub.Shutdown()

	if got := sub.GetActivePersona(); got != personas.IDDesigner {
		t.Errorf("subagent persona = %q, want %q", got, personas.IDDesigner)
	}

	if !strings.Contains(sub.GetSystemPrompt(), designSkillMarker) {
		t.Error("designer subagent must carry the design-system skill active from spawn, but the prompt lacks the skill fold")
	}
}

// TestCoderSubagentDoesNotActivateDesignSkill guards against over-activation:
// only the designer persona gets the skill folded in at spawn. A coder
// subagent must not silently gain design workflow instructions.
func TestCoderSubagentDoesNotActivateDesignSkill(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 200_000)
	defer parent.Shutdown()

	sub, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err != nil {
		t.Fatalf("createSubagent(coder) failed: %v", err)
	}
	defer sub.Shutdown()

	if strings.Contains(sub.GetSystemPrompt(), designSkillMarker) {
		t.Error("coder subagent must NOT carry the design-system skill fold — only the designer does")
	}
}

// TestRootAgentHasDesignAndSkillTools pins the toolset the design workflow
// depends on. The prompts and the design-system skill direct the root agent
// to these tools; a persona or low-context allowlist that drops any of them
// leaves the agent instructed to use tools it cannot call.
func TestRootAgentHasDesignAndSkillTools(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	toolNames := func() map[string]bool {
		names := map[string]bool{}
		for _, tool := range a.getOptimizedToolDefinitions(nil) {
			names[tool.Function.Name] = true
		}
		return names
	}
	core := []string{"activate_skill", "list_skills", "design_assets", "design_validate", "design_brief", "design_export_tokens", "design_sync"}

	full := toolNames()
	for _, name := range append(core, "design_render", "design_critique", "design_import_sketch") {
		if !full[name] {
			t.Errorf("full-context root agent is missing %s", name)
		}
	}

	lcm, err := configuration.ResolveContextProfile(&configuration.Config{ContextMode: configuration.ContextModeLowContext}, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.contextProfile = lcm
	low := toolNames()
	for _, name := range core {
		if !low[name] {
			t.Errorf("low-context root agent is missing %s", name)
		}
	}
}

func TestSkillDeclaredTools(t *testing.T) {
	cases := map[string][]string{
		"---\nname: X\ntools: a_b, c\n---\nbody":         {"a_b", "c"},
		"---\nname: X\ntools: [a, \"b\"]\n---\nbody":     {"a", "b"},
		"---\nname: X\n---\ntools: not-frontmatter\n":    nil,
		"no frontmatter\ntools: a\n":                     nil,
		"\ufeff---\ntools: a\ndescription: d\n---\nbody": {"a"},
	}
	for content, want := range cases {
		if got := skillDeclaredTools(content); !slices.Equal(got, want) {
			t.Errorf("skillDeclaredTools(%q) = %v, want %v", content, got, want)
		}
	}
}

// TestActivateSkillByID_NotesToolsThisHostLacks: the design skill declares
// its tools; a host that cannot call some of them gets the skill plus a note
// naming them, and a host with all of them gets no note.
func TestActivateSkillByID_NotesToolsThisHostLacks(t *testing.T) {
	const note = "Not available in this environment"

	full := newIsolatedTestAgent(t)
	defer full.Shutdown()
	if _, err := full.activateSkillByID(skills.SkillIDDesignSystem); err != nil {
		t.Fatalf("activateSkillByID: %v", err)
	}
	if strings.Contains(full.GetSystemPrompt(), note) {
		t.Error("a host with every declared tool must not get the unavailable-tools note")
	}

	low := newIsolatedTestAgent(t)
	defer low.Shutdown()
	profile, err := configuration.ResolveContextProfile(&configuration.Config{ContextMode: configuration.ContextModeLowContext}, 0)
	if err != nil {
		t.Fatal(err)
	}
	low.contextProfile = profile
	if _, err := low.activateSkillByID(skills.SkillIDDesignSystem); err != nil {
		t.Fatalf("activateSkillByID: %v", err)
	}
	prompt := low.GetSystemPrompt()
	if !strings.Contains(prompt, note) || !strings.Contains(prompt, "`design_render`") {
		t.Error("the low-context host lacks design_render; the fold must say so")
	}
	if strings.Contains(prompt[strings.Index(prompt, note):], "`design_validate`") {
		t.Error("design_validate is available in low-context mode and must not be listed as missing")
	}
}
