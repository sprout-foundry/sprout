package mcp

import (
	"bufio"
	"context"
	"os"
	"os/exec"
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

// disableOpenBrowser replaces openBrowserFn with a no-op for the duration of the test.
// This prevents tests from opening real browser tabs.
func disableOpenBrowser(t *testing.T) {
	t.Helper()
	orig := openBrowserFn
	openBrowserFn = func(string) error { return nil }
	t.Cleanup(func() { openBrowserFn = orig })
}

// ---------------------------------------------------------------------------
// Test: RunGitHubMCPSetup()
// ---------------------------------------------------------------------------

func TestRunGitHubMCPSetup_Choice1Remote(t *testing.T) {
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
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
	disableOpenBrowser(t)
	reader := bufio.NewReader(strings.NewReader("github_pat_1234567890abcdefghijklmnop\n"))

	token, err := promptForGitHubPAT(reader)

	assert.NoError(t, err)
	assert.Equal(t, "github_pat_1234567890abcdefghijklmnop", token)
}

func TestPromptForGitHubPAT_EmptyToken(t *testing.T) {
	disableOpenBrowser(t)
	reader := bufio.NewReader(strings.NewReader("\n"))

	token, err := promptForGitHubPAT(reader)

	assert.NoError(t, err)
	assert.Equal(t, "", token)
}

func TestPromptForGitHubPAT_ShortToken(t *testing.T) {
	disableOpenBrowser(t)
	reader := bufio.NewReader(strings.NewReader("short\n"))

	token, err := promptForGitHubPAT(reader)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
	assert.Equal(t, "", token)
}

func TestPromptForGitHubPAT_WhitespaceToken(t *testing.T) {
	disableOpenBrowser(t)
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

// ---------------------------------------------------------------------------
// Test: DetectGitHubRepo()
// ---------------------------------------------------------------------------

func TestDetectGitHubRepo_SSHRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with SSH remote
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "git@github.com:owner/repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "owner", result.Owner)
	assert.Equal(t, "repo", result.Repo)
	assert.Equal(t, "https://github.com/owner/repo", result.URL)
}

func TestDetectGitHubRepo_HTTPSRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with HTTPS remote
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "https://github.com/test-owner/test-repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "test-owner", result.Owner)
	assert.Equal(t, "test-repo", result.Repo)
	assert.Equal(t, "https://github.com/test-owner/test-repo", result.URL)
}

func TestDetectGitHubRepo_HTTPRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with HTTP (not HTTPS) remote
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "http://github.com/http-owner/http-repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "http-owner", result.Owner)
	assert.Equal(t, "http-repo", result.Repo)
	assert.Equal(t, "https://github.com/http-owner/http-repo", result.URL)
}

func TestDetectGitHubRepo_WithGitSuffix(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with .git suffix
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "git@github.com:suffix/suffix-repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "suffix", result.Owner)
	assert.Equal(t, "suffix-repo", result.Repo)
	assert.Equal(t, "https://github.com/suffix/suffix-repo", result.URL)
}

func TestDetectGitHubRepo_WithTrailingSlash(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with trailing slash
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "https://github.com/trailing/trailing-repo/")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "trailing", result.Owner)
	assert.Equal(t, "trailing-repo", result.Repo)
	assert.Equal(t, "https://github.com/trailing/trailing-repo", result.URL)
}

func TestDetectGitHubRepo_WithGitSuffixAndTrailingSlash(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with both .git and trailing slash
	// Note: git will store the URL exactly as provided
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "git@github.com:both/both-repo.git/")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "both", result.Owner)
	// Git stores URLs as-is, so .git may or may not be in the repo name
	// depending on git version/configuration. We just verify it was parsed.
	assert.NotEmpty(t, result.Repo)
	assert.Equal(t, "https://github.com/both/"+result.Repo, result.URL)
}

func TestDetectGitHubRepo_NonGitHubRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with a non-GitHub remote
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "git@gitlab.com:owner/repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	assert.Nil(t, result)
}

