package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// =============================================================================
// 1. containsFrontendKeywords tests (pure function, fully parallel-safe)
// =============================================================================

func TestContainsFrontendKeywords_HighPriority(t *testing.T) {
	t.Parallel()
	highKeywords := []string{
		"react", "vue", "angular", "nextjs", "next.js", "svelte",
		"app", "website", "webpage", "web app", "web application",
		"frontend", "front-end", "ui", "user interface", "interface",
		"layout", "design", "responsive", "mobile-first",
		"css", "html", "styling", "styles", "stylesheet",
		"component", "components", "widget", "widgets",
		"dashboard", "landing page", "homepage", "navigation",
		"mockup", "wireframe", "prototype", "screenshot",
	}
	for _, kw := range highKeywords {
		t.Run(kw, func(t *testing.T) {
			t.Parallel()
			if !containsFrontendKeywords(kw) {
				t.Errorf("containsFrontendKeywords(%q) = false, want true", kw)
			}
		})
	}
}

func TestContainsFrontendKeywords_HighPriorityInContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"build a react app", "Build a React app for my startup"},
		{"frontend design", "I need help with the frontend design"},
		{"CSS styling issue", "CSS styling issue on mobile"},
		{"vue component", "Create a Vue component with props"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !containsFrontendKeywords(tt.input) {
				t.Errorf("containsFrontendKeywords(%q) = false, want true", tt.input)
			}
		})
	}
}

func TestContainsFrontendKeywords_SecondaryNeedsTwo(t *testing.T) {
	t.Parallel()
	// Only 1 secondary keyword -> false
	if containsFrontendKeywords("I need help with button alignment") {
		t.Errorf("containsFrontendKeywords with 1 secondary keyword should be false")
	}
	if containsFrontendKeywords("Change the font size") {
		t.Errorf("containsFrontendKeywords with 1 secondary keyword should be false")
	}
	// 2+ secondary keywords -> true
	if !containsFrontendKeywords("I want a blue button and footer") {
		t.Errorf("containsFrontendKeywords with 2 secondary keywords should be true")
	}
	if !containsFrontendKeywords("Need grid layout with padding and margin") {
		t.Errorf("containsFrontendKeywords with 3 secondary keywords should be true")
	}
}

func TestContainsFrontendKeywords_NonFrontend(t *testing.T) {
	t.Parallel()
	tests := []string{
		"write a python script",
		"deploy to production",
		"fix the database query",
		"set up CI/CD pipeline",
		"implement authentication",
	}
	for _, input := range tests {
		if containsFrontendKeywords(input) {
			t.Errorf("containsFrontendKeywords(%q) = true, want false", input)
		}
	}
}

func TestContainsFrontendKeywords_CaseInsensitive(t *testing.T) {
	t.Parallel()
	if !containsFrontendKeywords("REACT") {
		t.Error("should be case-insensitive for REACT")
	}
	if !containsFrontendKeywords("I love HTML") {
		t.Error("should be case-insensitive for HTML")
	}
}

// =============================================================================
// 2. normalizeReasoningEffort tests (pure function)
// =============================================================================

