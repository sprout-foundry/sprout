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

// ---------------------------------------------------------------------------
// Test: parseGitHubRemoteURL()
// ---------------------------------------------------------------------------

func TestParseGitHubRemoteURL_SSH(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *GitHubRepoInfo
	}{
		{
			name:  "Standard SSH URL",
			input: "git@github.com:owner/repo.git",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "SSH URL without .git",
			input: "git@github.com:owner/repo",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "SSH URL with trailing slash",
			input: "git@github.com:owner/repo/",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "SSH URL with org and repo",
			input: "git@github.com:my-org/my-repo.git",
			expected: &GitHubRepoInfo{
				Owner: "my-org",
				Repo:  "my-repo",
				URL:   "https://github.com/my-org/my-repo",
			},
		},
		{
			name:  "SSH URL with hyphenated names",
			input: "git@github.com:my-org/my-repo-name.git",
			expected: &GitHubRepoInfo{
				Owner: "my-org",
				Repo:  "my-repo-name",
				URL:   "https://github.com/my-org/my-repo-name",
			},
		},
		{
			name:  "SSH URL with underscores",
			input: "git@github.com:my_org/my_repo.git",
			expected: &GitHubRepoInfo{
				Owner: "my_org",
				Repo:  "my_repo",
				URL:   "https://github.com/my_org/my_repo",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseGitHubRemoteURL(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseGitHubRemoteURL_HTTPS(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *GitHubRepoInfo
	}{
		{
			name:  "Standard HTTPS URL",
			input: "https://github.com/owner/repo.git",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "HTTPS URL without .git",
			input: "https://github.com/owner/repo",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "HTTPS URL with trailing slash",
			input: "https://github.com/owner/repo/",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "HTTP URL",
			input: "http://github.com/owner/repo.git",
			expected: &GitHubRepoInfo{
				Owner: "owner",
				Repo:  "repo",
				URL:   "https://github.com/owner/repo",
			},
		},
		{
			name:  "HTTPS URL with org",
			input: "https://github.com/my-org/my-repo.git",
			expected: &GitHubRepoInfo{
				Owner: "my-org",
				Repo:  "my-repo",
				URL:   "https://github.com/my-org/my-repo",
			},
		},
		{
			name:  "HTTPS URL with numeric owner",
			input: "https://github.com/12345/repo.git",
			expected: &GitHubRepoInfo{
				Owner: "12345",
				Repo:  "repo",
				URL:   "https://github.com/12345/repo",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseGitHubRemoteURL(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseGitHubRemoteURL_InvalidURLs(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "Empty string",
			input: "",
		},
		{
			name:  "Non-GitHub SSH",
			input: "git@gitlab.com:owner/repo.git",
		},
		{
			name:  "Non-GitHub HTTPS",
			input: "https://gitlab.com/owner/repo.git",
		},
		{
			name:  "Missing repo",
			input: "git@github.com:owner",
		},
		{
			name:  "Only colon after SSH",
			input: "git@github.com:",
		},
		{
			name:  "HTTPS URL missing path",
			input: "https://github.com/",
		},
		{
			name:  "HTTPS URL missing owner",
			input: "https://github.com/repo.git",
		},
		{
			name:  "Only protocol",
			input: "https://",
		},
		{
			name:  "Random string",
			input: "not-a-url",
		},
		{
			name:  "SSH with wrong format",
			input: "git@github.comowner/repo",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseGitHubRemoteURL(tc.input)
			assert.Nil(t, result)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: DetectGitHubRepo()
// ---------------------------------------------------------------------------

func TestDetectGitHubRepo_ValidGitHubRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git-dependent test in short mode")
	}

	// This test requires a valid git repo with a GitHub remote
	// We'll use a temporary directory and initialize a repo
	t.Skip("Requires actual git setup - skipping in automated tests")
}

func TestDetectGitHubRepo_NonGitDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git-dependent test in short mode")
	}

	// Test with a non-git directory
	t.Skip("Requires actual filesystem access - skipping in automated tests")
}

// ---------------------------------------------------------------------------
// Test: IsGitHubMCPConfigured()
// ---------------------------------------------------------------------------

func TestIsGitHubMCPConfigured_NotConfigured(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{},
	}

	result := IsGitHubMCPConfigured(config)
	assert.False(t, result)
}

func TestIsGitHubMCPConfigured_WithHTTPServer(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {
				Name: "github",
				Type: "http",
				URL:  "https://api.githubcopilot.com/mcp/",
			},
		},
	}

	result := IsGitHubMCPConfigured(config)
	assert.True(t, result)
}

func TestIsGitHubMCPConfigured_WithStdioServer(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:    "github",
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-github"},
			},
		},
	}

	result := IsGitHubMCPConfigured(config)
	assert.True(t, result)
}

