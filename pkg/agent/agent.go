package agent

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/validation"
)

const (
	inputInjectionBufferSize = 10
	asyncOutputBufferSize    = 256
)

// PauseState tracks the state when a task is paused for clarification
type PauseState struct {
	IsPaused       bool          `json:"is_paused"`
	PausedAt       time.Time     `json:"paused_at"`
	OriginalTask   string        `json:"original_task"`
	Clarifications []string      `json:"clarifications"`
	MessagesBefore []api.Message `json:"messages_before"`
}

// initSubManagers lazily initializes sub-managers for test compatibility with bare &Agent{}.
func (a *Agent) initSubManagers() {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}
	if a.output == nil {
		a.output = NewAgentOutputManager()
	}
	if a.security == nil {
		a.security = NewAgentSecurityManager()
	}
	if a.mcpSub == nil {
		a.mcpSub = NewAgentMCPManager()
	}
	if a.clarificationManager == nil && a.eventBus != nil {
		a.clarificationManager = NewClarificationManager(a.eventBus)
	}
	if a.shellCwd == nil {
		a.shellCwd = &shellCwdTracker{}
	}
}

// invalidateVisionCache clears vision capability state so the next vision
// request re-probes the active provider/model.
func (a *Agent) invalidateVisionCache() {
	a.visionProcMu.Lock()
	a.visionProc = nil
	a.visionProcMu.Unlock()

	a.visionProbeMu.Lock()
	a.visionProbeModel = ""
	a.visionProbeProvider = ""
	a.visionProbeResult = nil
	a.visionProbeMu.Unlock()
}