func TestNormalizeReasoningEffort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"low", "low", "low"},
		{"LOW", "LOW", "low"},
		{"Low", "Low", "low"},
		{" medium ", " medium ", "medium"},
		{"MEDIUM", "MEDIUM", "medium"},
		{"high", "high", "high"},
		{"HIGH", "HIGH", "high"},
		{" High ", " High ", "high"},
		{"empty", "", ""},
		{"invalid", "invalid", ""},
		{"none", "none", ""},
		{"extra", "extra", ""},
		{"auto", "auto", ""},
		{"min", "min", ""},
		{"max", "max", ""},
		{"normal", "normal", ""},
		{"whitespace only", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeReasoningEffort(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeReasoningEffort(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// 3. isGptOSSModelName tests (pure function)
// =============================================================================

func TestIsGptOSSModelName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"gpt-oss:20b", "gpt-oss:20b", true},
		{"GPT-OSS:20B", "GPT-OSS:20B", true},
		{"GPT-OSS", "GPT-OSS", true},
		{"openai/gpt-oss-120b", "openai/gpt-oss-120b", true},
		{"gpt-oss-120b", "gpt-oss-120b", true},
		{"gpt-4o", "gpt-4o", false},
		{"gpt-4o-mini", "gpt-4o-mini", false},
		{"gpt-4-turbo", "gpt-4-turbo", false},
		{"gpt-3.5-turbo", "gpt-3.5-turbo", false},
		{"empty", "", false},
		{"claude-sonnet", "claude-sonnet-4-20250514", false},
		{"gpt-oss-mixtral", "gpt-oss-mixtral", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isGptOSSModelName(tt.model)
			if got != tt.expected {
				t.Errorf("isGptOSSModelName(%q) = %v, want %v", tt.model, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// 4. shouldDisableThinking tests (uses newTestAgent -> no Parallel)
// =============================================================================

func TestShouldDisableThinking_DisabledWhenConfigNil(t *testing.T) {
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "openai",
			model:      "deepseek-chat",
		},
		state: NewAgentStateManager(false),
	}
	if agent.shouldDisableThinking() {
		t.Error("shouldDisableThinking with nil config should be false")
	}
}

func TestShouldDisableThinking_DisabledWhenNotEnabled(t *testing.T) {
	agent := newTestAgent(t)
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "deepseek-chat",
	}
	// Ensure DisableThinking is false (default)
	agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.DisableThinking = false
		return nil
	})
	if agent.shouldDisableThinking() {
		t.Error("shouldDisableThinking with DisableThinking=false should be false")
	}
}

func TestShouldDisableThinking_PureReasoningModelsCannotDisable(t *testing.T) {
	tests := []string{
		"deepseek-r1",
		"deepseek-r1-distill",
		"deepseek-reasoner",
		"qwq",
		"qwq-32b",
		"qwenvl",
		"qwenvl-2.5",
		"kimi-k2-thinking",
		"kimi-thinking",
	}
	for _, model := range tests {
		t.Run(model, func(t *testing.T) {
			agent := newTestAgent(t)
			agent.client = &reasoningProviderClient{
				TestClient: &factory.TestClient{},
				provider:   "openai",
				model:      model,
			}
			agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
				c.DisableThinking = true
				return nil
			})
			if agent.shouldDisableThinking() {
				t.Errorf("shouldDisableThinking(%q) = true, want false (pure reasoning)", model)
			}
		})
	}
}

func TestShouldDisableThinking_GptOSSCannotDisable(t *testing.T) {
	agent := newTestAgent(t)
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "gpt-oss:20b",
	}
	agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.DisableThinking = true
		return nil
	})
	if agent.shouldDisableThinking() {
		t.Error("gpt-oss should not support disable thinking")
	}
}

func TestShouldDisableThinking_OpenAISeriesCannotDisable(t *testing.T) {
	tests := []string{"o1", "o1-preview", "o2", "o2-preview", "o3", "o3-mini", "o4", "o4-mini"}
	for _, model := range tests {
		t.Run(model, func(t *testing.T) {
			agent := newTestAgent(t)
			agent.client = &reasoningProviderClient{
				TestClient: &factory.TestClient{},
				provider:   "openai",
				model:      model,
			}
			agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
				c.DisableThinking = true
				return nil
			})
			if agent.shouldDisableThinking() {
				t.Errorf("shouldDisableThinking(%q) = true, want false (o-series uses reasoning_effort)", model)
			}
		})
	}
}

