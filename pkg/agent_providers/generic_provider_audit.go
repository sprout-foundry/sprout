package providers

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_audit"
)

// emitCallAudit records one facts-only audit event for a model call. It hashes
// the request and response bytes it is given (never the content), reports the
// token usage, and classifies the outcome. It is nil-safe: a no-op when no
// audit sink is installed.
//
// The event carries digests and metadata only. The provider is the only place
// that sees the wire request and response bytes, so it is the natural seam for
// the digests; the session identity and trigger travel on ctx (the agent
// attaches them), keeping concurrent agents from clobbering each other.
func (p *GenericProvider) emitCallAudit(ctx context.Context, messages []api.Message, model string, requestBody, responseBody []byte, resp *api.ChatResponse, callErr error, failover, streaming bool) {
	if agent_audit.CurrentSink() == nil {
		return
	}

	reqHash, reqBytes := agent_audit.Digest(requestBody)
	respHash, respBytes := agent_audit.Digest(responseBody)

	outcome := agent_audit.OutcomeOK
	switch {
	case callErr != nil:
		outcome = agent_audit.OutcomeError
	case failover:
		outcome = agent_audit.OutcomeFailover
	}

	ev := agent_audit.CallEvent{
		Provider:       p.config.Name,
		Model:          model,
		EndpointHost:   agent_audit.EndpointHost(p.config.Endpoint),
		RequestSHA256:  reqHash,
		RequestBytes:   reqBytes,
		ResponseSHA256: respHash,
		ResponseBytes:  respBytes,
		Outcome:        outcome,
		Trigger:        agent_audit.ResolveTrigger(ctx, lastMessageRole(messages)),
		Streaming:      streaming,
		Failover:       failover,
	}
	if resp != nil {
		ev.PromptTokens = resp.Usage.PromptTokens
		ev.CompletionTokens = resp.Usage.CompletionTokens
		ev.ReasoningTokens = reasoningTokensForResponse(resp)
	}
	agent_audit.EmitCall(ctx, ev)
}

// reasoningTokensForResponse estimates the reasoning-token count returned by a
// model call from the captured reasoning_content (rel.1). It is a rough
// estimate — the wire does not always report a reasoning-token usage field —
// but it makes "0 reasoning tokens" visible in the run log so a model that is
// supposed to be reasoning can be told from one that is not.
func reasoningTokensForResponse(resp *api.ChatResponse) int {
	if resp == nil || len(resp.Choices) == 0 {
		return 0
	}
	rc := resp.Choices[0].Message.ReasoningContent
	if rc == "" {
		return 0
	}
	return api.EstimateTokens(rc)
}

// lastMessageRole returns the role of the final message in the request, which
// distinguishes a tool-call follow-up (last role "tool") from a fresh user
// turn. Empty when there are no messages.
func lastMessageRole(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Role
}
