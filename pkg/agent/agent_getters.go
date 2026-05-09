package agent

import (
	"fmt"
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/prompts"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/sprout-foundry/sprout/pkg/validation"
)

// GetDebugLogPath returns the path to the current debug log file (if any)
func (a *Agent) GetDebugLogPath() string { return a.debugLogPath }

// Logger returns the agent logger, initializing it lazily if needed
func (a *Agent) Logger() *AgentLogger {
	if a.logger == nil {
		a.logger = NewAgentLogger(a)
	}
	return a.logger
}

// GetUnsafeMode returns whether unsafe mode is enabled
func (a *Agent) GetUnsafeMode() bool { return a.security.GetUnsafeMode() }

// SetUnsafeMode sets the unsafe mode flag
func (a *Agent) SetUnsafeMode(unsafe bool) { a.security.SetUnsafeMode(unsafe) }

// IsSecurityBypassApproved returns whether the user has approved filesystem access outside CWD
func (a *Agent) IsSecurityBypassApproved() bool {
	return a.security.IsSecurityBypassApproved()
}

// SetSecurityBypassApproved marks that the user has approved filesystem access outside CWD for this session
func (a *Agent) SetSecurityBypassApproved() {
	a.security.SetSecurityBypassApproved()
}

// CheckFileContentSecurity runs security concern detection on file content after a write.
// In WebUI mode, it uses the event-bus-based ApprovalManager to show a dialog.
// In CLI mode, it falls back to the interactive logger prompt.
// Ignored concerns are tracked per-file so they are not re-prompted.
func (a *Agent) CheckFileContentSecurity(filePath string, content string) {
	promptManager := security.GetGlobalApprovalManager()
	eventBus := a.GetEventBus()

	if promptManager == nil && eventBus == nil {
		return
	}

	concerns, snippets := security.DetectSecurityConcernsWithContext(content, filePath)
	if len(concerns) == 0 {
		return
	}

	logger := utils.GetLogger(false)

	for _, concern := range concerns {
		if a.security.IsConcernIgnored(filePath, concern) {
			continue
		}

		snippet := ""
		if snippets != nil {
			snippet = snippets[concern]
		}
		prompt := prompts.PotentialSecurityConcernsFound(filePath, concern, snippet)

		var userResponse bool

		if eventBus != nil && promptManager != nil && a.security.HasActiveWebUIClients() {
			extras := map[string]string{
				"file_path": filePath,
				"concern":   concern,
			}
			userResponse = promptManager.RequestPrompt(eventBus, prompt, true, extras)
			logger.Logf("Security concern '%s' in %s user response: %v", concern, filePath, userResponse)
		} else {
			userResponse = logger.AskForConfirmation(prompt, true, false)
		}

		if userResponse {
			logger.Logf("Security concern '%s' in %s noted as an issue.", concern, filePath)
		} else {
			logger.Logf("Security concern '%s' in %s noted as unimportant.", concern, filePath)
			a.security.SetConcernIgnored(filePath, concern)
		}
	}
}

// GetMessages returns the current conversation messages
func (a *Agent) GetMessages() []api.Message {
	if a.state == nil {
		return nil
	}
	return a.state.GetMessages()
}

// SetMessages sets the conversation messages (for restore)
func (a *Agent) SetMessages(messages []api.Message) {
	if a.state != nil {
		a.state.SetMessages(messages)
	}
}

// AddMessage adds a single message to the conversation history
func (a *Agent) AddMessage(message api.Message) {
	if a.state != nil {
		a.state.AddMessage(message)
	}
}

// GetTotalCost returns the total cost of the conversation
func (a *Agent) GetTotalCost() float64 {
	return a.state.GetTotalCost()
}

// GetTaskActions returns completed task actions
func (a *Agent) GetTaskActions() []TaskAction {
	mu := a.state.GetTaskActionsMutex()
	mu.RLock()
	defer mu.RUnlock()
	return a.state.GetTaskActions()
}

// IsInteractiveMode returns true if running in interactive mode
func (a *Agent) IsInteractiveMode() bool {
	return configuration.GetEnvSimple("INTERACTIVE") == "1" ||
		configuration.GetEnvSimple("FROM_AGENT") != "1"
}

// SetStatsUpdateCallback sets a callback for token/cost updates
func (a *Agent) SetStatsUpdateCallback(callback func(int, float64)) {
	a.statsUpdateCallback.Store(callback)
}

// GetConfig returns the configuration
func (a *Agent) GetConfig() *configuration.Config {
	if a.configManager == nil {
		return nil
	}
	return a.configManager.GetConfig()
}

// SetWorkspaceRoot records the logical workspace root for this agent instance.
func (a *Agent) SetWorkspaceRoot(workspaceRoot string) {
	a.workspaceRoot = strings.TrimSpace(workspaceRoot)
}

// GetWorkspaceRoot returns the logical workspace root for this agent instance.
func (a *Agent) GetWorkspaceRoot() string {
	return strings.TrimSpace(a.workspaceRoot)
}

// SetConfigOverrides stores session-scoped config overrides on the agent.
// These are applied in-memory and persisted with the session state.
func (a *Agent) SetConfigOverrides(overrides map[string]interface{}) {
	a.state.SetConfigOverrides(overrides)
}

// GetConfigOverrides returns the session-scoped config overrides.
func (a *Agent) GetConfigOverrides() map[string]interface{} {
	return a.state.GetConfigOverrides()
}