func TestShouldDisableThinking_SupportsDisableForCertainModels(t *testing.T) {
	tests := []struct {
		name  string
		model string
	}{
		{"deepseek-chat", "deepseek-chat"},
		{"deepseek-coder", "deepseek-coder"},
		{"deepseek-v3", "deepseek-v3"},
		{"deepseek-v4", "deepseek-v4"},
		{"claude-4", "claude-4-sonnet"},
		{"claude-sonnet-4.6", "claude-sonnet-4.6"},
		{"claude-haiku-4.6", "claude-haiku-4.6"},
		{"claude-opus-4.6", "claude-opus-4.6"},
		{"qwen3", "qwen3"},
		{"qwen3-32b", "qwen3-32b"},
		{"qwen2.5", "qwen2.5-72b"},
		{"qwen2", "qwen2"},
		{"glm", "glm-4"},
		{"glm-4-plus", "glm-4-plus"},
		{"minimax", "minimax-m1"},
		{"gemini-2", "gemini-2.0-flash"},
		{"gemini-2.5", "gemini-2.5-pro"},
		{"gemini-3", "gemini-3-flash"},
		{"gemma-3", "gemma-3-27b"},
		{"kimi-k2", "kimi-k2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newTestAgent(t)
			agent.client = &reasoningProviderClient{
				TestClient: &factory.TestClient{},
				provider:   "openai",
				model:      tt.model,
			}
			agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
				c.DisableThinking = true
				return nil
			})
			if !agent.shouldDisableThinking() {
				t.Errorf("shouldDisableThinking(%q) = false, want true", tt.model)
			}
		})
	}
}

func TestShouldDisableThinking_UnknownModel(t *testing.T) {
	agent := newTestAgent(t)
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "some-unknown-model",
	}
	agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.DisableThinking = true
		return nil
	})
	if agent.shouldDisableThinking() {
		t.Error("shouldDisableThinking for unknown model should be false (default)")
	}
}

// =============================================================================
// 5. determineReasoningEffort additional tests (uses newTestAgent)
// =============================================================================

func TestDetermineReasoningEffort_EmptyMessages(t *testing.T) {
	agent := newTestAgent(t)
	cfg := agent.GetConfig()
	cfg.ReasoningEffort = ""

	got := agent.determineReasoningEffort(nil)
	if got != "medium" {
		t.Errorf("determineReasoningEffort(nil) = %q, want %q", got, "medium")
	}
	got = agent.determineReasoningEffort([]api.Message{})
	if got != "medium" {
		t.Errorf("determineReasoningEffort([]) = %q, want %q", got, "medium")
	}
}

func TestDetermineReasoningEffort_NoUserMessage(t *testing.T) {
	agent := newTestAgent(t)
	cfg := agent.GetConfig()
	cfg.ReasoningEffort = ""

	messages := []api.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "assistant", Content: "Hello!"},
	}
	got := agent.determineReasoningEffort(messages)
	if got != "medium" {
		t.Errorf("determineReasoningEffort(no user msg) = %q, want %q", got, "medium")
	}
}

func TestDetermineReasoningEffort_LowEffortKeywords(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"two low keywords short", "what is the define of this", "low"},
		{"two low keywords", "define this and list them all", "low"},
		{"rename and move", "rename this file and move it to another directory", "low"},
		{"format and indent", "format the code and fix the indentation", "low"},
		{"yes or no", "yes or no, is this correct", "low"},
		{"count how many", "count how many files are there", "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newTestAgent(t)
			cfg := agent.GetConfig()
			cfg.ReasoningEffort = ""
			agent.client = &reasoningProviderClient{
				TestClient: &factory.TestClient{},
				provider:   "openai",
				model:      "gpt-4o",
			}
			messages := []api.Message{{Role: "user", Content: tt.msg}}
			got := agent.determineReasoningEffort(messages)
			if got != tt.want {
				t.Errorf("determineReasoningEffort(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestDetermineReasoningEffort_HighEffortKeywordsLongQuery(t *testing.T) {
	agent := newTestAgent(t)
	cfg := agent.GetConfig()
	cfg.ReasoningEffort = ""
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "gpt-4o",
	}

	messages := []api.Message{{Role: "user", Content: "analyze this complex piece of code and determine what is wrong with the implementation in the main function"}}
	got := agent.determineReasoningEffort(messages)
	if got != "high" {
		t.Errorf("determineReasoningEffort(long query with 1 high keyword) = %q, want %q", got, "high")
	}
}

func TestDetermineReasoningEffort_QueryLengthThresholds(t *testing.T) {
	agent := newTestAgent(t)
	cfg := agent.GetConfig()
	cfg.ReasoningEffort = ""
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "gpt-4o",
	}

	longQuery := strings.Repeat("x", 201) // just over the 200-char "high effort" threshold
	messages := []api.Message{{Role: "user", Content: longQuery}}
	got := agent.determineReasoningEffort(messages)
	if got != "high" {
		t.Errorf("determineReasoningEffort(201 char query) = %q, want %q", got, "high")
	}

	shortQuery := "do something"
	messages = []api.Message{{Role: "user", Content: shortQuery}}
	got = agent.determineReasoningEffort(messages)
	if got != "low" {
		t.Errorf("determineReasoningEffort(%q) = %q, want %q", shortQuery, got, "low")
	}
}

