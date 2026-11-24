package core

import (
	"context"
	"sync/atomic"
	"testing"
)

type countingProvider struct {
	mockProvider
	calls atomic.Int32
}

func (c *countingProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	c.calls.Add(1)
	return c.mockProvider.Chat(ctx, req)
}

func TestIsRepetitiveContent_EarlierTurnDoesNotCount(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Reply with exactly: FIRST-CONVO"},
		{Role: "assistant", Content: "FIRST-CONVO"},
		{Role: "user", Content: "What word did you reply with? Just the word."},
		{Role: "assistant", Content: "FIRST-CONVO"},
	}
	ch := buildHandlerForRepetition(t, msgs)
	ch.queryStartIndex = 2
	if ch.isRepetitiveContent("FIRST-CONVO") {
		t.Fatal("an answer matching the previous turn's answer was rejected as repetition")
	}

	ch.queryStartIndex = 0
	if !ch.isRepetitiveContent("FIRST-CONVO") {
		t.Fatal("repetition within one run is no longer detected")
	}
}

func TestProcessQuery_SameAnswerTwoTurnsIsAccepted(t *testing.T) {
	provider := &countingProvider{mockProvider: mockProvider{
		info:       ProviderInfo{ContextSize: 10000},
		tokenCount: 100,
		chatResp: &ChatResponse{Choices: []ChatChoice{{
			Message:      Message{Role: "assistant", Content: "QUEUED-OK"},
			FinishReason: "stop",
		}}},
	}}
	a, err := NewAgent(Options{Provider: provider, Executor: &mockExecutor{}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "Reply with exactly: QUEUED-OK"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	before := provider.calls.Load()
	got, err := a.Run(context.Background(), "Say it once more, exactly.")
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if got != "QUEUED-OK" {
		t.Fatalf("second turn answered %q", got)
	}
	if extra := provider.calls.Load() - before; extra != 1 {
		t.Fatalf("second turn took %d model calls, want 1 (no repetition nudge)", extra)
	}
}