// LifetimeCtx returns a lazily-initialized, process-scoped context for background goroutines.
func (a *Agent) LifetimeCtx() context.Context {
	if a == nil {
		return context.Background()
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	if !a.lifetimeCtxSet {
		a.lifetimeCtx, a.lifetimeCancel = context.WithCancel(context.Background())
		a.lifetimeCtxSet = true
	}
	return a.lifetimeCtx
}

// refreshSystemPrompt re-derives the system prompt for the active provider and context profile.
// Used by setClient when RefreshSystemPromptOnModelChange is true. No-ops if prerequisites are missing.
func (a *Agent) refreshSystemPrompt() {
	if a == nil || a.configManager == nil || a.GetWorkspaceRoot() == "" {
		return
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || !cfg.GetRefreshSystemPromptOnModelChange() {
		return
	}
	providerName := api.GetProviderName(a.getClientType())
	contextWindow := a.getModelContextLimit()
	// Re-resolve the context profile against the current window so lite
	// prompts (LCM) carry over when the new model is also sub-ContextFloor.
	profile, err := configuration.ResolveContextProfile(cfg, contextWindow)
	if err != nil {
		if a.debug {
			a.Logger().Debug("refreshSystemPrompt: failed to resolve profile: %v", err)
		}
		return
	}
	prompt, err := GetEmbeddedSystemPromptForProfile(profile, providerName, contextWindow, a.GetWorkspaceRoot())
	if err != nil {
		if a.debug {
			a.Logger().Debug("refreshSystemPrompt: failed to load prompt: %v", err)
		}
		return
	}
	prompt = resolveConfiguredSystemPrompt(cfg, prompt)
	a.systemPrompt = prompt
	a.baseSystemPrompt = prompt
}

type Agent struct {
	// Core LLM coordination
	client     api.ClientInterface
	clientType api.ClientType
	// clientMu protects client and clientType from concurrent access.
	clientMu sync.RWMutex

	systemPrompt     string
	baseSystemPrompt string // Base prompt restored when persona is cleared
	maxIterations    int

	// Conversation timing
	conversationStartTime time.Time
	turnTimestamp         time.Time
	turnTimestampMu       sync.RWMutex

	// Configuration
	configManager *configuration.Manager
	// workspaceRootMu protects workspaceRoot from concurrent access.
	workspaceRootMu sync.RWMutex
	workspaceRoot   string
	debug           bool
	// contextProfile is the resolved set of context-engine levers (tool allowlist, prompt path, compaction trigger, etc.).
	// Resolved once at agent creation. Zero-value means full-context mode.
	contextProfile configuration.ContextProfile

	// effectiveContextCap is the resolved max context tokens for this session (min of native window and configured cap).
	// Resolved at agent creation and refreshed on every provider/model
	// switch via refreshEffectiveContextCap(). Call sites MUST use this
	// instead of Config.MaxContextTokens or client.GetModelContextLimit().
	effectiveContextCap int
	// nativeContextWindow is the native model context window that
	// effectiveContextCap was resolved against (0 = never resolved). Used to
	// detect a stale cap when the live window diverges from this value.
	nativeContextWindow int

	// contextCapMu guards effectiveContextCap/nativeContextWindow and the
	// lastResolved* user-cap snapshot. Reads happen on every seed iteration
	// (Info/reconcile) while writes come from model switches and runtime
	// /max-context or settings-API cap changes, so the two must not race.
	contextCapMu sync.RWMutex
	// lastResolvedUserCap/lastResolvedHasCap snapshot the config's
	// MaxContextTokens at the last cap resolution so reconcileContextCap can
	// detect a runtime cap change without re-resolving every iteration.
	lastResolvedUserCap int
	lastResolvedHasCap  bool

	// Shell CWD tracking — updated by cd commands so git operations use the correct directory.
	shellCwd *shellCwdTracker

	// Input handling
	inputInjectionChan  chan string
	inputInjectionMutex sync.Mutex

	// Notification queue for background task completions.
	pendingNotifications []Notification
	notifMu              sync.Mutex

	// Turn journal for crash recovery (SP-138). Non-nil only while a turn is
	// in flight; presence of the journal file on disk means a session ended
	// with a turn in progress. journalMu guards open/close against appends
	// from the checkpoint goroutine.
	turnJournal *TurnJournal
	journalMu   sync.Mutex
	journalBase int

	// Wakeup budget tracking for auto-resume.
	wakeupTokensConsumed int
	wakeupResumeCount    int
	wakeupDisabled       bool
	wakeupMu             sync.Mutex

	// lifetimeCtx is a process-scoped context for background goroutines that must outlive a single turn.
	lifetimeCtx    context.Context
	lifetimeCancel context.CancelFunc
	lifetimeCtxSet bool

	interruptMu     sync.Mutex // protects interruptCtx + interruptCancel
	interruptCtx    context.Context
	interruptCancel context.CancelFunc
	// parentInterruptCtx is the base context the subagent's interrupt context derives from.
	// For subagents it's the parent's runCtx so cancellation propagates even after resetInterruptForNewQuery.
	parentInterruptCtx context.Context

	// Sub-managers — Agent coordinates through these interfaces
	state    StateManager       // Conversation history, checkpoints, tokens, cost, persona, etc.
	output   OutputManager      // Streaming, async output, event metadata, output routing
	security SecurityManager    // Approvals, redaction, elevation, bypass
	mcpSub   MCPSubManager      // MCP server lifecycle and tool caching
	todoMgr  *tools.TodoManager // Per-agent todo manager for session isolation

	// Event system (bridges output and core)
	eventBus  *events.EventBus
	validator *validation.Validator

	// Tool execution support
	shellCommandHistory   map[string]*ShellCommandResult
	shellCommandHistoryMu sync.RWMutex
	changeTracker         *ChangeTracker
	preparedTools         sync.RWMutex
	lastToolNames         []string
	// toolFuncs is this agent's per-agent tool dispatch set, built by
	// wireAgentToolFuncs and carried into ToolEnv so agent-dependent tools
	// route to THIS agent, not the most recently constructed one.
	toolFuncs *tools.ToolFuncSet

	// UI integration
	ui UI

	// Stats callback (protected by atomic access)
	statsUpdateCallback atomic.Value // func(int, float64)

	// Debug logging
	debugLogFile  *os.File
	debugLogPath  string
	debugLogMutex sync.Mutex
	logger        *AgentLogger

	// Trace session for dataset collection
	traceSession interface{}

	// TerminalManager provides access to hidden PTY sessions for WebUI mode. nil = CLI mode (os/exec).
	// webuiMu protects terminalManager from concurrent SetTerminalManager/GetTerminalManager access.
	webuiMu         sync.RWMutex
	terminalManager tools.TerminalAccess

	// BackgroundProcessManager provides background shell execution for CLI mode.
	// Lazy-initialized on first use when terminalManager is nil.
	backgroundProcessManager *tools.BackgroundProcessManager

	// passwordPrompter handles interactive password prompts for shell commands (sudo, passwd, ssh-keygen).
	// Set at startup based on the execution surface (WebUI prompter, CLI prompter, or nil).
	passwordPrompter tools.PasswordPrompter

	// automateApprovedMu guards automateApprovedWorkflows.
	automateApprovedMu sync.Mutex
	// automateApprovedWorkflows tracks workflows the user has approved this session (skip re-confirmation).
	automateApprovedWorkflows map[string]struct{}

	// computerUseMu guards computerUseSessionApproved and computerUseAppAllowlist.
	computerUseMu sync.Mutex
	// computerUseSessionApproved records whether the user has consented to computer-use actions this session.
	// Reset when the session resets (ClearSessionOverrides).
	computerUseSessionApproved bool
	// computerUseAppAllowlist tracks apps the user has explicitly allowed for the rest of this session.
	// Keys are bundle IDs (macOS) or "class:<window_class>" (Linux).
	computerUseAppAllowlist map[string]bool

	// Embedding index manager for duplicate detection on file writes.
	embeddingMu  sync.RWMutex // protects embeddingMgr
	embeddingMgr *embedding.EmbeddingManager

	// Vision processor for image/PDF/OCR analysis. Lazily initialized on first GetVisionProcessor() call.
	visionProcMu sync.RWMutex // protects visionProcessor
	visionProc   *tools.VisionProcessor

	// backgroundWg tracks background goroutines. Shutdown() waits for these before closing resources.
	backgroundWg sync.WaitGroup

	// shutdown records that Shutdown() has run, making it observable and ensuring teardown is once-only.
	shutdown     atomic.Bool
	shutdownOnce sync.Once

	// SubagentRunner manages in-process subagent execution.
	subagentRunner *SubagentRunner

	// subagentDepth tracks the nesting depth of this agent. 0 = primary, 1 = orchestrator, 2 = coder/tester, etc.
	subagentDepth int

	// rootPersonaID tracks the persona of the top-level (depth 0) agent in the spawn chain.
	// Propagated to subagents so depth limits and spawn restrictions can be enforced based on the root persona.
	rootPersonaID string

	// allowedTools restricts which tools this agent may use. When non-nil, only tools whose names (lowercased) are keys can be invoked.
	allowedTools map[string]bool

	// clarificationManager handles clarification requests from a subagent back to the parent/user.
	// Shared by reference from the parent agent when spawned as a subagent; nil on root agents.
	clarificationManager *ClarificationManager

	// subagentID is this agent's identifier when acting as a subagent; empty for root agents.
	subagentID string

	// riskProfileOverride is a transient (per-session) override for the risk cascade profile.
	// Set by --risk-profile CLI flag and per-step risk_profile in workflow JSON. Empty means fall through to Config.RiskProfile, then "default".
	riskProfileOverride configuration.RiskProfile

	// filesReadThisTurn tracks paths the agent called read_file on during the current turn.
	// Used by the staleness rule in checkWriteStaleness. Reset at turn boundaries.
	filesReadThisTurn *turnFileTracker

	// fileMetadata holds per-path WorkspaceFileMetadata populated by the platform-side sync layer.
	// checkWriteStaleness consults it to refuse writes over files with pending unsynced browser edits.
	fileMetadata *workspaceMetadataStore

	fileReadsMu sync.Mutex

	// Fleet budget tracking (set by SubagentRunner for parallel subagents).
	// When non-nil/nonzero, each LLM call debits tokens to the shared fleet
	// budget tracker. If the budget is exceeded mid-run, fleetBudgetTrunc
	// is set and the conversation loop truncates gracefully.
	fleetBudgetTracker *atomic.Int64
	fleetBudgetLimit   int64
	fleetBudgetTrunc   atomic.Bool

	// Fleet USD budget — parallels fleetBudget but in dollars. Set by the workflow runner and propagated to subagents.
	fleetUsdBudget *FleetUsdBudget

	// budgetWarningCallback is invoked when the USD budget first crosses
	// a configured warning threshold. The function value is stored
	// atomically so the workflow runner can replace it without locking.
	budgetWarningCallback atomic.Value // func(threshold, spent, limit float64)
	// budgetExceededCallback is invoked when the USD budget is first
	// reached or surpassed. Same atomic-value pattern.
	budgetExceededCallback atomic.Value // func(spent, limit float64)

	// auditLogger records security decisions (blocks, approvals, loops) to a JSONL file for auditing.
	auditLogger *tools.AuditLogger

	// queryInProgress guards ProcessQuery against concurrent execution.
	// When two frontends share the same Agent instance, only one query can run at a time.
	queryInProgress atomic.Bool

	// Security telemetry counters — track post-caution LLM behavior.
	secCautionsIssued      atomic.Int64
	secRetriesAfterCaution atomic.Int64
	secLoopsDetected       atomic.Int64

	// Background rollup worker. Lazily initialized via rollupOnce for test compatibility.
	rollupOnce sync.Once
	rollupW    *rollupWorker

	// visionProbe caches the registry-sourced probe result for the current
	// model+provider so the vision decision doesn't re-fetch on every
	// message. Invalidated when the model or provider changes.
	visionProbeMu       sync.RWMutex
	visionProbeModel    string
	visionProbeProvider string
	visionProbeResult   *bool

	// slashCommands holds the command registry for this agent. Stored as any to avoid circular import.
	slashCommands any

	// Training data collection — opt-in session recording. Callback wired from cmd/ to avoid circular import.
	trainingMu       sync.RWMutex
	trainingEnabled  bool
	trainingEndpoint string
	trainingExclude  []string
	trainingPushFn   func(state ConversationState, endpoint string, excludePaths []string) error

	// securityAnalysisCache holds session-scoped LLM security analyses of shell commands.
	// Populated lazily by AnalyzeShellCommand. Cleared on Agent.Reset() / Clear(). Nil until first use.
	securityAnalysisCache *SecurityAnalysisCache

	// securityAnalysisCacheMu guards securityAnalysisCache against concurrent lazy-init and reset.
	securityAnalysisCacheMu sync.Mutex
}

// InjectWebUIManagers replaces the agent's internal approval and ask-user managers with the webui-owned instances.
func (a *Agent) InjectWebUIManagers(approvalMgr *security.ApprovalManager, askUserMgr *tools.AskUserManager) {
	a.security.SetApprovalMgr(approvalMgr)
	a.security.SetAskUserMgr(askUserMgr)
}

// SetFleetBudget enables per-LLM-call fleet budget tracking for this agent.
func (a *Agent) SetFleetBudget(tracker *atomic.Int64, limit int64) {
	a.fleetBudgetTracker = tracker
	a.fleetBudgetLimit = limit
	a.fleetBudgetTrunc.Store(false)
}

// FleetBudgetExceeded reports whether the fleet budget was exceeded (mid-run truncation).
func (a *Agent) FleetBudgetExceeded() bool {
	return a.fleetBudgetTrunc.Load()
}

// SetFleetUsdBudget attaches a shared USD budget to this agent. The budget
// is shared by reference, so all agents (primary + subagents) that hold
// the same pointer debit to the same counter.
func (a *Agent) SetFleetUsdBudget(b *FleetUsdBudget) {
	a.fleetUsdBudget = b
	a.fleetBudgetTrunc.Store(false)
}

// GetFleetUsdBudget returns the agent's USD budget, or nil if none is set.
// Used by the SubagentRunner to propagate the same budget to spawned
// subagents (so the cap is workflow-wide, not per-agent).
func (a *Agent) GetFleetUsdBudget() *FleetUsdBudget {
	return a.fleetUsdBudget
}

// SetBudgetWarningCallback registers a function invoked when the USD budget
// first crosses each configured warning threshold (fired at most once per
// threshold). Pass nil to unregister.
func (a *Agent) SetBudgetWarningCallback(fn func(threshold, spent, limit float64)) {
	if fn == nil {
		a.budgetWarningCallback.Store((func(threshold, spent, limit float64))(nil))
		return
	}
	a.budgetWarningCallback.Store(fn)
}

// SetBudgetExceededCallback registers a function invoked when the USD budget
// is first reached or surpassed. Pass nil to unregister.
func (a *Agent) SetBudgetExceededCallback(fn func(spent, limit float64)) {
	if fn == nil {
		a.budgetExceededCallback.Store((func(spent, limit float64))(nil))
		return
	}
	a.budgetExceededCallback.Store(fn)
}

// ---------------------------------------------------------------------------
// Per-session app allowlist for destructive-app gate.
// ---------------------------------------------------------------------------

// IsAppAllowedForComputerUse reports whether the given app key is in the
// per-session allowlist. The key is a bundle ID (macOS) or a window class
// (Linux). Guarded by computerUseMu.
func (a *Agent) IsAppAllowedForComputerUse(key string) bool {
	if a == nil {
		return false
	}
	a.computerUseMu.Lock()
	defer a.computerUseMu.Unlock()
	return a.computerUseAppAllowlist != nil && a.computerUseAppAllowlist[key]
}

// AllowAppForComputerUse adds the given app key to the per-session
// allowlist. Guarded by computerUseMu.
func (a *Agent) AllowAppForComputerUse(key string) {
	if a == nil {
		return
	}
	a.computerUseMu.Lock()
	defer a.computerUseMu.Unlock()
	if a.computerUseAppAllowlist == nil {
		a.computerUseAppAllowlist = make(map[string]bool)
	}
	a.computerUseAppAllowlist[key] = true
}

// ---------------------------------------------------------------------------
// Command Classification and Steer Allowlist.
// ---------------------------------------------------------------------------

// SetSlashCommands stores the command registry on the agent.
// Called after the registry is created in cmd/agent_mode_interactive.go.
func (a *Agent) SetSlashCommands(registry any) {
	a.slashCommands = registry
}

// SlashCommands returns the agent's command registry, or nil if not set.
func (a *Agent) SlashCommands() any {
	if a == nil {
		return nil
	}
	return a.slashCommands
}
