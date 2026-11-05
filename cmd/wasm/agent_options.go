//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// agentRunOptions are the per-chat settings a host passes to runAgent
// (args[7], a JSON object). They shape the chat's agent when it is built, so
// a change to them rebuilds the agent, as a provider change does.
type agentRunOptions struct {
	// Persona is the persona id (or alias) the chat's agent runs as: its
	// system prompt, and its allowed_tools as the agent's whole tool set
	// (host tools included only when the persona names them). Empty keeps
	// the default agent.
	Persona string `json:"persona,omitempty"`
	// Context is host-provided text appended to the system prompt (what the
	// host offers, its constraints), so the agent plans within it.
	Context string `json:"context,omitempty"`
}

// parseAgentRunOptions decodes runAgent's options argument. An empty string
// is no options; malformed JSON is an error, so a host's typo is not silently
// dropped and the agent run with the wrong persona.
func parseAgentRunOptions(raw string) (agentRunOptions, error) {
	var opts agentRunOptions
	if strings.TrimSpace(raw) == "" {
		return opts, nil
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return opts, fmt.Errorf("parse agent options: %w", err)
	}
	opts.Persona = strings.TrimSpace(opts.Persona)
	opts.Context = strings.TrimSpace(opts.Context)
	return opts, nil
}

// personaApplier is the part of *agent.Agent the options are applied
// through; an interface so the logic is testable without building an agent.
type personaApplier interface {
	ApplyPersona(personaID string) error
	ClearActivePersona()
	GetSystemPrompt() string
	SetSystemPrompt(prompt string)
}

// hostContextHeading introduces the host's context in the system prompt.
const hostContextHeading = "## Context from the host"

// applyAgentRunOptions selects the persona and appends the host context. The
// persona goes first: applying it replaces the system prompt, and the context
// must survive that. A persona is held to its allowed_tools, as every
// persona is. No persona clears any persona the agent would otherwise start
// with (a saved one), so the host gets the default agent it asked for.
func applyAgentRunOptions(ag personaApplier, opts agentRunOptions) error {
	if opts.Persona != "" {
		if err := ag.ApplyPersona(opts.Persona); err != nil {
			return fmt.Errorf("apply persona %q: %w", opts.Persona, err)
		}
	} else {
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
