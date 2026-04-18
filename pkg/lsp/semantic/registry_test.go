package semantic

import (
	"testing"
	"time"
)

type countingAdapter struct {
	runCount int
	sleepFor time.Duration
}

func (a *countingAdapter) Run(input ToolInput) (ToolResult, error) {
	_ = input
	a.runCount++
	if a.sleepFor > 0 {
		time.Sleep(a.sleepFor)
	}
	return ToolResult{Capabilities: Capabilities{Diagnostics: true}}, nil
}

func TestRegistryRegisterSingletonReusesAdapterAndTimesRun(t *testing.T) {
	registry := NewRegistry()
	adapter := &countingAdapter{sleepFor: 2 * time.Millisecond}
	registry.RegisterSingleton(adapter, "typescript")

	first, ok := registry.AdapterForLanguage("typescript")
	if !ok {
		t.Fatal("expected singleton adapter to be registered")
	}
	second, ok := registry.AdapterForLanguage("typescript")
	if !ok {
		t.Fatal("expected singleton adapter on second lookup")
	}

	firstResult, err := first.Run(ToolInput{})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	secondResult, err := second.Run(ToolInput{})
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	if adapter.runCount != 2 {
		t.Fatalf("expected shared singleton adapter to run twice, got %d", adapter.runCount)
	}
	if firstResult.DurationMs <= 0 {
		t.Fatalf("expected first result DurationMs > 0, got %d", firstResult.DurationMs)
	}
	if secondResult.DurationMs <= 0 {
		t.Fatalf("expected second result DurationMs > 0, got %d", secondResult.DurationMs)
	}
}
