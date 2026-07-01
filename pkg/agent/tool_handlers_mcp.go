package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

func handleMCPRefresh(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	operation, err := getMCPStringArg(args, "operation")
	if err != nil {
		return "", fmt.Errorf("operation is required")
	}

	switch operation {
	case "list":
		return handleMCPList(agent)
	case "refresh":
		return handleMCPRefreshConfig(ctx, agent)
	case "add":
		return handleMCPAdd(ctx, agent, args)
	case "remove":
		return handleMCPRemove(ctx, agent, args)
	default:
		return "", fmt.Errorf("unknown operation %q: must be one of: list, refresh, add, remove", operation)
	}
}

func handleMCPList(agent *Agent) (string, error) {
	mgr := agent.mcpSub.GetManager()
	if mgr == nil {
		return "", fmt.Errorf("MCP manager is not available")
	}

	servers := mgr.ListServers()
	result := struct {
		Operation string                   `json:"operation"`
		Servers   []map[string]interface{} `json:"servers"`
	}{
		Operation: "list",
		Servers:   make([]map[string]interface{}, 0, len(servers)),
	}

	for _, srv := range servers {
		cfg := srv.GetConfig()
		entry := map[string]interface{}{
			"name":        cfg.Name,
			"type":        cfg.Type,
			"command":     cfg.Command,
			"args":        cfg.Args,
			"url":         cfg.URL,
			"working_dir": cfg.WorkingDir,
			"auto_start":  cfg.AutoStart,
			"running":     srv.IsRunning(),
		}
		if cfg.Env != nil {
			keys := make([]string, 0, len(cfg.Env))
			for k := range cfg.Env {
				keys = append(keys, k)
			}
			entry["env_keys"] = keys
		}
		if cfg.HasCredentials() {
			entry["has_credentials"] = true
		}
		result.Servers = append(result.Servers, entry)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func handleMCPRefreshConfig(ctx context.Context, agent *Agent) (string, error) {
	if err := agent.RefreshRuntimeConfig(ctx); err != nil {
		return "", errors.NewTool("mcp", "refresh MCP", err)
	}

	result := map[string]interface{}{
		"operation": "refresh",
		"status":    "ok",
		"message":   "Configuration reloaded and MCP servers reconciled",
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func handleMCPAdd(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	name, err := getMCPStringArg(args, "name")
	if err != nil {
		return "", fmt.Errorf("name is required for add operation")
	}

	serverType, _ := getMCPStringArg(args, "type")
	if serverType == "" {
		serverType = "stdio"
	}

	command, _ := getMCPStringArg(args, "command")
	url, _ := getMCPStringArg(args, "url")
	workingDir, _ := getMCPStringArg(args, "working_dir")

	// Parse args array
	var cmdArgs []string
	if argsRaw, ok := args["args"]; ok {
		if arr, ok := argsRaw.([]interface{}); ok {
			cmdArgs = make([]string, len(arr))
			for i, v := range arr {
				cmdArgs[i] = fmt.Sprintf("%v", v)
			}
		}
	}

	// Parse env map
	var envMap map[string]string
	if envRaw, ok := args["env"]; ok {
		if m, ok := envRaw.(map[string]interface{}); ok {
			envMap = make(map[string]string, len(m))
			for k, v := range m {
				envMap[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	serverConfig := mcp.MCPServerConfig{
		Name:        name,
		Type:        serverType,
		Command:     command,
		Args:        cmdArgs,
		URL:         url,
		Env:         envMap,
		WorkingDir:  workingDir,
		AutoStart:   true,
		MaxRestarts: 3,
	}

	// Add to config and persist
	if err := agent.GetConfigManager().UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		cfg.MCP.Servers[name] = serverConfig
		cfg.MCP.Enabled = true
		return nil
	}); err != nil {
		return "", errors.NewTool("mcp", "update config", err)
	}

	// Refresh runtime to start the new server
	if err := agent.RefreshRuntimeConfig(ctx); err != nil {
		return "", errors.NewTool("mcp", "refresh after add", err)
	}

	result := map[string]interface{}{
		"operation": "add",
		"status":    "ok",
		"name":      name,
		"message":   fmt.Sprintf("MCP server %q added and started", name),
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func handleMCPRemove(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	name, err := getMCPStringArg(args, "name")
	if err != nil {
		return "", fmt.Errorf("name is required for remove operation")
	}

	// Remove from config and persist
	if err := agent.GetConfigManager().UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			return fmt.Errorf("no MCP servers configured")
		}
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}
		delete(cfg.MCP.Servers, name)
		if len(cfg.MCP.Servers) == 0 {
			cfg.MCP.Enabled = false
		}
		return nil
	}); err != nil {
		return "", errors.NewTool("mcp", "update config", err)
	}

	// Refresh runtime to stop the removed server
	if err := agent.RefreshRuntimeConfig(ctx); err != nil {
		return "", errors.NewTool("mcp", "refresh after remove", err)
	}

	result := map[string]interface{}{
		"operation": "remove",
		"status":    "ok",
		"name":      name,
		"message":   fmt.Sprintf("MCP server %q removed and stopped", name),
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// getMCPStringArg extracts a string value from the args map.
func getMCPStringArg(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}
