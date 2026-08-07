package agent

import (
	"sort"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// BuildToolDefinitions converts all handler-based tool definitions into the
// []api.Tool shape the LLM, persona allowlist, and MCP-merge code paths expect.
//
// mcp_tools is added as a synthetic entry because it is a meta-tool
// handled outside the registry (see pkg/agent/mcp.go::handleMCPToolsCommand
// and pkg/agent/tools.go's mcp_tools dispatch). Removing it would hide
// MCP discovery from the model.
func BuildToolDefinitions() []api.Tool {
	allHandlers := tools.GetNewToolRegistry().All()
	result := make([]api.Tool, 0, len(allHandlers)+1)
	for _, h := range allHandlers {
		// Superseded tools stay in the registry so direct calls still resolve,
		// but are not advertised — every advertised schema costs context on
		// every turn, and two tools that do the same job also cost the model a
		// choice it should not have to make.
		if h.Definition().Hidden {
			continue
		}
		result = append(result, convertHandlerToAPITool(h))
	}
	result = append(result, mcpToolsSyntheticEntry())
	sort.Slice(result, func(i, j int) bool {
		return result[i].Function.Name < result[j].Function.Name
	})
	return result
}

// BuildToolDefinitionsForAgent is BuildToolDefinitions filtered to the tools
// the given agent can actually execute. Tools marked RequiresEmbeddings are
// dropped when the agent has no embedding manager (the default — embeddings
// are OPT-IN), keeping this roster in sync with the seed registry filter in
// seed_tool_registry.go so the model is never offered a tool that would fail
// at call time.
func BuildToolDefinitionsForAgent(a *Agent) []api.Tool {
	tools := BuildToolDefinitions()
	if a == nil || a.GetEmbeddingManager() != nil {
		return tools
	}
	filtered := make([]api.Tool, 0, len(tools))
	for _, t := range tools {
		if toolRequiresEmbeddings(t.Function.Name) {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// toolRequiresEmbeddings reports whether the named tool is registered with
// RequiresEmbeddings=true in the handler registry. Unknown tools return false.
func toolRequiresEmbeddings(name string) bool {
	h, ok := tools.GetNewToolRegistry().Lookup(name)
	return ok && h.Definition().RequiresEmbeddings
}

// convertHandlerToAPITool converts a ToolHandler into an api.Tool for LLM consumption.
func convertHandlerToAPITool(h tools.ToolHandler) api.Tool {
	def := h.Definition()
	properties := make(map[string]api.ToolParameter, len(def.Parameters))
	required := make([]string, 0)
	requiredSet := make(map[string]struct{}, len(def.Required))
	for _, rn := range def.Required {
		requiredSet[rn] = struct{}{}
	}
	for _, p := range def.Parameters {
		properties[p.Name] = api.ToolParameter{
			Type:        p.Type,
			Description: p.Description,
		}
		if p.Required {
			required = append(required, p.Name)
		} else if _, ok := requiredSet[p.Name]; ok {
			required = append(required, p.Name)
		}
	}
	return api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        def.Name,
			Description: def.Description,
			Parameters: api.ToolParameters{
				Type:       "object",
				Properties: properties,
				Required:   required,
			},
		},
	}
}

func mcpToolsSyntheticEntry() api.Tool {
	return api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "mcp_tools",
			Description: "Access MCP server tools. Use action=\"list\" to discover available servers and their tools; use action=\"call\" with server+tool+arguments to invoke one.",
			Parameters: api.ToolParameters{
				Type: "object",
				Properties: map[string]api.ToolParameter{
					"action":    {Type: "string", Description: "Action to perform: 'list' to discover tools, 'call' to execute"},
					"server":    {Type: "string", Description: "Server name (optional for list, required for call)"},
					"tool":      {Type: "string", Description: "Tool name (required for call action)"},
					"arguments": {Type: "object", Description: "Arguments for the tool (required for call action)"},
				},
				Required: []string{"action"},
			},
		},
	}
}
