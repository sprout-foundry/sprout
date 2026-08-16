// Package agent: token-anchoring for sproutProvider.EstimateTokens.
//
// EstimateTokens (seed_provider.go) fed both seed's compaction trigger and
// CalculateOutputBudget's max_tokens sizing from a from-scratch heuristic
// estimate of the *entire* conversation, every single call — even though the
// exact actual prompt-token count is already known from the previous
// response's Usage.PromptTokens. This file anchors the estimate to that real
// number instead of discarding it: only the messages appended since the last
// real measurement go through the (error-prone) heuristic, so estimation
// error no longer compounds across a long-running conversation.
package agent

import (
	"hash/fnv"
	"sync"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// tokenAnchor caches the last real prompt-token count reported by the
// provider, keyed to a fingerprint of the exact message prefix that produced
// it and the model that measured it. It is safe for concurrent use.
type tokenAnchor struct {
	mu           sync.RWMutex
	messageCount int
	fingerprint  uint64
	toolCount    int
	model        string
	actualTokens int
}

// fingerprintMessages hashes the role/content/reasoning/tool-call shape of a
// message slice so a later, longer slice can be checked for "same prefix,
// purely appended to" without a deep comparison. Any edit inside the prefix —
// checkpoint substitution, rollup, /compact, observation masking — changes
// the hash and invalidates the anchor, so a stale anchor can never be reused
// against content it didn't actually measure.
func fingerprintMessages(messages []core.Message) uint64 {
	h := fnv.New64a()
	const sep = 0x1e // ASCII record separator; prevents field concatenation collisions
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{sep})
	}
	for _, m := range messages {
		write(m.Role)
		write(m.Content)
		write(m.ReasoningContent)
		write(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			write(tc.ID)
			write(tc.Type)
			write(tc.Function.Name)
			write(tc.Function.Arguments)
		}
	}
	return h.Sum64()
}

// update records the actual prompt-token count for the exact messages/tools
// that produced it (a real API response), so a later estimate() call can
// anchor to it. The anchor is bound to the measuring model: a mid-run model
// switch invalidates it because tokenizer output differs between models.
// Ignores non-positive counts (providers that don't report usage) so the
// anchor never gets poisoned by a zero.
func (a *tokenAnchor) update(model string, messages []core.Message, toolCount int, actualTokens int) {
	if actualTokens <= 0 {
		return
	}
	fp := fingerprintMessages(messages)
	a.mu.Lock()
	a.messageCount = len(messages)
	a.fingerprint = fp
	a.toolCount = toolCount
	a.model = model
	a.actualTokens = actualTokens
	a.mu.Unlock()
}

// estimate returns an anchored token estimate for messages/tools when the
// cached anchor's message prefix still matches the start of messages, or
// false when there's no usable anchor — first call, model changed, tool list
// changed, or the prefix was edited by compaction/substitution/masking since
// the anchor was recorded. Callers must fall back to a full heuristic
// estimate in that case.
//
// Returns (totalTokens, heuristicTokens, ok). When ok is true, totalTokens
// is the full estimate and heuristicTokens is the portion that went through
// the heuristic (subject to EstimationErrorPercent). The anchored portion
// (totalTokens - heuristicTokens) is a real measurement.
func (a *tokenAnchor) estimate(model string, messages []core.Message, toolCount int) (totalTokens, heuristicTokens int, ok bool) {
	a.mu.RLock()
	messageCount, fingerprint, anchoredToolCount, anchoredModel, actualTokens := a.messageCount, a.fingerprint, a.toolCount, a.model, a.actualTokens
	a.mu.RUnlock()

	if actualTokens <= 0 || messageCount == 0 {
		return 0, 0, false
	}
	if model != anchoredModel {
		return 0, 0, false
	}
	if toolCount != anchoredToolCount {
		return 0, 0, false
	}
	if len(messages) < messageCount {
		return 0, 0, false
	}
	if fingerprintMessages(messages[:messageCount]) != fingerprint {
		return 0, 0, false
	}

	delta := api.EstimateMessagesTokens(messages[messageCount:])
	return actualTokens + delta, delta, true
}
