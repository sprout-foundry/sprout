package agent

// persona_custom_test.go — custom personas (SP-175).
//
// A custom persona is owned entirely by its creator: its prompt, its tools,
// its skills, its MCP servers, where it may write. These tests pin that the
// roster is exactly what it declares, that a host wildcard picks up a tool
// registered later, and that "complete control" never widens a runtime safety
// gate.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// hostProbeTool is a registry tool a host registered at run time.
type hostProbeTool struct{ name string }

func (h *hostProbeTool) Name() string { return h.name }
func (h *hostProbeTool) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{Name: h.name, Description: "host probe"}
}
func (h *hostProbeTool) Validate(map[string]any) error { return nil }
func (h *hostProbeTool) Execute(context.Context, tools.ToolEnv, map[string]any) (tools.ToolResult, error) {
	return tools.ToolResult{Output: "ok"}, nil
}
func (h *hostProbeTool) Aliases() []string      { return nil }
func (h *hostProbeTool) Timeout() time.Duration { return 0 }
func (h *hostProbeTool) MaxResultSize() int     { return 0 }
func (h *hostProbeTool) SafeForParallel() bool  { return false }
func (h *hostProbeTool) Interactive() bool      { return false }
func (h *hostProbeTool) HostProvidedTool() bool { return true }

// addUserPersona writes a user-scope persona file and re-hydrates the agent's
// configuration, the way a real load does.
func addUserPersona(t *testing.T, agent *Agent, id, body string) {
	t.Helper()
	dir, err := configuration.UserPersonasDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rehydrateUserPersonas(t, agent)
}

