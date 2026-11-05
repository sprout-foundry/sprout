package summarizerclient

import (
	"context"
	"testing"
)

// fakeClient records the prompts it was given and returns a fixed answer, so
// the LastOutputOf contract can be checked without a provider.
type fakeClient struct {
	system, user string
	answer       string
}

func (f *fakeClient) Complete(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	f.system, f.user = systemPrompt, userPrompt
	return f.answer, nil
}

func TestNewReturnsNilWithoutAConfiguredRole(t *testing.T) {
	if got := New(nil); got != nil {
		t.Errorf("New(nil) = %v, want nil", got)
	}
}

// LastOutputOf reports "" for a client that does not track its output, so a
// caller's usage estimate degrades to its conservative floor rather than
// panicking on a type assertion.
func TestLastOutputOfFallsBackForClientsThatDoNotTrackIt(t *testing.T) {
	if got := LastOutputOf(&fakeClient{answer: "hello"}); got != "" {
		t.Errorf("LastOutputOf(fake) = %q, want empty", got)
	}
}

func TestLastOutputOfReadsATrackingClient(t *testing.T) {
	if got := LastOutputOf(&apiClient{lastOutput: "tracked"}); got != "tracked" {
		t.Errorf("LastOutputOf(apiClient) = %q, want %q", got, "tracked")
	}
}

func TestLastOutputOfHandlesNil(t *testing.T) {
	if got := LastOutputOf(nil); got != "" {
		t.Errorf("LastOutputOf(nil) = %q, want empty", got)
	}
}
