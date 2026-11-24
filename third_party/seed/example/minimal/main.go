// minimal demonstrates the simplest possible integration of seed into a Go project.
//
// This example wires up a mock provider with no tools, no event bus, and no UI.
// Replace the mock provider with your own implementation of core.Provider.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sprout-foundry/seed/core"
)

// mockProvider implements core.Provider for demonstration.
// In a real project, replace this with your LLM provider.
type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{
		Choices: []core.ChatChoice{
			{Message: core.Message{Role: "assistant", Content: "Hello from seed!"}},
		},
	}, nil
}

func (m *mockProvider) ChatStream(_ context.Context, _ *core.ChatRequest, _ core.StreamHandler) error {
	return nil
}

func (m *mockProvider) Info() core.ProviderInfo {
	return core.ProviderInfo{Model: "mock", ContextSize: 128000}
}

func (m *mockProvider) EstimateTokens(_ *core.ChatRequest) int {
	return 10
}

func main() {
	agent, err := core.NewAgent(core.Options{
		Provider: &mockProvider{},
		Executor: core.NoopExecutor,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create agent: %v\n", err)
		os.Exit(1)
	}

	result, err := agent.Run(context.Background(), "Say hello")
	if err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result)
}