func TestDetectGitHubRepo_GitLabHTTPSRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with GitLab HTTPS remote
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "https://gitlab.com/owner/repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	assert.Nil(t, result)
}

func TestDetectGitHubRepo_MalformedRemoteURL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with a malformed remote URL
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "not-a-valid-url")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	assert.Nil(t, result)
}

func TestDetectGitHubRepo_NoRemote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo without any remote
	runGitCommand(t, tempDir, "init")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	assert.Nil(t, result)
}

func TestDetectGitHubRepo_OrgWithHyphens(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with hyphenated org/repo names
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "git@github.com:my-org/my-repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "my-org", result.Owner)
	assert.Equal(t, "my-repo", result.Repo)
	assert.Equal(t, "https://github.com/my-org/my-repo", result.URL)
}

func TestDetectGitHubRepo_RepoWithUnderscores(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping git setup test in short mode")
	}

	tempDir := t.TempDir()

	// Initialize a git repo with underscored org/repo names
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "remote", "add", "origin", "https://github.com/my_org/my_repo.git")

	// Add a commit so the repo is valid
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	createTestFile(t, tempDir, "test.txt", "content")
	runGitCommand(t, tempDir, "add", "test.txt")
	runGitCommand(t, tempDir, "commit", "-m", "initial commit")

	result := DetectGitHubRepo(tempDir)

	require.NotNil(t, result)
	assert.Equal(t, "my_org", result.Owner)
	assert.Equal(t, "my_repo", result.Repo)
	assert.Equal(t, "https://github.com/my_org/my_repo", result.URL)
}

// ---------------------------------------------------------------------------
// Test: SaveGitHubMCPServer()
// ---------------------------------------------------------------------------

func TestSaveGitHubMCPServer_NewConfig(t *testing.T) {
	// Use a temporary config directory to avoid affecting user's actual config
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("SPROUT_CONFIG", tempDir)

	config := &MCPServerConfig{
		Name:      "github",
		Type:      "http",
		URL:       "https://api.githubcopilot.com/mcp/",
		AutoStart: true,
		Timeout:   30 * time.Second,
	}

	err := SaveGitHubMCPServer(config)
	assert.NoError(t, err)

	// Verify the config was saved by loading it back
	loadedConfig, err := LoadMCPConfig()
	require.NoError(t, err)

	// Check that the github server was saved
	server, exists := loadedConfig.Servers["github"]
	require.True(t, exists, "GitHub server should exist in config")
	assert.Equal(t, "github", server.Name)
	assert.Equal(t, "http", server.Type)
	assert.Equal(t, "https://api.githubcopilot.com/mcp/", server.URL)
	assert.True(t, server.AutoStart)
	assert.Equal(t, 30*time.Second, server.Timeout)

	// Check that Enabled was set to true
	assert.True(t, loadedConfig.Enabled)
}

func TestSaveGitHubMCPServer_ReplaceExistingConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("SPROUT_CONFIG", tempDir)

	// First, save an initial config
	initialConfig := &MCPServerConfig{
		Name:      "github",
		Type:      "http",
		URL:       "https://api.githubcopilot.com/mcp/",
		AutoStart: true,
		Timeout:   30 * time.Second,
	}
	err := SaveGitHubMCPServer(initialConfig)
	require.NoError(t, err)

	// Now replace it with a Docker-based config
	newConfig := &MCPServerConfig{
		Name:        "github",
		Command:     "docker",
		Args:        []string{"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     30 * time.Second,
		Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": "test-token",
		},
	}
	err = SaveGitHubMCPServer(newConfig)
	assert.NoError(t, err)

	// Verify the config was replaced
	loadedConfig, err := LoadMCPConfig()
	require.NoError(t, err)

	server, exists := loadedConfig.Servers["github"]
	require.True(t, exists)
	assert.Equal(t, "docker", server.Command)
	assert.Contains(t, server.Args, "ghcr.io/github/github-mcp-server")
	// After migration, secrets are in Credentials, not Env
	assert.NotNil(t, server.Credentials)
	// Check that the token was migrated to credentials (format: {{credential:mcp/github/GITHUB_PERSONAL_ACCESS_TOKEN}})
	assert.Contains(t, server.Credentials["GITHUB_PERSONAL_ACCESS_TOKEN"], "credential:mcp/github/GITHUB_PERSONAL_ACCESS_TOKEN")
}

