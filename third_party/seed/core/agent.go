// Package core provides a conversation engine for LLM-powered agents.
//
// Core concepts:
//
//   - Agent: the main entry point. Create with NewAgent(), run queries with Run().
//   - Provider: interface for LLM backends. Implement Chat() and ChatStream().
//   - ToolExecutor: interface for tool execution. Implement GetTools() and Execute().
//   - UI: optional interface for interactive output. Use NoopUI for headless mode.
//   - EventPublisher: optional interface for event-driven output.
//
// Quick start:
//
//	agent, err := core.NewAgent(core.Options{
//	    Provider: &myProvider{},
//	    Executor: core.NoopExecutor,
//	})
//	result, err := agent.Run(ctx, "Hello")
//
// The core package has zero external dependencies. The events and internal/test
// packages are optional.
package core

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// DefaultSystemPrompt is the minimal system prompt used when none is provided.
const DefaultSystemPrompt = "You are a helpful assistant that can execute tools to complete tasks."

// RetryConfig configures retry behavior for transient provider errors.
// Zero values use sensible defaults.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (initial + retries).
	// Zero means use the default of 3. Setting to 1 means no retries
	// (only the initial attempt).
	MaxAttempts int

	// InitialDelay is the delay before the first retry.
	// Zero means use the default of 100ms.
	InitialDelay time.Duration

	// MaxDelay caps the exponential backoff growth.
	// Zero means use the default of 5s.
	MaxDelay time.Duration

	// Multiplier is the exponential growth factor.
	// Zero means use the default of 2.0.
	Multiplier float64

	// Jitter adds randomness to delays. 0 = none, (0,1) = partial, >=1 = full.
	// Zero means use the default of 0.0 (no jitter).
	Jitter float64
}

// MaxAttemptsOrDefault returns MaxAttempts or the default (3).
func (rc RetryConfig) MaxAttemptsOrDefault() int {
	if rc.MaxAttempts > 0 {
		return rc.MaxAttempts
	}
	return 3
}

// InitialDelayOrDefault returns InitialDelay or the default (100ms).
func (rc RetryConfig) InitialDelayOrDefault() time.Duration {
	if rc.InitialDelay > 0 {
		return rc.InitialDelay
	}
	return 100 * time.Millisecond
}

// MaxDelayOrDefault returns MaxDelay or the default (5s).
func (rc RetryConfig) MaxDelayOrDefault() time.Duration {
	if rc.MaxDelay > 0 {
		return rc.MaxDelay
	}
	return 5 * time.Second
}

// MultiplierOrDefault returns Multiplier or the default (2.0).
func (rc RetryConfig) MultiplierOrDefault() float64 {
	if rc.Multiplier > 0 {
		return rc.Multiplier
	}
	return 2.0
}

// JitterOrDefault returns Jitter or the default (0.0).
// Unlike other *OrDefault methods, zero is a meaningful value here:
// Jitter 0.0 means "no jitter" (deterministic retries), so it is not
// replaced by a default. Negative values are not expected and are
// returned as-is.
// triggerFractionOrDefault returns the configured compaction trigger fraction
// (clamped to (0, 1]) or the default if unset / out of range.
func (a *Agent) triggerFractionOrDefault() float64 {
	if a.compactionTriggerFraction > 0 && a.compactionTriggerFraction <= 1.0 {
		return a.compactionTriggerFraction
	}
	return defaultCompactionTriggerFraction
}

// settleMarginOrDefault returns the configured compaction settle margin, or
// the default if unset / out of range.
func (a *Agent) settleMarginOrDefault() float64 {
	if a.compactionSettleMargin > 0 && a.compactionSettleMargin < 1.0 {
		return a.compactionSettleMargin
	}
	return defaultCompactionSettleMargin
}

// substitutionTargetOrDefault returns the configured substitution target
// fraction (clamped to (0, 1)) or the default if unset / out of range. This is
// the context share that Phase 0a (checkpoint substitution) aims for when
// pressure fires — deliberately well below the trigger so each substitution
// pass buys many turns of headroom.
func (a *Agent) substitutionTargetOrDefault() float64 {
	if a.substitutionTargetFraction > 0 && a.substitutionTargetFraction < 1.0 {
		return a.substitutionTargetFraction
	}
	return defaultSubstitutionTargetFraction
}

