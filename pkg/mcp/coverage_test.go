package mcp

import (
	"bufio"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// tool_wrapper.go - Additional coverage tests
// ===========================================================================

// TestMCPToolWrapper_CanExecute_ServerRunning tests CanExecute when the server
// is running (the positive case, already tested with negative cases)
func TestMCPToolWrapper_CanExecute_ServerRunning(t *testing.T) {
	// Create a manager with a mock server that reports as running
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockRunningServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)

	// CanExecute should return true when server is running
	assert.True(t, wrapper.CanExecute(context.Background(), Parameters{}))
}

// TestMCPToolWrapper_CanExecute_ServerNotRunning tests CanExecute when the server
// exists but is not running
func TestMCPToolWrapper_CanExecute_ServerNotRunning(t *testing.T) {
	// Create a manager with a mock server that reports as NOT running
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockStoppedServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)

	// CanExecute should return false when server is not running
	assert.False(t, wrapper.CanExecute(context.Background(), Parameters{}))
}

// TestMCPToolWrapper_IsAvailable_ServerRunningAndAvailable tests IsAvailable
// when the server is running AND the wrapper's available flag is true
func TestMCPToolWrapper_IsAvailable_ServerRunningAndAvailable(t *testing.T) {
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockRunningServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)
	wrapper.SetAvailable(true) // Default, but explicit for clarity

	// IsAvailable should return true when both conditions are met
	assert.True(t, wrapper.IsAvailable())
}

// TestMCPToolWrapper_IsAvailable_AvailableFlagFalse tests IsAvailable when the
// wrapper's available flag is set to false
func TestMCPToolWrapper_IsAvailable_AvailableFlagFalse(t *testing.T) {
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockRunningServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)
	wrapper.SetAvailable(false)

	// IsAvailable should return false when available flag is false, even if server is running
	assert.False(t, wrapper.IsAvailable())
}

// TestMCPToolWrapper_EstimatedDuration_Default tests EstimatedDuration returns
// the default 30-second timeout when SetTimeout is not called
func TestMCPToolWrapper_EstimatedDuration_Default(t *testing.T) {
	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)

	// Default timeout should be 30 seconds
	assert.Equal(t, 30*time.Second, wrapper.EstimatedDuration())
}

// TestMCPToolWrapper_RequiredPermissions_NonGitHubServer tests that
// RequiredPermissions only returns network_access for non-github servers
func TestMCPToolWrapper_RequiredPermissions_NonGitHubServer(t *testing.T) {
	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "myserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	perms := wrapper.RequiredPermissions()

	// Should only contain network_access, not github-specific permissions
	assert.Len(t, perms, 1)
	assert.Contains(t, perms, "network_access")
	assert.NotContains(t, perms, "mcp_github_access")
}

// TestMCPToolWrapper_Name_EdgeCases tests Name method with various edge cases
func TestMCPToolWrapper_Name_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		serverName string
		toolName   string
		expected   string
	}{
		{
			name:       "basic names",
			serverName: "server",
			toolName:   "tool",
			expected:   "mcp_server_tool",
		},
		{
			name:       "server with hyphens",
			serverName: "my-server",
			toolName:   "search_files",
			expected:   "mcp_my-server_search_files",
		},
		{
			name:       "server with underscores",
			serverName: "my_server",
			toolName:   "get_user",
			expected:   "mcp_my_server_get_user",
		},
		{
			name:       "tool with hyphens",
			serverName: "github",
			toolName:   "create-issue",
			expected:   "mcp_github_create-issue",
		},
		{
			name:       "empty tool name",
			serverName: "server",
			toolName:   "",
			expected:   "mcp_server_",
		},
		{
			name:       "empty server name",
			serverName: "",
			toolName:   "tool",
			expected:   "mcp__tool",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tool := MCPTool{
				Name:        tc.toolName,
				Description: "Test tool",
				ServerName:  tc.serverName,
			}
			wrapper := NewMCPToolWrapper(tool, nil)
			assert.Equal(t, tc.expected, wrapper.Name())
		})
	}
}

// ===========================================================================
// Mock servers for testing
// ===========================================================================

// mockRunningServer is a mock MCPServer that always reports as running
type mockRunningServer struct {
	name string
}

func (m *mockRunningServer) Start(ctx context.Context) error { return nil }
func (m *mockRunningServer) Stop(ctx context.Context) error  { return nil }
func (m *mockRunningServer) IsRunning() bool                { return true }
func (m *mockRunningServer) GetName() string                { return m.name }
func (m *mockRunningServer) GetConfig() MCPServerConfig {
	return MCPServerConfig{Name: m.name}
}
func (m *mockRunningServer) Initialize(ctx context.Context) error { return nil }
func (m *mockRunningServer) ListTools(ctx context.Context) ([]MCPTool, error) {
	return nil, nil
}
func (m *mockRunningServer) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	return nil, nil
}
func (m *mockRunningServer) ListResources(ctx context.Context) ([]MCPResource, error) {
	return nil, nil
}
func (m *mockRunningServer) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	return nil, nil
}
func (m *mockRunningServer) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	return nil, nil
}
func (m *mockRunningServer) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	return nil, nil
}

