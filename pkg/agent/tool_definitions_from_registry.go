package agent

import (
	"sort"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// BuildToolDefinitions converts the canonical tool registry
// (pkg/agent/tool_registrations.go) into the []api.Tool shape the LLM,
// persona allowlist, and MCP-merge code paths expect. Replaces the legacy
// pkg/agent_api/tools.go GetToolDefinitions() table — that file was a
// duplicate, hand-maintained source of tool definitions that drifted from
// this registry over time.
//
// The seed core also receives these tools (via Executor.GetTools on the
// seedRegistry built from the same ToolConfig entries), so the LLM sees
// exactly the descriptions defined in tool_registrations.go.
//
// mcp_tools is added as a synthetic entry because it is a meta-tool
// handled outside the registry (see pkg/agent/mcp.go::handleMCPToolsCommand
// and pkg/agent/tools.go's mcp_tools dispatch). Removing it would hide
// MCP discovery from the model.
func BuildToolDefinitions() []api.Tool {
	configs := GetToolRegistry().GetAllToolConfigs()
	tools := make([]api.Tool, 0, len(configs)+1)
	for _, cfg := range configs {
		tools = append(tools, toolConfigToAPITool(cfg))
	}
	tools = append(tools, mcpToolsSyntheticEntry())
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Function.Name < tools[j].Function.Name
	})
	return tools
}

func toolConfigToAPITool(cfg ToolConfig) api.Tool {
	properties := make(map[string]api.ToolParameter, len(cfg.Parameters))
	required := make([]string, 0, len(cfg.Parameters))
	for _, p := range cfg.Parameters {
		properties[p.Name] = api.ToolParameter{
			Type:        p.Type,
			Description: p.Description,
		}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        cfg.Name,
			Description: cfg.Description,
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
