//go:build !js

package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// --- TestMockLLMProvider_DefaultResponse ---
func TestMockLLMProvider_DefaultResponse(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()

	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "hello world"},
	}, nil, "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}

	content := resp.Choices[0].Message.Content
	if !strings.Contains(content, "I received:") {
		t.Errorf("expected default response to contain 'I received:', got: %s", content)
	}
	if !strings.Contains(content, "hello world") {
		t.Errorf("expected default response to echo user message, got: %s", content)
	}
}

// --- TestMockLLMProvider_LsPrompt ---
func TestMockLLMProvider_LsPrompt(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()

	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "please list files"},
	}, nil, "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := resp.Choices[0].Message.Content
	if !strings.Contains(strings.ToLower(content), "ls") {
		t.Errorf("expected ls stub response, got: %s", content)
	}
}

// --- TestMockLLMProvider_EchoPrompt ---
func TestMockLLMProvider_EchoPrompt(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()

	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "echo hi"},
	}, nil, "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := resp.Choices[0].Message.Content
	if !strings.Contains(strings.ToLower(content), "echo") {
		t.Errorf("expected echo stub response, got: %s", content)
	}
}

// --- TestMockLLMProvider_CustomResponse ---
func TestMockLLMProvider_CustomResponse(t *testing.T) {
	m := NewMockLLMProvider()
	m.ResponsesByPrompt["magic"] = "42"

	ctx := context.Background()
	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "tell me the magic answer"},
	}, nil, "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := resp.Choices[0].Message.Content
	if content != "42" {
		t.Errorf("expected custom response '42', got: %s", content)
	}
}

// --- TestMockLLMProvider_CallCount ---
func TestMockLLMProvider_CallCount(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()

	messages := []api.Message{{Role: "user", Content: "test"}}
	for i := 0; i < 3; i++ {
		_, err := m.SendChatRequest(ctx, messages, nil, "", false)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}

	if m.CallCount != 3 {
		t.Errorf("expected CallCount == 3, got %d", m.CallCount)
	}
}

// --- TestMockLLMProvider_ContextCancelled ---
func TestMockLLMProvider_ContextCancelled(t *testing.T) {
	m := NewMockLLMProvider()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "hello"},
	}, nil, "", false)

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// --- TestUseMockLLM_Toggle ---
func TestUseMockLLM_Toggle(t *testing.T) {
	// Save and restore the original value
	orig := UseMockLLM
	defer func() { UseMockLLM = orig }()

	// Test that NewMockLLMProvider returns a valid provider
	UseMockLLM = true
	m := NewMockLLMProvider()
	if m == nil {
		t.Fatal("NewMockLLMProvider returned nil")
	}

	// Verify it implements ClientInterface
	var _ api.ClientInterface = m

	// Basic sanity: send a request
	ctx := context.Background()
	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "ping"},
	}, nil, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	UseMockLLM = false
}

// --- TestServeCmdExists ---
func TestMockLLMProvider_ServePlumbing(t *testing.T) {
	// Save and restore UseMockLLM
	orig := UseMockLLM
	defer func() { UseMockLLM = orig }()
	UseMockLLM = false

	// The serve command is registered via init() in cmd/serve.go which
	// calls rootCmd.AddCommand(serveCmd).  We can verify it by checking
	// that rootCmd has a "serve" subcommand.
	//
	// Note: rootCmd is in the cmd package, not accessible from pkg/agent.
	// Instead, verify that serveCmd exists by checking that the agent
	// package variable and mock provider work together correctly.
	//
	// The actual cobra command registration is tested indirectly:
	// if cmd/serve.go compiles and imports correctly, the command exists.
	// Here we verify the plumbing: setting UseMockLLM=true is what
	// serveCmd.RunE does to enable the mock provider.

	UseMockLLM = true

	// Create a mock provider to verify the plumbing works
	m := NewMockLLMProvider()
	if m == nil {
		t.Fatal("expected non-nil mock provider")
	}

	ctx := context.Background()
	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "test serve plumbing"},
	}, nil, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response from mock provider")
	}

	// Reset
	UseMockLLM = false
}

// --- Additional interface coverage tests ---

func TestMockLLMProvider_CheckConnection(t *testing.T) {
	m := NewMockLLMProvider()
	err := m.CheckConnection()
	if err != nil {
		t.Errorf("CheckConnection should always succeed, got: %v", err)
	}
}

func TestMockLLMProvider_SetGetModel(t *testing.T) {
	m := NewMockLLMProvider()

	if m.GetModel() != "mock-model" {
		t.Errorf("default model should be 'mock-model', got: %s", m.GetModel())
	}

	err := m.SetModel("custom-model")
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if m.GetModel() != "custom-model" {
		t.Errorf("expected 'custom-model', got: %s", m.GetModel())
	}
}