func (rc RetryConfig) JitterOrDefault() float64 {
	return rc.Jitter
}

// Options configures an Agent.
type Options struct {
	Provider       Provider     // required — LLM communication
	Executor       ToolExecutor // required — tool execution
	UI             UI           // nil = headless
	SystemPrompt   string       // empty = minimal default for testing
	MaxIterations  int          // 0 = unlimited
	MaxTokens      int          // 0 = use provider default
	Debug          bool
	EventPublisher EventPublisher // nil = no events
	// OnIteration is an optional callback invoked synchronously at the start of
	// each conversation-loop iteration. It receives the iteration number (0-based),
	// the current message count in state, the estimated token count for the prompt,
	// and the model's context window size. The token estimate is computed from the
	// prepared message list (before any compaction). The agent does not await a
	// result or handle errors from this callback; if the callback panics, the
	// panic is caught and logged (the agent continues).
	OnIteration func(iteration int, messages int, tokenEstimate int, contextSize int)
	// Optimizer is used to optimize conversation history across iterations.
	Optimizer   *ConversationOptimizer
	RetryConfig RetryConfig // retry behavior for transient errors; zero values use defaults
	// CompactionTriggerFraction is the share of the model's context window
	// above which the chat loop runs proactive compaction before sending the
	// next request. Valid range is (0, 1]; zero (the default) uses
	// defaultCompactionTriggerFraction (0.85). Set to 1.0 to keep the legacy
	// "only compact when the estimate already exceeds the window" behavior.
	CompactionTriggerFraction float64
	// CompactionSettleMargin is how far below the trigger, as a share of the
	// context window, a proactive compaction settles. Each compaction rewrites
	// the conversation after the point it cuts, so a provider's prompt cache is
	// rebuilt from there on the next request; a deeper settle compacts less
	// often and keeps the cache warm for longer, at the cost of dropping more
	// history each time. Valid range is (0, 1); zero uses
	// defaultCompactionSettleMargin (0.05).
	CompactionSettleMargin float64
	// SubstitutionTargetFraction is the share of the model's context window
	// that Phase 0a (iterative checkpoint substitution) targets when it fires.
	// When context pressure crosses CompactionTriggerFraction, checkpoints are
	// substituted oldest-first until the estimate drops to
	// SubstitutionTargetFraction × contextSize (or all checkpoints are
	// consumed). Defaults to emergencyTargetFraction (0.85), preserving the
	// historical "substitute just enough to clear the trigger" behavior.
	// Consumers who want each substitution pass to buy substantial headroom
	// (e.g. sprout sets 0.50) can override here. The emergency drop/truncate
	// cascade (Phase 1+) is unaffected — it always targets
	// emergencyTargetFraction. Valid range is (0, 1); zero uses the default.
	SubstitutionTargetFraction float64
	// LLMSummarizer, if non-nil, enables LLM-based structural compaction. The
	// compaction pipeline calls it to compress a window of older middle history
	// into a single summary message, preserving intent better than the
	// rule-based drop/truncate fallback. See the LLMSummarizer doc for the
	// contract. When nil, seed uses its rule-based compaction only.
	LLMSummarizer LLMSummarizer
	// ContinuationCompaction, if non-nil, compacts first by a summary the
	// model writes as a continuation of the live request (see
	// ContinuationCompaction), falling back to the cascade below when that
	// pass does not settle the conversation under the trigger.
	ContinuationCompaction *ContinuationCompaction
	// CompactionArtifacts, if non-nil, renders the durable artifact record
	// (files touched) for a summarized span. Structural compaction calls it
	// once per pass and appends the result to the summary body, so a
	// consumer's file manifest survives compaction as derived fact rather
	// than something the summarizer has to recall. It runs after the layered
	// merge, so the record appears once even when three summarizer calls
	// built the body. When nil, summaries carry no artifact record.
	CompactionArtifacts ArtifactBlockFunc
	// Pruner, if non-nil, replaces the default rule-based compaction with the
	// configured pruning strategy (sliding-window / importance / hybrid /
	// adaptive). The pruner runs alongside the optimizer and LLMSummarizer.
	// When nil, seed uses its built-in Compact() pipeline.
	Pruner *ConversationPruner
	// InitialMessages seeds the agent's conversation state with pre-existing
	// messages (e.g., from a previous query in the same session). When empty,
	// the agent starts with a blank slate.
	InitialMessages []Message

	// InitialCheckpoints seeds the agent's checkpoint state with pre-existing
	// turn checkpoints (e.g., from a previous session restore). When non-empty,
	// these checkpoints are used by BuildCheckpointCompactedMessages in
	// prepareMessages to compact the conversation before sending to the provider.
	InitialCheckpoints []TurnCheckpoint
	// DisableFallbackParser disables the fallback tool-call parser. When
	// disabled, malformed tool calls in model responses will not be recovered.
	// Default (false): fallback parser is enabled when tools are configured.
	DisableFallbackParser bool
	// DisableValidator disables the response validator (truncation/tentative
	// detection). When disabled, incomplete responses will not trigger
	// automatic continuation. Default (false): validator is enabled.
	DisableValidator bool
	// DisableNormalizer disables the tool call normalizer. When disabled,
	// structured tool calls will not be cleaned before execution. Default
	// (false): normalizer is enabled.
	DisableNormalizer bool
	// DisableMinimumSubstanceGuard disables the minimum-substance-after-tools
	// check. When enabled, the guard catches short meta-acknowledgements
	// ("I've reviewed the files.") returned after tool results and retries
	// with a reminder to synthesize findings. Default false: guard enabled.
	DisableMinimumSubstanceGuard bool
	// OnCheckpoint is an optional fire-and-forget callback invoked synchronously
	// after each completed turn. It receives the TurnCheckpoint summarizing what
	// happened in the turn. The callback is invoked immediately after the
	// checkpoint is built and stored in state, before finalize() returns.
	// If the callback panics, the panic is caught and the agent continues
	// normally.
	OnCheckpoint func(TurnCheckpoint)
	// OnDiagnosticCapture is an optional fire-and-forget callback invoked when
	// the agent detects a severe, hard-to-reproduce condition that warrants
	// saving the full prepared message transcript for offline analysis. The
	// primary trigger is a tool-call threading rejection (e.g. MiniMax HTTP 400
	// error 2013: "tool call result does not follow tool call") — a recurring
	// failure the reordering fix did not fully eliminate.
	//
	// The callback receives a DiagnosticCapture with the exact message list,
	// the detected violations, the provider error (when reactive), and the
	// active system prompt. Implementations typically write the payload to disk
	// (see WriteDiagnosticTranscript) and/or publish an event. If the callback
	// panics, the panic is caught and the agent continues normally. The capture
	// never blocks the conversation flow or alters error handling — it is
	// purely observational.
	OnDiagnosticCapture func(DiagnosticCapture)
}