// currentWorkspaceRoot resolves the agent workspace, falling back to the process cwd.
func (a *Agent) currentWorkspaceRoot() string {
	if root := strings.TrimSpace(a.workspaceRoot); root != "" {
		return root
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// OutputRouter returns the current output router (nil if not initialized)
func (a *Agent) OutputRouter() *OutputRouter { return a.output.GetOutputRouter() }

// PrintTerminalOnly writes text to the terminal without publishing to the event bus.
// Use this for output already published via a more specific event type.
func (a *Agent) PrintTerminalOnly(text string) {
	if a == nil {
		return
	}
	if a.output == nil {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	router := a.output.GetOutputRouter()
	if router == nil {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		fmt.Print(text)
		return
	}
	router.RouteTerminalOnly(text)
}

// GetSecurityApprovalMgr returns the security approval manager
func (a *Agent) GetSecurityApprovalMgr() *security.ApprovalManager {
	return a.security.GetSecurityApprovalMgr()
}

// SetHasActiveWebUIClients sets a callback that returns whether any WebUI
// clients are currently connected. The security prompting logic uses this
// to decide between WebUI event-bus routing and CLI-based prompting.
func (a *Agent) SetHasActiveWebUIClients(fn func() bool) {
	a.security.SetHasActiveWebUIClients(fn)
}

// HasActiveWebUIClients calls the registered callback (or returns false if
// none is set) to check whether WebUI clients are connected.
func (a *Agent) HasActiveWebUIClients() bool {
	return a.security.HasActiveWebUIClients()
}

// SetSystemPrompt sets the system prompt for the agent
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = a.ensureStopInformation(prompt)
}

// SetBaseSystemPrompt updates the baseline prompt used when persona overrides are cleared.
func (a *Agent) SetBaseSystemPrompt(prompt string) {
	a.baseSystemPrompt = a.ensureStopInformation(prompt)
	if strings.TrimSpace(a.baseSystemPrompt) == "" {
		a.baseSystemPrompt = a.systemPrompt
	}
}

// GetSystemPrompt returns the current system prompt
func (a *Agent) GetSystemPrompt() string {
	return a.systemPrompt
}

// GetValidator returns the syntax validator (nil until SetEventBus is called).
func (a *Agent) GetValidator() *validation.Validator {
	return a.validator
}

// SetTraceSessionsets the trace session for dataset collection
func (a *Agent) SetTraceSession(traceSession interface{}) {
	a.traceSession = traceSession
	a.state.SetTraceSession(traceSession)
}

// GetShellCommandHistoryEntry retrieves a shell command result from history
func (a *Agent) GetShellCommandHistoryEntry(command string) (*ShellCommandResult, bool) {
	a.shellCommandHistoryMu.RLock()
	defer a.shellCommandHistoryMu.RUnlock()
	result, exists := a.shellCommandHistory[command]
	return result, exists
}

// SetShellCommandHistoryEntry stores a shell command result in history
func (a *Agent) SetShellCommandHistoryEntry(command string, result *ShellCommandResult) {
	a.shellCommandHistoryMu.Lock()
	defer a.shellCommandHistoryMu.Unlock()
	a.shellCommandHistory[command] = result
}

// ClearShellCommandHistory removes all entries from shell command history
func (a *Agent) ClearShellCommandHistory() {
	a.shellCommandHistoryMu.Lock()
	defer a.shellCommandHistoryMu.Unlock()
	a.shellCommandHistory = make(map[string]*ShellCommandResult)
}

// GetAllShellCommandHistory returns a copy of the shell command history
func (a *Agent) GetAllShellCommandHistory() map[string]*ShellCommandResult {
	a.shellCommandHistoryMu.RLock()
	defer a.shellCommandHistoryMu.RUnlock()
	result := make(map[string]*ShellCommandResult, len(a.shellCommandHistory))
	for k, v := range a.shellCommandHistory {
		result[k] = v
	}
	return result
}

// SetTerminalManager sets the terminal manager for WebUI mode.
// When set (non-nil), shell commands can access hidden PTY sessions.
// When nil (CLI mode), shell commands use os/exec unchanged.
func (a *Agent) SetTerminalManager(tm tools.TerminalAccess) {
	a.terminalManager = tm
}

// GetTerminalManager returns the terminal manager (may be nil in CLI mode).
func (a *Agent) GetTerminalManager() tools.TerminalAccess {
	return a.terminalManager
}

// GetEmbeddingManager returns the embedding index manager (may be nil if
// embedding is not configured or enabled in the agent's config).
func (a *Agent) GetEmbeddingManager() *embedding.EmbeddingManager {
	return a.embeddingMgr
}

// GenerateResponse generates a simple response using the current model without tool calls
func (a *Agent) GenerateResponse(messages []api.Message) (string, error) {
	resp, err := a.client.SendChatRequest(messages, nil, "", false) // No tools, no reasoning, no disableThinking
	if err != nil {
		return "", agenterrors.NewProviderError("failed to generate response", err, a.GetProvider(), a.GetModel())
	}

	if len(resp.Choices) == 0 {
		return "", agenterrors.NewProviderError(fmt.Sprintf("no response generated for %d messages", len(messages)), nil, a.GetProvider(), a.GetModel())
	}

	return resp.Choices[0].Message.Content, nil
}
