package agent

import (
	"context"
	"testing"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// TestDoChatWithRetryStreamingRetriesNetworkTypedError pins rel.5(a): a
// mid-stream connection reset (agenterrors.NewNetwork) or the
// first-token/idle timeout used to be read as non-retryable by the LLM retry
// loops — the turn failed on attempt 1 with "giving up attempt 1/4". After
// the classification fix the loop retries the retryable network/timeout
// TypedError and the turn succeeds on the next attempt.
func TestDoChatWithRetryStreamingRetriesNetworkTypedError(t *testing.T) {
	client := NewScriptedClient(
		NewScriptedResponseBuilder().
			Error(agenterrors.NewNetwork("streaming response idle timeout (no chunk for 2m0s)", nil)).
			Build(),
		NewScriptedResponseBuilder().Content("recovered").Build(),
	)
	sp := &sproutProvider{
		client: client,
	}
	// No agent: the retry-event path is skipped, and trackFleetBudget /
	// tokenAnchor are nil-safe on a bare provider.
	resp, err := sp.doChatWithRetryStreaming(context.Background(), nil, nil, "", false, func(string, string) {}, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected the network TypedError to be retried and succeed, got: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response after retry")
	}
	if got := resp.Choices[0].Message.Content; got != "recovered" {
		t.Errorf("content = %q, want %q", got, "recovered")
	}
	// Two client calls: the failed attempt and the successful retry.
	if n := client.GetIndex(); n != 2 {
		t.Errorf("client saw %d attempts, want 2", n)
	}
}

// TestDoChatWithRetryStreamingRetriesTimeoutTypedError: the first-token /
// idle timeout is also a retryable network-shaped failure.
func TestDoChatWithRetryStreamingRetriesTimeoutTypedError(t *testing.T) {
	client := NewScriptedClient(
		NewScriptedResponseBuilder().
			Error(agenterrors.NewNetwork("streaming response first-token timeout (no chunk within 10m0s)", nil)).
			Build(),
		NewScriptedResponseBuilder().Content("ok").Build(),
	)
	sp := &sproutProvider{client: client}
	if _, err := sp.doChatWithRetryStreaming(context.Background(), nil, nil, "", false, func(string, string) {}, nil, nil, nil); err != nil {
		t.Fatalf("first-token timeout should be retried, got: %v", err)
	}
}

// TestDoChatWithRetryStreamingNonRetryableTypedErrorFailsAtOnce: a typed
// error that is not network/timeout (e.g. a validation failure) must NOT be
// retried — it surfaces on the first attempt, just like a 401.
func TestDoChatWithRetryStreamingNonRetryableTypedErrorFailsAtOnce(t *testing.T) {
	client := NewScriptedClient(
		NewScriptedResponseBuilder().
			Error(agenterrors.NewValidation("model not found", nil)).
			Build(),
		NewScriptedResponseBuilder().Content("should not run").Build(),
	)
	sp := &sproutProvider{client: client}
	_, err := sp.doChatWithRetryStreaming(context.Background(), nil, nil, "", false, func(string, string) {}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected a non-retryable typed error to fail immediately")
	}
	if n := client.GetIndex(); n != 1 {
		t.Errorf("client saw %d attempts, want exactly 1 (no retry)", n)
	}
}

// TestIsRetryableProviderErrorTypedTable pins the classification the retry
// loops depend on: retryable network/timeout typed errors retry; a
// non-retryable typed error and nil do not.
func TestIsRetryableProviderErrorTypedTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"network", agenterrors.NewNetwork("reset", nil), true},
		{"timeout", agenterrors.NewTimeout("idle", 3*time.Second), true},
		{"validation", agenterrors.NewValidation("bad", nil), false},
		{"not_found", agenterrors.NewNotFound("model"), false},
		{"config", agenterrors.NewConfig("missing", nil), false},
	}
	for _, c := range cases {
		if got := isRetryableProviderError(c.err); got != c.want {
			t.Errorf("isRetryableProviderError(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