// Agent is the main entry point for the conversation engine.
type Agent struct {
	provider       Provider
	executor       ToolExecutor
	ui             UI
	systemPrompt   string
	maxIterations  int
	maxTokens      int
	debug          bool
	eventPublisher EventPublisher

	state     *State
	outputMgr OutputManager

	paused             bool
	inputInjectionChan chan string

	fallbackParser *FallbackParser
	normalizer     *ToolCallNormalizer
	validator      *ResponseValidator
	// disableSubstanceGuard disables the minimum-substance-after-tools check.
	// When true, short meta-acknowledgements after tool results ("I've
	// reviewed the files.") are accepted without a retry. Default false.
	disableSubstanceGuard bool
	optimizer             *ConversationOptimizer
	onIteration           func(iteration int, messages int, tokenEstimate int, contextSize int)
	onCheckpoint          func(TurnCheckpoint)
	onDiagnostic          func(DiagnosticCapture)
	retryConfig           RetryConfig

	// compactionTriggerFraction is the share of the model's context window
	// above which the chat loop runs proactive compaction. See
	// Options.CompactionTriggerFraction.
	compactionTriggerFraction float64
	// compactionSettleMargin is Options.CompactionSettleMargin.
	compactionSettleMargin float64

	// substitutionTargetFraction is the target context share that Phase 0a
	// (checkpoint substitution) aims for when pressure fires. See
	// Options.SubstitutionTargetFraction.
	substitutionTargetFraction float64

	// llmSummarizer is the consumer-supplied LLM-summary callback used by
	// structural compaction. Nil = disabled, fall back to rule-based compaction.
	llmSummarizer LLMSummarizer

	// continuationCompaction is Options.ContinuationCompaction.
	continuationCompaction *ContinuationCompaction

	// compactionArtifacts renders the durable artifact record appended to
	// structural summaries. Nil = summaries carry no artifact record. See
	// Options.CompactionArtifacts.
	compactionArtifacts ArtifactBlockFunc

	// pruner replaces rule-based compaction when non-nil.
	pruner *ConversationPruner

	// steerMu / steerMsgs hold externally-queued steering messages.
	// They are drained into the ConversationHandler when Run is called,
	// so the steer messages appear in the next API call and are consumed once.
	steerMu   sync.Mutex
	steerMsgs []Message

	// interruptCtx / interruptCancel provide external cancellation.
	// Interrupt() calls interruptCancel to stop a running query from outside.
	// ResetInterrupt() recreates the context pair for the next Run() call.
	// interruptMu protects concurrent access to interruptCtx and interruptCancel.
	interruptMu     sync.Mutex
	interruptCtx    context.Context
	interruptCancel context.CancelFunc
}