func TestDetermineReasoningEffort_ConfigOverride(t *testing.T) {
	agent := newTestAgent(t)
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "openai",
		model:      "gpt-4o",
	}

	agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ReasoningEffort = "high"
		return nil
	})
	messages := []api.Message{{Role: "user", Content: "what is this"}}
	got := agent.determineReasoningEffort(messages)
	if got != "high" {
		t.Errorf("determineReasoningEffort with config override = %q, want %q", got, "high")
	}

	agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ReasoningEffort = "low"
		return nil
	})
	messages = []api.Message{{Role: "user", Content: "analyze and debug this complex workflow"}}
	got = agent.determineReasoningEffort(messages)
	if got != "low" {
		t.Errorf("determineReasoningEffort with low config override = %q, want %q", got, "low")
	}
}

func TestDetermineReasoningEffort_ProviderSpecificOverride(t *testing.T) {
	agent := newTestAgent(t)
	agent.client = &reasoningProviderClient{
		TestClient: &factory.TestClient{},
		provider:   "my-custom-provider",
		model:      "gpt-4o",
	}
	agent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ReasoningEffort = ""
		c.CustomProviders = map[string]configuration.CustomProviderConfig{
			"my-custom-provider": {ReasoningEffort: "low"},
		}
		return nil
	})

	messages := []api.Message{{Role: "user", Content: "analyze and debug this complex workflow"}}
	got := agent.determineReasoningEffort(messages)
	if got != "low" {
		t.Errorf("determineReasoningEffort with provider override = %q, want %q", got, "low")
	}
}

// =============================================================================
// 6. handleIncompleteResponse tests
// =============================================================================

func TestHandleIncompleteResponse(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent:             &Agent{state: NewAgentStateManager(false)},
		transientMessages: []api.Message{},
	}
	ch.handleIncompleteResponse()

	if len(ch.transientMessages) != 1 {
		t.Fatalf("expected 1 transient message, got %d", len(ch.transientMessages))
	}
	msg := ch.transientMessages[0]
	if msg.Role != "user" {
		t.Errorf("expected role 'user', got %q", msg.Role)
	}
	if !strings.Contains(msg.Content, "Please continue") {
		t.Errorf("expected content to contain 'Please continue', got %q", msg.Content)
	}
}

func TestHandleIncompleteResponse_DuplicateSuppressed(t *testing.T) {
	t.Parallel()
	ch := &ConversationHandler{
		agent:             &Agent{state: NewAgentStateManager(false)},
		transientMessages: []api.Message{},
	}
	ch.handleIncompleteResponse()
	ch.handleIncompleteResponse()
	ch.handleIncompleteResponse()

	if len(ch.transientMessages) != 1 {
		t.Errorf("expected 1 transient message (duplicates suppressed), got %d", len(ch.transientMessages))
	}
}

// =============================================================================
// 7. isStrictToolCallSyntaxModel tests (pure struct, parallel-safe)
// =============================================================================

func TestIsStrictToolCallSyntaxModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		model    string
		expected bool
	}{
		{"minimax provider", "minimax", "minimax-m1", true},
		{"deepseek provider", "deepseek", "deepseek-chat", true},
		{"minimax in model", "openai", "minimax/minimax-m1", true},
		{"deepseek in model", "openai", "deepseek/deepseek-chat", true},
		{"openai gpt-4o", "openai", "gpt-4o", false},
		{"openai gpt-4", "openai", "gpt-4", false},
		{"spaces in provider minimax", "  minimax  ", "m1", true},
		{"spaces in provider deepseek", "  deepseek  ", "v3", true},
		{"empty provider with minimax model", "", "minimax", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &Agent{
				client: &reasoningProviderClient{
					TestClient: &factory.TestClient{},
					provider:   tt.provider,
					model:      tt.model,
				},
			}
			got := agent.isStrictToolCallSyntaxModel()
			if got != tt.expected {
				t.Errorf("isStrictToolCallSyntaxModel(provider=%q, model=%q) = %v, want %v", tt.provider, tt.model, got, tt.expected)
			}
		})
	}
}

func TestIsStrictToolCallSyntaxModel_NilAgent(t *testing.T) {
	t.Parallel()
	var agent *Agent
	if agent.isStrictToolCallSyntaxModel() {
		t.Error("nil agent should return false")
	}
}

// =============================================================================
// 8. buildSwitchContextRefreshMessage tests
// =============================================================================

func TestBuildSwitchContextRefreshMessage_NilAgent(t *testing.T) {
	t.Parallel()
	var agent *Agent
	report := strictSyntaxNormalizationReport{
		beforeMessages:                  10,
		afterMessages:                   5,
		beforeTokens:                    5000,
		afterTokens:                     2000,
		removedToolMessages:             3,
		strippedAssistantToolCallBlocks: 2,
		droppedEmptyAssistantMessages:   1,
	}
	got := agent.buildSwitchContextRefreshMessage(report, "openai", "gpt-4o")
	if got != "" {
		t.Errorf("expected empty string for nil agent, got %q", got)
	}
}

func TestBuildSwitchContextRefreshMessage_Basic(t *testing.T) {
	t.Parallel()
	agent := &Agent{
		client: &reasoningProviderClient{
			TestClient: &factory.TestClient{},
			provider:   "minimax",
			model:      "minimax-m1",
		},
		state: NewAgentStateManager(false),
	}
	report := strictSyntaxNormalizationReport{
		beforeMessages:                  10,
		afterMessages:                   5,
		beforeTokens:                    5000,
		afterTokens:                     2000,
		removedToolMessages:             3,
		strippedAssistantToolCallBlocks: 2,
		droppedEmptyAssistantMessages:   1,
	}
	got := agent.buildSwitchContextRefreshMessage(report, "openai", "gpt-4o")

	if !strings.Contains(got, "Provider/model switch compatibility refresh") {
		t.Errorf("expected header in message, got %q", got)
	}
	if !strings.Contains(got, "openai") {
		t.Errorf("expected from provider in message, got %q", got)
	}
	if !strings.Contains(got, "minimax") {
		t.Errorf("expected to provider in message, got %q", got)
	}
	if !strings.Contains(got, "gpt-4o") {
		t.Errorf("expected from model in message, got %q", got)
	}
	if !strings.Contains(got, "5000") {
		t.Errorf("expected before tokens in message, got %q", got)
	}
	if !strings.Contains(got, "2000") {
		t.Errorf("expected after tokens in message, got %q", got)
	}
}

// =============================================================================
// 9. setPendingSystemSupplement / consumePendingSystemSupplement tests
// =============================================================================

func TestSetAndConsumePendingSystemSupplement(t *testing.T) {
	t.Parallel()
	agent := &Agent{state: NewAgentStateManager(false)}
	agent.setPendingSystemSupplement("supplement content")
	got := agent.consumePendingSystemSupplement()
	if got != "supplement content" {
		t.Errorf("expected 'supplement content', got %q", got)
	}
	got = agent.consumePendingSystemSupplement()
	if got != "" {
		t.Errorf("expected empty on second consume, got %q", got)
	}
}

