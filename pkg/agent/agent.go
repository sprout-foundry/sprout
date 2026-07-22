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

// initSubManagers ensures all sub-managers are initialized.
// This is called lazily to support tests that create bare Agent structs
// with &Agent{} and then call methods that depend on sub-managers.
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

// refreshSystemPrompt re-derives the agent's system prompt for the
// active provider and context profile. Used by setClient when the
// config flag RefreshSystemPromptOnModelChange is true. Falls back
// to no-op silently when prerequisites (workspaceRoot, configManager,
// non-nil cfg) are missing — the prompt is already correct from
// agent creation in that case, so a partial refresh would just
// introduce noise.
//
// Re-resolution matches what initAgentFromResolvedProvider does at
// agent creation: resolve the context profile against the current
// model context window (so LCM auto-detection carries over to the
// new model when its window is also below subagentContextThreshold),
// then load the embedded prompt for that profile and re-apply any
// configured SystemPromptText override.
//
// Both a.systemPrompt and a.baseSystemPrompt are updated — the base
// prompt is the "persona cleared" snapshot SetSystemPrompt consults
// indirectly via SetBaseSystemPrompt; leaving it stale would let a
// later persona clear reintroduce the old model's prompt.
func (a *Agent) refreshSystemPrompt() {
	if a == nil || a.configManager == nil || a.workspaceRoot == "" {
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
	prompt, err := GetEmbeddedSystemPromptForProfile(profile, providerName, contextWindow, a.workspaceRoot)
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
	// SetProvider/SetModel swap these fields while the query loop
	// (seed_query.go), metrics, rollup, and vision checks read them.
	// Without synchronization the two-word interface value can tear,
	// producing garbage pointers and intermittent crashes.
	clientMu sync.RWMutex

	systemPrompt     string
	baseSystemPrompt string // Base prompt restored when persona is cleared
	maxIterations    int

	// Conversation timing
	conversationStartTime time.Time

	// Configuration
	configManager   *configuration.Manager
	workspaceRoot   string
	debug           bool
	// contextProfile (SP-125) is the resolved set of context-engine
	// levers (tool allowlist, prompt path, compaction trigger, etc.).
	// Resolved once at agent creation by ResolveContextProfile and read
	// by every call site that depends on it (conversation.go,
	// embedded_prompts.go, context_budget.go, seed_query.go, rollup.go).
	// Zero-value means full-context mode (all defaults).
	contextProfile  configuration.ContextProfile

	// effectiveContextCap (SP-126) is the resolved maximum number of
	// context tokens sprout will use for any request in this session.
	// Always equal to the smaller of (a) the model's native context
	// window and (b) the user's configured MaxContextTokens cap. Zero
	// means "no cap" — the native window flows through unconstrained.
	//
	// Resolved exactly once at agent creation by
	// configuration.ResolveEffectiveContextCap and re-read by every
	// call site that needs the ceiling (seed_provider.Info(),
	// seed_query.OnIteration). Call sites MUST NEVER re-derive it
	// from Config.MaxContextTokens and MUST NEVER call
	// client.GetModelContextLimit() directly — those paths bypass the
	// cap. Independent of ContextProfile: a 1M model can run in full
	// mode with a 300K cap; a 32K model can run in LCM with no cap.
	effectiveContextCap int

	// Shell CWD tracking — updated by cd commands in handleShellCommand.
	// Tools like commit use this instead of workspaceRoot when available,
	// so that git operations run in the correct directory after the agent
	// has changed directories via shell commands.
	shellCwd *shellCwdTracker

	// Input handling
	inputInjectionChan  chan string
	inputInjectionMutex sync.Mutex

	// Notification queue for background task completions (SP-108).
	pendingNotifications []Notification
	notifMu              sync.Mutex

	// Wakeup budget tracking for auto-resume (SP-108).
	wakeupTokensConsumed int
	wakeupResumeCount    int
	wakeupDisabled       bool
	wakeupMu             sync.Mutex

	interruptMu     sync.Mutex // protects interruptCtx + interruptCancel
	interruptCtx    context.Context
	interruptCancel context.CancelFunc
	// parentInterruptCtx is the base context the subagent's interrupt
	// context should always derive from. For the primary agent this is
	// nil (equivalent to context.Background()). For subagents it is the
	// parent's runCtx passed to createSubagent, so that cancelling the
	// parent (Ctrl+C, timeout) propagates into the subagent's LLM calls
	// even after resetInterruptForNewQuery or ClearInterrupt replace the
	// interruptCtx.
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

	// TerminalManager provides access to hidden PTY sessions for WebUI mode.
	// When nil (CLI mode), shell commands use os/exec unchanged.
	//
	// webuiMu protects terminalManager from concurrent access: the webui
	// server calls SetTerminalManager from getChatAgent on every query path,
	// potentially from multiple goroutines (one per chat session), while the
	// shell tool reads it via GetTerminalManager during tool execution.
	webuiMu         sync.RWMutex
	terminalManager tools.TerminalAccess

	// BackgroundProcessManager provides background shell execution for CLI mode.
	// When nil AND terminalManager is nil, background shell features are unavailable.
	// Lazy-initialized on first use when terminalManager is nil.
	backgroundProcessManager *tools.BackgroundProcessManager

	// passwordPrompter handles interactive password prompts for shell commands
	// (sudo, passwd, ssh-keygen passphrase). When nil, password-requiring
	// commands are blocked by the classifier as before. Set at startup based
	// on the execution surface (WebUI prompter if event bus + clients, CLI
	// prompter if TTY, nil otherwise).
	passwordPrompter tools.PasswordPrompter

	// automateApprovedMu guards automateApprovedWorkflows.
	automateApprovedMu sync.Mutex
	// automateApprovedWorkflows tracks workflow filenames the user has
	// explicitly approved during this chat session. Subsequent run_automate
	// calls for the same workflow (e.g. retry-after-failure kicked off by
	// the primary agent) skip the intent-confirmation prompt — the user has
	// already opted in once.
	automateApprovedWorkflows map[string]struct{}

	// computerUseMu guards computerUseSessionApproved and computerUseAppAllowlist.
	computerUseMu sync.Mutex
	// computerUseSessionApproved records whether the user has consented
	// to computer-use actions during this chat session (SP-063 per-session
	// opt-in). Reset when the session resets (ClearSessionOverrides).
	computerUseSessionApproved bool
	// computerUseAppAllowlist tracks apps the user has explicitly allowed
	// for the rest of this session (SP-063-4h-prompt allow-always).
	// Keys are bundle IDs (macOS) or "class:<window_class>" (Linux).
	computerUseAppAllowlist map[string]bool

	// Embedding index manager for duplicate detection on file writes.
	embeddingMu  sync.RWMutex // protects embeddingMgr
	embeddingMgr *embedding.EmbeddingManager

	// Vision processor for image/PDF/OCR analysis (SP-079-1).
	// Lazily initialized on first GetVisionProcessor() call.
	visionProcMu sync.RWMutex // protects visionProcessor
	visionProc   *tools.VisionProcessor

	// backgroundWg tracks background goroutines that use embeddingMgr or other
	// resources. Shutdown() waits for these before closing resources.
	backgroundWg sync.WaitGroup

	// SubagentRunner manages in-process subagent execution.
	subagentRunner *SubagentRunner

	// subagentDepth tracks the nesting depth of this agent.
	// 0 = primary agent (EA), 1 = orchestrator, 2 = coder/tester, etc.
	// Used to control tool availability and prevent excessive nesting.
	subagentDepth int

	// rootPersonaID tracks the persona of the top-level (depth 0) agent in the spawn chain.
	// Propagated to subagents so that depth limits and spawn restrictions can be enforced
	// based on the root persona (e.g., EA gets 3 levels, orchestrator gets 2).
	rootPersonaID string

	// allowedTools restricts which tools this agent may use.
	// When non-nil, only tools whose names (lowercased) are keys in this map
	// can be invoked. Used to limit tool access for subagents.
	allowedTools map[string]bool

	// clarificationManager handles clarification requests from a subagent
	// back to the parent / user. Shared by reference from the parent
	// agent when this agent is spawned as a subagent; nil on root agents
	// that haven't been wired by their event bus initializer.
	clarificationManager *ClarificationManager

	// subagentID is this agent's identifier when acting as a subagent;
	// empty for root agents. Used as the requester ID when calling
	// request_clarification so the response routes back to the right
	// subagent.
	subagentID string

	// riskProfileOverride is a transient (per-session) override for
	// the risk cascade profile. Set by the --risk-profile CLI flag
	// and by per-step risk_profile in workflow JSON. Empty means
	// fall through to Config.RiskProfile, then to "default".
	// See agent_getters.go:EvaluateOperationRisk for resolution.
	riskProfileOverride configuration.RiskProfile

	// filesReadThisTurn tracks paths the agent called read_file on during
	// the current turn. Used by the SP-046 staleness rule in
	// checkWriteStaleness — see workspace_sync.go. Reset at turn boundaries
	// via ResetFileReadsForNewTurn.
	filesReadThisTurn *turnFileTracker

	// fileMetadata holds per-path WorkspaceFileMetadata populated by the
	// platform-side sync layer. checkWriteStaleness consults it to refuse
	// writes over files with pending unsynced browser edits.
	fileMetadata *workspaceMetadataStore

	fileReadsMu sync.Mutex

	// Fleet budget tracking (set by SubagentRunner for parallel subagents).
	// When non-nil/nonzero, each LLM call debits tokens to the shared fleet
	// budget tracker. If the budget is exceeded mid-run, fleetBudgetTrunc
	// is set and the conversation loop truncates gracefully.
	fleetBudgetTracker *atomic.Int64
	fleetBudgetLimit   int64
	fleetBudgetTrunc   atomic.Bool

	// Fleet USD budget — parallels fleetBudget but in dollars. Set by the
	// workflow runner from cfg.Budget and propagated to subagents. When
	// non-nil and limit > 0, each LLM response debits its cost to the
	// shared budget. Crossing warn thresholds emits stdout/event notices;
	// hitting the cap sets the same fleetBudgetTrunc flag the token path
	// uses so the conversation loop stops gracefully in one place.
	fleetUsdBudget *FleetUsdBudget

	// budgetWarningCallback is invoked when the USD budget first crosses
	// a configured warning threshold. The function value is stored
	// atomically so the workflow runner can replace it without locking.
	budgetWarningCallback atomic.Value // func(threshold, spent, limit float64)
	// budgetExceededCallback is invoked when the USD budget is first
	// reached or surpassed. Same atomic-value pattern.
	budgetExceededCallback atomic.Value // func(spent, limit float64)

	// auditLogger records security decisions (blocks, approvals, loops) to a
	// JSONL file for auditing. Set via SetAuditLogger; nil-safe everywhere.
	// Stored as a value-backed pointer so tests can swap it freely.
	auditLogger *tools.AuditLogger

	// queryInProgress guards ProcessQuery against concurrent execution.
	// When two frontends share the same Agent instance (CLI + WebUI in
	// non-daemon mode), only one query can run at a time. The second caller
	// gets ErrQueryInProgress and must retry or show a "busy" message.
	queryInProgress atomic.Bool

	// Security telemetry counters (Task 3) — track post-caution LLM behavior.
	secCautionsIssued      atomic.Int64
	secRetriesAfterCaution atomic.Int64
	secLoopsDetected       atomic.Int64

	// SP-066 Phase 2: background rollup worker. rollupW is lazily
	// initialized via rollupOnce so existing tests that construct bare
	// *Agent values continue to work without a constructor change.
	rollupOnce sync.Once
	rollupW    *rollupWorker

	// visionProbe caches the registry-sourced probe result for the current
	// model+provider so the vision decision doesn't re-fetch on every
	// message. Invalidated when the model or provider changes.
	visionProbeMu       sync.RWMutex
	visionProbeModel    string
	visionProbeProvider string
	visionProbeResult   *bool

	// slashCommands holds the command registry for this agent. Set via
	// SetSlashCommands after the registry is created in cmd/agent_mode_interactive.go.
	// Used by the steer coordinator and WebUI to look up commands for mid-turn
	// execution (SP-114 Phase 1).
	slashCommands any // *commands.CommandRegistry — stored as any to avoid circular import

	// Training data collection — opt-in session recording. The callback
	// is wired from cmd/ (which imports both pkg/agent and pkg/training)
	// to avoid a circular import. When trainingPushFn is non-nil and
	// trainingEnabled is true, SaveStateScoped fires the callback in a
	// goroutine after each save.
	trainingMu       sync.RWMutex
	trainingEnabled  bool
	trainingEndpoint string
	trainingExclude  []string
	trainingPushFn   func(state ConversationState, endpoint string, excludePaths []string) error

	// securityAnalysisCache holds session-scoped LLM security analyses of
	// shell commands. Populated lazily by AnalyzeShellCommand when an
	// approval prompt fires for a CAUTION/DANGEROUS shell command. Cleared
	// on Agent.Reset() / Clear() (match the lifecycle of similar state).
	//
	// SP-124. Nil until first use.
	securityAnalysisCache *SecurityAnalysisCache

	// securityAnalysisCacheMu guards securityAnalysisCache against
	// concurrent lazy-init (getSecurityAnalysisCache) and reset
	// (ClearSecurityAnalysisCache). The cache itself has its own RWMutex
	// for map access; this mutex only protects the pointer.
	securityAnalysisCacheMu sync.Mutex
}

// InjectWebUIManagers replaces the agent's internal approval and ask-user
// managers with the webui-owned instances.  This is called after the web
// server is constructed so that security prompts and ask_user requests
// created by the agent are routed through the same manager that the webui
// handlers resolve responses on — eliminating the need for global singletons.
func (a *Agent) InjectWebUIManagers(approvalMgr *security.ApprovalManager, askUserMgr *tools.AskUserManager) {
	a.security.SetApprovalMgr(approvalMgr)
	a.security.SetAskUserMgr(askUserMgr)
}

// SetFleetBudget enables per-LLM-call fleet budget tracking for this agent.
// When tracker is non-nil and limit > 0, each LLM call will debit its token
// usage to the shared tracker. If the budget is exceeded, fleetBudgetTrunc
// is set and the conversation loop will truncate gracefully.
func (a *Agent) SetFleetBudget(tracker *atomic.Int64, limit int64) {
	a.fleetBudgetTracker = tracker
	a.fleetBudgetLimit = limit
	a.fleetBudgetTrunc.Store(false)
}

// FleetBudgetExceeded reports whether the fleet budget was exceeded during
// this agent's execution (mid-run truncation). Returns true if EITHER the
// token budget or the USD budget tripped — both use the same truncation
// flag because the downstream behavior is identical.
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
// SP-063-4h: Per-session app allowlist for destructive-app gate
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
// SP-114 Phase 1: Command Classification and Steer Allowlist
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
