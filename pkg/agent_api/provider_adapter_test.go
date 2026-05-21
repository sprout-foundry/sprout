package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	utils "github.com/sprout-foundry/sprout/pkg/utils"
)

// mockClient implements ClientInterface for testing
type mockClient struct {
	sendChatRequestFunc func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error)
	provider            string
	model               string
}

func (m *mockClient) SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	if m.sendChatRequestFunc != nil {
		return m.sendChatRequestFunc(messages, tools, reasoning, disableThinking)
	}
	return &ChatResponse{
		Choices: []Choice{{
			Message: Message{
				Content: "test response",
			},
		}},
	}, nil
}

func (m *mockClient) SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
	return m.SendChatRequest(context.Background(), messages, tools, reasoning, disableThinking)
}

func (m *mockClient) CheckConnection() error                                      { return nil }
func (m *mockClient) SetDebug(debug bool)                                         {}
func (m *mockClient) SetModel(model string) error                                  { m.model = model; return nil }
func (m *mockClient) GetModel() string                                             { return m.model }
func (m *mockClient) GetProvider() string                                          { return m.provider }
func (m *mockClient) GetModelContextLimit() (int, error)                           { return 128000, nil }
func (m *mockClient) ListModels(ctx context.Context) ([]ModelInfo, error)          { return nil, nil }
func (m *mockClient) SupportsVision() bool                                         { return false }
func (m *mockClient) GetVisionModel() string                                       { return "" }
func (m *mockClient) SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	return &ChatResponse{
		Choices: []Choice{{
			Message: Message{
				Content: "vision",
			},
		}},
	}, nil
}
func (m *mockClient) GetLastTPS() float64            { return 0 }
func (m *mockClient) GetAverageTPS() float64         { return 0 }
func (m *mockClient) GetTPSStats() map[string]float64 { return nil }
func (m *mockClient) ResetTPSStats()                 {}

func TestProviderAdapterRateLimiter_AcquiresToken(t *testing.T) {
	provider := "test-rate-limit-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Set a restrictive rate: 10 tps, burst 1
	utils.SetProviderRate(provider, 10.0, 1)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	// First request should succeed immediately (burst=1)
	resp, err := adapter.SendChatRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("Expected at least one choice in response")
	}
	if resp.Choices[0].Message.Content != "test response" {
		t.Errorf("Expected 'test response', got %s", resp.Choices[0].Message.Content)
	}
}

func TestProviderAdapterRateLimiter_ContextCancellation(t *testing.T) {
	provider := "test-cancel-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Set a very restrictive rate: 0.1 tps, burst 1
	utils.SetProviderRate(provider, 0.1, 1)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	// Drain the single burst token
	limiter := utils.GetProviderRateLimiter(provider)
	limiter.TryWait() // Use the burst token

	// Now the bucket is empty; next request will need to wait ~10 seconds
	// Cancel the context after 50ms to test cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	_, err := adapter.SendChatRequest(ctx, req)
	if err == nil {
		t.Fatal("Expected context cancellation error")
	}
	if !strings.Contains(err.Error(), "rate limit wait canceled") {
		t.Errorf("Expected rate limit wait canceled error, got: %v", err)
	}
}

func TestProviderAdapterRateLimiter_ConcurrentRequests(t *testing.T) {
	provider := "test-concurrent-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Allow burst=10 to let multiple concurrent requests through quickly
	utils.SetProviderRate(provider, 100.0, 10)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)
	errs := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			defer wg.Done()
			req := &ProviderChatRequest{
				Messages: []Message{{Role: "user", Content: "hello"}},
				Tools:    []Tool{},
			}
			_, err := adapter.SendChatRequest(context.Background(), req)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)
		}
	}
}

func TestProviderAdapterRateLimiter_TokenReplenishment(t *testing.T) {
	provider := "test-replenish-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// burst=1, rate=100 (fast refill, ~10ms per token)
	utils.SetProviderRate(provider, 100.0, 1)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	// First request uses the burst token
	resp, err := adapter.SendChatRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "test response" {
		t.Fatal("First request returned unexpected content")
	}

	// Wait briefly for token replenishment (at 100 tps, ~10ms per token)
	time.Sleep(20 * time.Millisecond)

	// Second request should succeed after replenishment
	resp, err = adapter.SendChatRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("Second request (after replenishment) failed: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "test response" {
		t.Fatal("Second request returned unexpected content")
	}
}

func TestProviderAdapterRateLimiter_PropagatesErrors(t *testing.T) {
	provider := "test-err-prop-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Generous rate so the limiter doesn't block
	utils.SetProviderRate(provider, 100.0, 10)

	expectedErr := errors.New("simulated API error")
	mock := &mockClient{
		provider: provider,
		sendChatRequestFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return nil, expectedErr
		},
	}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	_, err := adapter.SendChatRequest(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error from mock client")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected wrapped error, got: %v", err)
	}
}

func TestProviderAdapterRateLimiter_ClientTypeMapping(t *testing.T) {
	// Verify that real ClientType constants produce the expected rate limiter defaults.
	// This catches mismatches between ClientType string values and getDefaultRateForProvider.
	testCases := []struct {
		clientType   ClientType
		expectedRate float64
		expectedBurst int
	}{
		{OpenAIClientType, 1.0, 5},
		{OpenRouterClientType, 2.0, 10},
		{DeepInfraClientType, 1.0, 5},
		{DeepSeekClientType, 0.5, 3},
		{OllamaLocalClientType, 10.0, 20},
		{OllamaTurboClientType, 10.0, 20},
		{ZAIClientType, 2.0, 10},
		{ChutesClientType, 2.0, 10},
		{LMStudioClientType, 10.0, 20},
		{MistralClientType, 1.0, 5},
		{CerebrasClientType, 2.0, 10},
	}

	for _, tc := range testCases {
		t.Run(string(tc.clientType), func(t *testing.T) {
			providerKey := string(tc.clientType)
			utils.RemoveProviderRateLimiter(providerKey)
			defer utils.RemoveProviderRateLimiter(providerKey)

			limiter := utils.GetProviderRateLimiter(providerKey)
			if limiter.GetRate() != tc.expectedRate {
				t.Errorf("Provider %s: expected rate %f, got %f",
					tc.clientType, tc.expectedRate, limiter.GetRate())
			}
			if limiter.GetBurst() != tc.expectedBurst {
				t.Errorf("Provider %s: expected burst %d, got %d",
					tc.clientType, tc.expectedBurst, limiter.GetBurst())
			}
		})
	}
}