// rehydrateUserPersonas re-runs the hydration on the live config.
func rehydrateUserPersonas(t *testing.T, agent *Agent) {
	t.Helper()
	if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		configuration.HydrateCustomPersonas(cfg, "")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCustomPersona_RosterIsExactlyWhatItDeclares is the core promise: a
// user-scope persona loads, applies, and the tool set is exactly its
// `allowed_tools` plus its host tools plus the skill tools.
func TestCustomPersona_RosterIsExactlyWhatItDeclares(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	probe := &hostProbeTool{name: "foundry_account_lookup"}
	reg := tools.GetNewToolRegistry()
	if err := reg.Register(probe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Unregister(probe.Name()) })

	addUserPersona(t, agent, "foundry", `{"personas":[{
		"id": "foundry",
		"name": "Foundry",
		"description": "Platform planner",
		"system_prompt_text": "You plan platform work.",
		"allowed_tools": ["read_file", "search_files"],
		"host_tools": ["*"],
		"skills": ["project-planning"],
		"mcp": ["github"]
	}]}`)

	persona := agent.resolvePersona("foundry")
	if persona == nil {
		t.Fatalf("custom persona did not load; issues: %+v", agent.GetConfig().PersonaIssues)
	}
	if persona.Source != string(personas.SourceUser) {
		t.Fatalf("Source = %q, want user", persona.Source)
	}

	if err := agent.ApplyPersona("foundry"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := agent.GetActivePersona(); got != "foundry" {
		t.Fatalf("active persona = %q", got)
	}
	if prompt := agent.GetSystemPrompt(); prompt != "You plan platform work." {
		t.Fatalf("system prompt = %q", prompt)
	}

	roster := agent.getActivePersonaToolAllowlist()
	want := []string{"activate_skill", "foundry_account_lookup", "list_skills", "read_file", "search_files"}
	slices.Sort(roster)
	if !slices.Equal(roster, want) {
		t.Fatalf("roster = %v, want %v", roster, want)
	}

	// And the registry the model is offered is limited to it.
	names := seedToolNames(NewSeedToolRegistry(agent))
	for name := range names {
		if !slices.Contains(roster, name) {
			t.Errorf("tool %q is registered but not declared by the persona", name)
		}
	}
	if !names["read_file"] || names["write_file"] {
		t.Errorf("registry = %v", names)
	}
}

// TestCustomPersona_HostWildcardSeesLateRegistration pins that
// host_tools: ["*"] resolves at roster-build time, so a tool the host
// registers after the agent was created is still offered.
func TestCustomPersona_HostWildcardSeesLateRegistration(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	addUserPersona(t, agent, "late", `{"personas":[{
		"id": "late",
		"name": "Late",
		"description": "Waits for a host tool",
		"system_prompt_text": "hi",
		"allowed_tools": ["read_file"],
		"host_tools": ["*"]
	}]}`)
	if err := agent.ApplyPersona("late"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The persona is applied; the host registers its tool afterwards.
	probe := &hostProbeTool{name: "registered_after_create"}
	reg := tools.GetNewToolRegistry()
	if err := reg.Register(probe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Unregister(probe.Name()) })

	roster := agent.getActivePersonaToolAllowlist()
	if !slices.Contains(roster, "registered_after_create") {
		t.Fatalf("roster = %v, want the late-registered host tool", roster)
	}
}

// TestCustomPersona_AutoApproveCannotExceedGateOne pins decision 4: a
// persona's auto_approve_rules cannot auto-approve what the global classifier
// blocks.
func TestCustomPersona_AutoApproveCannotExceedGateOne(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Rules that mark everything low risk, as a maximally permissive creator
	// would write them.
	addUserPersona(t, agent, "permissive", `{"personas":[{
		"id": "permissive",
		"name": "Permissive",
		"description": "Tries to approve everything",
		"system_prompt_text": "hi",
		"allowed_tools": ["shell_command"],
		"auto_approve_rules": {
			"low_risk": ["git", "file_read", "file_write", "shell", "network", "unknown"],
			"high_risk_never": []
		}
	}]}`)

	persona := agent.resolvePersona("permissive")
	if persona == nil {
		t.Fatalf("persona did not load; issues: %+v", agent.GetConfig().PersonaIssues)
	}

	// Gate 1's critical set is absolute: no rule can lift it.
	if risk := persona.EvaluateOperationRisk("rm -rf /"); risk != configuration.RiskLevelCritical {
		t.Fatalf("rm -rf / risk = %v, want critical despite the persona's rules", risk)
	}
	if risk := persona.EvaluateOperationRisk("mkfs.ext4 /dev/sda1"); risk != configuration.RiskLevelCritical {
		t.Fatalf("mkfs risk = %v, want critical", risk)
	}
}

// TestCustomPersona_ComputerUseStillGated pins that a custom persona holding
// the computer_use capability is held to the activation gates: the flag, the
// top-level rule, and the non-interactive rule all still apply.
func TestCustomPersona_ComputerUseStillGated(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	addUserPersona(t, agent, "desk_user", `{"personas":[{
		"id": "desk_user",
		"name": "Desk User",
		"description": "Drives the desktop",
		"system_prompt_text": "hi",
		"allowed_tools": ["take_screenshot"],
		"capabilities": ["computer_use"]
	}]}`)
	if agent.resolvePersona("desk_user") == nil {
		t.Fatalf("persona did not load; issues: %+v", agent.GetConfig().PersonaIssues)
	}

	// computer_use is off by default; the capability cannot switch it on.
	if err := agent.ApplyPersona("desk_user"); err == nil {
		t.Fatal("a custom persona activated computer use with the gate closed")
	}
}

// TestCustomPersona_MCPSelectors pins the `mcp` selector grammar and that a
// custom persona with no `mcp` gets no MCP tool at all.
func TestCustomPersona_MCPSelectors(t *testing.T) {
	cases := []struct {
		name      string
		selectors []string
		server    string
		tool      string
		want      bool
	}{
		{"absent: none", nil, "github", "read", false},
		{"wildcard", []string{"*"}, "github", "read", true},
		{"server covers its tools", []string{"github"}, "github", "delete_repo", true},
		{"server does not cover another server", []string{"github"}, "gitlab", "read", false},
		{"tool entry covers that tool", []string{"github__read"}, "github", "read", true},
		{"tool entry does not cover a sibling", []string{"github__read"}, "github", "delete_repo", false},
		{"whole-segment: acme never matches acmecorp", []string{"acme"}, "acmecorp", "read", false},
		{"underscore in server name", []string{"my_server__read"}, "my_server", "read", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpSelectorAllows(tt.selectors, tt.server, tt.tool); got != tt.want {
				t.Errorf("mcpSelectorAllows(%v, %q, %q) = %v, want %v",
					tt.selectors, tt.server, tt.tool, got, tt.want)
			}
		})
	}
}