func TestPendingSystemSupplement_NilAgent(t *testing.T) {
	t.Parallel()
	var agent *Agent
	agent.setPendingSystemSupplement("content") // should not panic
	got := agent.consumePendingSystemSupplement() // should not panic
	if got != "" {
		t.Errorf("expected empty for nil agent, got %q", got)
	}
}

func TestPendingSystemSupplement_NilState(t *testing.T) {
	t.Parallel()
	agent := &Agent{state: nil}
	agent.setPendingSystemSupplement("content") // should not panic
	got := agent.consumePendingSystemSupplement() // should not panic
	if got != "" {
		t.Errorf("expected empty for nil state, got %q", got)
	}
}

// =============================================================================
// 10. buildUserInputTruncationNotice tests (pure function)
// =============================================================================

func TestBuildUserInputTruncationNotice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		omitted     int
		archivePath string
		archiveErr  error
		inputType   string
		contains    []string
	}{
		{
			name:        "interactive with path",
			omitted:     5000,
			archivePath: "/tmp/sprout/inputs/file.txt",
			inputType:   "interactive input",
			contains:    []string{"SPROUT_INTERACTIVE_INPUT_MAX_CHARS", "Full input saved to", "5000"},
		},
		{
			name:        "automation with path",
			omitted:     5000,
			archivePath: "/tmp/sprout/inputs/file.txt",
			inputType:   "automation input",
			contains:    []string{"SPROUT_AUTOMATION_INPUT_MAX_CHARS", "Full input saved to"},
		},
		{
			name:        "unknown type with path",
			omitted:     5000,
			archivePath: "/tmp/sprout/inputs/file.txt",
			inputType:   "unknown",
			contains:    []string{"SPROUT_USER_INPUT_MAX_CHARS", "Full input saved to"},
		},
		{
			name:        "no path with error",
			omitted:     1000,
			archivePath: "",
			archiveErr:  fmt.Errorf("permission denied"),
			inputType:   "interactive input",
			contains:    []string{"Failed to save full input", "SPROUT_INTERACTIVE_INPUT_MAX_CHARS"},
		},
		{
			name:        "no path no error",
			omitted:     1000,
			archivePath: "",
			archiveErr:  nil,
			inputType:   "automation input",
			contains:    []string{"Full input path unavailable", "SPROUT_AUTOMATION_INPUT_MAX_CHARS"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildUserInputTruncationNotice(tt.omitted, tt.archivePath, tt.archiveErr, tt.inputType)
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("expected %q in output, got %q", c, got)
				}
			}
		})
	}
}

// =============================================================================
// 11. getInputLimit tests (uses env vars, NOT parallel)
// =============================================================================

func TestGetInputLimit_AutomationModeNoAgent(t *testing.T) {
	t.Setenv("SPROUT_USER_INPUT_MAX_CHARS", "")
	t.Setenv("SPROUT_INTERACTIVE_INPUT_MAX_CHARS", "")
	t.Setenv("SPROUT_AUTOMATION_INPUT_MAX_CHARS", "")
	t.Setenv("LEDIT_USER_INPUT_MAX_CHARS", "")
	t.Setenv("LEDIT_INTERACTIVE_INPUT_MAX_CHARS", "")
	t.Setenv("LEDIT_AUTOMATION_INPUT_MAX_CHARS", "")

	ch := &ConversationHandler{}
	maxChars, inputType := ch.getInputLimit()
	if maxChars != 0 {
		t.Errorf("expected 0 (unlimited) for automation mode, got %d", maxChars)
	}
	if inputType != "automation input" {
		t.Errorf("expected 'automation input', got %q", inputType)
	}
}

func TestGetInputLimit_InteractiveMode(t *testing.T) {
	t.Setenv("SPROUT_INTERACTIVE", "1")
	t.Setenv("SPROUT_FROM_AGENT", "")
	t.Setenv("SPROUT_USER_INPUT_MAX_CHARS", "")
	t.Setenv("LEDIT_INTERACTIVE", "1")
	t.Setenv("LEDIT_FROM_AGENT", "")
	t.Setenv("LEDIT_USER_INPUT_MAX_CHARS", "")

	agent := &Agent{state: NewAgentStateManager(false)}
	ch := &ConversationHandler{agent: agent}
	maxChars, inputType := ch.getInputLimit()
	if maxChars != 100000 {
		t.Errorf("expected 100000 for interactive mode, got %d", maxChars)
	}
	if inputType != "interactive input" {
		t.Errorf("expected 'interactive input', got %q", inputType)
	}
}