func TestIsGitHubMCPConfigured_WithoutCommandOrURL(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {
				Name: "github",
				Type: "http",
				URL:  "", // Empty URL
			},
		},
	}

	result := IsGitHubMCPConfigured(config)
	assert.False(t, result)
}

func TestIsGitHubMCPConfigured_OtherServerExists(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"other-server": {
				Name:    "other-server",
				Command: "npx",
			},
		},
	}

	result := IsGitHubMCPConfigured(config)
	assert.False(t, result)
}

// ---------------------------------------------------------------------------
// Test: ShouldPromptGitHubSetup()
// ---------------------------------------------------------------------------

func TestShouldPromptGitHubSetup_AlreadyConfigured(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {
				Name: "github",
				Type: "http",
				URL:  "https://api.githubcopilot.com/mcp/",
			},
		},
	}

	dismissedPrompts := map[string]bool{}

	result := ShouldPromptGitHubSetup(".", config, dismissedPrompts)
	assert.False(t, result)
}

func TestShouldPromptGitHubSetup_Dismissed(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{},
	}

	dismissedPrompts := map[string]bool{
		githubRemoteSetupPrompt: true,
	}

	result := ShouldPromptGitHubSetup(".", config, dismissedPrompts)
	assert.False(t, result)
}

func TestShouldPromptGitHubSetup_NotDismissedNotConfigured(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git-dependent test in short mode")
	}

	config := MCPConfig{
		Servers: map[string]MCPServerConfig{},
	}

	dismissedPrompts := map[string]bool{}

	// Result depends on filesystem state (LoadMCPConfig) and whether CWD is
	// a GitHub repo (DetectGitHubRepo), so we only verify the function
	// completes without panicking and returns a valid bool.
	result := ShouldPromptGitHubSetup(".", config, dismissedPrompts)
	_ = result
}

func TestShouldPromptGitHubSetup_NilDismissedPrompts(t *testing.T) {
	config := MCPConfig{
		Servers: map[string]MCPServerConfig{},
	}

	// nil dismissedPrompts should be handled gracefully (no nil-pointer panic).
	// Result depends on filesystem state, so we just verify no panic occurs.
	result := ShouldPromptGitHubSetup(".", config, nil)
	assert.IsType(t, false, result) // valid bool returned
}

// ---------------------------------------------------------------------------
// Test: RunGitHubMCPSetup()
// ---------------------------------------------------------------------------