// mockStoppedServer is a mock MCPServer that always reports as NOT running
type mockStoppedServer struct {
	name string
}

func (m *mockStoppedServer) Start(ctx context.Context) error { return nil }
func (m *mockStoppedServer) Stop(ctx context.Context) error  { return nil }
func (m *mockStoppedServer) IsRunning() bool                { return false }
func (m *mockStoppedServer) GetName() string                { return m.name }
func (m *mockStoppedServer) GetConfig() MCPServerConfig {
	return MCPServerConfig{Name: m.name}
}
func (m *mockStoppedServer) Initialize(ctx context.Context) error { return nil }
func (m *mockStoppedServer) ListTools(ctx context.Context) ([]MCPTool, error) {
	return nil, nil
}
func (m *mockStoppedServer) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	return nil, nil
}
func (m *mockStoppedServer) ListResources(ctx context.Context) ([]MCPResource, error) {
	return nil, nil
}
func (m *mockStoppedServer) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	return nil, nil
}
func (m *mockStoppedServer) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	return nil, nil
}
func (m *mockStoppedServer) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	return nil, nil
}

// ===========================================================================
// github_setup.go - Additional coverage tests
// ===========================================================================

// TestIsCommandAvailable tests the isCommandAvailable helper function
func TestIsCommandAvailable(t *testing.T) {
	t.Run("shell command exists", func(t *testing.T) {
		// 'sh' should exist on all Unix-like systems
		assert.True(t, isCommandAvailable("sh"))
	})

	t.Run("shell command does not exist", func(t *testing.T) {
		// This command should not exist
		assert.False(t, isCommandAvailable("nonexistentcommandthatdoesnotexist12345"))
	})
}

// TestGitHubRemoteSetupPromptConstant verifies the constant value
func TestGitHubRemoteSetupPromptConstant(t *testing.T) {
	assert.Equal(t, "github_mcp_setup", githubRemoteSetupPrompt)
}

// TestGitHubMCPServerURLConstant verifies the constant value
func TestGitHubMCPServerURLConstant(t *testing.T) {
	assert.Equal(t, "https://api.githubcopilot.com/mcp/", githubMCPServerURL)
}

// TestGitHubPATHelpURLConstant verifies the constant value
func TestGitHubPATHelpURLConstant(t *testing.T) {
	assert.Equal(t, "https://github.com/settings/personal-access-tokens/new", gitHubPATHelpURL)
}

// TestIsGitHubMCPConfigured_NilServersMap tests with nil servers map
func TestIsGitHubMCPConfigured_NilServersMap(t *testing.T) {
	config := MCPConfig{
		Servers: nil,
	}
	assert.False(t, IsGitHubMCPConfigured(config))
}

// TestGitHubRepoInfo_Structure tests the GitHubRepoInfo struct
func TestGitHubRepoInfo_Structure(t *testing.T) {
	info := GitHubRepoInfo{
		Owner: "testowner",
		Repo:  "testrepo",
		URL:   "https://github.com/testowner/testrepo",
	}

	assert.Equal(t, "testowner", info.Owner)
	assert.Equal(t, "testrepo", info.Repo)
	assert.Equal(t, "https://github.com/testowner/testrepo", info.URL)
}

// TestRunGitHubMCPSetup_Choices tests all three setup choices
func TestRunGitHubMCPSetup_Choices(t *testing.T) {
	origOpenBrowser := openBrowserFn
	openBrowserFn = func(string) error { return nil }
	defer func() { openBrowserFn = origOpenBrowser }()

	repo := &GitHubRepoInfo{
		Owner: "testowner",
		Repo:  "testrepo",
		URL:   "https://github.com/testowner/testrepo",
	}

	tests := []struct {
		name       string
		choice     string
		wantType   string
		wantURL    string
		wantCmd    string
		wantEnvKey string
	}{
		{
			name:     "choice 1 - remote OAuth",
			choice:   "1",
			wantType: "http",
			wantURL:  githubMCPServerURL,
		},
		{
			name:       "choice 2 - Docker + PAT",
			choice:     "2",
			wantType:   "stdio",
			wantCmd:    "docker",
			wantEnvKey: "GITHUB_PERSONAL_ACCESS_TOKEN",
		},
		{
			name:       "choice 3 - npx + PAT",
			choice:     "3",
			wantType:   "stdio",
			wantCmd:    "npx",
			wantEnvKey: "GITHUB_PERSONAL_ACCESS_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create reader with choice and mock token
			input := tt.choice + "\ntest_token_1234567890\n"
			reader := bufio.NewReader(strings.NewReader(input))

			config, err := RunGitHubMCPSetup(context.Background(), repo, reader)
			require.NoError(t, err)
			require.NotNil(t, config)

			assert.Equal(t, "github", config.Name)
			assert.True(t, config.AutoStart)
			assert.Equal(t, 30*time.Second, config.Timeout)

			if tt.wantType == "http" {
				assert.Equal(t, tt.wantURL, config.URL)
			} else {
				assert.Equal(t, tt.wantCmd, config.Command)
				if tt.wantEnvKey != "" {
					assert.Contains(t, config.Env, tt.wantEnvKey)
					assert.Equal(t, "test_token_1234567890", config.Env[tt.wantEnvKey])
				}
			}
		})
	}
}

