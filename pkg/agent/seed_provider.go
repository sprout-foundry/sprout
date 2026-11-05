// Package agent provides the seed integration layer.
// sproutProvider implements seed/core.Provider by wrapping api.ClientInterface.
// This file holds the provider surface (NewSproutProvider, the info / model /
// estimate / max-tokens methods) and the shared state. The chat engine, retry /
// backoff, and cost / billing layers live in seed_provider_chat.go,
// seed_provider_retry.go, and seed_provider_cost.go.

package agent

import (
	"strings"
	"sync"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// sproutProvider adapts sprout's ClientInterface to seed's Provider interface.
type sproutProvider struct {
	agent           *Agent
	client          api.ClientInterface
	pastedImages    map[string][]api.ImageData
	pastedImagesMu  sync.RWMutex
	tokenAnchor     tokenAnchor
	maxTokensHint   int
	maxTokensHintMu sync.RWMutex
	// specialTokenGuard bounds corrective hints for the special-token
	// truncation failure (see seed_special_token_guard.go).
	specialTokenGuard specialTokenGuardState

	// steerFlushHook is invoked at the end of every Chat/ChatStream call —
	// a conversation-loop boundary in seed's loop goroutine where staged
	// steer messages can be handed to seed before its injection checks.
	steerFlushHookMu sync.RWMutex
	steerFlushHook   func()

	// recoveryState is a handle to seed's live per-query State, installed by
	// the query setup (setRecoveryState) once the seed agent is built. The
	// repetition-recovery compaction writes its durable de-loop back through
	// this handle so the CURRENT query's later iterations (not just the
	// in-flight retry) read a state that no longer carries the model's own
	// degenerate loop. Nil until the seed agent exists; a nil handle means
	// the durable write is skipped and only the in-flight re-seed runs.
	recoveryStateMu sync.RWMutex
	recoveryState   recoveryStateHandle
}

// setRecoveryState installs the live seed State the repetition-recovery
// compaction persists its durable de-loop into. It is called once per query,
// after the seed agent is constructed, and is a no-op for a nil handle (the
// recovery then falls back to the in-flight re-seed only).
func (sp *sproutProvider) setRecoveryState(st recoveryStateHandle) {
	sp.recoveryStateMu.Lock()
	sp.recoveryState = st
	sp.recoveryStateMu.Unlock()
}

// currentRecoveryState returns the installed live seed State handle (nil
// when none).
func (sp *sproutProvider) currentRecoveryState() recoveryStateHandle {
	sp.recoveryStateMu.RLock()
	defer sp.recoveryStateMu.RUnlock()
	return sp.recoveryState
}

// setSteerFlushHook installs (or with nil, removes) the boundary hook.
func (sp *sproutProvider) setSteerFlushHook(hook func()) {
	sp.steerFlushHookMu.Lock()
	sp.steerFlushHook = hook
	sp.steerFlushHookMu.Unlock()
}

// fireSteerFlushHook runs the boundary hook if installed.
func (sp *sproutProvider) fireSteerFlushHook() {
	sp.steerFlushHookMu.RLock()
	hook := sp.steerFlushHook
	sp.steerFlushHookMu.RUnlock()
	if hook != nil {
		hook()
	}
}

// setRecoveryState installs the live seed State on the sproutProvider that
// `p`, if it is one, so the repetition-recovery compaction can persist its
// durable de-loop into seed's live per-query state. It is the provider-facing
// counterpart of sproutProvider.setRecoveryState and is a no-op for a nil
// state or a non-sprout provider.
func setRecoveryState(p core.Provider, st *core.State) {
	if sp, ok := p.(*sproutProvider); ok {
		sp.setRecoveryState(st)
	}
}

// currentClient returns the agent's live client if available, otherwise the snapshot.
func (sp *sproutProvider) currentClient() api.ClientInterface {
	if sp.agent != nil {
		if c := sp.agent.getClient(); c != nil {
			return c
		}
	}
	return sp.client
}

// NewSproutProvider creates a Provider that wraps a sprout ClientInterface.
func NewSproutProvider(agent *Agent, client api.ClientInterface) (core.Provider, error) {
	if client == nil {
		return nil, agenterrors.NewValidation("sprout provider requires a non-nil client", nil)
	}
	return &sproutProvider{
		agent:        agent,
		client:       client,
		pastedImages: make(map[string][]api.ImageData),
	}, nil
}

// effectiveReasoning resolves the reasoning effort and disable-thinking flag
// the agent wants for this request (rel.1: the configured reasoning_effort
// and the disable_thinking setting actually reach the provider). An empty
// effort lets the provider apply the model family's default depth, so a
// reasoning-capable model reasons by default with no configuration needed.
func (sp *sproutProvider) effectiveReasoning() (reasoning string, disableThinking bool) {
	if sp.agent == nil {
		return "", false
	}
	return sp.agent.configuredReasoningEffort(), sp.agent.shouldDisableThinking()
}

// cappedRecoveryReasoning is the reasoning-effort cap for the recovery final
// attempt. A model that double-loops is most degenerate at its highest
// effort levels (the failure is effort-dose dependent: longer reasoning
// surfaces form more loops and burn more of the budget), so the recovery
// re-seed runs at no higher than "high" — the recommended agentic default —
// and never ABOVE the configured effort (a user pinned to "low" is not
// raised). This is a pure pre-model lever: on a model with no reasoning-effort
// parameter the value is a no-op, so capping is safe and model-agnostic.
// Returns the lower of (reasoning, "high").
//
// An unrecognized non-empty level is treated as above the cap and clamped:
// the cap exists to bound the thinking surface, so a level this function
// cannot rank must not be the one that slips past it.
func cappedRecoveryReasoning(reasoning string) string {
	order := map[string]int{"low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 5}
	const capLevel = "high"
	r := strings.ToLower(strings.TrimSpace(reasoning))
	if r == "" {
		// Unset: the provider applies its family default; leave it alone.
		return reasoning
	}
	if level, known := order[r]; known && level <= order[capLevel] {
		return reasoning
	}
	return capLevel
}

// RegisterPastedImages associates extracted image data with file paths for multimodal attachment.
func (sp *sproutProvider) RegisterPastedImages(images map[string][]api.ImageData) {
	if images == nil {
		return
	}
	sp.pastedImagesMu.Lock()
	for k, v := range images {
		sp.pastedImages[k] = v
	}
	sp.pastedImagesMu.Unlock()
}

func (sp *sproutProvider) Info() core.ProviderInfo {
	client := sp.currentClient()
	ctxLimit, limitErr := client.GetModelContextLimit()
	if limitErr != nil || ctxLimit <= 0 {
		// A 0 ContextSize disables seed's compaction trigger entirely —
		// fall back to the last known effective cap (or 32K) instead.
		ctxLimit = 0
		if sp.agent != nil {
			ctxLimit = sp.agent.effectiveCapSnapshot()
		}
		if ctxLimit <= 0 {
			ctxLimit = 32000
		}
	}
	if sp.agent != nil {
		ctxLimit = sp.agent.reconcileContextCap(ctxLimit)
	}
	return core.ProviderInfo{
		Model:       client.GetModel(),
		ContextSize: ctxLimit,
		HasVision:   api.ResolveVisionCapability(client).AcceptsImages,
	}
}

func (sp *sproutProvider) GetModel() string {
	if c := sp.currentClient(); c != nil {
		return c.GetModel()
	}
	return "unknown"
}

// EstimateTokens estimates the input token count for a chat request.
// Uses the token anchor when available; falls back to the centralized estimator.
func (sp *sproutProvider) EstimateTokens(req *core.ChatRequest) int {
	if req == nil {
		return 0
	}
	// Anchor to the last real Usage.PromptTokens count when the message prefix still matches.
	// Falls back to a full from-scratch heuristic estimate on the first call or after compaction.
	if total, _, ok := sp.tokenAnchor.estimate(sp.currentClient().GetModel(), req.Messages, len(req.Tools), sp.deltaEstimator()); ok {
		return total
	}

	// Delegate to sprout's centralized estimator. When the provider does not
	// replay historical reasoning content on the wire, use the wire-view
	// variant so the estimate matches what the provider actually receives —
	// otherwise a missed anchor inflates the estimate by the full reasoning
	// mass (observed as the context meter jumping ~80K tokens between
	// iterations on reasoning-heavy agents).
	if !sp.replaysReasoningHistory() {
		return api.EstimateInputTokensWireView(req.Messages, req.Tools)
	}
	return api.EstimateInputTokens(req.Messages, req.Tools)
}

// replaysReasoningHistory reports whether the current client replays
// historical assistant reasoning on requests. Unknown client types are
// treated as replaying (conservative: their estimate stays as before).
func (sp *sproutProvider) replaysReasoningHistory() bool {
	c := sp.currentClient()
	if c == nil {
		return true
	}
	if r, ok := c.(providers.ReasoningHistoryReplayer); ok {
		return r.ReplaysReasoningHistory()
	}
	return true
}

// deltaEstimator returns the estimator for the non-anchored message tail —
// the wire-view estimator when the provider does not replay reasoning
// history, nil (default) otherwise.
func (sp *sproutProvider) deltaEstimator() func([]core.Message) int {
	if sp.replaysReasoningHistory() {
		return nil
	}
	return api.EstimateMessagesTokensWireView
}

// setMaxTokensHint stores a pre-computed max_tokens hint for the next request.
func (sp *sproutProvider) setMaxTokensHint(v int) {
	sp.maxTokensHintMu.Lock()
	sp.maxTokensHint = v
	sp.maxTokensHintMu.Unlock()
}

// getMaxTokensHint returns the pre-computed max_tokens hint.
func (sp *sproutProvider) getMaxTokensHint() int {
	sp.maxTokensHintMu.RLock()
	v := sp.maxTokensHint
	sp.maxTokensHintMu.RUnlock()
	return v
}

// getContextLimit returns the effective context limit for the current provider.
func (sp *sproutProvider) getContextLimit() int {
	info := sp.Info()
	if info.ContextSize > 0 {
		return info.ContextSize
	}
	return 32000
}

// computeMaxTokensHint pre-computes max_tokens using the anchored token breakdown when available.
func (sp *sproutProvider) computeMaxTokensHint(req *core.ChatRequest) {
	if req == nil {
		sp.setMaxTokensHint(0)
		return
	}
	total, heuristic, ok := sp.tokenAnchor.estimate(sp.currentClient().GetModel(), req.Messages, len(req.Tools), sp.deltaEstimator())
	if !ok {
		sp.setMaxTokensHint(0) // no hint — let provider compute from scratch
		return
	}
	contextLimit := sp.getContextLimit()
	// The !ok case (estimate says input fills the window) now returns the
	// remaining window instead of a sentinel; pinning a tiny floor here
	// decapitated responses (finish=output_limit at exactly the floor).
	maxOutput, _ := api.CalculateOutputBudgetAnchored(contextLimit, total-heuristic, heuristic)
	sp.setMaxTokensHint(maxOutput)
}

func (sp *sproutProvider) clearPastedImages() {
	sp.pastedImagesMu.Lock()
	defer sp.pastedImagesMu.Unlock()
	sp.pastedImages = nil
}
