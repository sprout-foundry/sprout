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

// GitHubSetupAgentInterface defines the interface needed from an agent for GitHub MCP setup
type GitHubSetupAgentInterface interface {
	GetConfigManager() interface {
		GetConfig() *configuration.Config
		UpdateConfig(func(c *configuration.Config) error) error
	}
	RefreshMCPTools() error
}

// Function variables for dependency injection (useful for testing)
var (
	getwdFunc                = os.Getwd
	shouldPromptGitHubSetup  = mcp.ShouldPromptGitHubSetup
	detectGitHubRepo         = mcp.DetectGitHubRepo
	runGitHubMCPSetup        = mcp.RunGitHubMCPSetup
	saveGitHubMCPServer      = mcp.SaveGitHubMCPServer
	newReaderFromStdin       = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}
)

// promptGitHubMCPSetupIfNeeded checks whether we should offer GitHub MCP
// setup and, in interactive mode, asks the user. It is safe to call for
// direct/non-interactive invocations too — those will simply return.
func promptGitHubMCPSetupIfNeeded(chatAgent interface{}) {
	// Get the config manager from either an interface or *agent.Agent
	var cfgManager interface {
		GetConfig() *configuration.Config
		UpdateConfig(func(c *configuration.Config) error) error
	}
	var refreshMCPTools func() error

	switch a := chatAgent.(type) {
	case GitHubSetupAgentInterface:
		cfgManager = a.GetConfigManager()
		refreshMCPTools = a.RefreshMCPTools
	case *agent.Agent:
		cfgManager = a.GetConfigManager()
		refreshMCPTools = a.RefreshMCPTools
	default:
		// If it's not a type we recognize, try to use the adapter approach
		if adapter, ok := chatAgent.(*AgentAdapter); ok {
			cfgManager = adapter.GetConfigManager()
			refreshMCPTools = adapter.RefreshMCPTools
		} else {
			return
		}
	}

	cfg := cfgManager.GetConfig()
	if cfg == nil || cfg.SkipPrompt {
		return
	}

	workingDir, err := getwdFunc()
	if err != nil {
		return
	}

	if !shouldPromptGitHubSetup(workingDir, cfg.MCP, cfg.DismissedPrompts) {
		return
	}

	repo := detectGitHubRepo(workingDir)
	if repo == nil {
		return // shouldPromptGitHubSetup already guards, but be safe.
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

	reader := newReaderFromStdin(os.Stdin)
	choice, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	choice = strings.TrimSpace(strings.ToLower(choice))

	switch choice {
	case "s", "setup", "yes", "y":
		server, setupErr := runGitHubMCPSetup(context.Background(), repo, reader)
		if setupErr != nil {
			fmt.Printf("[WARN] GitHub MCP setup failed: %v\n", setupErr)
			return
		}
		if server == nil {
			return // User cancelled
		}
		if saveErr := saveGitHubMCPServer(server); saveErr != nil {
			fmt.Printf("[WARN] Failed to save GitHub MCP config: %v\n", saveErr)
			return
		}
		// Reload MCP in the running agent so tools become available immediately.
		if refreshMCPTools != nil {
			refreshMCPTools()
		}
		fmt.Println("   (GitHub tools will be available for this session)")

	case "n", "never", "no":
		if saveErr := cfgManager.UpdateConfig(func(c *configuration.Config) error {
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

// AgentAdapter wraps *agent.Agent to implement GitHubSetupAgentInterface
type AgentAdapter struct {
	agent *agent.Agent
}

func (a *AgentAdapter) GetConfigManager() interface {
	GetConfig() *configuration.Config
	UpdateConfig(func(c *configuration.Config) error) error
} {
	return a.agent.GetConfigManager()
}

func (a *AgentAdapter) RefreshMCPTools() error {
	return a.agent.RefreshMCPTools()
}