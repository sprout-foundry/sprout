package agent

import (
	"context"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

func seedToolNames(reg *core.ToolRegistry) map[string]bool {
	names := map[string]bool{}
	for _, t := range reg.GetTools() {
		names[t.Function.Name] = true
	}
	return names
}

func TestSeedToolRoster_PersonaLimitsRegistry(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	if err := agent.ApplyPersona(personas.IDDesigner); err != nil {
		t.Fatalf("apply designer: %v", err)
	}
	allowed := makeAllowedToolSet(agent.getActivePersonaToolAllowlist())

	names := seedToolNames(NewSeedToolRegistry(agent))
	if len(names) == 0 {
		t.Fatal("enforcement left no tools")
	}
	for name := range names {
		if _, ok := allowed[name]; !ok {
			t.Errorf("tool %q is registered but not in the designer's allowed_tools", name)
		}
	}
	if !names["design_render"] {
		t.Error("design_render (on the designer's list) is missing")
	}
}

func TestSeedToolRoster_NoPersonaKeepsEveryTool(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	if names := seedToolNames(NewSeedToolRegistry(agent)); !names["ask_user"] || !names["write_file"] {
		t.Fatalf("with no persona every tool stays; got %d tools", len(names))
	}
}

// schemaTool is a registry tool defined by a full JSON schema.
type schemaTool struct{ def tools.ToolDefinition }

func (s *schemaTool) Name() string                     { return s.def.Name }
func (s *schemaTool) Definition() tools.ToolDefinition { return s.def }
func (s *schemaTool) Validate(map[string]any) error    { return nil }
func (s *schemaTool) Execute(context.Context, tools.ToolEnv, map[string]any) (tools.ToolResult, error) {
	return tools.ToolResult{Output: "ok"}, nil
}
func (s *schemaTool) Aliases() []string      { return nil }
func (s *schemaTool) Timeout() time.Duration { return 0 }
func (s *schemaTool) MaxResultSize() int     { return 0 }
func (s *schemaTool) SafeForParallel() bool  { return false }
func (s *schemaTool) Interactive() bool      { return false }

func TestSeedToolRoster_PersonaListedToolIsKept(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	probe := &schemaTool{def: tools.ToolDefinition{Name: "roster_host_probe", Description: "probe"}}
	reg := tools.GetNewToolRegistry()
	if err := reg.Register(probe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Unregister(probe.Name()) })

	// A registry tool registered at runtime (a host tool) is kept when the
	// persona names it, like any built-in tool.
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SubagentTypes["roster_probe"] = configuration.SubagentType{
			ID:           "roster_probe",
			Name:         "Roster probe",
			Enabled:      true,
			AllowedTools: []string{"ask_user", "roster_host_probe"},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.ApplyPersona("roster_probe"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	names := seedToolNames(NewSeedToolRegistry(agent))
	if len(names) != 2 || !names["ask_user"] || !names["roster_host_probe"] {
		t.Fatalf("registry = %v, want exactly ask_user and roster_host_probe", names)
	}
}

func TestWithHandlerSchemas_AdvertisesFullSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope": map[string]any{"type": "object", "properties": map[string]any{"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}},
		},
	}
	tool := &schemaTool{def: tools.ToolDefinition{
		Name:        "roster_schema_probe",
		Description: "probe",
		Parameters:  []tools.ParameterDef{{Name: "scope", Type: "object"}},
		Schema:      schema,
	}}
	reg := tools.GetNewToolRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Unregister(tool.Name()) })

	list := []core.Tool{
		{Type: "function", Function: core.ToolFunction{Name: "roster_schema_probe", Parameters: "rebuilt"}},
		{Type: "function", Function: core.ToolFunction{Name: "read_file", Parameters: "kept"}},
	}
	got := withHandlerSchemas(list)
	if params, ok := got[0].Function.Parameters.(map[string]any); !ok || params["properties"] == nil {
		t.Errorf("schema tool advertises %v, want its own schema", got[0].Function.Parameters)
	}
	if got[1].Function.Parameters != "kept" {
		t.Errorf("a tool without a schema changed: %v", got[1].Function.Parameters)
	}
	if list[0].Function.Parameters != "rebuilt" {
		t.Error("the input slice was modified")
	}

	// The model-facing roster (BuildToolDefinitions) carries it too.
	for _, def := range BuildToolDefinitions() {
		if def.Function.Name == "roster_schema_probe" {
			if params, ok := def.Function.Parameters.(map[string]any); !ok || params["properties"] == nil {
				t.Errorf("BuildToolDefinitions lost the schema: %v", def.Function.Parameters)
			}
		}
	}
}