func TestMockLLMProvider_GetProvider(t *testing.T) {
	m := NewMockLLMProvider()
	if m.GetProvider() != "mock" {
		t.Errorf("expected provider 'mock', got: %s", m.GetProvider())
	}
}

func TestMockLLMProvider_GetModelContextLimit(t *testing.T) {
	m := NewMockLLMProvider()
	limit, err := m.GetModelContextLimit()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default mock context window is 128K (realistic for agentic use).
	// Tests needing a smaller window use NewMockLLMProviderWithLimit.
	if limit != 128_000 {
		t.Errorf("expected context limit 128000, got: %d", limit)
	}
}

func TestMockLLMProvider_ListModels(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()
	models, err := m.ListModels(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least one model")
	}
	if models[0].ID != "mock-model" {
		t.Errorf("expected model ID 'mock-model', got: %s", models[0].ID)
	}
}

func TestMockLLMProvider_SupportsVision(t *testing.T) {
	m := NewMockLLMProvider()
	if m.SupportsVision() {
		t.Error("mock provider should not support vision")
	}
}

func TestMockLLMProvider_GetVisionModel(t *testing.T) {
	m := NewMockLLMProvider()
	if m.GetVisionModel() != "" {
		t.Errorf("expected empty vision model, got: %s", m.GetVisionModel())
	}
}

func TestMockLLMProvider_SendVisionRequest(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()
	_, err := m.SendVisionRequest(ctx, nil, nil, "", false)
	if err == nil {
		t.Error("expected error from SendVisionRequest")
	}
}

func TestMockLLMProvider_TPSStats(t *testing.T) {
	m := NewMockLLMProvider()

	if m.GetLastTPS() != 100.0 {
		t.Errorf("expected last TPS 100.0, got: %f", m.GetLastTPS())
	}
	if m.GetAverageTPS() != 100.0 {
		t.Errorf("expected average TPS 100.0, got: %f", m.GetAverageTPS())
	}

	stats := m.GetTPSStats()
	if stats == nil {
		t.Fatal("expected non-nil TPS stats")
	}

	m.ResetTPSStats() // should not panic
}

func TestMockLLMProvider_SetDebug(t *testing.T) {
	m := NewMockLLMProvider()
	m.SetDebug(true) // should not panic
	m.SetDebug(false)
}

func TestMockLLMProvider_StreamCallback(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()

	var streamedContent string
	callback := func(content string, contentType string) {
		streamedContent = content
	}

	resp, err := m.SendChatRequestStream(ctx, []api.Message{
		{Role: "user", Content: "stream test"},
	}, nil, "", false, callback)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if streamedContent == "" {
		t.Error("expected callback to be invoked with content")
	}
}

func TestMockLLMProvider_DefaultResponseConfigured(t *testing.T) {
	m := NewMockLLMProvider()
	m.DefaultResponse = "custom default response"

	ctx := context.Background()
	resp, err := m.SendChatRequest(ctx, []api.Message{
		{Role: "user", Content: "some random message that matches nothing"},
	}, nil, "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := resp.Choices[0].Message.Content
	if content != "custom default response" {
		t.Errorf("expected configured default response, got: %s", content)
	}
}

// --- F1: Concurrent access test ---
func TestMockLLMProvider_Concurrent(t *testing.T) {
	m := NewMockLLMProvider()
	ctx := context.Background()
	messages := []api.Message{{Role: "user", Content: "concurrent test"}}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := m.SendChatRequest(ctx, messages, nil, "", false)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}

	wg.Wait()

	if m.CallCount != goroutines {
		t.Errorf("expected CallCount == %d, got %d", goroutines, m.CallCount)
	}
}

// --- F4: Stream with cancelled context ---
func TestMockLLMProvider_StreamContextCancelled(t *testing.T) {
	m := NewMockLLMProvider()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := m.SendChatRequestStream(ctx, []api.Message{
		{Role: "user", Content: "stream hello"},
	}, nil, "", false, func(content string, contentType string) {})

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// --- F5: Integration test for UseMockLLM early-return branch ---
func TestAgentCreation_WithUseMockLLM(t *testing.T) {
	prev := UseMockLLM
	t.Cleanup(func() { UseMockLLM = prev })
	UseMockLLM = true

	// Use isolated config directory (matches agent_creation_test.go style)
	configDir := t.TempDir() + "/.sprout"
	t.Setenv("SPROUT_CONFIG", configDir)

	ag, err := NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed under UseMockLLM: %v", err)
	}
	defer ag.Shutdown()

	if ag == nil {
		t.Fatal("expected non-nil agent")
	}

	// Verify the agent uses the mock provider (clientType == TestClientType)
	if got := ag.GetProviderType(); got != "test" {
		t.Errorf("GetProviderType = %q, want %q", got, "test")
	}

	// Verify the provider name is "mock"
	if got := ag.GetProvider(); got != "mock" {
		t.Errorf("GetProvider = %q, want %q", got, "mock")
	}
}