// NewAgent creates a new Agent from the given options. Returns an error if
// required options (Provider or ToolExecutor) are not provided.
func NewAgent(opts Options) (*Agent, error) {
	if opts.Provider == nil {
		return nil, ErrNoProvider
	}
	if opts.Executor == nil {
		return nil, ErrNoExecutor
	}

	systemPrompt := opts.SystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = DefaultSystemPrompt
	}

	// Build a set of known tool names for the fallback parser.
	knownTools := make(map[string]bool)
	for _, t := range opts.Executor.GetTools() {
		knownTools[t.Function.Name] = true
	}

	var fallbackParser *FallbackParser
	if !opts.DisableFallbackParser {
		fallbackParser = NewFallbackParser(FallbackParserOptions{KnownToolNames: func(name string) bool { return knownTools[name] }})
	}

	var validator *ResponseValidator
	if !opts.DisableValidator {
		validator = NewResponseValidator(ResponseValidatorOptions{DebugLog: func(format string, args ...interface{}) {
			if opts.Debug {
				fmt.Printf(format, args...)
			}
		}})
	}

	var normalizer *ToolCallNormalizer
	if !opts.DisableNormalizer {
		normalizer = NewToolCallNormalizer()
	}

	// Nil-guard: if no EventPublisher is provided, use a no-op so call sites
	// never need to nil-check. Uses reflection to catch the Go nil-interface
	// gotcha: a nil *T stored in an interface value is not == nil.
	ep := opts.EventPublisher
	if ep == nil || reflect.ValueOf(ep).IsNil() {
		ep = noopEventPublisher{}
	}

	// Create state, seeding with initial messages if provided.
	st := NewState()
	if len(opts.InitialMessages) > 0 {
		// AddMessage (not SetMessages) so every seeded message carries a
		// MetaKeyMsgID identity — the compaction survivor map matches on
		// it, and restored/imported histories (identical nudge texts,
		// repeated file reads) are exactly the duplicate-content case
		// positional/content matching cannot disambiguate.
		for _, m := range opts.InitialMessages {
			st.AddMessage(m)
		}
	}
	if len(opts.InitialCheckpoints) > 0 {
		st.SetCheckpoints(opts.InitialCheckpoints)
	}

	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	return &Agent{
		provider:                   opts.Provider,
		executor:                   opts.Executor,
		ui:                         opts.UI,
		systemPrompt:               systemPrompt,
		maxIterations:              opts.MaxIterations,
		maxTokens:                  opts.MaxTokens,
		debug:                      opts.Debug,
		eventPublisher:             ep,
		state:                      st,
		outputMgr:                  NewOutputManager(ep),
		inputInjectionChan:         make(chan string, 1),
		fallbackParser:             fallbackParser,
		normalizer:                 normalizer,
		validator:                  validator,
		disableSubstanceGuard:      opts.DisableMinimumSubstanceGuard,
		optimizer:                  opts.Optimizer,
		onIteration:                opts.OnIteration,
		onCheckpoint:               opts.OnCheckpoint,
		onDiagnostic:               opts.OnDiagnosticCapture,
		retryConfig:                opts.RetryConfig,
		compactionTriggerFraction:  opts.CompactionTriggerFraction,
		compactionSettleMargin:     opts.CompactionSettleMargin,
		substitutionTargetFraction: opts.SubstitutionTargetFraction,
		llmSummarizer:              opts.LLMSummarizer,
		continuationCompaction:     opts.ContinuationCompaction,
		compactionArtifacts:        opts.CompactionArtifacts,
		pruner:                     opts.Pruner,
		interruptCtx:               interruptCtx,
		interruptCancel:            interruptCancel,
	}, nil
}

