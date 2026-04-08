package configuration

import (
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/mcp"
)

func TestRedactConfig_NilConfig(t *testing.T) {
	// Test that nil config returns empty struct
	result := RedactConfig(nil)

	if result.Version != "" {
		t.Errorf("Expected empty version for nil config, got: %s", result.Version)
	}
	if result.MCP.Enabled != false {
		t.Errorf("Expected MCP.Enabled to be false for nil config, got: %v", result.MCP.Enabled)
	}
	if result.MCP.AutoStart != false {
		t.Errorf("Expected MCP.AutoStart to be false for nil config, got: %v", result.MCP.AutoStart)
	}
}

func TestRedactConfig_EmptyConfig(t *testing.T) {
	// Test that empty config is copied correctly
	emptyConfig := Config{}
	result := RedactConfig(&emptyConfig)

	// Verify it's a copy, not the same reference
	if &result == &emptyConfig {
		t.Error("Expected a copy, not the same reference")
	}

	// Verify MCP is redacted (empty MCPConfig should still be redacted)
	// Note: We can't directly compare MCP configs with maps, so we verify the redaction function was called
	_ = mcp.RedactMCPConfig(emptyConfig.MCP)
}

func TestRedactConfig_WithMCPServers(t *testing.T) {
	// Create a config with MCP servers containing sensitive data
	config := Config{
		Version: "2.0",
		MCP: mcp.MCPConfig{
			Enabled:     true,
			AutoStart:   true,
			AutoDiscover: true,
			Timeout:     30 * time.Second,
			Servers: map[string]mcp.MCPServerConfig{
				"test-server": {
					Name:     "test-server",
					Command:  "npx",
					Args:     []string{"-y", "@server"},
					Env: map[string]string{
						"API_KEY":               "secret123",
						"NORMAL_VAR":           "public_value",
						"ANOTHER_SECRET":       "topsecret",
					},
					Credentials: map[string]string{
						"DB_PASSWORD": "$secret:db_password",
						"JWT_TOKEN":   "$secret:jwt_token",
					},
					AutoStart: true,
					Timeout:   60 * time.Second,
				},
				"http-server": {
					Name:  "http-server",
					Type:  "http",
					URL:   "https://api.example.com",
					Env: map[string]string{
						"HTTP_SECRET": "http_secret_value",
					},
					AutoStart: false,
				},
			},
		},
		CustomProviders: map[string]CustomProviderConfig{
			"custom-provider": {
				Name:        "custom-provider",
				Endpoint:    "https://api.custom.com",
				ModelName:   "custom-model",
				ContextSize: 8192,
			},
		},
	}

	result := RedactConfig(&config)

	// Verify MCP servers are redacted
	if len(result.MCP.Servers) != len(config.MCP.Servers) {
		t.Fatalf("Expected same number of servers, got %d vs %d", len(result.MCP.Servers), len(config.MCP.Servers))
	}

	// Check test-server env vars are redacted
	testServer := result.MCP.Servers["test-server"]
	if testServer.Env["API_KEY"] != "[REDACTED]" {
		t.Errorf("Expected API_KEY to be redacted, got: %s", testServer.Env["API_KEY"])
	}
	if testServer.Env["ANOTHER_SECRET"] != "[REDACTED]" {
		t.Errorf("Expected ANOTHER_SECRET to be redacted, got: %s", testServer.Env["ANOTHER_SECRET"])
	}
	// Normal vars should remain unchanged
	if testServer.Env["NORMAL_VAR"] != "public_value" {
		t.Errorf("Expected NORMAL_VAR to remain unchanged, got: %s", testServer.Env["NORMAL_VAR"])
	}

	// Check test-server credentials are redacted (masked)
	if testServer.Credentials["DB_PASSWORD"] != "$sec****" {
		t.Errorf("Expected DB_PASSWORD to be redacted, got: %s", testServer.Credentials["DB_PASSWORD"])
	}
	if testServer.Credentials["JWT_TOKEN"] != "$sec****" {
		t.Errorf("Expected JWT_TOKEN to be redacted, got: %s", testServer.Credentials["JWT_TOKEN"])
	}

	// Check http-server env vars are redacted
	httpServer := result.MCP.Servers["http-server"]
	if httpServer.Env["HTTP_SECRET"] != "[REDACTED]" {
		t.Errorf("Expected HTTP_SECRET to be redacted, got: %s", httpServer.Env["HTTP_SECRET"])
	}

	// Verify other config fields are preserved
	if result.Version != config.Version {
		t.Errorf("Expected version to be preserved, got: %s", result.Version)
	}
	if len(result.CustomProviders) != len(config.CustomProviders) {
		t.Errorf("Expected same number of custom providers")
	}
}

