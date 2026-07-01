package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ---------------------------------------------------------------------------
// Mock Client for api.ClientInterface
// ---------------------------------------------------------------------------

// mockAgentClient implements api.ClientInterface for testing spec package functions.
type mockAgentClient struct {
	sendChatFunc func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error)
	model        string
	provider     string
}

func (m *mockAgentClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	if m.sendChatFunc != nil {
		return m.sendChatFunc(messages, tools, reasoning, disableThinking)
	}
	return &api.ChatResponse{
		Choices: []api.Choice{{
			Message: api.Message{Content: `{"objective":"test","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.9,"reasoning":"test"}`},
		}},
	}, nil
}

func (m *mockAgentClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return m.SendChatRequest(context.Background(), messages, tools, reasoning, disableThinking)
}
func (m *mockAgentClient) CheckConnection() error                                  { return nil }
func (m *mockAgentClient) SetDebug(debug bool)                                     {}
func (m *mockAgentClient) SetModel(model string) error                             { return nil }
func (m *mockAgentClient) GetModel() string                                        { return m.model }
func (m *mockAgentClient) GetProvider() string                                     { return m.provider }
func (m *mockAgentClient) GetModelContextLimit() (int, error)                      { return 128000, nil }
func (m *mockAgentClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) { return nil, nil }
func (m *mockAgentClient) SupportsVision() bool                                    { return false }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (m *mockAgentClient) SupportsConversationalVision() bool {
	return false
}
func (m *mockAgentClient) GetVisionModel() string                                  { return "" }
func (m *mockAgentClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, nil
}
func (m *mockAgentClient) GetLastTPS() float64             { return 0 }
func (m *mockAgentClient) GetAverageTPS() float64          { return 0 }
func (m *mockAgentClient) GetTPSStats() map[string]float64 { return nil }
func (m *mockAgentClient) ResetTPSStats()                  {}

// newTestExtractor creates a SpecExtractor with a mock client for testing.
func newTestExtractor(mockFn func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error)) *SpecExtractor {
	return &SpecExtractor{
		agentClient: &mockAgentClient{sendChatFunc: mockFn, model: "test-model"},
		logger:      utils.GetLogger(true),
		cfg:         &configuration.Config{},
	}
}

