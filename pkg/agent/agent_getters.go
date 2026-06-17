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
	"github.com/sprout-foundry/sprout/pkg/personas"
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

// GetUnsafeShellMode returns whether unsafe shell mode is enabled
func (a *Agent) GetUnsafeShellMode() bool { return a.security.GetUnsafeShellMode() }

// SetUnsafeShellMode sets the unsafe shell mode flag
func (a *Agent) SetUnsafeShellMode(unsafe bool) { a.security.SetUnsafeShellMode(unsafe) }

// IsSecurityBypassApproved returns whether the user has approved any
// external filesystem access this session. Coarse signal: prefer the
// per-path IsFolderSessionAllowed for new code.
func (a *Agent) IsSecurityBypassApproved() bool {
	return a.security.IsSecurityBypassApproved()
}

// IsFolderSessionAllowed reports whether absPath sits under a folder
// the user has allowlisted via "Allow this folder for the rest of the
// session" on the filesystem approval dialog.
func (a *Agent) IsFolderSessionAllowed(absPath string) bool {
	return a.security.IsFolderSessionAllowed(absPath)
}

// AddSessionAllowedFolder records the folder picked by the user from
// the filesystem approval dialog so future accesses under it are
// auto-approved for the rest of this session.
func (a *Agent) AddSessionAllowedFolder(folder string) {
	a.security.AddSessionAllowedFolder(folder)
}

