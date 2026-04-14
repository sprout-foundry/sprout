package mcp

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// githubRemoteSetupPrompt is the key used in DismissedPrompts to track
	// whether the user has dismissed the GitHub MCP setup prompt.
	githubRemoteSetupPrompt = "github_mcp_setup"

	// githubMCPServerURL is the URL for the remote GitHub Copilot MCP server.
	githubMCPServerURL = "https://api.githubcopilot.com/mcp/"

	// gitHubPATHelpURL is the URL to open in the browser for PAT creation.
	gitHubPATHelpURL = "https://github.com/settings/personal-access-tokens/new"
)

// GitHubRepoInfo holds information about a detected GitHub repository.
type GitHubRepoInfo struct {
	Owner string // GitHub owner/organisation
	Repo  string // Repository name
	URL   string // Full HTTPS URL, e.g. https://github.com/owner/repo
}

// DetectGitHubRepo checks if the current working directory is inside a git repo
// with a GitHub remote. Returns nil if not a GitHub repo.
func DetectGitHubRepo(workingDir string) *GitHubRepoInfo {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}

	remoteURL := strings.TrimSpace(string(output))
	return parseGitHubRemoteURL(remoteURL)
}

// parseGitHubRemoteURL extracts owner/repo from various git remote URL formats.
func parseGitHubRemoteURL(remote string) *GitHubRepoInfo {
	// Normalise: strip trailing .git and /
	remote = strings.TrimSuffix(remote, ".git")
	remote = strings.TrimSuffix(remote, "/")

	var owner, repo string

	if strings.HasPrefix(remote, "git@github.com:") {
		// SSH: git@github.com:owner/repo
		path := strings.TrimPrefix(remote, "git@github.com:")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			owner = parts[0]
			repo = parts[1]
		}
	} else if strings.HasPrefix(remote, "https://github.com/") ||
		strings.HasPrefix(remote, "http://github.com/") {
		// HTTPS: https://github.com/owner/repo
		prefix := "https://github.com/"
		if strings.HasPrefix(remote, "http://") {
			prefix = "http://github.com/"
		}
		path := strings.TrimPrefix(remote, prefix)
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			owner = parts[0]
			repo = parts[1]
		}
	}

	if owner == "" || repo == "" {
		return nil
	}

	return &GitHubRepoInfo{
		Owner: owner,
		Repo:  repo,
		URL:   fmt.Sprintf("https://github.com/%s/%s", owner, repo),
	}
}

// IsGitHubMCPConfigured checks if a GitHub MCP server is already configured
// in the given MCPConfig.
func IsGitHubMCPConfigured(config MCPConfig) bool {
	server, exists := config.Servers["github"]
	if !exists {
		return false
	}
	return server.URL != "" || server.Command != ""
}

// ShouldPromptGitHubSetup returns true if we should prompt the user about
// GitHub MCP setup. Returns false if already configured, the prompt was
// previously dismissed, or the working directory is not a GitHub repo.
func ShouldPromptGitHubSetup(workingDir string, cfg MCPConfig, dismissedPrompts map[string]bool) bool {
	if IsGitHubMCPConfigured(cfg) {
		return false
	}
	// Also check legacy mcp_config.json since that's where SaveGitHubMCPServer writes
	if legacyCfg, err := LoadMCPConfig(); err == nil && IsGitHubMCPConfigured(legacyCfg) {
		return false
	}
	if dismissedPrompts != nil && dismissedPrompts[githubRemoteSetupPrompt] {
		return false
	}
	repo := DetectGitHubRepo(workingDir)
	return repo != nil
}