// newTestValidator creates a ScopeValidator with a mock client for testing.
func newTestValidator(mockFn func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error)) *ScopeValidator {
	return &ScopeValidator{
		agentClient: &mockAgentClient{sendChatFunc: mockFn, model: "test-model"},
		logger:      utils.GetLogger(true),
		cfg:         &configuration.Config{},
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Input Validation
// ---------------------------------------------------------------------------

func TestExtractSpec_InputValidation(t *testing.T) {
	extractor := newTestExtractor(nil)

	t.Run("empty userIntent derives from conversation", func(t *testing.T) {
		extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
			return &api.ChatResponse{
				Choices: []api.Choice{{
					Message: api.Message{Content: `{"objective":"derived","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.8,"reasoning":"test"}`},
				}},
			}, nil
		})
		result, err := extractor.ExtractSpec(context.Background(), []Message{{Role: "user", Content: "Build a CLI tool"}}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Spec.UserPrompt != "Build a CLI tool" {
			t.Errorf("expected derived UserPrompt='Build a CLI tool', got %q", result.Spec.UserPrompt)
		}
	})

	t.Run("empty userIntent with no user-role message uses fallback", func(t *testing.T) {
		extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
			return &api.ChatResponse{
				Choices: []api.Choice{{
					Message: api.Message{Content: `{"objective":"fallback","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.5,"reasoning":"test"}`},
				}},
			}, nil
		})
		result, err := extractor.ExtractSpec(context.Background(),
			[]Message{{Role: "assistant", Content: "Here's the output"}},
			"",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "(no explicit user intent provided — infer objective from conversation context)"
		if result.Spec.UserPrompt != expected {
			t.Errorf("expected fallback placeholder, got %q", result.Spec.UserPrompt)
		}
	})

	t.Run("empty userIntent with empty conversation returns error", func(t *testing.T) {
		_, err := extractor.ExtractSpec(context.Background(), []Message{}, "")
		if err == nil {
			t.Fatal("expected error for empty userIntent + empty conversation")
		}
		if !strings.Contains(err.Error(), "conversation cannot be empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty userIntent with only assistant/tool messages uses fallback", func(t *testing.T) {
		extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
			return &api.ChatResponse{
				Choices: []api.Choice{{
					Message: api.Message{Content: `{"objective":"fallback","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.5,"reasoning":"test"}`},
				}},
			}, nil
		})
		result, err := extractor.ExtractSpec(context.Background(),
			[]Message{
				{Role: "assistant", Content: "Processing..."},
				{Role: "tool", Content: "Result"},
			},
			"",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "(no explicit user intent provided — infer objective from conversation context)"
		if result.Spec.UserPrompt != expected {
			t.Errorf("expected fallback placeholder, got %q", result.Spec.UserPrompt)
		}
	})

	t.Run("empty conversation with valid userIntent returns error", func(t *testing.T) {
		_, err := extractor.ExtractSpec(context.Background(), []Message{}, "build a thing")
		if err == nil {
			t.Fatal("expected error for empty conversation")
		}
		if !strings.Contains(err.Error(), "conversation cannot be empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil conversation with valid userIntent returns error", func(t *testing.T) {
		_, err := extractor.ExtractSpec(context.Background(), nil, "build a thing")
		if err == nil {
			t.Fatal("expected error for nil conversation")
		}
		if !strings.Contains(err.Error(), "conversation cannot be empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// deriveUserIntent
// ---------------------------------------------------------------------------

func TestDeriveUserIntent(t *testing.T) {
	t.Run("first user message wins", func(t *testing.T) {
		intent := deriveUserIntent([]Message{
			{Role: "user", Content: "First request"},
			{Role: "assistant", Content: "OK"},
			{Role: "user", Content: "Second request"},
		})
		if intent != "First request" {
			t.Errorf("expected 'First request', got %q", intent)
		}
	})

	t.Run("case-insensitive role match", func(t *testing.T) {
		intent := deriveUserIntent([]Message{
			{Role: "User", Content: "Mixed case role"},
		})
		if intent != "Mixed case role" {
			t.Errorf("expected 'Mixed case role', got %q", intent)
		}
	})

	t.Run("empty content skipped", func(t *testing.T) {
		intent := deriveUserIntent([]Message{
			{Role: "user", Content: "   "},
			{Role: "assistant", Content: "Response"},
			{Role: "user", Content: "Real request"},
		})
		if intent != "Real request" {
			t.Errorf("expected 'Real request', got %q", intent)
		}
	})

	t.Run("no user message returns placeholder", func(t *testing.T) {
		intent := deriveUserIntent([]Message{
			{Role: "assistant", Content: "Hello"},
			{Role: "tool", Content: "Result"},
		})
		expected := "(no explicit user intent provided — infer objective from conversation context)"
		if intent != expected {
			t.Errorf("expected placeholder, got %q", intent)
		}
	})

	t.Run("empty conversation returns placeholder", func(t *testing.T) {
		intent := deriveUserIntent([]Message{})
		expected := "(no explicit user intent provided — infer objective from conversation context)"
		if intent != expected {
			t.Errorf("expected placeholder, got %q", intent)
		}
	})

	t.Run("nil conversation returns placeholder", func(t *testing.T) {
		intent := deriveUserIntent(nil)
		expected := "(no explicit user intent provided — infer objective from conversation context)"
		if intent != expected {
			t.Errorf("expected placeholder, got %q", intent)
		}
	})

	t.Run("trims whitespace from content", func(t *testing.T) {
		intent := deriveUserIntent([]Message{
			{Role: "user", Content: "  Trimmed request  "},
		})
		if intent != "Trimmed request" {
			t.Errorf("expected 'Trimmed request', got %q", intent)
		}
	})
}

// ---------------------------------------------------------------------------
// ExtractSpec - Successful extraction with valid JSON
// ---------------------------------------------------------------------------

func TestExtractSpec_ValidJSON(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		// Verify the prompt includes conversation text
		if len(messages) == 0 {
			t.Error("expected at least one message")
		}
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{
					"objective": "Build a REST API",
					"in_scope": ["CRUD operations", "Validation"],
					"out_of_scope": ["Authentication", "Logging"],
					"acceptance": ["All endpoints return correct status codes"],
					"context": "Go backend",
					"confidence": 0.92,
					"reasoning": "Clear requirements"
				}`},
			}},
		}, nil
	})

	result, err := extractor.ExtractSpec(context.Background(),
		[]Message{
			{Role: "user", Content: "I need a REST API"},
			{Role: "assistant", Content: "I'll build that for you"},
		},
		"Build a REST API",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Spec == nil {
		t.Fatal("expected non-nil spec")
	}
	if result.Spec.Objective != "Build a REST API" {
		t.Errorf("Objective: expected 'Build a REST API', got %q", result.Spec.Objective)
	}
	if result.Spec.UserPrompt != "Build a REST API" {
		t.Errorf("UserPrompt: expected 'Build a REST API', got %q", result.Spec.UserPrompt)
	}
	if len(result.Spec.InScope) != 2 {
		t.Errorf("InScope: expected 2 items, got %d", len(result.Spec.InScope))
	}
	if len(result.Spec.OutOfScope) != 2 {
		t.Errorf("OutOfScope: expected 2 items, got %d", len(result.Spec.OutOfScope))
	}
	if len(result.Spec.Acceptance) != 1 {
		t.Errorf("Acceptance: expected 1 item, got %d", len(result.Spec.Acceptance))
	}
	if result.Spec.Context != "Go backend" {
		t.Errorf("Context: expected 'Go backend', got %q", result.Spec.Context)
	}
	if result.Confidence != 0.92 {
		t.Errorf("Confidence: expected 0.92, got %f", result.Confidence)
	}
	if result.Reasoning != "Clear requirements" {
		t.Errorf("Reasoning: expected 'Clear requirements', got %q", result.Reasoning)
	}
	// Verify spec ID format
	if !strings.HasPrefix(result.Spec.ID, "spec-") {
		t.Errorf("expected spec ID to start with 'spec-', got %q", result.Spec.ID)
	}
	// Verify conversation is preserved
	if len(result.Spec.Conversation) != 2 {
		t.Errorf("expected 2 conversation messages, got %d", len(result.Spec.Conversation))
	}
	// Verify CreatedAt is set (not zero)
	if result.Spec.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - JSON wrapped in markdown code blocks
// ---------------------------------------------------------------------------

func TestExtractSpec_MarkdownWrappedJSON(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: "Here is the extracted spec:\n```json\n{\"objective\":\"Test obj\",\"in_scope\":[\"item1\"],\"out_of_scope\":[],\"acceptance\":[],\"context\":\"\",\"confidence\":0.8,\"reasoning\":\"test\"}\n```"},
			}},
		}, nil
	})

	result, err := extractor.ExtractSpec(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"test intent",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Spec.Objective != "Test obj" {
		t.Errorf("expected 'Test obj', got %q", result.Spec.Objective)
	}
	if result.Confidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", result.Confidence)
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Invalid JSON response
// ---------------------------------------------------------------------------

func TestExtractSpec_InvalidJSON(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: "I cannot parse this as JSON since there are no braces at all"},
			}},
		}, nil
	})

	_, err := extractor.ExtractSpec(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"test intent",
	)
	if err == nil {
		t.Fatal("expected error for unparseable JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse spec JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Rate limiting error
// ---------------------------------------------------------------------------

func TestExtractSpec_RateLimitError(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return nil, errors.New("HTTP 429: rate limit exceeded")
	})

	_, err := extractor.ExtractSpec(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"test intent",
	)
	if err == nil {
		t.Fatal("expected error for rate limiting")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Generic API error
// ---------------------------------------------------------------------------

func TestExtractSpec_GenericAPIError(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return nil, errors.New("connection timeout")
	})

	_, err := extractor.ExtractSpec(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"test intent",
	)
	if err == nil {
		t.Fatal("expected error for API failure")
	}
	if !strings.Contains(err.Error(), "failed to extract spec") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Prompt includes conversation and intent
// ---------------------------------------------------------------------------

func TestExtractSpec_PromptContent(t *testing.T) {
	var capturedMessages []api.Message
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		capturedMessages = messages
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{"objective":"test","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.5,"reasoning":"test"}`},
			}},
		}, nil
	})

	_, err := extractor.ExtractSpec(context.Background(),
		[]Message{
			{Role: "user", Content: "I want to build a CLI tool"},
			{Role: "assistant", Content: "Sure, I'll help with that"},
		},
		"Build a CLI tool",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}

	content := capturedMessages[0].Content
	// Should include conversation text
	if !strings.Contains(content, "user: I want to build a CLI tool") {
		t.Error("prompt should include user message from conversation")
	}
	if !strings.Contains(content, "assistant: Sure, I'll help with that") {
		t.Error("prompt should include assistant message from conversation")
	}
	// Should include user intent
	if !strings.Contains(content, "Build a CLI tool") {
		t.Error("prompt should include user intent")
	}
	// Should include extraction prompt instructions
	if !strings.Contains(content, "canonical specification") {
		t.Error("prompt should include extraction instructions")
	}
}

