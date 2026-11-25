package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// failingProvider fails every chat call with err and counts the calls.
type failingProvider struct {
	calls int
	err   error
}

func (p *failingProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	p.calls++
	return nil, p.err
}

func (p *failingProvider) ChatStream(_ context.Context, _ *ChatRequest, _ StreamHandler) error {
	p.calls++
	return p.err
}

func (p *failingProvider) Info() ProviderInfo { return ProviderInfo{Model: "test", ContextSize: 20000} }

func (p *failingProvider) EstimateTokens(req *ChatRequest) int { return roughTokens(req.Messages) }

func TestProcessQuery_ClientErrorFailsFast(t *testing.T) {
	for name, providerErr := range map[string]error{
		"typed client error": &ClientError{Provider: "test", Wrapped: errors.New("HTTP 404: model gone")},
		"402 by message":     errors.New("HTTP 402: You're out of credits."),
	} {
		t.Run(name, func(t *testing.T) {
			p := &failingProvider{err: providerErr}
			a, err := NewAgent(Options{
				Provider:    p,
				Executor:    &mockExecutor{},
				RetryConfig: RetryConfig{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.Run(context.Background(), "hello"); !IsClientError(err) {
				t.Fatalf("expected a client error, got %v", err)
			}
			if p.calls != 1 {
				t.Fatalf("a client error must not be retried: got %d calls", p.calls)
			}
		})
	}
}

func TestProcessQuery_TransientErrorStillRetries(t *testing.T) {
	p := &failingProvider{err: errors.New("HTTP 503: service unavailable")}
	a, err := NewAgent(Options{
		Provider:    p,
		Executor:    &mockExecutor{},
		RetryConfig: RetryConfig{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "hello"); err == nil {
		t.Fatal("expected failure")
	}
	if p.calls < 2 {
		t.Fatalf("a transient error should be retried, got %d calls", p.calls)
	}
}