// RunGitHubMCPSetup performs the interactive GitHub MCP server setup.
// It presents the user with three options matching cmd/mcp.go's setupGitHubMCPServer:
// (1) OAuth remote, (2) Docker+PAT, (3) npx+PAT.
// Returns the configured server config on success, or an error.
func RunGitHubMCPSetup(_ context.Context, repo *GitHubRepoInfo, reader *bufio.Reader) (*MCPServerConfig, error) {
	fmt.Println()
	fmt.Println("[oct] GitHub MCP Server Setup")
	fmt.Println("==========================")
	fmt.Println()

	// Installation method selection (matches cmd/mcp.go's setupGitHubMCPServer)
	fmt.Println("Select installation method:")
	fmt.Println()
	fmt.Println("  1. GitHub Remote MCP (OAuth) — recommended")
	fmt.Println("     • GitHub's hosted endpoint: https://api.githubcopilot.com/mcp/")
	fmt.Println("     • OAuth authentication (no token management needed)")
	fmt.Println("     • Requires a GitHub Copilot or Copilot Enterprise seat")
	fmt.Println()
	fmt.Println("  2. GitHub Local MCP (Docker + PAT)")
	fmt.Println("     • Runs via Docker locally")
	fmt.Println("     • Requires a Personal Access Token (PAT)")
	fmt.Println()
	fmt.Println("  3. GitHub Local MCP (npx + PAT)")
	fmt.Println("     • Runs via npx locally")
	fmt.Println("     • Requires a Personal Access Token (PAT)")
	fmt.Println()
	fmt.Print("Choice (1-3): ")

	choice, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}
	choice = strings.TrimSpace(choice)

	var server *MCPServerConfig

	switch choice {
	case "1", "":
		// Remote OAuth server — no token needed
		fmt.Println()
		fmt.Println("[info] Remote OAuth server selected.")
		fmt.Println("   Authentication will happen automatically via OAuth")
		fmt.Println("   when the agent first connects to the server.")
		fmt.Println()

		server = &MCPServerConfig{
			Name:      "github",
			Type:      "http",
			URL:       githubMCPServerURL,
			AutoStart: true,
			Timeout:   30 * time.Second,
		}

	case "2":
		// Docker + PAT configuration
		token, err := promptForGitHubPAT(reader)
		if err != nil {
			return nil, err
		}
		if token == "" {
			fmt.Println("   Cancelled.")
			return nil, nil
		}

		server = &MCPServerConfig{
			Name:        "github",
			Command:     "docker",
			Args:        []string{"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"},
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
			Env: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": token,
			},
		}

	case "3":
		// npx + PAT configuration
		token, err := promptForGitHubPAT(reader)
		if err != nil {
			return nil, err
		}
		if token == "" {
			fmt.Println("   Cancelled.")
			return nil, nil
		}

		server = &MCPServerConfig{
			Name:        "github",
			Command:     "npx",
			Args:        []string{"-y", "@modelcontextprotocol/server-github"},
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
			Env: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": token,
			},
		}

	default:
		return nil, fmt.Errorf("invalid choice: %s", choice)
	}

	fmt.Println("[OK] GitHub MCP server configured successfully!")
	return server, nil
}

// promptForGitHubPAT prompts the user for a GitHub Personal Access Token.
// Returns the trimmed token, or an empty string if the user cancels.
// Returns an error only for read failures.
func promptForGitHubPAT(reader *bufio.Reader) (string, error) {
	fmt.Println()
	fmt.Println("GitHub Personal Access Token is required for local MCP servers.")
	fmt.Printf("Create one at: %s\n", gitHubPATHelpURL)
	fmt.Println("Required permissions: repo, read:user, read:org, issues")
	fmt.Println()

	if err := openBrowser(gitHubPATHelpURL); err != nil {
		fmt.Printf("[WARN] Could not open browser: %v\n", err)
		fmt.Printf("   Please visit: %s\n", gitHubPATHelpURL)
	}

	fmt.Print("Paste your GitHub PAT (or press Enter to cancel): ")
	token, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	token = strings.TrimSpace(token)

	if token == "" {
		return "", nil
	}

	if len(token) < 10 {
		return "", fmt.Errorf("token seems too short, please paste the full PAT")
	}

	return token, nil
}

// openBrowser attempts to open a URL in the user's default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch {
	case isCommandAvailable("termux-open-url"):
		cmd = exec.Command("termux-open-url", url)
	case isCommandAvailable("termux-open"):
		cmd = exec.Command("termux-open", url)
	case isCommandAvailable("xdg-open"):
		cmd = exec.Command("xdg-open", url)
	case isCommandAvailable("open"):
		cmd = exec.Command("open", url)
	case isCommandAvailable("wslview"):
		cmd = exec.Command("wslview", url)
	default:
		return fmt.Errorf("no browser command found")
	}

	return cmd.Start()
}

// isCommandAvailable checks if a command exists on PATH.
func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// SaveGitHubMCPServer loads the current MCP config, adds/replaces the github
// server entry, and persists it via SaveMCPConfig.
func SaveGitHubMCPServer(config *MCPServerConfig) error {
	mcpConfig, err := LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	mcpConfig.Servers["github"] = *config
	mcpConfig.Enabled = true

	if err := SaveMCPConfig(&mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	return nil
}