func TestRedactConfig_OriginalNotMutated(t *testing.T) {
	// Test that the original config is not mutated
	originalConfig := Config{
		Version: "2.0",
		MCP: mcp.MCPConfig{
			Enabled: true,
			Servers: map[string]mcp.MCPServerConfig{
				"server1": {
					Name:  "server1",
					Command: "test",
					Env: map[string]string{
						"SECRET_KEY": "original_secret_value",
					},
					Args: []string{"-e", "SECRET_KEY=original_secret_value"},
				},
			},
		},
	}

	result := RedactConfig(&originalConfig)

	// Verify original secret values are unchanged
	if originalConfig.MCP.Servers["server1"].Env["SECRET_KEY"] != "original_secret_value" {
		t.Errorf("Original Env was mutated: got %q", originalConfig.MCP.Servers["server1"].Env["SECRET_KEY"])
	}

	// Verify original Args are unchanged
	if len(originalConfig.MCP.Servers["server1"].Args) != 2 ||
		originalConfig.MCP.Servers["server1"].Args[0] != "-e" {
		t.Errorf("Original Args were mutated: got %v", originalConfig.MCP.Servers["server1"].Args)
	}

	// Verify result has a new Servers map (not the same reference)
	if &result.MCP.Servers == &originalConfig.MCP.Servers {
		t.Error("Result should be a redacted copy, not the original")
	}

	// Verify result has redacted values
	if result.MCP.Servers["server1"].Env["SECRET_KEY"] == "original_secret_value" {
		t.Error("Result should have redacted secret values")
	}

	// Modifying the result should not affect the original
	result.MCP.Servers["server1"].Env["SECRET_KEY"] = "modified"
	result.Version = "modified"
	result.MCP.Servers["server1"].Args[0] = "modified_arg"

	if originalConfig.MCP.Servers["server1"].Env["SECRET_KEY"] != "original_secret_value" {
		t.Error("Original config Env was modified when result was changed")
	}
	if originalConfig.Version != "2.0" {
		t.Error("Original config version was modified")
	}
	if originalConfig.MCP.Servers["server1"].Args[0] != "-e" {
		t.Error("Original config Args were modified when result was changed")
	}
}

func TestRedactConfig_PreservesNonSecretFields(t *testing.T) {
	// Test that non-secret fields are preserved correctly
	config := Config{
		Version: "2.0",
		ProviderModels: map[string]string{
			"openai": "gpt-4",
		},
		ProviderPriority: []string{"openai", "anthropic"},
		MCP: mcp.MCPConfig{
			Enabled:      true,
			AutoStart:    true,
			AutoDiscover: true,
			Timeout:      45 * time.Second,
			Servers: map[string]mcp.MCPServerConfig{
				"server1": {
					Name:        "server1",
					Command:     "npx",
					Args:        []string{"-y", "@server"},
					WorkingDir:  "/home/user/project",
					AutoStart:   true,
					MaxRestarts: 5,
					Timeout:     60 * time.Second,
					Env: map[string]string{
						"PUBLIC_VAR": "public_value",
					},
				},
			},
		},
		Preferences: map[string]interface{}{
			"theme": "dark",
			"fontSize": 14,
		},
		EnablePreWriteValidation: true,
		AllowOrchestratorGitWrite: true,
		ResourceDirectory:         "/tmp/resources",
		ReasoningEffort:           "high",
	}

	result := RedactConfig(&config)

	// Verify non-secret fields are preserved
	if result.Version != config.Version {
		t.Errorf("Expected version to be preserved, got: %s", result.Version)
	}
	if len(result.ProviderModels) != len(config.ProviderModels) {
		t.Error("Expected ProviderModels to be preserved")
	}
	if len(result.ProviderPriority) != len(config.ProviderPriority) {
		t.Error("Expected ProviderPriority to be preserved")
	}
	if result.EnablePreWriteValidation != config.EnablePreWriteValidation {
		t.Error("Expected EnablePreWriteValidation to be preserved")
	}
	if result.AllowOrchestratorGitWrite != config.AllowOrchestratorGitWrite {
		t.Error("Expected AllowOrchestratorGitWrite to be preserved")
	}
	if result.ResourceDirectory != config.ResourceDirectory {
		t.Errorf("Expected ResourceDirectory to be preserved, got: %s", result.ResourceDirectory)
	}
	if result.ReasoningEffort != config.ReasoningEffort {
		t.Errorf("Expected ReasoningEffort to be preserved, got: %s", result.ReasoningEffort)
	}

	// Verify MCP config fields are preserved (except secrets)
	if result.MCP.Enabled != config.MCP.Enabled {
		t.Error("Expected MCP.Enabled to be preserved")
	}
	if result.MCP.AutoStart != config.MCP.AutoStart {
		t.Error("Expected MCP.AutoStart to be preserved")
	}
	if result.MCP.AutoDiscover != config.MCP.AutoDiscover {
		t.Error("Expected MCP.AutoDiscover to be preserved")
	}
	if result.MCP.Timeout != config.MCP.Timeout {
		t.Error("Expected MCP.Timeout to be preserved")
	}

	// Verify server fields are preserved
	server := result.MCP.Servers["server1"]
	if server.Name != config.MCP.Servers["server1"].Name {
		t.Error("Expected server name to be preserved")
	}
	if server.Command != config.MCP.Servers["server1"].Command {
		t.Error("Expected server command to be preserved")
	}
	if server.WorkingDir != config.MCP.Servers["server1"].WorkingDir {
		t.Error("Expected server working dir to be preserved")
	}
	if server.MaxRestarts != config.MCP.Servers["server1"].MaxRestarts {
		t.Error("Expected server max restarts to be preserved")
	}
	if server.Timeout != config.MCP.Servers["server1"].Timeout {
		t.Error("Expected server timeout to be preserved")
	}
}