// ---------------------------------------------------------------------------
// UpdateSpec
// ---------------------------------------------------------------------------

func TestUpdateSpec(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{
					"objective": "Updated objective",
					"in_scope": ["New feature"],
					"out_of_scope": [],
					"acceptance": ["New criterion"],
					"context": "Updated context",
					"confidence": 0.85,
					"reasoning": "Updated based on new messages"
				}`},
			}},
		}, nil
	})

	existing := &CanonicalSpec{
		ID:           "spec-original-123",
		CreatedAt:    parseTime("2025-01-01T00:00:00Z"),
		UserPrompt:   "Build something",
		Objective:    "Original objective",
		Conversation: []Message{{Role: "user", Content: "original"}},
	}

	newMessages := []Message{
		{Role: "user", Content: "Also add logging"},
		{Role: "assistant", Content: "I'll add logging"},
	}

	updated, err := extractor.UpdateSpec(context.Background(), existing, newMessages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should preserve original ID and creation time
	if updated.ID != "spec-original-123" {
		t.Errorf("ID should be preserved: expected 'spec-original-123', got %q", updated.ID)
	}
	if !updated.CreatedAt.Equal(parseTime("2025-01-01T00:00:00Z")) {
		t.Errorf("CreatedAt should be preserved: got %v", updated.CreatedAt)
	}
	// Should update objective
	if updated.Objective != "Updated objective" {
		t.Errorf("Objective: expected 'Updated objective', got %q", updated.Objective)
	}
	// Should include new messages in conversation
	if len(updated.Conversation) < 2 {
		t.Errorf("expected at least 2 conversation messages, got %d", len(updated.Conversation))
	}
}

func TestUpdateSpec_ExtractionError(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return nil, errors.New("API unavailable")
	})

	existing := &CanonicalSpec{
		ID:           "spec-test",
		Conversation: []Message{{Role: "user", Content: "original"}},
	}

	_, err := extractor.UpdateSpec(context.Background(), existing, []Message{{Role: "user", Content: "update"}})
	if err == nil {
		t.Fatal("expected error from extraction failure")
	}
	if !strings.Contains(err.Error(), "parse spec extraction result") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Input Validation
// ---------------------------------------------------------------------------

func TestValidateScope_InputValidation(t *testing.T) {
	validator := newTestValidator(nil)

	t.Run("empty diff returns error", func(t *testing.T) {
		_, err := validator.ValidateScope(context.Background(), "", &CanonicalSpec{Objective: "test"})
		if err == nil {
			t.Fatal("expected error for empty diff")
		}
		if !strings.Contains(err.Error(), "diff cannot be empty") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil spec returns error", func(t *testing.T) {
		_, err := validator.ValidateScope(context.Background(), "some diff", nil)
		if err == nil {
			t.Fatal("expected error for nil spec")
		}
		if !strings.Contains(err.Error(), "spec cannot be nil") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateScope - Successful validation (in scope)
// ---------------------------------------------------------------------------

func TestValidateScope_InScope(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{
					"in_scope": true,
					"violations": [],
					"summary": "All changes are within scope",
					"suggestions": []
				}`},
			}},
		}, nil
	})

	result, err := validator.ValidateScope(context.Background(), "diff --git a/main.go b/main.go\n+func test() {}", &CanonicalSpec{
		Objective: "Build a test",
		InScope:   []string{"Test functions"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.InScope {
		t.Error("expected InScope=true")
	}
	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if result.Summary != "All changes are within scope" {
		t.Errorf("unexpected summary: %q", result.Summary)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Out of scope with violations
// ---------------------------------------------------------------------------

func TestValidateScope_OutOfScope(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{
					"in_scope": false,
					"violations": [
						{
							"file": "auth.go",
							"line": 0,
							"type": "addition",
							"severity": "high",
							"description": "Added authentication module",
							"why": "Auth was marked out of scope"
						}
					],
					"summary": "1 scope violation found",
					"suggestions": ["Remove auth code"]
				}`},
			}},
		}, nil
	})

	result, err := validator.ValidateScope(context.Background(), "diff --git a/auth.go b/auth.go\n+func auth() {}", &CanonicalSpec{
		Objective:  "Build REST API",
		InScope:    []string{"CRUD"},
		OutOfScope: []string{"Authentication"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.InScope {
		t.Error("expected InScope=false")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
	v := result.Violations[0]
	if v.File != "auth.go" {
		t.Errorf("expected file 'auth.go', got %q", v.File)
	}
	if v.Severity != "high" {
		t.Errorf("expected severity 'high', got %q", v.Severity)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Rate limiting returns fail-closed result
// ---------------------------------------------------------------------------

func TestValidateScope_RateLimit(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return nil, errors.New("HTTP 429: rate limit exceeded")
	})

	result, err := validator.ValidateScope(context.Background(), "some diff", &CanonicalSpec{Objective: "test"})
	if err != nil {
		t.Fatalf("unexpected error (rate limit should be handled gracefully): %v", err)
	}
	// Should fail closed
	if result.InScope {
		t.Error("expected InScope=false when rate limited (fail closed)")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
	if result.Violations[0].Type != "validation_unavailable" {
		t.Errorf("expected type 'validation_unavailable', got %q", result.Violations[0].Type)
	}
	if result.Violations[0].Severity != "high" {
		t.Errorf("expected severity 'high', got %q", result.Violations[0].Severity)
	}
	if !strings.Contains(result.Summary, "rate limiting") {
		t.Errorf("summary should mention rate limiting: %q", result.Summary)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Generic API error
// ---------------------------------------------------------------------------

func TestValidateScope_GenericError(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return nil, errors.New("connection refused")
	})

	_, err := validator.ValidateScope(context.Background(), "some diff", &CanonicalSpec{Objective: "test"})
	if err == nil {
		t.Fatal("expected error for generic API failure")
	}
	if !strings.Contains(err.Error(), "failed to validate scope") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Invalid JSON response
// ---------------------------------------------------------------------------

func TestValidateScope_InvalidJSON(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: "This is just plain text with no JSON at all"},
			}},
		}, nil
	})

	_, err := validator.ValidateScope(context.Background(), "some diff", &CanonicalSpec{Objective: "test"})
	if err == nil {
		t.Fatal("expected error for unparseable JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse scope validation JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Markdown-wrapped JSON response
// ---------------------------------------------------------------------------

func TestValidateScope_MarkdownWrappedJSON(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: "Here's the result:\n```json\n{\"in_scope\":true,\"violations\":[],\"summary\":\"OK\",\"suggestions\":[]}\n```"},
			}},
		}, nil
	})

	result, err := validator.ValidateScope(context.Background(), "some diff", &CanonicalSpec{Objective: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.InScope {
		t.Error("expected InScope=true")
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Prompt includes spec JSON and diff
// ---------------------------------------------------------------------------

func TestValidateScope_PromptContent(t *testing.T) {
	var capturedMessages []api.Message
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		capturedMessages = messages
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{"in_scope":true,"violations":[],"summary":"ok","suggestions":[]}`},
			}},
		}, nil
	})

	spec := &CanonicalSpec{
		Objective: "Build REST API",
		InScope:   []string{"CRUD operations"},
	}

	_, err := validator.ValidateScope(context.Background(), "diff --git a/main.go\n+func main() {}", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}

	content := capturedMessages[0].Content
	// Should include spec JSON
	if !strings.Contains(content, "Build REST API") {
		t.Error("prompt should include spec objective")
	}
	// Should include diff
	if !strings.Contains(content, "diff --git") {
		t.Error("prompt should include the diff")
	}
	// Should include validation prompt instructions
	if !strings.Contains(content, "scope violations") {
		t.Error("prompt should include scope validation instructions")
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Line number post-processing for violations
// ---------------------------------------------------------------------------

func TestValidateScope_LineNumberPostProcessing(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+func added() {}
 func existing() {}`

	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: fmt.Sprintf(`{
					"in_scope": false,
					"violations": [
						{
							"file": "main.go",
							"line": 0,
							"type": "addition",
							"severity": "medium",
							"description": "added",
							"why": "test"
						}
					],
					"summary": "found violation",
					"suggestions": []
				}`)},
			}},
		}, nil
	})

	result, err := validator.ValidateScope(context.Background(), diff, &CanonicalSpec{Objective: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
	// Line should have been populated from diff (was 0)
	if result.Violations[0].Line <= 0 {
		t.Errorf("expected line number to be populated from diff, got %d", result.Violations[0].Line)
	}
}

// ---------------------------------------------------------------------------
// SpecReviewService - ExtractAndValidate
// ---------------------------------------------------------------------------

func TestExtractAndValidate(t *testing.T) {
	mockFn := func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		// Determine whether this is extraction or validation based on content
		content := ""
		if len(messages) > 0 {
			content = messages[0].Content
		}

		if strings.Contains(content, "You are extracting") {
			// Spec extraction request - prompt starts with "You are extracting a canonical specification"
			return &api.ChatResponse{
				Choices: []api.Choice{{
					Message: api.Message{Content: `{
						"objective": "Build feature X",
						"in_scope": ["Feature X implementation"],
						"out_of_scope": ["Feature Y"],
						"acceptance": ["Feature X works correctly"],
						"context": "test context",
						"confidence": 0.9,
						"reasoning": "Clear request"
					}`},
				}},
			}, nil
		}

		// Scope validation request
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{
					"in_scope": true,
					"violations": [],
					"summary": "All changes within scope",
					"suggestions": []
				}`},
			}},
		}, nil
	}

	mockClient := &mockAgentClient{sendChatFunc: mockFn, model: "test-model"}

	service := &SpecReviewService{
		extractor: &SpecExtractor{
			agentClient: mockClient,
			logger:      utils.GetLogger(true),
			cfg:         &configuration.Config{},
		},
		validator: &ScopeValidator{
			agentClient: mockClient,
			logger:      utils.GetLogger(true),
			cfg:         &configuration.Config{},
		},
		logger: utils.GetLogger(true),
		cfg:    &configuration.Config{},
	}

	scopeResult, spec, err := service.ExtractAndValidate(context.Background(),
		[]Message{{Role: "user", Content: "Build feature X"}},
		"diff --git a/main.go\n+func featureX() {}",
		"Build feature X",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec == nil {
		t.Fatal("expected non-nil spec")
	}
	if spec.Objective != "Build feature X" {
		t.Errorf("unexpected objective: %q", spec.Objective)
	}
	if scopeResult == nil {
		t.Fatal("expected non-nil scope result")
	}
	if !scopeResult.InScope {
		t.Error("expected InScope=true")
	}
}

func TestExtractAndValidate_ExtractionError(t *testing.T) {
	mockFn := func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return nil, errors.New("API unavailable")
	}

	mockClient := &mockAgentClient{sendChatFunc: mockFn, model: "test-model"}

	service := &SpecReviewService{
		extractor: &SpecExtractor{
			agentClient: mockClient,
			logger:      utils.GetLogger(true),
			cfg:         &configuration.Config{},
		},
		validator: &ScopeValidator{
			agentClient: mockClient,
			logger:      utils.GetLogger(true),
			cfg:         &configuration.Config{},
		},
		logger: utils.GetLogger(true),
		cfg:    &configuration.Config{},
	}

	_, _, err := service.ExtractAndValidate(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"diff",
		"intent",
	)
	if err == nil {
		t.Fatal("expected error from extraction failure")
	}
	if !strings.Contains(err.Error(), "failed to extract spec") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractAndValidate_ValidationError(t *testing.T) {
	callCount := 0
	mockFn := func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		callCount++
		if callCount == 1 {
			// First call: spec extraction succeeds
			return &api.ChatResponse{
				Choices: []api.Choice{{
					Message: api.Message{Content: `{"objective":"test","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.5,"reasoning":"test"}`},
				}},
			}, nil
		}
		// Second call: scope validation fails (non-rate-limit)
		return nil, errors.New("server error")
	}

	mockClient := &mockAgentClient{sendChatFunc: mockFn, model: "test-model"}

	service := &SpecReviewService{
		extractor: &SpecExtractor{
			agentClient: mockClient,
			logger:      utils.GetLogger(true),
			cfg:         &configuration.Config{},
		},
		validator: &ScopeValidator{
			agentClient: mockClient,
			logger:      utils.GetLogger(true),
			cfg:         &configuration.Config{},
		},
		logger: utils.GetLogger(true),
		cfg:    &configuration.Config{},
	}

	_, spec, err := service.ExtractAndValidate(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"diff",
		"intent",
	)
	if err == nil {
		t.Fatal("expected error from validation failure")
	}
	// Spec should still be returned even when validation fails
	if spec == nil {
		t.Error("expected spec to be returned even on validation error")
	}
}