// SnapshotSessionAllowedFolders returns a copy of the session
// allowlist. Used by SubagentRunner to seed a new subagent's
// allowlist from the parent (so previously approved folders remain
// usable inside delegated work).
func (a *Agent) SnapshotSessionAllowedFolders() []string {
	return a.security.SnapshotSessionAllowedFolders()
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
// GetContextTokens returns the current and max token counts for the active
// model's context window. (0, 0) when state is unavailable. SP-048-3.
func (a *Agent) GetContextTokens() (used, limit int) {
	if a == nil || a.state == nil {
		return 0, 0
	}
	return a.state.GetCurrentContextTokens(), a.state.GetMaxContextTokens()
}

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

// GetSecurityApprovalMgr returns the security approval manager. Returns nil
// when the security subsystem is not initialized (e.g., bare &Agent{} in
// tests), so callers can safely nil-check the result.
func (a *Agent) GetSecurityApprovalMgr() *security.ApprovalManager {
	if a.security == nil {
		return nil
	}
	return a.security.GetSecurityApprovalMgr()
}

// SetHasActiveWebUIClients sets a callback that returns whether any WebUI
// clients are currently connected. The security prompting logic uses this
// to decide between WebUI event-bus routing and CLI-based prompting.
func (a *Agent) SetHasActiveWebUIClients(fn func() bool) {
	a.security.SetHasActiveWebUIClients(fn)
}

// HasActiveWebUIClients calls the registered callback (or returns false if
// none is set) to check whether WebUI clients are connected. Returns false
// when the security submanager is unset (typical for partially-constructed
// agents in unit tests).
func (a *Agent) HasActiveWebUIClients() bool {
	if a == nil || a.security == nil {
		return false
	}
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

// SetBackgroundProcessManager sets the background process manager for CLI mode.
// When set, shell commands can run in background without PTY (os/exec).
func (a *Agent) SetBackgroundProcessManager(bpm *tools.BackgroundProcessManager) {
	a.backgroundProcessManager = bpm
}

// GetBackgroundProcessManager returns the background process manager.
func (a *Agent) GetBackgroundProcessManager() *tools.BackgroundProcessManager {
	return a.backgroundProcessManager
}

// GetEmbeddingManager returns the embedding index manager (may be nil if
// embedding is not configured or enabled in the agent's config).
func (a *Agent) GetEmbeddingManager() *embedding.EmbeddingManager {
	a.embeddingMu.RLock()
	defer a.embeddingMu.RUnlock()
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
			EmbeddingMgr:  a.GetEmbeddingManager(),
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

	// Coordinator root: 3 levels (coordinator → orchestrator → coder)
	if a.rootPersonaID == personas.IDCoordinator {
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
// Returns RiskLevelCritical / High / Medium / Low.
//
// Resolution order (matches the SP-058 risk profile design):
//  1. Critical patterns (rm -rf root, fork bomb) — ALWAYS return Critical,
//     regardless of persona, profile, or active mode.
//  2. Active persona has its own AutoApproveRules → use them (preserves
//     EA autonomy and any other persona-specific carve-outs).
//  3. Otherwise → resolve the agent's active risk profile and use its
//     baked-in rules.
//  4. No persona at all → return Low (no cascade gating, classic
//     non-EA behavior).
func (a *Agent) EvaluateOperationRisk(command string) configuration.RiskLevel {
	// Step 1: Critical is absolute and orthogonal to persona/profile.
	if configuration.IsCriticalOperation(command) {
		return configuration.RiskLevelCritical
	}

	personaID := a.GetActivePersona()
	if personaID == "" {
		return configuration.RiskLevelLow
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return configuration.RiskLevelLow
	}

	// Step 2: Persona-defined rules win when present.
	persona := cfg.GetSubagentType(personaID)
	if persona != nil && persona.AutoApproveRules != nil {
		return persona.EvaluateOperationRisk(command)
	}

	// Step 3: Fall back to the active risk profile. Use the
	// config-aware resolver so user overrides in Config.RiskProfiles
	// take precedence over baked-in defaults. A synthetic
	// SubagentType reuses the existing rule-matching code path.
	rules := configuration.ResolveRiskProfileRules(cfg, a.activeRiskProfile())
	synthetic := &configuration.SubagentType{AutoApproveRules: &rules}
	return synthetic.EvaluateOperationRisk(command)
}

// activeRiskProfile returns the risk profile that should apply for
// the next operation. Resolution: per-agent override (set by CLI
// flag / workflow step) → config.RiskProfile → "default".
func (a *Agent) activeRiskProfile() configuration.RiskProfile {
	if a.riskProfileOverride != "" {
		return a.riskProfileOverride
	}
	if cfg := a.GetConfig(); cfg != nil && cfg.RiskProfile != "" && configuration.IsValidRiskProfile(cfg.RiskProfile) {
		return configuration.RiskProfile(cfg.RiskProfile)
	}
	return configuration.RiskProfileDefault
}

// SetRiskProfileOverride installs a transient risk profile that
// overrides the config-level setting for the lifetime of this agent.
// Used by the --risk-profile CLI flag and per-step workflow overrides.
// Pass "" to clear.
func (a *Agent) SetRiskProfileOverride(profile configuration.RiskProfile) {
	a.riskProfileOverride = profile
}

// GetActiveRiskProfile returns the profile currently in effect for
// this agent (override > config > default). Exposed for status
// commands / debug logging.
func (a *Agent) GetActiveRiskProfile() configuration.RiskProfile {
	return a.activeRiskProfile()
}

// IsSessionElevated reports whether the user has elevated the session
// to a permissive or unrestricted risk profile. When true, all three
// security gates (static classifier, filesystem tier, shell risk
// cascade) must skip their interactive prompts and auto-approve —
// the user explicitly opted out of per-operation prompts for this
// session. Critical-tier operations (rm -rf /, fork bombs) are NOT
// covered by elevation and always block regardless.
func (a *Agent) IsSessionElevated() bool {
	profile := a.activeRiskProfile()
	return profile == configuration.RiskProfilePermissive || profile == configuration.RiskProfileUnrestricted
}

// GenerateResponse generates a simple response using the current model without tool calls.
//
// SP-073: uses a.interruptCtx so Stop/cancel aborts the in-flight call. If
// callers need to pass their own context, they can set it via SetInterruptCtx
// before calling.
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

// ReadFileContent reads the content of a file from the workspace.
// The path is resolved relative to the agent's workspace root.
// Returns an error if the file does not exist or cannot be read.
func (a *Agent) ReadFileContent(path string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("agent is nil")
	}
	workspaceRoot := a.currentWorkspaceRoot()
	absPath := filepath.Join(workspaceRoot, path)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return string(data), nil
}