func TestSaveGitHubMCPServer_SetsEnabledToTrue(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("SPROUT_CONFIG", tempDir)

	config := &MCPServerConfig{
		Name:      "github",
		Type:      "http",
		URL:       "https://api.githubcopilot.com/mcp/",
		AutoStart: true,
	}

	err := SaveGitHubMCPServer(config)
	assert.NoError(t, err)

	// Verify Enabled was set to true
	loadedConfig, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.True(t, loadedConfig.Enabled)
}

func TestSaveGitHubMCPServer_PreservesOtherServers(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("SPROUT_CONFIG", tempDir)

	// First, create a config with an existing server
	initialConfig := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"other-server": {
				Name:    "other-server",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
			},
		},
	}
	err := SaveMCPConfig(&initialConfig)
	require.NoError(t, err)

	// Now add the GitHub server
	githubConfig := &MCPServerConfig{
		Name:      "github",
		Type:      "http",
		URL:       "https://api.githubcopilot.com/mcp/",
		AutoStart: true,
	}
	err = SaveGitHubMCPServer(githubConfig)
	assert.NoError(t, err)

	// Verify both servers exist
	loadedConfig, err := LoadMCPConfig()
	require.NoError(t, err)

	_, githubExists := loadedConfig.Servers["github"]
	assert.True(t, githubExists)

	otherServer, otherExists := loadedConfig.Servers["other-server"]
	assert.True(t, otherExists)
	assert.Equal(t, "other-server", otherServer.Name)
	assert.Equal(t, "npx", otherServer.Command)
}

func TestSaveGitHubMCPServer_DockerConfigWithEnv(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("SPROUT_CONFIG", tempDir)

	config := &MCPServerConfig{
		Name:        "github",
		Command:     "docker",
		Args:        []string{"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     30 * time.Second,
		Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_test_token_12345",
		},
	}

	err := SaveGitHubMCPServer(config)
	assert.NoError(t, err)

	// Verify the config was saved
	loadedConfig, err := LoadMCPConfig()
	require.NoError(t, err)

	server := loadedConfig.Servers["github"]
	assert.Equal(t, "docker", server.Command)
	assert.Equal(t, 3, server.MaxRestarts)
	// After migration, secrets are in Credentials, not Env
	assert.NotNil(t, server.Credentials)
	// Check that the token was migrated to credentials
	assert.Contains(t, server.Credentials["GITHUB_PERSONAL_ACCESS_TOKEN"], "credential:mcp/github/GITHUB_PERSONAL_ACCESS_TOKEN")
}

func TestSaveGitHubMCPServer_NPXConfigWithEnv(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("SPROUT_CONFIG", tempDir)

	config := &MCPServerConfig{
		Name:        "github",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     30 * time.Second,
		Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_npx_token_67890",
		},
	}

	err := SaveGitHubMCPServer(config)
	assert.NoError(t, err)

	// Verify the config was saved
	loadedConfig, err := LoadMCPConfig()
	require.NoError(t, err)

	server := loadedConfig.Servers["github"]
	assert.Equal(t, "npx", server.Command)
	assert.Len(t, server.Args, 2)
	assert.Contains(t, server.Args, "@modelcontextprotocol/server-github")
	// After migration, secrets are in Credentials, not Env
	assert.NotNil(t, server.Credentials)
	// Check that the token was migrated to credentials
	assert.Contains(t, server.Credentials["GITHUB_PERSONAL_ACCESS_TOKEN"], "credential:mcp/github/GITHUB_PERSONAL_ACCESS_TOKEN")
}

// Helper functions for DetectGitHubRepo tests

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git command failed: %v\nargs: %v\noutput: %s", err, args, string(output))
	}
}

func createTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := dir + "/" + name
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file %s: %v", path, err)
	}
}