func TestGetInputLimit_LegacyEnvOverride(t *testing.T) {
	t.Setenv("SPROUT_USER_INPUT_MAX_CHARS", "5000")
	t.Setenv("SPROUT_INTERACTIVE", "1")
	t.Setenv("LEDIT_USER_INPUT_MAX_CHARS", "5000")
	t.Setenv("LEDIT_INTERACTIVE", "1")

	agent := &Agent{state: NewAgentStateManager(false)}
	ch := &ConversationHandler{agent: agent}
	maxChars, _ := ch.getInputLimit()
	if maxChars != 5000 {
		t.Errorf("expected 5000 (legacy override), got %d", maxChars)
	}
}

// =============================================================================
// 12. InjectInputContext tests (parallel-safe, no env vars)
// =============================================================================

func TestInjectInputContext_Success(t *testing.T) {
	t.Parallel()
	a := &Agent{
		state:              NewAgentStateManager(false),
		inputInjectionChan: make(chan string, 2),
	}
	err := a.InjectInputContext("test input")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestInjectInputContext_ChannelFull(t *testing.T) {
	t.Parallel()
	ch := make(chan string, 1)
	ch <- "existing" // fill the channel

	a := &Agent{
		state:              NewAgentStateManager(false),
		inputInjectionChan: ch,
	}

	err := a.InjectInputContext("test input")
	if err == nil {
		t.Fatal("expected error for full channel, got nil")
	}
	if !strings.Contains(err.Error(), "failed to inject input") {
		t.Errorf("expected error about channel full, got %v", err)
	}
}

func TestGetInputInjectionContext(t *testing.T) {
	t.Parallel()
	expectedCh := make(chan string, 1)
	a := &Agent{
		state:              NewAgentStateManager(false),
		inputInjectionChan: expectedCh,
	}
	got := a.GetInputInjectionContext()
	if got == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestClearInputInjectionContext(t *testing.T) {
	t.Parallel()
	ch := make(chan string, 3)
	ch <- "item1"
	ch <- "item2"
	ch <- "item3"

	a := &Agent{
		state:              NewAgentStateManager(false),
		inputInjectionChan: ch,
	}
	a.ClearInputInjectionContext()

	select {
	case v := <-ch:
		t.Fatalf("expected empty channel, got item %q", v)
	default:
		// Channel is empty, as expected
	}
}

func TestIsInterrupted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	a := &Agent{
		state:           NewAgentStateManager(false),
		interruptCtx:    ctx,
		interruptCancel: cancel,
	}

	if a.IsInterrupted() {
		t.Error("expected false before interrupt")
	}

	cancel()
	if !a.IsInterrupted() {
		t.Error("expected true after cancel (TriggerInterrupt uses cancel)")
	}
}

// =============================================================================
// 13. ResponseValidator additional edge cases (parallel-safe)
// =============================================================================

func TestIsIncomplete_WithReasoningContent(t *testing.T) {
	t.Parallel()
	rv := newTestResponseValidator()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"ellipsis always triggers", "thinking about it...", true},
		{"long complete text", "This is a very complete and detailed answer that provides comprehensive coverage of all the topics we discussed in this conversation and beyond.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rv.IsIncomplete(tt.content)
			if got != tt.want {
				t.Errorf("IsIncomplete(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestLooksLikeTentativePostToolResponse_WordBoundary40(t *testing.T) {
	t.Parallel()
	rv := newTestResponseValidator()

	// Exactly 40 words with "let me " prefix -> true
	// "let me" = 2 words + 38 "word"s = 40 total
	content40 := "let me " + strings.Repeat("word ", 38)
	if !rv.LooksLikeTentativePostToolResponse(content40) {
		t.Errorf("expected true for 40-word prefix match")
	}
	// 41 words with "let me " prefix -> false
	content41 := "let me " + strings.Repeat("word ", 39)
	if rv.LooksLikeTentativePostToolResponse(content41) {
		t.Errorf("expected false for 41-word content (exceeds 40 word limit for prefix)")
	}
}

func TestLooksLikeTentativePostToolResponse_WordBoundary50(t *testing.T) {
	t.Parallel()
	rv := newTestResponseValidator()

	// Exactly 50 words with "good," prefix + "now i need to" -> true
	// "good, now i need to" = 5 words + 45 "word"s = 50 total
	content50 := "good, now i need to " + strings.Repeat("word ", 45)
	if !rv.LooksLikeTentativePostToolResponse(content50) {
		t.Errorf("expected true for 50-word acknowledgement match")
	}
	// 51 words with "good," prefix + "now i need to" -> false
	content51 := "good, now i need to " + strings.Repeat("word ", 46)
	if rv.LooksLikeTentativePostToolResponse(content51) {
		t.Errorf("expected false for 51-word content (exceeds 50 word limit for acknowledgement)")
	}
}

// =============================================================================
// 14. PublishEvent nil-safety and SetEventBus/GetEventBus
// =============================================================================

func TestPublishQueryProgress_NilEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	// Should not panic with nil eventBus
	a.PublishQueryProgress("test", 1, 100)
}

func TestPublishToolExecution_NilEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	a.PublishToolExecution("read_file", "read", map[string]interface{}{"path": "test.go"})
}

func TestPublishToolStart_NilEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	a.PublishToolStart("read_file", "call-1", "{\"path\":\"test.go\"}", "Read File", "coder", false, "", 0)
}