// Run executes a single query through the conversation loop.
//
// The query can be cancelled in two ways:
//  1. Cancelling the caller's context (standard Go pattern).
//  2. Calling Interrupt() on this Agent (independent external cancellation).
//
// Interrupt() resets after each Run() call, so it only affects the current query.
func (a *Agent) Run(ctx context.Context, query string) (string, error) {
	if a.paused {
		return "", ErrPaused
	}

	// Reset interrupt state so Interrupt() only affects this run.
	// Capture the new interruptCtx atomically from ResetInterrupt().
	intCtx := a.ResetInterrupt()

	ch := newConversationHandler(a)

	// Derive a context that cancels when EITHER the caller cancels
	// OR Interrupt() is called. This bridges both cancellation sources.
	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-intCtx.Done():
			runCancel()
		case <-done:
		}
	}()
	defer runCancel()

	return ch.ProcessQuery(runCtx, query)
}

// RunStream executes a single query through the streaming conversation loop.
// It uses provider.ChatStream() instead of provider.Chat(), so content is
// delivered incrementally via the StreamHandler callbacks. The return value
// is the final response content extracted from conversation state.
//
// Like Run, this respects both the caller's context cancellation and Interrupt().
func (a *Agent) RunStream(ctx context.Context, query string) (string, error) {
	if a.paused {
		return "", ErrPaused
	}

	// Reset interrupt state so Interrupt() only affects this run.
	// Capture the new interruptCtx atomically from ResetInterrupt().
	intCtx := a.ResetInterrupt()

	ch := newConversationHandler(a)

	// Derive a context that cancels when EITHER the caller cancels
	// OR Interrupt() is called. This bridges both cancellation sources.
	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-intCtx.Done():
			runCancel()
		case <-done:
		}
	}()
	defer runCancel()

	return ch.ProcessQueryStream(runCtx, query)
}

// Interrupt cancels the currently running query, if any.
// This provides a way to stop the agent independently of the caller's context.
//
// Interrupt() has no effect if no query is running. After a Run() call
// completes (whether normally or due to interruption), the interrupt is
// automatically reset for the next call.
//
// If Run() returns ErrInterrupted, the caller can retry by calling Run() again
// — the interrupt has already been reset.
func (a *Agent) Interrupt() {
	a.interruptMu.Lock()
	defer a.interruptMu.Unlock()
	a.interruptCancel()
}

// ResetInterrupt recreates the interrupt context. This is called automatically
// at the start of each Run() call, but can be called manually if needed.
// Returns the new interruptCtx so the caller can capture it atomically.
func (a *Agent) ResetInterrupt() context.Context {
	a.interruptMu.Lock()
	defer a.interruptMu.Unlock()
	a.interruptCancel()
	a.interruptCtx, a.interruptCancel = context.WithCancel(context.Background())
	return a.interruptCtx
}

