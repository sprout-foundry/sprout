// Package agent: delegation getters and setters for sub-managers (split from agent_getters.go)
package agent

import (
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/validation"
)

// GetDebugLogPath returns the path to the current debug log file (if any)
func (a *Agent) GetDebugLogPath() string { return a.debugLogPath }

// getClient safely returns the current LLM client under the client read lock.
// Callers must not retain the returned pointer beyond the immediate call site
// — SetProvider may swap it concurrently. For operations that need a stable
// reference across multiple calls, use withClient instead.
func (a *Agent) getClient() api.ClientInterface {
	a.clientMu.RLock()
	defer a.clientMu.RUnlock()
	return a.client
}

// getClientType safely returns the current provider type under the read lock.
func (a *Agent) getClientType() api.ClientType {
	a.clientMu.RLock()
	defer a.clientMu.RUnlock()
	return a.clientType
}

// setClient safely swaps both the client and clientType under the write lock.
// Used by SetProvider and SetModel to atomically update both fields.
func (a *Agent) setClient(client api.ClientInterface, clientType api.ClientType) {
	a.clientMu.Lock()
	a.client = client
	a.clientType = clientType
	a.clientMu.Unlock()
}

// withClient runs fn while holding the client read lock, passing a stable
// snapshot of the current client. Use for read-only access patterns that
// need consistency across multiple calls (e.g. metrics, vision checks).
func (a *Agent) withClient(fn func(c api.ClientInterface)) {
	a.clientMu.RLock()
	defer a.clientMu.RUnlock()
	fn(a.client)
}

// Logger returns the agent logger, initializing it lazily if needed
func (a *Agent) Logger() *AgentLogger {
	if a.logger == nil {
		a.logger = NewAgentLogger(a)
	}
	return a.logger
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

// SetTerminalManager sets the terminal manager for WebUI mode.
// When set (non-nil), shell commands can access hidden PTY sessions.
// When nil (CLI mode), shell commands use os/exec unchanged.
func (a *Agent) SetTerminalManager(tm tools.TerminalAccess) {
	a.webuiMu.Lock()
	defer a.webuiMu.Unlock()
	a.terminalManager = tm
}

// GetTerminalManager returns the terminal manager (may be nil in CLI mode).
func (a *Agent) GetTerminalManager() tools.TerminalAccess {
	a.webuiMu.RLock()
	defer a.webuiMu.RUnlock()
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

// SetPasswordPrompter registers a password prompter for shell commands.
// When set, privileged commands (sudo, passwd) are allowed to run with
// password assistance instead of being hard-blocked. Pass nil to disable.
func (a *Agent) SetPasswordPrompter(pp tools.PasswordPrompter) {
	a.passwordPrompter = pp
}

// GetPasswordPrompter returns the registered password prompter, or nil.
func (a *Agent) GetPasswordPrompter() tools.PasswordPrompter {
	return a.passwordPrompter
}

// HasPasswordPrompter returns true if a password prompter is registered.
// Used by the risk resolver to decide whether to downgrade privileged
// commands from block to prompt.
func (a *Agent) HasPasswordPrompter() bool {
	return a.passwordPrompter != nil
}

// GetEmbeddingManager returns the embedding index manager (may be nil if
// embedding is not configured or enabled in the agent's config).
func (a *Agent) GetEmbeddingManager() *embedding.EmbeddingManager {
	a.embeddingMu.RLock()
	defer a.embeddingMu.RUnlock()
	return a.embeddingMgr
}

// GetVisionProcessor returns the agent's vision processor, creating it
// lazily on first call. The processor is cached for the life of the Agent
// so that subsequent calls reuse the same vision client and cache.
// Returns nil if no vision-capable provider is available, or if the agent
// is nil.
func (a *Agent) GetVisionProcessor() *tools.VisionProcessor {
	if a == nil {
		return nil
	}
	a.visionProcMu.RLock()
	if a.visionProc != nil {
		p := a.visionProc
		a.visionProcMu.RUnlock()
		return p
	}
	a.visionProcMu.RUnlock()

	// Double-check pattern: lock and check again under write lock
	a.visionProcMu.Lock()
	defer a.visionProcMu.Unlock()
	if a.visionProc != nil {
		return a.visionProc
	}

	// Lazy-init: create a vision processor using the agent's active provider
	proc, err := tools.NewVisionProcessorWithProvider(a.debug, a.getClientType())
	if err != nil {
		return nil
	}
	a.visionProc = proc
	return proc
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

// getSecurityAnalysisCache returns the session-scoped LLM security analysis cache,
// creating it lazily on first use. Nil-safe. Uses double-checked locking
// (similar to GetVisionProcessor) so the fast path is a single read.
func (a *Agent) getSecurityAnalysisCache() *SecurityAnalysisCache {
	if a == nil {
		return nil
	}
	a.securityAnalysisCacheMu.Lock()
	defer a.securityAnalysisCacheMu.Unlock()
	if a.securityAnalysisCache == nil {
		a.securityAnalysisCache = NewSecurityAnalysisCache()
	}
	return a.securityAnalysisCache
}

// ClearSecurityAnalysisCache resets the cache to empty. Call this when
// the session resets to avoid stale analyses from a previous session.
// Guards the pointer swap so a concurrent get/Set can't see a torn cache.
func (a *Agent) ClearSecurityAnalysisCache() {
	if a == nil {
		return
	}
	a.securityAnalysisCacheMu.Lock()
	defer a.securityAnalysisCacheMu.Unlock()
	// Replace the map contents under the new cache's write lock rather
	// than swapping the pointer, so any goroutine that already held a
	// reference to the old cache (via a prior get/Set) keeps operating
	// on a valid map.
	cache := a.securityAnalysisCache
	if cache == nil {
		a.securityAnalysisCache = NewSecurityAnalysisCache()
		return
	}
	cache.Clear()
}
