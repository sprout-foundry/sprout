//go:build js && wasm

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

type fakePersonaAgent struct {
	cleared   bool
	prompt    string
	applied   []string
	appliedFn []personas.Definition
	fail      error
}

func (f *fakePersonaAgent) ApplyPersona(id string) error {
	if f.fail != nil {
		return f.fail
	}
	f.applied = append(f.applied, id)
	f.prompt = "persona prompt for " + id
	return nil
}

func (f *fakePersonaAgent) ApplyPersonaDefinition(def personas.Definition) error {
	if f.fail != nil {
		return f.fail
	}
	f.appliedFn = append(f.appliedFn, def)
	f.prompt = "persona prompt for " + def.ID
	return nil
}

func (f *fakePersonaAgent) ClearActivePersona() { f.cleared = true }

func (f *fakePersonaAgent) GetSystemPrompt() string  { return f.prompt }
func (f *fakePersonaAgent) SetSystemPrompt(p string) { f.prompt = p }

func TestParseAgentRunOptions(t *testing.T) {
	opts, err := parseAgentRunOptions("")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if opts.personaID != "" || opts.personaDef != nil || opts.Context != "" {
		t.Fatalf("empty options parsed to %+v", opts)
	}

	opts, err = parseAgentRunOptions(`{"persona": " designer ", "context": " offer: web apps "}`)
	if err != nil {
		t.Fatal(err)
	}
	if opts.personaID != "designer" || opts.Context != "offer: web apps" {
		t.Errorf("opts = %+v", opts)
	}
	if _, err := parseAgentRunOptions(`{"persona":`); err == nil {
		t.Error("malformed options accepted")
	}
	if _, err := parseAgentRunOptions(`{"persona": 42}`); err == nil {
		t.Error("a persona that is neither a string nor an object must be rejected")
	}
}

// TestParseAgentRunOptions_PersonaDefinition pins the definition form of the
// persona option (SP-175): an object is decoded as a PersonaDefinition and
// applied through the definition path, not the id path.
func TestParseAgentRunOptions_PersonaDefinition(t *testing.T) {
	opts, err := parseAgentRunOptions(`{"persona": {
		"id": "foundry_planner",
		"name": "Foundry Planner",
		"description": "Plans work for the platform",
		"system_prompt_text": "You plan.",
		"allowed_tools": ["read_file"],
		"host_tools": ["*"],
		"skills": ["project-planning"],
		"mcp": ["github"]
	}}`)
	if err != nil {
		t.Fatal(err)
	}
	if opts.personaID != "" {
		t.Errorf("personaID = %q, want empty for a definition", opts.personaID)
	}
	if opts.personaDef == nil {
		t.Fatal("personaDef is nil")
	}
	def := opts.personaDef
	if def.ID != "foundry_planner" || def.SystemPromptText != "You plan." {
		t.Errorf("definition = %+v", def)
	}
	if len(def.HostTools) != 1 || def.HostTools[0] != "*" {
		t.Errorf("host_tools = %v", def.HostTools)
	}
	if len(def.Skills) != 1 || def.Skills[0] != "project-planning" {
		t.Errorf("skills = %v", def.Skills)
	}
	if len(def.MCP) != 1 || def.MCP[0] != "github" {
		t.Errorf("mcp = %v", def.MCP)
	}
}

func TestApplyAgentRunOptions_PersonaThenContext(t *testing.T) {
	ag := &fakePersonaAgent{prompt: "base"}
	opts, err := parseAgentRunOptions(`{"persona": "designer", "context": "Only web apps."}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyAgentRunOptions(ag, opts); err != nil {
		t.Fatal(err)
	}
	if len(ag.applied) != 1 || ag.applied[0] != "designer" {
		t.Errorf("applied = %v", ag.applied)
	}
	// The persona replaced the prompt; the context is appended after it.
	if !strings.HasPrefix(ag.prompt, "persona prompt for designer") ||
		!strings.HasSuffix(ag.prompt, hostContextHeading+"\n\nOnly web apps.") {
		t.Errorf("prompt = %q", ag.prompt)
	}
}

func TestApplyAgentRunOptions_NoOptionsLeavesAgent(t *testing.T) {
	ag := &fakePersonaAgent{prompt: "base"}
	if err := applyAgentRunOptions(ag, agentRunOptions{}); err != nil {
		t.Fatal(err)
	}
	if !ag.cleared {
		t.Error("no persona must clear any persona the agent starts with")
	}
	if ag.prompt != "base" || len(ag.applied) != 0 {
		t.Errorf("agent changed: %+v", ag)
	}
}

func TestApplyAgentRunOptions_UnknownPersona(t *testing.T) {
	ag := &fakePersonaAgent{fail: errors.New("persona nope not found")}
	err := applyAgentRunOptions(ag, agentRunOptions{personaID: "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v", err)
	}
}