func TestPublishToolEnd_NilEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	a.PublishToolEnd("call-1", "read_file", "success", "content", "", time.Duration(100))
}

func TestPublishTodoUpdate_NilEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	a.PublishTodoUpdate([]map[string]interface{}{{"content": "task"}})
}

func TestPublishAgentMessage_NilEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	a.PublishAgentMessage("test", "hello", map[string]interface{}{"key": "value"})
}

func TestGetEventBus_NilBeforeSet(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	if a.GetEventBus() != nil {
		t.Error("expected nil eventBus before SetEventBus")
	}
}

func TestSetEventBusAndGetEventBus(t *testing.T) {
	t.Parallel()
	a := &Agent{output: NewAgentOutputManager()}
	bus := events.NewEventBus()
	a.SetEventBus(bus)

	got := a.GetEventBus()
	if got != bus {
		t.Error("expected same eventBus instance after SetEventBus")
	}
}

// =============================================================================
// 15. EstimateMessageTokens with reasoning content
// =============================================================================

func TestEstimateMessageTokens_ReasoningContent(t *testing.T) {
	t.Parallel()

	t.Run("reasoning adds tokens", func(t *testing.T) {
		t.Parallel()
		messages1 := []api.Message{
			{Role: "assistant", Content: "answer", ReasoningContent: ""},
		}
		noReasoning := estimateMessageTokens(messages1)

		messages2 := []api.Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "Here is my detailed reasoning process that takes some tokens to count"},
		}
		withReasoning := estimateMessageTokens(messages2)

		if withReasoning <= noReasoning {
			t.Errorf("expected reasoning content to add tokens: no_reasoning=%d, with_reasoning=%d", noReasoning, withReasoning)
		}
	})

	t.Run("empty reasoning adds nothing", func(t *testing.T) {
		t.Parallel()
		messages1 := []api.Message{{Role: "assistant", Content: "answer"}}
		messages2 := []api.Message{{Role: "assistant", Content: "answer", ReasoningContent: ""}}
		t1 := estimateMessageTokens(messages1)
		t2 := estimateMessageTokens(messages2)
		if t1 != t2 {
			t.Errorf("expected same tokens for empty reasoning content: %d vs %d", t1, t2)
		}
	})
}
