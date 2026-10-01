package agent

// tool_handlers_shell_commit.go — the commit / pull-request tool
// handlers: handleCommitTool + executeCommit, the git-approval prompter
// adapter (gitApprovalPrompterAdapter + PromptForApproval), and
// handleCreatePullRequest. Split out of tool_handlers_shell.go.
import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// gitApprovalPrompterAdapter implements the GitApprovalPrompter interface using the Agent
type gitApprovalPrompterAdapter struct {
	agent *Agent
}

// PromptForApproval prompts the user for approval to execute a git write operation.
// When a WebUI client is connected, the approval request is routed through the event
// bus so a dialog appears in the browser. Otherwise it falls back to the terminal UI
// or stdin.
func (a *gitApprovalPrompterAdapter) PromptForApproval(command string) (bool, error) {
	ag := a.agent

	// Prefer the WebUI approval path when a browser tab is connected and the
	// security approval manager is available. This mirrors the pattern used by
	// the main ExecuteTool security flow in tool_definitions.go.
	if mgr := ag.GetSecurityApprovalMgr(); mgr != nil && ag.GetEventBus() != nil && !ag.IsSubagent() && ag.HasActiveWebUIClients() {
		if ag.debug {
			ag.debugLog("[GIT] Requesting git approval via webui for: %s\n", command)
		}
		clientID := ag.GetEventClientID()
		userID := ag.GetEventUserID()
		approved := mgr.RequestToolApproval(ag.GetEventBus(), clientID, userID, "git", "CAUTION", fmt.Sprintf("Git operation: %s", command), nil)
		return approved, nil
	}

	// Terminal UI or stdin fallback
	prompt := fmt.Sprintf("Execute git command: %s", command)

	choices := []ChoiceOption{
		{Label: "Approve", Value: "y"},
		{Label: "Cancel", Value: "n"},
	}

	fmt.Printf("\n[LOCK] Git Operation Requires Approval\n")
	fmt.Printf("Command: %s\n", command)
	fmt.Printf("\n")

	choice, err := ag.PromptChoice(prompt, choices)
	if err != nil {
		if err == ErrUINotAvailable {
			return tools.PromptForGitApprovalStdin(command)
		}
		return false, agenterrors.NewTransientError("failed to prompt for git approval", err)
	}

	return choice == "y", nil
}

// handleCommitTool handles the dedicated commit tool
// This tool allows committing without requiring user interaction,
// using the automated commit flow with message generation
func handleCommitTool(_ context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract optional message parameter
	var message string
	if msg, exists := args["message"]; exists {
		var err error
		message, err = convertToString(msg, "message")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert message parameter", err)
		}
	}

	// Extract optional notes parameter (for context to integrate into auto-generated commit message)
	var notes string
	if n, exists := args["notes"]; exists {
		var err error
		notes, err = convertToString(n, "notes")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert notes parameter", err)
		}
	}

	// Check for staged changes first
	dir := a.effectiveCwd()
	cmd := exec.Command("git", "diff", "--staged", "--name-only")
	cmd.Dir = dir
	stagedOutput, err := cmd.CombinedOutput()
	if err != nil {
		return "", agenterrors.NewTransientError("failed to check for staged changes", err)
	}

	if len(strings.TrimSpace(string(stagedOutput))) == 0 {
		return "No staged changes to commit. Stage files first using 'git add' or the git tool, then use the commit tool.", nil
	}

	// Get the agent's config manager to access provider settings
	var configManager configManagerInterface
	if cm := a.GetConfigManager(); cm != nil {
		configManager = cm
	}

	// EA risk cascade: reject commit if the message or notes contain force flags
	// or other high-risk patterns. This prevents the EA from being tricked into
	// committing messages with embedded shell commands or dangerous patterns.
	// Note: This is a defense-in-depth check; commit messages are not shell commands,
	// but an LLM might construct a message containing patterns that could be
	// misinterpreted by downstream systems.
	if message != "" {
		if risk := a.EvaluateOperationRisk(message); risk == configuration.RiskLevelHigh {
			return "", agenterrors.NewSecurityError(
				fmt.Sprintf("commit rejected by persona risk cascade: high-risk pattern detected in message (message: '%s')", message), nil,
			)
		}
	}

	// Auto-approve commits when the persona has CapabilityGitWrite.
	// Subagents also auto-approve because they have no interactive UI to prompt with.
	isSubagent := a.IsSubagent()
	canGitWrite := a.isGitWriteAllowed()

	if !canGitWrite && !isSubagent {
		// Prompt user for approval before committing (only in interactive mode)
		choices := []ChoiceOption{
			{Label: "Approve", Value: "approve"},
			{Label: "Deny", Value: "deny"},
		}

		choice, err := a.PromptChoice("Allow agent to commit staged changes?", choices)
		if err != nil {
			if errors.Is(err, ErrUINotAvailable) {
				// Fall back to allowing the commit when UI is not available,
				// since this tool is designed for autonomous agents and was explicitly called
			} else {
				return "", agenterrors.NewTransientError("approval prompt failed", err)
			}
		} else if choice != "approve" {
			return "Commit cancelled by user.", nil
		}
	}

	// Execute the commit using the shared helper function
	commitHash, err := executeCommit(message, notes, configManager, a)
	if err != nil {
		return "", agenterrors.NewTransientError("failed to execute commit", err)
	}

	return fmt.Sprintf("Committed successfully: %s", commitHash), nil
}

