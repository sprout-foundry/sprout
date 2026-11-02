//go:build js && wasm

package main

import (
	"errors"
	"strings"
	"testing"
)

type fakePersonaAgent struct {
	cleared bool
	prompt  string
	applied []string
	fail    error
}

func (f *fakePersonaAgent) ApplyPersona(id string) error {
	if f.fail != nil {
		return f.fail
	}
	f.applied = append(f.applied, id)
	f.prompt = "persona prompt for " + id
	return nil
}
func (f *fakePersonaAgent) ClearActivePersona() { f.cleared = true }

func (f *fakePersonaAgent) GetSystemPrompt() string  { return f.prompt }
func (f *fakePersonaAgent) SetSystemPrompt(p string) { f.prompt = p }

func TestParseAgentRunOptions(t *testing.T) {
	opts, err := parseAgentRunOptions("")
	if err != nil || opts != (agentRunOptions{}) {
		t.Fatalf("empty: %+v %v", opts, err)
	}
	opts, err = parseAgentRunOptions(`{"persona": " designer ", "context": " offer: web apps "}`)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Persona != "designer" || opts.Context != "offer: web apps" {
		t.Errorf("opts = %+v", opts)
	}
	if _, err := parseAgentRunOptions(`{"persona":`); err == nil {
		t.Error("malformed options accepted")
	}
}

func TestApplyAgentRunOptions_PersonaThenContext(t *testing.T) {
	ag := &fakePersonaAgent{prompt: "base"}
	if err := applyAgentRunOptions(ag, agentRunOptions{Persona: "designer", Context: "Only web apps."}); err != nil {
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
	err := applyAgentRunOptions(ag, agentRunOptions{Persona: "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("err = %v", err)
	}
}
