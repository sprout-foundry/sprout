package agent

import (
	core "github.com/sprout-foundry/seed/core"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// personaToolAllowSet is the set of tool names the seed registry is limited
// to, or nil for no limit. The active persona's allowed_tools are the agent's
// whole tool set: the registry built for each turn holds only the tools the
// persona names, so the model is offered nothing else and a call to anything
// else is an unknown tool. No active persona, or a persona without
// allowed_tools, keeps every tool. The rules are the ones
// getOptimizedToolDefinitions applies — the root agent's low-context
// allowlist stays final, and a subagent narrows it further.
func personaToolAllowSet(a *Agent) map[string]struct{} {
	if a == nil {
		return nil
	}
	if len(a.contextProfile.ToolAllowlist) > 0 && !a.IsSubagent() {
		return nil
	}
	allowlist := a.getActivePersonaToolAllowlist()
	if len(allowlist) == 0 {
		return nil
	}
	return makeAllowedToolSet(allowlist)
}

// withHandlerSchemas replaces a tool's advertised parameters with its
// handler's own JSON schema (ToolDefinition.Schema) where one is set. The
// seed registry rebuilds each schema from the flat parameter list, which
// keeps names, types and descriptions only; a tool defined by a full schema
// (a host tool) would otherwise lose its nested properties, enums and array
// items.
func withHandlerSchemas(list []core.Tool) []core.Tool {
	reg := tools.GetNewToolRegistry()
	var out []core.Tool
	for i, t := range list {
		h, ok := reg.Lookup(t.Function.Name)
		if !ok {
			continue
		}
		schema := h.Definition().Schema
		if schema == nil {
			continue
		}
		if out == nil {
			out = make([]core.Tool, len(list))
			copy(out, list)
		}
		out[i].Function.Parameters = schema
	}
	if out == nil {
		return list
	}
	return out
}