// ---------------------------------------------------------------------------
// SpecReviewService - GetExtractor / GetValidator
// ---------------------------------------------------------------------------

func TestSpecReviewService_Getters(t *testing.T) {
	mockClient := &mockAgentClient{model: "test-model"}

	extractor := &SpecExtractor{
		agentClient: mockClient,
		logger:      utils.GetLogger(true),
		cfg:         &configuration.Config{},
	}
	validator := &ScopeValidator{
		agentClient: mockClient,
		logger:      utils.GetLogger(true),
		cfg:         &configuration.Config{},
	}

	service := &SpecReviewService{
		extractor: extractor,
		validator: validator,
		logger:    utils.GetLogger(true),
		cfg:       &configuration.Config{},
	}

	t.Run("GetExtractor returns the extractor", func(t *testing.T) {
		got := service.GetExtractor()
		if got != extractor {
			t.Error("GetExtractor should return the same extractor instance")
		}
	})

	t.Run("GetValidator returns the validator", func(t *testing.T) {
		got := service.GetValidator()
		if got != validator {
			t.Error("GetValidator should return the same validator instance")
		}
	})
}

// ---------------------------------------------------------------------------
// ChangeReviewResult JSON roundtrip
// ---------------------------------------------------------------------------

func TestChangeReviewResult_JSON_NilResults(t *testing.T) {
	result := ChangeReviewResult{
		SpecResult:   nil,
		ScopeResult:  nil,
		FilesChanged: 0,
		TotalChanges: 0,
		RevisionID:   "rev-nil",
		Summary:      "",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ChangeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.RevisionID != "rev-nil" {
		t.Errorf("expected RevisionID='rev-nil', got %q", got.RevisionID)
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - LLM returns invalid JSON within braces
// ---------------------------------------------------------------------------

func TestExtractSpec_InvalidJSONInsideBraces(t *testing.T) {
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: "Here's my response: {this is not valid JSON}"},
			}},
		}, nil
	})

	_, err := extractor.ExtractSpec(context.Background(),
		[]Message{{Role: "user", Content: "test"}},
		"test intent",
	)
	if err == nil {
		t.Fatal("expected error for invalid JSON within braces")
	}
	if !strings.Contains(err.Error(), "failed to parse spec JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Invalid JSON inside braces
// ---------------------------------------------------------------------------

func TestValidateScope_InvalidJSONInsideBraces(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: "Result: {not valid json}"},
			}},
		}, nil
	})

	_, err := validator.ValidateScope(context.Background(), "some diff", &CanonicalSpec{Objective: "test"})
	if err == nil {
		t.Fatal("expected error for invalid JSON within braces")
	}
	if !strings.Contains(err.Error(), "failed to parse scope validation JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Conversation with many messages
// ---------------------------------------------------------------------------

func TestExtractSpec_LongConversation(t *testing.T) {
	var capturedMessages []api.Message
	extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		capturedMessages = messages
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{"objective":"test","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":0.7,"reasoning":"test"}`},
			}},
		}, nil
	})

	// Create a conversation with many messages
	conversation := make([]Message, 20)
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		conversation[i] = Message{
			Role:    role,
			Content: fmt.Sprintf("Message %d content", i),
		}
	}

	_, err := extractor.ExtractSpec(context.Background(), conversation, "Long conversation test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 20 messages should be included in the prompt
	content := capturedMessages[0].Content
	for i := 0; i < 20; i++ {
		expected := fmt.Sprintf("Message %d content", i)
		if !strings.Contains(content, expected) {
			t.Errorf("prompt missing message %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Multiple violations with line numbers already set
// ---------------------------------------------------------------------------

func TestValidateScope_MultipleViolations_WithLineNumbers(t *testing.T) {
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{
					"in_scope": false,
					"violations": [
						{"file":"a.go","line":10,"type":"addition","severity":"critical","description":"Added auth","why":"Out of scope"},
						{"file":"b.go","line":25,"type":"modification","severity":"high","description":"Modified logger","why":"Not requested"},
						{"file":"c.go","line":5,"type":"removal","severity":"medium","description":"Removed test","why":"Should keep tests"}
					],
					"summary": "3 violations found",
					"suggestions": ["Remove auth code", "Revert logger changes", "Restore tests"]
				}`},
			}},
		}, nil
	})

	result, err := validator.ValidateScope(context.Background(), "some diff content here", &CanonicalSpec{Objective: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.InScope {
		t.Error("expected InScope=false")
	}
	if len(result.Violations) != 3 {
		t.Fatalf("expected 3 violations, got %d", len(result.Violations))
	}

	// Line numbers should be preserved (not overwritten) since they're non-zero
	if result.Violations[0].Line != 10 {
		t.Errorf("violation 0 line: expected 10, got %d", result.Violations[0].Line)
	}
	if result.Violations[1].Line != 25 {
		t.Errorf("violation 1 line: expected 25, got %d", result.Violations[1].Line)
	}
	if result.Violations[2].Line != 5 {
		t.Errorf("violation 2 line: expected 5, got %d", result.Violations[2].Line)
	}

	// Check suggestions
	if len(result.Suggestions) != 3 {
		t.Errorf("expected 3 suggestions, got %d", len(result.Suggestions))
	}
}

// ---------------------------------------------------------------------------
// ExtractSpec - Confidence boundary values
// ---------------------------------------------------------------------------

func TestExtractSpec_ConfidenceBoundaryValues(t *testing.T) {
	testCases := []struct {
		name       string
		confidence float64
	}{
		{"zero", 0.0},
		{"one", 1.0},
		{"half", 0.5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			confStr := fmt.Sprintf("%g", tc.confidence)
			extractor := newTestExtractor(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
				return &api.ChatResponse{
					Choices: []api.Choice{{
						Message: api.Message{Content: fmt.Sprintf(`{"objective":"test","in_scope":[],"out_of_scope":[],"acceptance":[],"context":"","confidence":%s,"reasoning":"test"}`, confStr)},
					}},
				}, nil
			})

			result, err := extractor.ExtractSpec(context.Background(),
				[]Message{{Role: "user", Content: "test"}},
				"test intent",
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Confidence != tc.confidence {
				t.Errorf("confidence: expected %f, got %f", tc.confidence, result.Confidence)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateScope - Spec marshaling in prompt
// ---------------------------------------------------------------------------

func TestValidateScope_SpecMarshaledInPrompt(t *testing.T) {
	var capturedMessages []api.Message
	validator := newTestValidator(func(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
		capturedMessages = messages
		return &api.ChatResponse{
			Choices: []api.Choice{{
				Message: api.Message{Content: `{"in_scope":true,"violations":[],"summary":"ok","suggestions":[]}`},
			}},
		}, nil
	})

	spec := &CanonicalSpec{
		ID:         "spec-marshaling-test",
		Objective:  "Build REST API with CRUD",
		InScope:    []string{"GET /users", "POST /users"},
		OutOfScope: []string{"Authentication", "Rate limiting"},
		Acceptance: []string{"Returns 200 on valid requests"},
	}

	_, err := validator.ValidateScope(context.Background(), "diff content", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := capturedMessages[0].Content
	// Spec fields should be present as JSON
	if !strings.Contains(content, "Build REST API with CRUD") {
		t.Error("prompt should contain spec objective as JSON")
	}
	if !strings.Contains(content, "GET /users") {
		t.Error("prompt should contain in-scope items as JSON")
	}
	if !strings.Contains(content, "Authentication") {
		t.Error("prompt should contain out-of-scope items as JSON")
	}
}

// ---------------------------------------------------------------------------
// Helper: parse time for tests
// ---------------------------------------------------------------------------

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