func TestRedactConfig_EmptyServers(t *testing.T) {
	// Test with empty MCP servers map
	config := Config{
		Version: "2.0",
		MCP: mcp.MCPConfig{
			Enabled:      false,
			AutoStart:    false,
			AutoDiscover: false,
			Servers:      make(map[string]mcp.MCPServerConfig),
		},
	}

	result := RedactConfig(&config)

	if result.MCP.Servers == nil {
		t.Error("Expected MCP.Servers to be preserved (empty map)")
	}
	if len(result.MCP.Servers) != 0 {
		t.Error("Expected MCP.Servers to be empty")
	}
}

func TestRedactConfig_MultipleServers(t *testing.T) {
	// Test with multiple servers having different credential patterns
	config := Config{
		Version: "2.0",
		MCP: mcp.MCPConfig{
			Enabled: true,
			Servers: map[string]mcp.MCPServerConfig{
				"server1": {
					Name:  "server1",
					Command: "cmd1",
					Env: map[string]string{
						"SECRET1": "value1",
					},
				},
				"server2": {
					Name:  "server2",
					Command: "cmd2",
					Credentials: map[string]string{
						"SECRET2": "$secret:secret2",
					},
				},
				"server3": {
					Name:  "server3",
					Command: "cmd3",
					Env: map[string]string{
						"SECRET3": "value3",
					},
					Credentials: map[string]string{
						"SECRET4": "$secret:secret4",
					},
				},
			},
		},
	}

	result := RedactConfig(&config)

	// All servers should be present
	if len(result.MCP.Servers) != 3 {
		t.Fatalf("Expected 3 servers, got %d", len(result.MCP.Servers))
	}

	// Check each server's secrets are redacted
	server1 := result.MCP.Servers["server1"]
	if server1.Env["SECRET1"] != "[REDACTED]" {
		t.Errorf("Expected SECRET1 to be redacted, got: %s", server1.Env["SECRET1"])
	}

	server2 := result.MCP.Servers["server2"]
	if server2.Credentials["SECRET2"] != "$sec****" {
		t.Errorf("Expected SECRET2 to be redacted, got: %s", server2.Credentials["SECRET2"])
	}

	server3 := result.MCP.Servers["server3"]
	if server3.Env["SECRET3"] != "[REDACTED]" {
		t.Errorf("Expected SECRET3 to be redacted, got: %s", server3.Env["SECRET3"])
	}
	if server3.Credentials["SECRET4"] != "$sec****" {
		t.Errorf("Expected SECRET4 to be redacted, got: %s", server3.Credentials["SECRET4"])
	}
}

func TestRedactConfig_ReturnsCopyNotReference(t *testing.T) {
	// Test that modifying the result doesn't affect the original
	config := Config{
		Version: "2.0",
		MCP: mcp.MCPConfig{
			Enabled: true,
			Servers: map[string]mcp.MCPServerConfig{
				"server1": {
					Name:  "server1",
					Command: "cmd",
					Env: map[string]string{
						"SECRET": "original",
					},
				},
			},
		},
	}

	result := RedactConfig(&config)

	// Modify the result
	result.MCP.Servers["server1"].Env["SECRET"] = "modified"
	result.Version = "modified"

	// Verify original is unchanged
	if config.MCP.Servers["server1"].Env["SECRET"] != "original" {
		t.Error("Original config was modified when result was changed")
	}
	if config.Version != "2.0" {
		t.Error("Original config version was modified")
	}
}
