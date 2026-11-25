package core

import (
	"context"
	"fmt"
	"time"
)

// doChatWithRetry performs a single LLM chat call with exponential backoff
// retry for transient and rate-limit errors. It classifies errors to decide
// whether to retry, recover via compaction, fail fast, or continue retrying.
//
// The retry loop is:
//  1. Create an exponential backoff from agent config
//  2. Loop: call provider.Chat(), record result on success
//  3. On error: classify, recover-via-compaction on context overflow (once),
//     fail-fast on auth/client errors, retry on transient
//  4. Return last classified error when retries exhausted
func (ch *ConversationHandler) doChatWithRetry(ctx context.Context, req *ChatRequest, iter int) (*ChatResponse, error) {
	backoff := NewExponentialBackoff(
		ch.agent.retryConfig.InitialDelayOrDefault(),
		ch.agent.retryConfig.MaxDelayOrDefault(),
		ch.agent.retryConfig.MultiplierOrDefault(),
		ch.agent.retryConfig.MaxAttemptsOrDefault(),
		ch.agent.retryConfig.JitterOrDefault(),
	)
	// contextOverflowRecovered tracks whether we have already run the
	// compact-and-retry recovery path for this call. Recovery runs at most once
	// per chat invocation: if the provider rejects the request a second time
	// for the same reason after compaction, we fail fast rather than thrash.
	contextOverflowRecovered := false
	var lastErr error
	for backoff.NextAttempt() {
		if backoff.attempt > 1 {
			ch.agent.debugLog("[retry] Attempt %d, waiting %v\n", backoff.attempt, backoff.Delay())
			select {
			case <-time.After(backoff.Delay()):
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %w", ErrInterrupted, ctx.Err())
			}
		}

		resp, err := ch.agent.provider.Chat(ctx, req)
		if err == nil {
			// Record token usage and assistant message inline
			if resp.Usage.TotalTokens > 0 {
				ch.agent.state.AddTokens(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

				// Publish metrics update event
				if ch.agent.eventPublisher != nil {
					ch.agent.eventPublisher.Publish(EventTypeMetricsUpdate, map[string]interface{}{
						"total_tokens":       ch.agent.state.TotalTokens(),
						"context_tokens":     resp.Usage.PromptTokens,
						"max_context_tokens": ch.agent.provider.Info().ContextSize,
						"iteration":          iter,
						"total_cost":         ch.agent.state.TotalCost(),
					})
				}
			}

			ch.agent.state.AddMessage(resp.ToMessage())
			return resp, nil
		}

		// Classify the error
		classifiedErr := ClassifyError(err, ch.agent.provider.Info().Model)
		lastErr = classifiedErr

		// Fail fast on auth errors — retry won't help
		if IsAuthError(classifiedErr) {
			ch.agent.debugLog("[!!] Auth error, failing fast: %v\n", classifiedErr)
			return nil, classifiedErr
		}

		// Context overflow — try one round of aggressive recovery compaction
		// then retry. If we have already recovered once, or compaction had
		// nothing left to drop, fail fast: thrashing on the same error helps
		// nobody.
		if IsContextOverflow(classifiedErr) {
			if contextOverflowRecovered {
				ch.agent.debugLog("[!!] Context overflow persisted after recovery compaction, failing fast: %v\n", classifiedErr)
				return nil, classifiedErr
			}
			if !ch.tryContextOverflowRecovery(req) {
				ch.agent.debugLog("[!!] Context overflow — compaction could not reduce further, failing fast: %v\n", classifiedErr)
				return nil, classifiedErr
			}
			contextOverflowRecovered = true
			ch.agent.debugLog("[retry] Context overflow — compacted prompt, retrying\n")
			continue
		}

		// Fail fast on client errors — retry won't help
		if IsClientError(classifiedErr) {
			ch.agent.debugLog("[!!] Client error, failing fast: %v\n", classifiedErr)
			return nil, classifiedErr
		}

		// Retry on transient/rate-limit errors.
		// (ClassifyError defaults to TransientError for unknown errors, so this
		// path always matches after auth/context-overflow/client errors are handled above.)
		if IsTransient(classifiedErr) || IsRateLimit(classifiedErr) {
			ch.agent.debugLog("[retry] Retryable error: %v\n", classifiedErr)
			// Publish error event for each retry attempt (observability).
			if ch.agent.eventPublisher != nil {
				ch.agent.eventPublisher.Publish(EventTypeError, map[string]interface{}{
					"message": "chat failed",
					"error":   classifiedErr.Error(),
				})
			}
			continue
		}
	}

	// All retries exhausted — return the last classified error
	return nil, lastErr
}
