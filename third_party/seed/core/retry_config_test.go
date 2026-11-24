package core

import (
	"context"
	"testing"
	"time"

	"github.com/sprout-foundry/seed/events"
)

// =============================================================================
// Zero-value RetryConfig returns correct defaults
// =============================================================================

func TestRetryConfigDefaults(t *testing.T) {
	rc := RetryConfig{} // all zero values

	if got := rc.MaxAttemptsOrDefault(); got != 3 {
		t.Errorf("MaxAttemptsOrDefault() = %d, want 3", got)
	}
	if got := rc.InitialDelayOrDefault(); got != 100*time.Millisecond {
		t.Errorf("InitialDelayOrDefault() = %v, want 100ms", got)
	}
	if got := rc.MaxDelayOrDefault(); got != 5*time.Second {
		t.Errorf("MaxDelayOrDefault() = %v, want 5s", got)
	}
	if got := rc.MultiplierOrDefault(); got != 2.0 {
		t.Errorf("MultiplierOrDefault() = %v, want 2.0", got)
	}
	if got := rc.JitterOrDefault(); got != 0.0 {
		t.Errorf("JitterOrDefault() = %v, want 0.0", got)
	}
}

// =============================================================================
// Non-zero values are returned as-is
// =============================================================================

func TestRetryConfigCustomValues(t *testing.T) {
	rc := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   3.0,
		Jitter:       0.5,
	}

	if got := rc.MaxAttemptsOrDefault(); got != 5 {
		t.Errorf("MaxAttemptsOrDefault() = %d, want 5", got)
	}
	if got := rc.InitialDelayOrDefault(); got != 200*time.Millisecond {
		t.Errorf("InitialDelayOrDefault() = %v, want 200ms", got)
	}
	if got := rc.MaxDelayOrDefault(); got != 10*time.Second {
		t.Errorf("MaxDelayOrDefault() = %v, want 10s", got)
	}
	if got := rc.MultiplierOrDefault(); got != 3.0 {
		t.Errorf("MultiplierOrDefault() = %v, want 3.0", got)
	}
	if got := rc.JitterOrDefault(); got != 0.5 {
		t.Errorf("JitterOrDefault() = %v, want 0.5", got)
	}
}

// =============================================================================
// Partial customization — unspecified fields use defaults
// =============================================================================

func TestRetryConfigPartialCustomization(t *testing.T) {
	rc := RetryConfig{
		MaxAttempts: 10, // only override MaxAttempts
	}

	if got := rc.MaxAttemptsOrDefault(); got != 10 {
		t.Errorf("MaxAttemptsOrDefault() = %d, want 10", got)
	}
	if got := rc.InitialDelayOrDefault(); got != 100*time.Millisecond {
		t.Errorf("InitialDelayOrDefault() = %v, want 100ms (default)", got)
	}
	if got := rc.MaxDelayOrDefault(); got != 5*time.Second {
		t.Errorf("MaxDelayOrDefault() = %v, want 5s (default)", got)
	}
	if got := rc.MultiplierOrDefault(); got != 2.0 {
		t.Errorf("MultiplierOrDefault() = %v, want 2.0 (default)", got)
	}
	if got := rc.JitterOrDefault(); got != 0.0 {
		t.Errorf("JitterOrDefault() = %v, want 0.0 (default)", got)
	}
}

// =============================================================================
// Inline mocks for agent-level tests (avoids import cycle with test package)
// =============================================================================

type _mockProvider struct {
	chatResp   *ChatResponse
	chatErr    error
	streamErr  error
	info       ProviderInfo
	tokenCount int
}

func (m *_mockProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return m.chatResp, m.chatErr
}
func (m *_mockProvider) ChatStream(_ context.Context, _ *ChatRequest, _ StreamHandler) error {
	return m.streamErr
}
func (m *_mockProvider) Info() ProviderInfo {
	return m.info
}
func (m *_mockProvider) EstimateTokens(_ *ChatRequest) int {
	return m.tokenCount
}

type _mockExecutor struct {
	tools   []Tool
	results []Message
}

func (m *_mockExecutor) GetTools() []Tool {
	return m.tools
}
func (m *_mockExecutor) Execute(_ context.Context, calls []ToolCall) []Message {
	return m.results
}

