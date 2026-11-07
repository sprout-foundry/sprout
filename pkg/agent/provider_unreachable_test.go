package agent

import (
	"errors"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// A provider that fails its connection check is reported as unreachable with
// its cause, and still counts as not configured so a daemon starts without an
// agent.
func TestProviderUnreachableErrorKeepsItsCause(t *testing.T) {
	cause := errors.New("dial tcp 10.0.0.1:8080: connection refused")
	err := agenterrors.NewProviderError("provider check", &providerUnreachableError{provider: "LLM Bridge", cause: cause}, "", "")

	if !errors.Is(err, ErrProviderUnreachable) {
		t.Fatal("want errors.Is(err, ErrProviderUnreachable)")
	}
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatal("want errors.Is(err, ErrProviderNotConfigured), so the daemon still starts")
	}
	got, ok := ProviderUnreachableCause(err)
	if !ok || got != cause.Error() {
		t.Fatalf("ProviderUnreachableCause = %q, %v; want %q, true", got, ok, cause.Error())
	}
	if _, ok := ProviderUnreachableCause(ErrProviderNotConfigured); ok {
		t.Fatal("a not-configured error is not unreachable")
	}
}
