package agent

import (
	"errors"
	"testing"

	"github.com/sprout-foundry/seed/core"
)

func TestClassifyTerminal_PaymentRequiredIsNotRetried(t *testing.T) {
	sp := &sproutProvider{}
	err := sp.classifyTerminal(errors.New("HTTP 402: You're out of platform credits."))
	if !core.IsAuthError(err) {
		t.Fatalf("402 should become a fail-fast AuthError, got %T: %v", err, err)
	}
	if core.IsTransient(core.ClassifyError(err, "managed")) {
		t.Fatal("seed must not reclassify the 402 as transient")
	}
}

func TestClassifyTerminal_LeavesOtherErrorsAlone(t *testing.T) {
	sp := &sproutProvider{}
	orig := errors.New("HTTP 503: upstream unavailable")
	if got := sp.classifyTerminal(orig); got != orig {
		t.Fatalf("non-402 error changed: %v", got)
	}
	if sp.classifyTerminal(nil) != nil {
		t.Fatal("nil should stay nil")
	}
}