// TestCustomPersona_MCPGateIsPerPersona checks personaMCPAllows: a core
// persona keeps today's behaviour, a custom one is limited to its selectors.
func TestCustomPersona_MCPGateIsPerPersona(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Core persona: the gate does not narrow (allowed_tools does).
	if err := agent.ApplyPersona(personas.IDGeneral); err != nil {
		t.Fatal(err)
	}
	if !agent.personaMCPAllows("github", "read") {
		t.Error("a core persona's MCP gate must not narrow")
	}

	addUserPersona(t, agent, "no_mcp", `{"personas":[{
		"id": "no_mcp",
		"name": "No MCP",
		"description": "x",
		"system_prompt_text": "hi",
		"allowed_tools": ["read_file"]
	}]}`)
	if err := agent.ApplyPersona("no_mcp"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if agent.personaMCPAllows("github", "read") {
		t.Error("a custom persona without `mcp` must be allowed no MCP tool")
	}

	addUserPersona(t, agent, "with_mcp", `{"personas":[{
		"id": "with_mcp",
		"name": "With MCP",
		"description": "x",
		"system_prompt_text": "hi",
		"allowed_tools": ["read_file"],
		"mcp": ["github__read"]
	}]}`)
	if err := agent.ApplyPersona("with_mcp"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !agent.personaMCPAllows("github", "read") {
		t.Error("a selected MCP tool was denied")
	}
	if agent.personaMCPAllows("github", "delete_repo") {
		t.Error("an unselected MCP tool was allowed")
	}
}

// TestDefinePersona_RejectsCoreCollision covers the host scope: a definition
// minted in memory cannot shadow a core persona.
func TestDefinePersona_RejectsCoreCollision(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	err := agent.DefinePersona(personas.Definition{
		ID: "planner", Name: "Fake Planner", Description: "x", SystemPromptText: "mine",
	})
	if err == nil {
		t.Fatal("a host definition reused a core persona id")
	}
	if agent.resolvePersona("planner").Source != string(personas.SourceCore) {
		t.Fatal("the core planner was replaced")
	}

	// A free id works, and the definition is applied.
	if err := agent.ApplyPersonaDefinition(personas.Definition{
		ID: "foundry_planner", Name: "Foundry Planner", Description: "Plans platform work",
		SystemPromptText: "You plan.", AllowedTools: []string{"read_file"},
	}); err != nil {
		t.Fatalf("apply definition: %v", err)
	}
	if agent.GetActivePersona() != "foundry_planner" {
		t.Fatalf("active persona = %q", agent.GetActivePersona())
	}
	if got := agent.getActivePersonaToolAllowlist(); !slices.Equal(got, []string{"read_file"}) {
		t.Fatalf("roster = %v", got)
	}
	if !slices.Contains(agent.HostPersonaIDs(), "foundry_planner") {
		t.Errorf("host persona ids = %v", agent.HostPersonaIDs())
	}
}

// TestCustomPersona_SkillsRestrictSkillTools pins that naming skills offers
// the two skill tools and that list_skills/activate_skill honour the list.
func TestCustomPersona_SkillsRestrictSkillTools(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	addUserPersona(t, agent, "one_skill", `{"personas":[{
		"id": "one_skill",
		"name": "One Skill",
		"description": "x",
		"system_prompt_text": "hi",
		"allowed_tools": ["read_file"],
		"skills": ["project-planning"]
	}]}`)
	if err := agent.ApplyPersona("one_skill"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	roster := agent.getActivePersonaToolAllowlist()
	if !slices.Contains(roster, "activate_skill") || !slices.Contains(roster, "list_skills") {
		t.Fatalf("roster = %v, want the skill tools", roster)
	}
	if got := agent.activePersonaSkills(); !slices.Equal(got, []string{"project-planning"}) {
		t.Fatalf("activePersonaSkills = %v", got)
	}

	// A core persona places no skill restriction (its `skills` field is not
	// part of its shipped definition).
	if err := agent.ApplyPersona(personas.IDGeneral); err != nil {
		t.Fatal(err)
	}
	if got := agent.activePersonaSkills(); got != nil {
		t.Fatalf("core persona skill restriction = %v, want none", got)
	}

	// A custom persona that declares no `skills` but lists the skill tools in
	// allowed_tools gets deny-all ("absent means none"), not unrestricted.
	addUserPersona(t, agent, "no_skills", `{"personas":[{
		"id": "no_skills",
		"name": "No Skills",
		"description": "x",
		"system_prompt_text": "hi",
		"allowed_tools": ["activate_skill", "list_skills"]
	}]}`)
	if err := agent.ApplyPersona("no_skills"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := agent.activePersonaSkills(); got == nil || len(got) != 0 {
		t.Fatalf("no-skills persona restriction = %v, want an empty deny-all list", got)
	}
}

// TestCustomPersona_ConfinementUsesTheEffectiveSet pins that a confined
// custom persona's execution-time check matches its roster: host tools its
// `host_tools` selects are allowed, a tool outside the set is not, and a
// write outside `write_paths` is refused.
func TestCustomPersona_ConfinementUsesTheEffectiveSet(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	probe := &hostProbeTool{name: "foundry_probe"}
	reg := tools.GetNewToolRegistry()
	if err := reg.Register(probe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Unregister(probe.Name()) })

	addUserPersona(t, agent, "confined", `{"personas":[{
		"id": "confined",
		"name": "Confined",
		"description": "x",
		"system_prompt_text": "hi",
		"allowed_tools": ["write_file"],
		"host_tools": ["*"],
		"write_paths": ["docs/"]
	}]}`)
	if err := agent.ApplyPersona("confined"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if err := agent.checkPersonaConfinement("foundry_probe", nil); err != nil {
		t.Errorf("a host tool the persona selects was refused: %v", err)
	}
	if err := agent.checkPersonaConfinement("shell_command", nil); err == nil {
		t.Error("a tool outside the persona's set was allowed")
	}
	if err := agent.checkPersonaConfinement("write_file", map[string]interface{}{"path": "src/x.go"}); err == nil {
		t.Error("a write outside write_paths was allowed")
	}
	if err := agent.checkPersonaConfinement("write_file", map[string]interface{}{"path": "docs/ok.md"}); err != nil {
		t.Errorf("a write inside write_paths was refused: %v", err)
	}
}

// TestCustomPersona_ConfinementAllowsSelectedMCPTools pins that the
// execution-time gate and the roster agree for a confined custom persona's
// `mcp` selectors: a selected MCP tool passes confinement, an unselected one
// is refused.
func TestCustomPersona_ConfinementAllowsSelectedMCPTools(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	addUserPersona(t, agent, "mcp_confined", `{"personas":[{
		"id": "mcp_confined",
		"name": "MCP Confined",
		"description": "x",
		"system_prompt_text": "hi",
		"allowed_tools": ["read_file"],
		"mcp": ["github__read"],
		"write_paths": ["docs/"]
	}]}`)
	if err := agent.ApplyPersona("mcp_confined"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The execution gate resolves the name and lets the selected tool
	// through, even though it is not in allowed_tools (no server is
	// registered, so the empty-name fallback split applies).
	if err := agent.checkPersonaConfinement("mcp_github_read", nil); err != nil {
		t.Errorf("a selected MCP tool was refused at execution: %v", err)
	}
	if err := agent.checkPersonaConfinement("mcp_github_delete_repo", nil); err == nil {
		t.Error("an unselected MCP tool was allowed at execution")
	}
}
