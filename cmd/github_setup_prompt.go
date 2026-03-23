// GitHub MCP setup prompt for interactive mode
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// promptGitHubMCPSetupIfNeeded checks whether we should offer GitHub MCP
// setup and, in interactive mode, asks the user. It is safe to call for
// direct/non-interactive invocations too — those will simply return.
func promptGitHubMCPSetupIfNeeded(chatAgent *agent.Agent) {
	cfg := chatAgent.GetConfigManager().GetConfig()
	if cfg == nil || cfg.SkipPrompt {
		return
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return
	}

	if !mcp.ShouldPromptGitHubSetup(workingDir, cfg.MCP, cfg.DismissedPrompts) {
		return
	}

	repo := mcp.DetectGitHubRepo(workingDir)
	if repo == nil {
		return // ShouldPromptGitHubSetup already guards, but be safe.
	}

	fmt.Println()
	fmt.Printf("[link] Detected GitHub repo: %s/%s\n", repo.Owner, repo.Repo)
	fmt.Println()
	fmt.Println("Set up GitHub MCP integration for rich context (issues, PRs,")
	fmt.Println("actions, discussions, releases directly from GitHub)?")
	fmt.Println()
	fmt.Println("  [s] Set up with a Personal Access Token (PAT)")
	fmt.Println("  [n] Don't ask again")
	fmt.Println("  [Enter] Skip for now")
	fmt.Print("  Choose: ")

	reader := bufio.NewReader(os.Stdin)
	choice, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	choice = strings.TrimSpace(strings.ToLower(choice))

	switch choice {
	case "s", "setup", "yes", "y":
		server, setupErr := mcp.RunGitHubMCPSetup(context.Background(), repo, reader)
		if setupErr != nil {
			fmt.Printf("[WARN] GitHub MCP setup failed: %v\n", setupErr)
			return
		}
		if server == nil {
			return // User cancelled
		}
		if saveErr := mcp.SaveGitHubMCPServer(server); saveErr != nil {
			fmt.Printf("[WARN] Failed to save GitHub MCP config: %v\n", saveErr)
			return
		}
		// Reload MCP in the running agent so tools become available immediately.
		chatAgent.RefreshMCPTools()
		fmt.Println("   (GitHub tools will be available for this session)")

	case "n", "never", "no":
		if saveErr := chatAgent.GetConfigManager().UpdateConfig(func(c *configuration.Config) error {
			if c.DismissedPrompts == nil {
				c.DismissedPrompts = make(map[string]bool)
			}
			c.DismissedPrompts["github_mcp_setup"] = true
			return nil
		}); saveErr != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to save preference: %v\n", saveErr)
		}
		fmt.Println("   Won't ask again. Re-enable with: ledit config set dismissed_prompts.github_mcp_setup false")
	}
	// Unrecognized choices (including empty/Enter) are silently ignored.
}
