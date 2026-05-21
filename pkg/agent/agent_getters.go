package agent

import (
	"fmt"
	"os"
	"path/filepath"
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
	promptManager := a.security.GetSecurityApprovalMgr()
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
			userResponse = promptManager.RequestPrompt(eventBus, a.GetEventUserID(), prompt, true, extras)
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
		(a != nil && !a.IsSubagent())
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

// GetShellCwd returns the current logical shell working directory.
func (a *Agent) GetShellCwd() string {
	a.shellCwdMu.RLock()
	defer a.shellCwdMu.RUnlock()
	return a.shellCwd
}

// SetShellCwd sets the logical shell working directory and records the previous.
func (a *Agent) SetShellCwd(dir string) {
	a.shellCwdMu.Lock()
	defer a.shellCwdMu.Unlock()
	a.prevShellCwd = a.shellCwd
	a.shellCwd = dir
}

// effectiveCwd returns the directory that tools should use for file/git operations.
// It returns shellCwd when set (updated by cd commands), falling back to the workspace root.
func (a *Agent) effectiveCwd() string {
	if cwd := a.GetShellCwd(); cwd != "" {
		return cwd
	}
	return a.currentWorkspaceRoot()
}

// updateShellCwd parses a shell command string and updates the tracked
// shell working directory when the command is a cd directive.
// It handles: cd <path>, cd, cd -, cd ~, cd .., cd <path> &&/;/|| <more>.
// It does NOT update for subshell cd (e.g., "(cd /path && ...)").
func (a *Agent) updateShellCwd(cmd string) {
	trimmed := strings.TrimSpace(cmd)

	// Skip subshells — they don't affect the parent shell's CWD.
	if strings.HasPrefix(trimmed, "(") {
		return
	}

	// Only act on commands that start with "cd"
	if !strings.HasPrefix(trimmed, "cd") {
		return
	}

	// Must be "cd" alone or "cd " followed by arguments.
	if len(trimmed) == 2 {
		// Bare "cd" — goes to $HOME.
	} else if len(trimmed) == 3 && trimmed[2] == ' ' {
		trimmed = trimmed[:2] + strings.TrimSpace(trimmed[3:])
	} else {
		return // e.g., "cddir" — not a cd command.
	}

	// Extract the argument from a compound command (stop at && || ; |).
	var arg string
	for _, sep := range []string{" && ", " || ", ";", " |"} {
		if idx := strings.Index(trimmed, sep); idx >= 0 {
			arg = strings.TrimSpace(trimmed[:idx])
			trimmed = arg
			break
		}
	}
	if arg == "" {
		arg = strings.TrimSpace(trimmed)
	}

	a.shellCwdMu.Lock()
	defer a.shellCwdMu.Unlock()

	current := a.shellCwd
	if current == "" {
		current = a.currentWorkspaceRoot()
	}

	resolved := resolveShellCdArg(arg, current)

	if arg == "-" {
		// cd - swaps current and previous.
		a.prevShellCwd, a.shellCwd = a.shellCwd, a.prevShellCwd
		return
	}

	a.prevShellCwd = a.shellCwd
	a.shellCwd = resolved
}

// resolveShellCdArg resolves a cd argument to an absolute path.
func resolveShellCdArg(arg, currentCwd string) string {
	if arg == "" {
		// Bare cd → $HOME.
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return currentCwd
	}
	if arg == "-" {
		return "-" // Handled specially by caller.
	}
	if arg == "~" {
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return currentCwd
	}
	if strings.HasPrefix(arg, "~/") {
		if home := os.Getenv("HOME"); home != "" {
			return filepath.Join(home, arg[2:])
		}
		return arg[2:]
	}
	if !filepath.IsAbs(arg) {
		return filepath.Join(currentCwd, arg)
	}
	return arg
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

// SetTraceSession sets the trace session for dataset collection
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

// GetTodoManager returns the per-agent todo manager.
// This ensures session isolation in daemon mode where multiple agents
// run concurrently.
func (a *Agent) GetTodoManager() *tools.TodoManager {
	if a.todoMgr == nil {
		a.todoMgr = tools.NewTodoManager()
	}
	return a.todoMgr
}

// GetSubagentRunner returns the per-agent subagent runner, creating it lazily.
func (a *Agent) GetSubagentRunner() *SubagentRunner {
	if a.subagentRunner == nil {
		a.subagentRunner = NewSubagentRunner(a, &SharedState{
			EventBus:      a.eventBus,
			TodoManager:   a.todoMgr,
			EmbeddingMgr:  a.embeddingMgr,
			ConfigManager: a.configManager,
			WorkspaceRoot: a.workspaceRoot,
		})
	}
	return a.subagentRunner
}

// IsSubagent returns true if this agent was spawned as a subagent (depth > 0).
// Used to prevent nested subagent spawning and skip interactive prompts.
func (a *Agent) IsSubagent() bool {
	return a.subagentDepth > 0
}

// SubagentDepth returns the nesting depth of this agent.
// 0 = primary agent (EA), 1 = orchestrator, 2 = coder/tester, etc.
func (a *Agent) SubagentDepth() int {
	return a.subagentDepth
}

// MaxSubagentDepth returns the configured maximum nesting depth.
// EA root gets 3 levels (max depth 2), non-EA root gets 2 levels (max depth 1).
func (a *Agent) MaxSubagentDepth() int {
	// Check config override first
	if cfg := a.GetConfig(); cfg != nil && cfg.SubagentMaxDepth > 0 {
		return cfg.SubagentMaxDepth
	}

	// EA root: 3 levels (EA → orchestrator → coder)
	if a.rootPersonaID == "executive_assistant" {
		return 2
	}

	// Non-EA root: 2 levels (orchestrator → coder)
	return 1
}

// CanSpawnSubagents returns true if this agent is allowed to spawn subagents
// (i.e., current depth is less than the configured max depth).
func (a *Agent) CanSpawnSubagents() bool {
	if configuration.GetEnvSimple("NO_SUBAGENTS") == "1" {
		return false
	}
	return a.subagentDepth < a.MaxSubagentDepth()
}

// IsLocalMode returns true when the agent is running locally (CLI or local WebUI),
// not in a cloud environment. This controls whether LocalOnly personas (like the
// Executive Assistant) are available.
//
// Cloud mode is detected via the SPROUT_CLOUD environment variable.
// Local mode is the default when the variable is unset or empty.
func (a *Agent) IsLocalMode() bool {
	return configuration.GetEnvSimple("CLOUD") != "1"
}

// EvaluateOperationRisk determines the risk level of a command for the
// currently active persona, using the persona's auto-approve rules.
// Returns RiskLevelLow, RiskLevelMedium, or RiskLevelHigh.
// For personas without auto-approve rules, returns RiskLevelLow (no EA risk cascade).
func (a *Agent) EvaluateOperationRisk(command string) configuration.RiskLevel {
	personaID := a.GetActivePersona()
	if personaID == "" {
		return configuration.RiskLevelLow
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return configuration.RiskLevelLow
	}
	persona := cfg.GetSubagentType(personaID)
	if persona == nil || persona.AutoApproveRules == nil {
		return configuration.RiskLevelLow
	}
	return persona.EvaluateOperationRisk(command)
}

// GenerateResponse generates a simple response using the current model without tool calls.
//
// TODO(SP-034-1c): accept a ctx parameter and forward it so callers can abort
// in-flight calls. The interruptCtx on the agent is the natural source, but
// changing this signature ripples into many callsites — handle in 1c.
func (a *Agent) GenerateResponse(messages []api.Message) (string, error) {
	resp, err := a.client.SendChatRequest(a.interruptCtx, messages, nil, "", false) // No tools, no reasoning, no disableThinking
	if err != nil {
		return "", agenterrors.NewProviderError("failed to generate response", err, a.GetProvider(), a.GetModel())
	}

	if len(resp.Choices) == 0 {
		return "", agenterrors.NewProviderError(fmt.Sprintf("no response generated for %d messages", len(messages)), nil, a.GetProvider(), a.GetModel())
	}

	return resp.Choices[0].Message.Content, nil
}
