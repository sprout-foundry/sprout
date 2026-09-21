package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/errors"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

func handleMCPRefresh(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	operation, err := getMCPStringArg(args, "operation")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("operation is required", nil)
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
	case "set-credential":
		return handleMCPSetCredential(ctx, agent, args)
	case "remove-credential":
		return handleMCPRemoveCredential(ctx, agent, args)
	default:
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("unknown operation %q: must be one of: list, refresh, add, remove, set-credential, remove-credential", operation), nil)
	}
}

func handleMCPList(agent *Agent) (string, error) {
	mgr := agent.mcpSub.GetManager()
	if mgr == nil {
		return "", agenterrors.NewAgent("mcp", "MCP manager is not available", nil)
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
		// Per-credential status so the agent can verify a token it just
		// stored actually resolves: env var name -> "set" | "missing".
		if credStatus := mcpCredentialStatus(cfg); len(credStatus) > 0 {
			entry["credentials"] = credStatus
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

// mcpCredentialStatus reports, per credential-bearing env var, whether the
// credential backend actually holds a value. "missing" means the placeholder
// is wired but the store has nothing (the server will start without it and
// likely fail auth).
func mcpCredentialStatus(cfg mcp.MCPServerConfig) map[string]string {
	status := make(map[string]string)
	check := func(envVarName, value string) {
		if !mcp.IsSecretRef(value) {
			return
		}
		_, actualEnvVar, ok := mcp.ParseSecretRef(value)
		if !ok {
			actualEnvVar = envVarName
		}
		key := mcp.CredentialKey(cfg.Name, actualEnvVar)
		stored, _, err := credentials.GetFromActiveBackend(key)
		if err != nil || stored == "" {
			status[actualEnvVar] = "missing"
			return
		}
		status[actualEnvVar] = "set"
	}
	for envVarName, value := range cfg.Env {
		check(envVarName, value)
	}
	for envVarName, value := range cfg.Credentials {
		check(envVarName, value)
	}
	return status
}

// handleMCPSetCredential stores one secret in the credential backend and
// wires the server's config to it via a placeholder. The plaintext value
// lives only in the backend; config.json receives "{{credential:...}}".
// The runtime is refreshed afterwards so a running server picks the value up.
func handleMCPSetCredential(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	name, err := getMCPStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("name is required for set-credential", nil)
	}
	envVar, err := getMCPStringArg(args, "env_var")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("env_var is required for set-credential (the environment variable the credential maps to)", nil)
	}
	value, err := getMCPStringArg(args, "value")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("value is required for set-credential", nil)
	}
	if !mcp.IsValidEnvVarName(envVar) {
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("invalid env_var %q: must match [A-Za-z_][A-Za-z0-9_]*", envVar), nil)
	}

	cm := agent.GetConfigManager()
	if cm == nil {
		return "", agenterrors.NewAgent("mcp", "config manager is not available", nil)
	}

	// Store first; only touch config once the backend write succeeded, so a
	// failure never leaves a placeholder pointing at nothing.
	key := mcp.CredentialKey(name, envVar)
	if err := credentials.SetToActiveBackend(key, value); err != nil {
		return "", errors.NewTool("mcp", fmt.Sprintf("store credential %s", key), err)
	}

	cfgErr := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			return fmt.Errorf("MCP server %q not found", name)
		}
		server, exists := cfg.MCP.Servers[name]
		if !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}
		if server.Credentials == nil {
			server.Credentials = make(map[string]string)
		}
		server.Credentials[envVar] = mcp.SecretRef(name, envVar)
		// Remove any Env twin so the placeholder path is authoritative.
		if server.Env != nil {
			delete(server.Env, envVar)
		}
		cfg.MCP.Servers[name] = server
		cfg.MCP.Enabled = true
		return nil
	})
	if cfgErr != nil {
		// Roll the backend write back so we don't orphan it.
		if delErr := credentials.DeleteFromActiveBackend(key); delErr != nil {
			log.Printf("[mcp] failed to roll back credential %s after config error: %v", key, delErr)
		}
		if strings.Contains(cfgErr.Error(), "not found") {
			return "", agenterrors.NewInvalidInputError(cfgErr.Error(), nil)
		}
		return "", errors.NewTool("mcp", "wire credential placeholder", cfgErr)
	}

	if err := agent.RefreshRuntimeConfig(ctx); err != nil {
		return "", errors.NewTool("mcp", "refresh after set-credential", err)
	}

	result := map[string]interface{}{
		"operation": "set-credential",
		"status":    "ok",
		"server":    name,
		"env_var":   envVar,
		"message":   fmt.Sprintf("Credential for %s (%s) stored securely and wired to the server. It is not visible in this conversation. Check mcp_refresh list to confirm the server is running.", name, envVar),
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// handleMCPRemoveCredential deletes one stored credential and unwires its
// placeholder. The server keeps running until the next refresh.
func handleMCPRemoveCredential(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	name, err := getMCPStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("name is required for remove-credential", nil)
	}
	envVar, err := getMCPStringArg(args, "env_var")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("env_var is required for remove-credential", nil)
	}

	key := mcp.CredentialKey(name, envVar)
	if err := credentials.DeleteFromActiveBackend(key); err != nil {
		return "", errors.NewTool("mcp", fmt.Sprintf("delete credential %s", key), err)
	}

	cfgErr := agent.GetConfigManager().UpdateConfig(func(cfg *configuration.Config) error {
		server, exists := cfg.MCP.Servers[name]
		if !exists {
			return fmt.Errorf("MCP server %q not found", name)
		}
		delete(server.Credentials, envVar)
		delete(server.Env, envVar)
		cfg.MCP.Servers[name] = server
		return nil
	})
	if cfgErr != nil {
		if strings.Contains(cfgErr.Error(), "not found") {
			return "", agenterrors.NewInvalidInputError(cfgErr.Error(), nil)
		}
		return "", errors.NewTool("mcp", "unwire credential placeholder", cfgErr)
	}

	if err := agent.RefreshRuntimeConfig(ctx); err != nil {
		return "", errors.NewTool("mcp", "refresh after remove-credential", err)
	}

	result := map[string]interface{}{
		"operation": "remove-credential",
		"status":    "ok",
		"server":    name,
		"env_var":   envVar,
		"message":   fmt.Sprintf("Credential for %s (%s) deleted.", name, envVar),
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func handleMCPAdd(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	name, err := getMCPStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("name is required for add operation", nil)
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

	// Plaintext secret-looking env values (API tokens etc.) must never be
	// persisted to config.json. Migrate them to the credential backend and
	// replace the config values with placeholders. Count is advisory only —
	// the result message confirms storage without echoing values.
	migrated, migErr := mcp.MigrateEnvSecretsFromServer(name, &serverConfig)
	if migErr != nil {
		return "", errors.NewTool("mcp", "migrate plaintext env secrets to the credential backend (add the server again with a non-secret env, or set the value via set-credential)", migErr)
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
	if migrated > 0 {
		result["message"] = fmt.Sprintf("MCP server %q added and started; %d secret env value(s) were migrated to the credential store (config carries placeholders only)", name, migrated)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func handleMCPRemove(ctx context.Context, agent *Agent, args map[string]interface{}) (string, error) {
	name, err := getMCPStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("name is required for remove operation", nil)
	}

	// Remove from config and persist
	if err := agent.GetConfigManager().UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			return agenterrors.NewAgent("mcp", "no MCP servers configured", nil)
		}
		if _, exists := cfg.MCP.Servers[name]; !exists {
			return agenterrors.NewNotFoundCause(fmt.Sprintf("MCP server %q", name), nil)
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
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("%s is required", key), nil)
	}
	s, ok := v.(string)
	if !ok {
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("%s must be a string", key), nil)
	}
	return s, nil
}