// Checkpoints returns a copy of all recorded turn checkpoints.
func (a *Agent) Checkpoints() []TurnCheckpoint {
	return a.state.GetCheckpoints()
}

// State returns the agent's conversation state.
func (a *Agent) State() *State {
	return a.state
}

// ExportState serializes the current state to JSON.
func (a *Agent) ExportState() ([]byte, error) {
	return a.state.ExportState()
}

// ImportState deserializes state from JSON.
func (a *Agent) ImportState(data []byte) error {
	return a.state.ImportState(data)
}

// SetSystemPrompt updates the system prompt for future queries.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = prompt
}

// SetFlushCallback sets a callback to flush streaming output.
func (a *Agent) SetFlushCallback(fn func()) {
	a.outputMgr.SetFlushCallback(fn)
}

// StreamingBuffer returns the content streaming buffer.
func (a *Agent) StreamingBuffer() *StreamingBuffer {
	return a.outputMgr.ContentBuffer()
}

// ReasoningBuffer returns the reasoning streaming buffer.
func (a *Agent) ReasoningBuffer() *StreamingBuffer {
	return a.outputMgr.ReasoningBuffer()
}

// Pause pauses the agent for user clarification.
func (a *Agent) Pause() {
	a.paused = true
}

// Resume resumes a paused agent.
func (a *Agent) Resume() {
	a.paused = false
}

// IsPaused returns whether the agent is paused.
func (a *Agent) IsPaused() bool {
	return a.paused
}

// Provider returns the provider (for accessing Info, etc.).
func (a *Agent) Provider() Provider {
	return a.provider
}

// SetProvider swaps the provider at runtime. The new provider will be used
// for all subsequent calls. It is safe to call between queries; calling
// during an active query may cause undefined behavior.
// Panics if p is nil — use NewAgent to create an agent without a provider
// and then call SetProvider with a non-nil provider.
func (a *Agent) SetProvider(p Provider) {
	if p == nil {
		panic("core: SetProvider called with nil provider")
	}
	a.provider = p
}

// InjectInput injects a user message into the conversation via a buffered
// channel. Returns true if the input was accepted (queued for the next loop
// iteration), false if a prior injection is still pending. The injection is
// fire-and-forget with backpressure: a true return means the input is queued,
// but the caller has no guarantee it was consumed yet.
func (a *Agent) InjectInput(input string) bool {
	select {
	case a.inputInjectionChan <- input:
		return true
	default:
		return false
	}
}

// Steer queues a transient message that will be appended to the next API call
// made by this agent. The message is consumed once and is not persisted in the
// conversation state. Use Steer to inject temporary guidance (e.g., "Focus on
// security concerns" or "Respond in JSON format") that should influence only
// the next model response.
//
// Steer must be called between Run() calls. If called during an active Run(),
// the message is queued and will not be consumed until the next Run().
//
// Example:
//
//	agent.Steer(core.Message{Role: "user", Content: "Focus on performance."})
func (a *Agent) Steer(msg Message) {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	a.steerMsgs = append(a.steerMsgs, msg)
}

// SteerSystem is a convenience for injecting a system-level steering message.
// It creates a Message with Role "system" and queues it via Steer, so the
// guidance takes effect on the next API call and is consumed once (like all
// steer messages). The message is not persisted in the conversation state.
//
// Example:
//
//	agent.SteerSystem("Focus on performance, not correctness.")
func (a *Agent) SteerSystem(content string) {
	a.Steer(Message{Role: "system", Content: content})
}

// drainSteerMessages atomically takes all queued steering messages and clears
// the queue. Returns nil (not empty slice) if no messages were queued, so
// callers can distinguish "no steer" from "empty steer".
func (a *Agent) drainSteerMessages() []Message {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if len(a.steerMsgs) == 0 {
		return nil
	}
	msgs := a.steerMsgs
	a.steerMsgs = nil
	return msgs
}

// debugLog logs a debug message if debug mode is enabled.
func (a *Agent) debugLog(format string, args ...interface{}) {
	if a.debug {
		if a.ui != nil {
			a.ui.PrintLine(fmt.Sprintf(format, args...))
		}
	}
}