// TestParseGitHubRemoteURL_AdditionalEdgeCases tests more edge cases
func TestParseGitHubRemoteURL_AdditionalEdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *GitHubRepoInfo
	}{
		{
			name:  "empty string",
			input: "",
			expected: &GitHubRepoInfo{},
		},
		{
			name:     "just git@github.com: prefix",
			input:    "git@github.com:",
			expected:  nil,
		},
		{
			name:     "only owner, no repo",
			input:    "git@github.com:owner",
			expected:  nil,
		},
		{
			name:  "both owner and repo empty",
			input: "git@github.com:/",
			expected: &GitHubRepoInfo{},
		},
		{
			name:     "non-GitLab HTTPS",
			input:    "https://gitlab.com/owner/repo.git",
			expected: nil,
		},
		{
			name:     "Bitbucket SSH",
			input:    "git@bitbucket.org:owner/repo.git",
			expected: nil,
		},
		{
			name:  "HTTPS with port number (invalid for GitHub)",
			input: "https://github.com:443/owner/repo.git",
			expected: &GitHubRepoInfo{
				Owner: "",
				Repo:  "",
				URL:   "",
			},
		},
		{
			name:  "multiple slashes in owner",
			input: "git@github.com:owner//repo.git",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "/repo",
				URL:   "https://github.com/owner//repo",
			},
		},
		{
			name:  "owner and repo with dots",
			input: "git@github.com:owner.name/repo.name.git",
			expected: &GitHubRepoInfo{
				Owner: "owner.name",
				Repo:  "repo.name",
				URL:   "https://github.com/owner.name/repo.name",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseGitHubRemoteURL(tc.input)
			if tc.expected == nil || (tc.expected.Owner == "" && tc.expected.Repo == "") {
				// If expected is nil or empty, result should also be nil
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

// ===========================================================================
// Constants and type coverage
// ===========================================================================

// TestCategoryWebConstant verifies the CategoryWeb constant
func TestCategoryWebConstant(t *testing.T) {
	assert.Equal(t, "web", CategoryWeb)
}

// TestPermissionNetworkAccessConstant verifies the PermissionNetworkAccess constant
func TestPermissionNetworkAccessConstant(t *testing.T) {
	assert.Equal(t, "network_access", PermissionNetworkAccess)
}

// TestMCPToolWrapper_SetCategory tests SetCategory explicitly
func TestMCPToolWrapper_SetCategory(t *testing.T) {
	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	assert.Equal(t, "web", wrapper.Category())

	wrapper.SetCategory("filesystem")
	assert.Equal(t, "filesystem", wrapper.Category())

	wrapper.SetCategory("database")
	assert.Equal(t, "database", wrapper.Category())
}

// TestMCPToolWrapper_ToAgentTool_FullSchema tests ToAgentTool with a complex schema
func TestMCPToolWrapper_ToAgentTool_FullSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file path",
			},
			"line": map[string]interface{}{
				"type":        "integer",
				"description": "The line number",
			},
		},
		"required": []interface{}{"path"},
	}

	tool := MCPTool{
		Name:        "read_file",
		Description: "Read a file from the filesystem",
		InputSchema: schema,
		ServerName:  "myserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	agentTool := wrapper.ToAgentTool()

	assert.Equal(t, "function", agentTool.Type)
	assert.Equal(t, "mcp_myserver_read_file", agentTool.Function.Name)
	// Description() adds the [MCP:server] prefix
	assert.Equal(t, "[MCP:myserver] Read a file from the filesystem", agentTool.Function.Description)
	assert.Equal(t, schema, agentTool.Function.Parameters)
}

// TestMCPToolWrapper_Description_Long tests with long description
func TestMCPToolWrapper_Description_Long(t *testing.T) {
	longDesc := strings.Repeat("This is a very long description. ", 20)

	tool := MCPTool{
		Name:        "tool",
		Description: longDesc,
		ServerName:  "server",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	expected := "[MCP:server] " + longDesc
	assert.Equal(t, expected, wrapper.Description())
}
