package commands

// commit_helpers.go — helpers for CommitCommand extracted from
// commit_command.go: LLM client preparation, the manual commit-message
// input, the staged-file summary, the commit-confirm picker, the commit
// creation, and the $EDITOR handler. Split out of commit_command.go.
import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	agent "github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// prepareCommitClient builds the LLM client for commit message generation:
// the commit role's provider/model first (SP-150 §150b — the commit
// settings alias the commit role), falling back to the chat agent's
// provider/model.
func (c *CommitCommand) prepareCommitClient(cfg *configuration.Config, chatAgent *agent.Agent) (api.ClientInterface, api.ClientType, string) {
	var client api.ClientInterface
	var clientType api.ClientType
	var model string

	// Use the resolved commit role (commit settings alias it) if available
	if cfg != nil {
		commitProvider, commitModel := cfg.ResolveRole(configuration.RoleCommit)
		if commitProvider != "" {
			clientType = api.ClientType(commitProvider)
			model = commitModel
			if cl, ce := factory.CreateProviderClient(clientType, model); ce == nil {
				client = cl
			}
		}
	}

	// Fall back to chatAgent's config if client not created
	if client == nil && chatAgent != nil {
		configManager := chatAgent.GetConfigManager()
		if configManager != nil {
			if ct, e := configManager.GetProvider(); e == nil {
				clientType = ct
				model = configManager.GetModelForProvider(clientType)
				if cl, ce := factory.CreateProviderClient(clientType, model); ce == nil {
					client = cl
				}
			}
		}
	}
	return client, clientType, model
}

// readManualCommitMessage prints the truncated staged diff and reads a commit
// message from reader (terminated by a blank line). Returns the message and
// whether the commit should be aborted (empty input).
func (c *CommitCommand) readManualCommitMessage(reader *bufio.Reader, diffOutput []byte) (string, bool) {
	// Manual fallback when LLM client isn't available
	c.println("")
	c.println(console.GlyphInfo.Prefix() + "Staged diff (truncated):")
	preview := string(diffOutput)
	if len(preview) > 2000 {
		preview = preview[:2000] + "\n... (truncated)"
	}
	c.println(preview)
	c.println("")
	c.println(console.GlyphAction.Prefix() + "Enter commit message (end with a blank line):")
	var b strings.Builder
	empty := 0
	for {
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) == "" {
			empty++
			if empty >= 1 {
				break
			}
		} else {
			empty = 0
		}
		b.WriteString(line)
	}
	commitMessage := strings.TrimSpace(b.String())
	if commitMessage == "" {
		c.println(console.GlyphError.Prefix() + "Empty commit message; aborting")
		return "", true
	}
	return commitMessage, false
}

// printStagedFileSummary prints the staged-files list (capped at ten) and
// the commit message.
func (c *CommitCommand) printStagedFileSummary(stagedFilenames []string, commitMessage string) {
	c.println("")
	if len(stagedFilenames) > 0 {
		c.printf("Committing %d staged file(s):\n", len(stagedFilenames))
		const maxList = 10
		for i, name := range stagedFilenames {
			if i >= maxList {
				remaining := len(stagedFilenames) - maxList
				if remaining > 0 {
					c.printf("... (+%d more)\n", remaining)
				}
				break
			}
			c.printf("- %s\n", name)
		}
	}
	c.println("")
}

// promptCommitChoice shows the commit-confirm picker (Approve / Retry / Edit /
// Cancel) and returns (editedMessage, action, cancelled, error) where action
// is "y" (proceed), "r" (regenerate), or "e" (proceed with edited message).
func (c *CommitCommand) promptCommitChoice(message string) (string, string, bool, error) {
	// Unified picker for the commit-confirm choice. Replaces the
	// prior dual-path (PromptChoice when AGENT_CONSOLE=1 / stdin
	// y/n/e/r loop otherwise) with a single SelectList that
	// degrades to numbered-list+stdin on non-TTY. The retry loop
	// stays — Retry re-enters the LLM call, Edit opens $EDITOR
	// and exits the retry loop.
	picker := console.NewSelectList(console.SelectListOptions{
		Title: "Proceed with commit?",
		Items: []console.SelectItem{
			{Label: "Approve", Detail: "create the commit now", Value: "y"},
			{Label: "Retry", Detail: "regenerate message", Value: "r"},
			{Label: "Edit", Detail: "open $EDITOR", Value: "e"},
			{Label: "Cancel", Detail: "abort", Value: "n"},
		},
		PageSize: 4,
	})
	choice, ok, perr := picker.Run(context.Background())
	if perr != nil {
		return "", "", false, fmt.Errorf("confirmation failed: %w", perr)
	}
	if !ok || choice == "n" {
		c.println("Commit cancelled")
		return "", "", true, nil
	}
	switch choice {
	case "r":
		c.println("Regenerating commit message...")
		return "", "r", false, nil
	case "e":
		edited, err := editInEditor(message)
		if err != nil {
			return "", "", false, fmt.Errorf("editor failed: %w", err)
		}
		if strings.TrimSpace(edited) == "" {
			c.println("Empty commit message; aborting")
			return "", "", true, nil
		}
		return edited, "e", false, nil
	case "y":
		return "", "y", false, nil
	default:
		// Unreachable: the picker only yields y/r/e/n, and n is handled above.
		return "", "r", false, nil
	}
}

// runCommitCreation writes the message to a temp file, runs `git commit -F`,
// and reports the result.
func (c *CommitCommand) runCommitCreation(message string) error {
	c.println("")
	c.println(console.GlyphAction.Prefix() + "Creating commit...")

	tempFile := "commit_msg.txt"
	err := os.WriteFile(tempFile, []byte(message), 0644)
	if err != nil {
		return fmt.Errorf("failed to create temporary commit message file: %w", err)
	}
	defer os.Remove(tempFile)

	cmd := gitCommand("commit", "-F", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create commit: %w\nOutput: %s", err, string(output))
	}

	c.println(console.GlyphSuccess.Prefix() + "Commit created successfully!")
	c.printf("Output: %s\n", string(output))
	return nil
}

// editInEditor opens $VISUAL or $EDITOR to edit content, returns the edited text
func editInEditor(initial string) (string, error) {
	// Create temp file
	f, err := os.CreateTemp("", "sprout_commit_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	path := f.Name()
	_, _ = f.WriteString(initial)
	f.Close()

	// Choose editor
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Release stdin to cooked mode so the editor reads keystrokes
	// normally. No-op when no turn / steer reader is active (the
	// common slash-command path).
	if err := clihooks.WithCookedStdin(cmd.Run); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	// Read back
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return "", fmt.Errorf("failed to read edited file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
