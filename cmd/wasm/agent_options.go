//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// agentRunOptions are the per-chat settings a host passes to runAgent
// (args[7], a JSON object). They shape the chat's agent when it is built, so
// a change to them rebuilds the agent, as a provider change does.
type agentRunOptions struct {
	// Persona is the persona the chat's agent runs as: a JSON string naming
	// a persona id or alias (a core persona, or one the host defined with
	// definePersona), or a definition object the host mints for this chat
	// (SP-175). Empty keeps the default agent.
	Persona json.RawMessage `json:"persona,omitempty"`
	// Context is host-provided text appended to the system prompt (what the
	// host offers, its constraints), so the agent plans within it.
	Context string `json:"context,omitempty"`

	// personaID is set when Persona was a string; personaDef when it was a
	// definition object. Exactly one is set, or neither.
	personaID  string
	personaDef *personas.Definition
}

// equal reports whether two option sets build the same agent. The cached
// agent is reused only when they do.
func (o agentRunOptions) equal(other agentRunOptions) bool {
	return o.personaID == other.personaID &&
		o.Context == other.Context &&
		string(o.Persona) == string(other.Persona)
}

// parseAgentRunOptions decodes runAgent's options argument. An empty string
// is no options; malformed JSON is an error, so a host's typo is not silently
// dropped and the agent run with the wrong persona. A persona that is neither
// a string nor an object is an error for the same reason.
func parseAgentRunOptions(raw string) (agentRunOptions, error) {
	var opts agentRunOptions
	if strings.TrimSpace(raw) == "" {
		return opts, nil
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return opts, fmt.Errorf("parse agent options: %w", err)
	}
	opts.Context = strings.TrimSpace(opts.Context)

	persona := strings.TrimSpace(string(opts.Persona))
	if persona == "" || persona == "null" || persona == `""` {
		opts.Persona = nil
		return opts, nil
	}
	switch persona[0] {
	case '"':
		var id string
		if err := json.Unmarshal(opts.Persona, &id); err != nil {
			return opts, fmt.Errorf("parse agent options persona: %w", err)
		}
		opts.personaID = strings.TrimSpace(id)
	case '{':
		var def personas.Definition
		if err := json.Unmarshal(opts.Persona, &def); err != nil {
			return opts, fmt.Errorf("parse agent persona definition: %w", err)
		}
		opts.personaDef = &def
	default:
		return opts, fmt.Errorf("agent options persona must be a persona id (string) or a definition (object)")
	}
	return opts, nil
}

// personaApplier is the part of *agent.Agent the options are applied
// through; an interface so the logic is testable without building an agent.
type personaApplier interface {
	ApplyPersona(personaID string) error
	ApplyPersonaDefinition(def personas.Definition) error
	ClearActivePersona()
	GetSystemPrompt() string
	SetSystemPrompt(prompt string)
}

// hostContextHeading introduces the host's context in the system prompt.
const hostContextHeading = "## Context from the host"

// applyAgentRunOptions selects the persona and appends the host context. The
// persona goes first: applying it replaces the system prompt, and the context
// must survive that. A persona is held to its own tool set, as every persona
// is. No persona clears any persona the agent would otherwise start with (a
// saved one), so the host gets the default agent it asked for.
func applyAgentRunOptions(ag personaApplier, opts agentRunOptions) error {
	switch {
	case opts.personaDef != nil:
		if err := ag.ApplyPersonaDefinition(*opts.personaDef); err != nil {
			return fmt.Errorf("apply persona definition: %w", err)
		}
	case opts.personaID != "":
		if err := ag.ApplyPersona(opts.personaID); err != nil {
			return fmt.Errorf("apply persona %q: %w", opts.personaID, err)
		}
	default:
		ag.ClearActivePersona()
	}
	if opts.Context != "" {
		block := hostContextHeading + "\n\n" + opts.Context
		if current := strings.TrimSpace(ag.GetSystemPrompt()); current != "" {
			ag.SetSystemPrompt(ag.GetSystemPrompt() + "\n\n---\n\n" + block)
		} else {
			ag.SetSystemPrompt(block)
		}
	}
	return nil
}