type _mockUI struct {
	promptResp   string
	promptErr    error
	printBuf     string
	printLineBuf string
}

func (m *_mockUI) Prompt(_ string) (string, error) { return m.promptResp, m.promptErr }
func (m *_mockUI) Confirm(_ string) (bool, error)  { return true, nil }
func (m *_mockUI) Print(s string)                  { m.printBuf += s }
func (m *_mockUI) PrintLine(s string)              { m.printLineBuf += s + "\n" }

// =============================================================================
// RetryConfig flows through Options to Agent — verified via direct field access
// (tests are in package core, so private fields are accessible)
// =============================================================================

func TestRetryConfigInOptions(t *testing.T) {
	customConfig := RetryConfig{
		MaxAttempts:  7,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   3.0,
		Jitter:       0.25,
	}

	provider := &_mockProvider{
		info: ProviderInfo{ContextSize: 10000},
	}
	executor := &_mockExecutor{}
	bus := events.NewEventBus()

	agent, err := NewAgent(Options{
		Provider:       provider,
		Executor:       executor,
		UI:             &_mockUI{},
		EventPublisher: bus,
		RetryConfig:    customConfig,
	})
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}

	// Verify the config was stored by checking the private field directly
	// (tests are in package core so private fields are accessible).
	if got := agent.retryConfig; got != customConfig {
		t.Errorf("agent.retryConfig = %+v, want %+v", got, customConfig)
	}
}

// =============================================================================
// Zero-value RetryConfig in Options creates valid agent (defaults apply)
// =============================================================================

func TestRetryConfigZeroOptions(t *testing.T) {
	provider := &_mockProvider{
		info: ProviderInfo{ContextSize: 10000},
	}
	executor := &_mockExecutor{}
	bus := events.NewEventBus()

	agent, err := NewAgent(Options{
		Provider:       provider,
		Executor:       executor,
		UI:             &_mockUI{},
		EventPublisher: bus,
		RetryConfig:    RetryConfig{}, // explicitly zero
	})
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent with zero-value RetryConfig")
	}

	// Verify the zero-value config was stored (defaults will be applied
	// at call sites via OrDefault, but the stored value is what was passed).
	if got := agent.retryConfig; got != (RetryConfig{}) {
		t.Errorf("agent.retryConfig = %+v, want empty struct", got)
	}
}

// =============================================================================
// Negative and boundary values — ensure OrDefault handles them
// =============================================================================

func TestRetryConfig_NegativeMaxAttempts(t *testing.T) {
	rc := RetryConfig{MaxAttempts: -1}
	// Negative values are treated as zero → should return the default
	if got := rc.MaxAttemptsOrDefault(); got != 3 {
		t.Errorf("MaxAttemptsOrDefault(-1) = %d, want 3 (default for zero/negative)", got)
	}
}

func TestRetryConfig_NegativeInitialDelay(t *testing.T) {
	rc := RetryConfig{InitialDelay: -100 * time.Millisecond}
	// Negative durations should fall through to the default
	if got := rc.InitialDelayOrDefault(); got != 100*time.Millisecond {
		t.Errorf("InitialDelayOrDefault(negative) = %v, want 100ms (default)", got)
	}
}

func TestRetryConfig_ZeroMaxDelay(t *testing.T) {
	rc := RetryConfig{} // zero MaxDelay
	// Zero is the default, should return the default
	if got := rc.MaxDelayOrDefault(); got != 5*time.Second {
		t.Errorf("MaxDelayOrDefault(0) = %v, want 5s (default)", got)
	}
}

func TestRetryConfig_ZeroMultiplier(t *testing.T) {
	rc := RetryConfig{} // zero Multiplier
	// Zero is the default, should return the default
	if got := rc.MultiplierOrDefault(); got != 2.0 {
		t.Errorf("MultiplierOrDefault(0) = %v, want 2.0 (default)", got)
	}
}

func TestRetryConfig_ZeroJitter(t *testing.T) {
	rc := RetryConfig{} // zero Jitter
	if got := rc.JitterOrDefault(); got != 0.0 {
		t.Errorf("JitterOrDefault(0) = %v, want 0.0", got)
	}
}