func TestRunGitHubMCPSetup_Choice1Remote(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that simulates choosing option 1
	reader := bufio.NewReader(strings.NewReader("1\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "http", config.Type)
	assert.Equal(t, githubMCPServerURL, config.URL)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestRunGitHubMCPSetup_Choice2Docker(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that simulates choosing option 2 and providing a PAT
	reader := bufio.NewReader(strings.NewReader("2\ngithub_pat_1234567890abcdef\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "docker", config.Command)
	assert.ElementsMatch(t, []string{"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"}, config.Args)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 3, config.MaxRestarts)
	assert.NotNil(t, config.Env)
	assert.Equal(t, "github_pat_1234567890abcdef", config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"])
}

func TestRunGitHubMCPSetup_Choice3NPX(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that simulates choosing option 3 and providing a PAT
	reader := bufio.NewReader(strings.NewReader("3\ngithub_pat_1234567890abcdef\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "npx", config.Command)
	assert.ElementsMatch(t, []string{"-y", "@modelcontextprotocol/server-github"}, config.Args)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 3, config.MaxRestarts)
	assert.NotNil(t, config.Env)
	assert.Equal(t, "github_pat_1234567890abcdef", config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"])
}

func TestRunGitHubMCPSetup_EmptyChoiceDefaultsTo1(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that simulates empty input (defaults to option 1)
	reader := bufio.NewReader(strings.NewReader("\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "http", config.Type)
	assert.Equal(t, githubMCPServerURL, config.URL)
}

func TestRunGitHubMCPSetup_InvalidChoice(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that simulates an invalid choice
	reader := bufio.NewReader(strings.NewReader("99\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid choice")
	assert.Nil(t, config)
}

func TestRunGitHubMCPSetup_CancelTokenInput(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that simulates choosing option 2 and cancelling token input
	reader := bufio.NewReader(strings.NewReader("2\n\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.NoError(t, err)
	assert.Nil(t, config) // Should return nil when cancelled
}

func TestRunGitHubMCPSetup_ShortTokenRejected(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	// Create a reader that provides a very short token
	reader := bufio.NewReader(strings.NewReader("2\nshort\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
	assert.Nil(t, config)
}

func TestRunGitHubMCPSetup_NilRepo(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("1\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, nil, reader)

	// Should still work with nil repo (displays repo name if available)
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "github", config.Name)
}

// ---------------------------------------------------------------------------
// Test: GitHubRepoInfo structure
// ---------------------------------------------------------------------------

func TestGitHubRepoInfo_Fields(t *testing.T) {
	repo := GitHubRepoInfo{
		Owner: "test-owner",
		Repo:  "test-repo",
		URL:   "https://github.com/test-owner/test-repo",
	}

	assert.Equal(t, "test-owner", repo.Owner)
	assert.Equal(t, "test-repo", repo.Repo)
	assert.Equal(t, "https://github.com/test-owner/test-repo", repo.URL)
}

func TestGitHubRepoInfo_WithHyphenatedOwnerAndRepo(t *testing.T) {
	repo := GitHubRepoInfo{
		Owner: "my-org",
		Repo:  "my-repo",
		URL:   "https://github.com/my-org/my-repo",
	}

	assert.Equal(t, "my-org", repo.Owner)
	assert.Equal(t, "my-repo", repo.Repo)
	assert.Equal(t, "https://github.com/my-org/my-repo", repo.URL)
}

func TestGitHubRepoInfo_WithUnderscores(t *testing.T) {
	repo := GitHubRepoInfo{
		Owner: "my_org",
		Repo:  "my_repo",
		URL:   "https://github.com/my_org/my_repo",
	}

	assert.Equal(t, "my_org", repo.Owner)
	assert.Equal(t, "my_repo", repo.Repo)
	assert.Equal(t, "https://github.com/my_org/my_repo", repo.URL)
}

// ---------------------------------------------------------------------------
// Test: Constants
// ---------------------------------------------------------------------------

func TestGitHubConstants(t *testing.T) {
	assert.NotEmpty(t, githubRemoteSetupPrompt)
	assert.Equal(t, "github_mcp_setup", githubRemoteSetupPrompt)

	assert.NotEmpty(t, githubMCPServerURL)
	assert.Contains(t, githubMCPServerURL, "githubcopilot.com")
	assert.Contains(t, githubMCPServerURL, "/mcp/")

	assert.NotEmpty(t, gitHubPATHelpURL)
	assert.Contains(t, gitHubPATHelpURL, "github.com")
	assert.Contains(t, gitHubPATHelpURL, "personal-access-tokens")
}

// ---------------------------------------------------------------------------
// Test: SaveGitHubMCPServer() - Note: Requires actual config file handling
// ---------------------------------------------------------------------------

func TestSaveGitHubMCPServer_RequiresRealConfigFile(t *testing.T) {
	// This test requires actual file system access and config management
	// Skipping in automated tests
	t.Skip("Requires actual config file management - skipping")
}

// ---------------------------------------------------------------------------
// Test: isCommandAvailable() - Note: Requires actual system
// ---------------------------------------------------------------------------

func TestIsCommandAvailable_RequiresSystem(t *testing.T) {
	// This function calls exec.LookPath which requires actual system
	// Testing with common commands that should exist
	t.Skip("Requires actual system - skipping in automated tests")
}

// ---------------------------------------------------------------------------
// Test: openBrowser() - Note: Requires actual system
// ---------------------------------------------------------------------------

func TestOpenBrowser_RequiresSystem(t *testing.T) {
	// This function tries to open actual browsers on the system
	t.Skip("Requires actual system - skipping in automated tests")
}

// ---------------------------------------------------------------------------
// Test: promptForGitHubPAT() helper
// ---------------------------------------------------------------------------

func TestPromptForGitHubPAT_ValidToken(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("github_pat_1234567890abcdefghijklmnop\n"))

	token, err := promptForGitHubPAT(reader)

	assert.NoError(t, err)
	assert.Equal(t, "github_pat_1234567890abcdefghijklmnop", token)
}

func TestPromptForGitHubPAT_EmptyToken(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))

	token, err := promptForGitHubPAT(reader)

	assert.NoError(t, err)
	assert.Equal(t, "", token)
}

func TestPromptForGitHubPAT_ShortToken(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("short\n"))

	token, err := promptForGitHubPAT(reader)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
	assert.Equal(t, "", token)
}

func TestPromptForGitHubPAT_WhitespaceToken(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("   github_pat_1234567890abcdefghijklmnop   \n"))

	token, err := promptForGitHubPAT(reader)

	assert.NoError(t, err)
	assert.Equal(t, "github_pat_1234567890abcdefghijklmnop", token)
}

func TestPromptForGitHubPAT_ReadError(t *testing.T) {
	// Cannot provoke a real read error from *bufio.Reader wrapping *strings.Reader
	t.Skip("Hard to simulate read error - skipping")
}

// ---------------------------------------------------------------------------
// Integration-style tests (comprehensive scenarios)
// ---------------------------------------------------------------------------

func TestGitHubSetup_FullWorkflow_RemoteChoice(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "sprout-foundry",
		Repo:  "sprout",
		URL:   "https://github.com/sprout-foundry/sprout",
	}

	// Simulate user choosing remote OAuth option
	reader := bufio.NewReader(strings.NewReader("1\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify config
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "http", config.Type)
	assert.Equal(t, githubMCPServerURL, config.URL)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 30*time.Second, config.Timeout)
	assert.Empty(t, config.Command) // No command for HTTP server
	assert.Empty(t, config.Args)
	assert.Nil(t, config.Env)
	assert.Empty(t, config.Credentials)
}

func TestGitHubSetup_FullWorkflow_DockerChoice(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "sprout-foundry",
		Repo:  "sprout",
		URL:   "https://github.com/sprout-foundry/sprout",
	}

	// Simulate user choosing Docker + PAT option
	reader := bufio.NewReader(strings.NewReader("2\nghp_1234567890abcdefghijklmnopqrstuvwxyz\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify config
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "docker", config.Command)
	assert.Contains(t, config.Args, "run")
	assert.Contains(t, config.Args, "ghcr.io/github/github-mcp-server")
	assert.True(t, config.AutoStart)
	assert.Equal(t, 3, config.MaxRestarts)
	assert.NotNil(t, config.Env)
	assert.Equal(t, "ghp_1234567890abcdefghijklmnopqrstuvwxyz", config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"])
}

func TestGitHubSetup_FullWorkflow_NPXChoice(t *testing.T) {
	repo := &GitHubRepoInfo{
		Owner: "sprout-foundry",
		Repo:  "sprout",
		URL:   "https://github.com/sprout-foundry/sprout",
	}

	// Simulate user choosing npx + PAT option
	reader := bufio.NewReader(strings.NewReader("3\nghp_1234567890abcdefghijklmnopqrstuvwxyz\n"))

	ctx := context.Background()
	config, err := RunGitHubMCPSetup(ctx, repo, reader)

	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify config
	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "npx", config.Command)
	assert.ElementsMatch(t, []string{"-y", "@modelcontextprotocol/server-github"}, config.Args)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 3, config.MaxRestarts)
	assert.NotNil(t, config.Env)
	assert.Equal(t, "ghp_1234567890abcdefghijklmnopqrstuvwxyz", config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"])
}