// executeCommit performs the actual commit operation using the shared git.CommitExecutor.
// The agent is optional; when non-nil its elevation gate is consulted for secret
// detection before the commit is created.
func executeCommit(userMessage, notes string, configManager configManagerInterface, chatAgent *Agent) (string, error) {
	// Create LLM client if config manager is available
	var client api.ClientInterface
	if configManager != nil {
		provider, err := configManager.GetProvider()
		if err == nil {
			model := configManager.GetModelForProvider(provider)
			client, err = factory.CreateProviderClient(api.ClientType(provider), model)
			if err != nil {
				client = nil
			}
		}
	}

	var executor *git.CommitExecutor

	// Get workspace directory from the agent, if available.
	workDir := ""
	if chatAgent != nil {
		workDir = chatAgent.effectiveCwd()
	}

	// Wire the elevation gate into the commit executor if available.
	if chatAgent != nil && chatAgent.GetElevationGate() != nil {
		gate := chatAgent.GetElevationGate()
		secretHandler := func(securityResult git.CommitSecurityResult) bool {
			if !securityResult.HasConcerns {
				return true
			}
			action, err := gate.Evaluate(securityResult.Concerns, "commit")
			if err != nil {
				chatAgent.debugLog("[security] commit elevation error: %v\n", err)
				return false // default to blocking on error
			}
			return action != security.SecretBlock
		}
		executor = git.NewCommitExecutorWithSecurityCheck(client, userMessage, notes, secretHandler)
	} else {
		executor = git.NewCommitExecutor(client, userMessage, notes)
	}
	executor.Dir = workDir

	return executor.ExecuteCommit()
}

// handleCreatePullRequest handles the create_pull_request tool, creating a
// pull request on GitHub via the git.CreatePullRequest backend. Gated as a
// git-write operation — the persona must have CapabilityGitWrite (or the
// user must approve interactively).
func handleCreatePullRequest(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract required title parameter
	title, err := convertToString(args["title"], "title")
	if err != nil {
		return "", agenterrors.NewInvalidInputError("failed to convert title parameter", err)
	}
	if title == "" {
		return "", agenterrors.NewInvalidInputError("title parameter is required and must not be empty", nil)
	}

	// Extract optional body parameter
	var body string
	if b, exists := args["body"]; exists {
		body, err = convertToString(b, "body")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert body parameter", err)
		}
	}

	// Extract optional base parameter
	var base string
	if ba, exists := args["base"]; exists {
		base, err = convertToString(ba, "base")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert base parameter", err)
		}
	}

	// Extract optional head parameter
	var head string
	if h, exists := args["head"]; exists {
		head, err = convertToString(h, "head")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert head parameter", err)
		}
	}

	// Extract optional draft parameter
	var draft bool
	if d, exists := args["draft"]; exists {
		if dBool, ok := d.(bool); ok {
			draft = dBool
		}
	}

	// Extract optional repo_dir parameter
	var repoDir string
	if rd, exists := args["repo_dir"]; exists {
		repoDir, err = convertToString(rd, "repo_dir")
		if err != nil {
			return "", agenterrors.NewInvalidInputError("failed to convert repo_dir parameter", err)
		}
	}

	// Default repoDir to the agent's workspace root
	if repoDir == "" {
		repoDir = a.effectiveCwd()
	}

	// Git-write gate: mirror the handleCommitTool approval pattern.
	isSubagent := a.IsSubagent()
	canGitWrite := a.isGitWriteAllowed()

	if !canGitWrite && !isSubagent {
		// Prompt user for approval before creating PR
		choices := []ChoiceOption{
			{Label: "Approve", Value: "approve"},
			{Label: "Deny", Value: "deny"},
		}

		choice, err := a.PromptChoice("Allow agent to create a pull request?", choices)
		if err != nil {
			if errors.Is(err, ErrUINotAvailable) {
				// Fall back to allowing when UI is not available,
				// since this tool is designed for autonomous agents and was explicitly called
			} else {
				return "", agenterrors.NewTransientError("approval prompt failed", err)
			}
		} else if choice != "approve" {
			return "Pull request creation cancelled by user.", nil
		}
	}

	// Call the backend
	result, err := git.CreatePullRequest(ctx, repoDir, git.PullRequestRequest{
		Title: title,
		Body:  body,
		Base:  base,
		Head:  head,
		Draft: draft,
	})
	if err != nil {
		return "", agenterrors.NewTransientError("failed to create pull request", err)
	}

	return fmt.Sprintf("Pull request created successfully!\n\nURL: %s\nNumber: #%d\nState: %s", result.URL, result.Number, result.State), nil
}
