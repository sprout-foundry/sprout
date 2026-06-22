package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Mock LLM client (api.ClientInterface)
// ---------------------------------------------------------------------------
//
// Analyze() only calls SendChatRequest, but ClientInterface has ~15 methods.
// We implement the full interface: SendChatRequest is fully controllable,
// the rest are no-ops. CallCount lets tests assert caching behavior —
// a cached hit must NOT increment it.
//
// This mock is purpose-built for the security classifier tests. It is NOT a
// general-purpose ClientInterface fake: only SendChatRequest does real work.

// mockLLMClient is a controllable api.ClientInterface for the security
// classifier tests. The test sets Response, ResponseErr, and/or SlowDuration
// to control what SendChatRequest returns and how fast.
type mockLLMClient struct {
	mu sync.Mutex

	// Response is the canned ChatResponse returned from SendChatRequest.
	// Ignored when ResponseErr is non-nil.
	Response *api.ChatResponse

	// ResponseErr, when non-nil, makes SendChatRequest return this error.
	ResponseErr error

	// SlowDuration, when > 0, causes SendChatRequest to sleep before
	// returning. If the caller's context deadline expires first, the
	// sleep is interrupted and ctx.Err() is propagated — simulating a
	// slow LLM that triggers the classifier's timeout.
	SlowDuration time.Duration

	// CallCount tracks how many times SendChatRequest was invoked.
	// Tests assert on this to verify caching (a cache hit does NOT
	// increment the counter).
	CallCount int

	// LastMessages captures the messages passed to the most recent
	// SendChatRequest call, so tests can assert the prompt contains
	// the command and heuristic risk.
	LastMessages []api.Message
}

func (m *mockLLMClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	m.mu.Lock()
	m.CallCount++
	m.LastMessages = messages
	m.mu.Unlock()

	// Simulate a slow response. Honor the context deadline so the
	// classifier's WithTimeout can interrupt us.
	if m.SlowDuration > 0 {
		select {
		case <-time.After(m.SlowDuration):
			// sleep finished before deadline
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.ResponseErr != nil {
		return nil, m.ResponseErr
	}
	return m.Response, nil
}

// --- Unused ClientInterface methods (no-ops; Analyze never calls them) ---

func (m *mockLLMClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, nil
}
func (m *mockLLMClient) CheckConnection() error                                      { return nil }
func (m *mockLLMClient) SetDebug(debug bool)                                         {}
func (m *mockLLMClient) SetModel(model string) error                                 { return nil }
func (m *mockLLMClient) GetModel() string                                            { return "test-model" }
func (m *mockLLMClient) GetProvider() string                                         { return "test" }
func (m *mockLLMClient) GetModelContextLimit() (int, error)                          { return 128000, nil }
func (m *mockLLMClient) ListModels(ctx context.Context) ([]api.ModelInfo, error)     { return nil, nil }
func (m *mockLLMClient) SupportsVision() bool                                        { return false }
func (m *mockLLMClient) GetVisionModel() string                                      { return "" }
func (m *mockLLMClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, nil
}
func (m *mockLLMClient) GetLastTPS() float64             { return 0 }
func (m *mockLLMClient) GetAverageTPS() float64          { return 0 }
func (m *mockLLMClient) GetTPSStats() map[string]float64 { return nil }
func (m *mockLLMClient) ResetTPSStats()                  {}

// callCount is a thread-safe accessor for tests to read the mock's counter.
func (m *mockLLMClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.CallCount
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeChatResponse builds an api.ChatResponse with the given content string
// as the sole choice's message content.
func makeChatResponse(content string) *api.ChatResponse {
	return &api.ChatResponse{
		Choices: []api.ChatChoice{
			{Message: api.Message{Role: "assistant", Content: content}},
		},
	}
}

// makeEnabledClassifier builds an enabled SecurityLLMClassifier with the
// given mock client and a fresh cache. This bypasses the constructor (which
// requires provider resolution) so we can test Analyze in isolation.
func makeEnabledClassifier(client api.ClientInterface) *SecurityLLMClassifier {
	return &SecurityLLMClassifier{
		client:  client,
		cache:   make(map[string]SecurityLLMAnalysis),
		timeout: securityLLMClassifierTimeout,
		enabled: true,
	}
}

// ---------------------------------------------------------------------------
// Constructor / disabled-state tests
// ---------------------------------------------------------------------------

func TestNewSecurityLLMClassifier_DisabledByConfig(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	cfg := mgr.GetConfig()
	cfg.SecurityLLMClassifier = &configuration.SecurityLLMClassifierConfig{
		Enabled: false,
	}

	classifier, err := NewSecurityLLMClassifier(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, classifier)
	assert.False(t, classifier.enabled, "classifier should be disabled when config sets Enabled=false")

	// Analyze must return (_, false) — never an error.
	analysis, ok := classifier.Analyze(context.Background(), "shell_command", "rm -rf /tmp/test", "DANGEROUS")
	assert.False(t, ok, "disabled classifier must return ok=false")
	assert.Equal(t, SecurityLLMAnalysis{}, analysis, "disabled classifier must return zero-value analysis")
}

func TestNewSecurityLLMClassifier_NilConfig(t *testing.T) {
	// Passing nil config must NOT panic and must NOT return an error.
	// Whether the classifier ends up enabled or disabled depends on the
	// environment (available API keys, env vars) — the constructor degrades
	// gracefully either way. We assert only the invariant: no panic, no error,
	// non-nil result.
	classifier, err := NewSecurityLLMClassifier(nil, nil)
	require.NoError(t, err, "constructor must never error — it degrades gracefully")
	require.NotNil(t, classifier)
}

func TestNewSecurityLLMClassifier_DefaultEnabled(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	cfg := mgr.GetConfig()
	// SecurityLLMClassifier block is nil — the feature is ON by default.
	assert.True(t, cfg.IsSecurityLLMClassifierEnabled(),
		"nil SecurityLLMClassifier block should mean enabled (default-on)")

	// Also test the Config-level nil-safety.
	var nilCfg *configuration.Config
	assert.True(t, nilCfg.IsSecurityLLMClassifierEnabled(),
		"nil Config pointer should also report enabled")
}

// ---------------------------------------------------------------------------
// Analyze — happy path
// ---------------------------------------------------------------------------

func TestAnalyze_ParsesValidJSON(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"high","recommendation":"ask","summary":"force-pushes to the remote main branch"}`),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "git push --force origin main", "DANGEROUS")
	require.True(t, ok, "valid JSON response should parse successfully")
	assert.Equal(t, "high", analysis.Risk)
	assert.Equal(t, "ask", analysis.Recommendation)
	assert.Equal(t, "force-pushes to the remote main branch", analysis.Summary)
}

func TestAnalyze_StripsMarkdownFence(t *testing.T) {
	// The LLM may wrap JSON in ```json fences despite the prompt instructions.
	mock := &mockLLMClient{
		Response: makeChatResponse("```json\n{\"risk\":\"medium\",\"recommendation\":\"ask\",\"summary\":\"installs dependencies\"}\n```"),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "npm install", "CAUTION")
	require.True(t, ok, "JSON wrapped in markdown fences should still parse via the substring-extraction fallback")
	assert.Equal(t, "medium", analysis.Risk)
	assert.Equal(t, "ask", analysis.Recommendation)
	assert.Equal(t, "installs dependencies", analysis.Summary)
}

func TestAnalyze_ParsesJSONWithSurroundingProse(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse("Here is the analysis:\n{\"risk\":\"low\",\"recommendation\":\"approve\",\"summary\":\"lists files\"}\nThanks!"),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls -la", "CAUTION")
	require.True(t, ok, "JSON surrounded by prose should parse via substring extraction")
	assert.Equal(t, "low", analysis.Risk)
	assert.Equal(t, "approve", analysis.Recommendation)
	assert.Equal(t, "lists files", analysis.Summary)
}

func TestAnalyze_CachesResult(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"high","recommendation":"ask","summary":"deletes data"}`),
	}
	c := makeEnabledClassifier(mock)
	cmd := "rm -rf build/"

	// First call — hits the LLM.
	analysis1, ok1 := c.Analyze(context.Background(), "shell_command", cmd, "DANGEROUS")
	require.True(t, ok1)
	assert.Equal(t, 1, mock.callCount(), "first call should hit the LLM")

	// Second call with the same command — must hit the cache, not the LLM.
	analysis2, ok2 := c.Analyze(context.Background(), "shell_command", cmd, "DANGEROUS")
	require.True(t, ok2)
	assert.Equal(t, 1, mock.callCount(), "second call should hit the cache, not the LLM")
	assert.Equal(t, analysis1, analysis2, "cached result should match the first call")
}

func TestAnalyze_DifferentCommandsNotCached(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"medium","recommendation":"ask","summary":"some command"}`),
	}
	c := makeEnabledClassifier(mock)

	c.Analyze(context.Background(), "shell_command", "rm -rf build/", "DANGEROUS")
	c.Analyze(context.Background(), "shell_command", "npm install", "CAUTION")

	assert.Equal(t, 2, mock.callCount(), "two different commands should produce two LLM calls")
}

// ---------------------------------------------------------------------------
// Analyze — graceful degradation (ok=false, never an error)
// ---------------------------------------------------------------------------

func TestAnalyze_LLMError(t *testing.T) {
	mock := &mockLLMClient{
		ResponseErr: errors.New("connection refused"),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "rm -rf /tmp/x", "DANGEROUS")
	assert.False(t, ok, "LLM error must degrade to ok=false")
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)

	// Verify the failure was NOT cached.
	assert.Equal(t, 1, mock.callCount())
}

func TestAnalyze_LLMTimeout(t *testing.T) {
	mock := &mockLLMClient{
		Response:     makeChatResponse(`{"risk":"high","recommendation":"ask","summary":"should never see this"}`),
		SlowDuration: 10 * time.Second, // far longer than the classifier's timeout
	}
	c := makeEnabledClassifier(mock)

	// The classifier applies a 5s timeout internally. A 10s sleep will be
	// interrupted and ctx.Err() propagated as the error.
	start := time.Now()
	analysis, ok := c.Analyze(context.Background(), "shell_command", "git push --force", "DANGEROUS")
	elapsed := time.Since(start)

	assert.False(t, ok, "LLM timeout must degrade to ok=false")
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)
	assert.Less(t, elapsed, 8*time.Second, "Analyze should return near the classifier timeout (~5s), not wait for the full slow response")

	// Failure should not be cached.
	assert.Equal(t, 1, mock.callCount())
}

func TestAnalyze_EmptyResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *api.ChatResponse
	}{
		{
			name:     "nil response",
			response: nil,
		},
		{
			name:     "empty choices slice",
			response: &api.ChatResponse{Choices: []api.ChatChoice{}},
		},
		{
			name: "empty content string",
			response: makeChatResponse(""),
		},
		{
			name: "whitespace-only content",
			response: makeChatResponse("   \n\t  "),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLLMClient{Response: tt.response}
			c := makeEnabledClassifier(mock)

			analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
			assert.False(t, ok, "empty/whitespace response must degrade to ok=false")
			assert.Equal(t, SecurityLLMAnalysis{}, analysis)
		})
	}
}

func TestAnalyze_UnparseableResponse(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse("not json at all"),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
	assert.False(t, ok, "non-JSON response must degrade to ok=false")
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)
}

func TestAnalyze_MalformedJSON(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse("{broken json"),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
	assert.False(t, ok, "malformed JSON must degrade to ok=false")
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)
}

func TestAnalyze_NeverCachesFailures(t *testing.T) {
	mock := &mockLLMClient{
		ResponseErr: errors.New("transient error"),
	}
	c := makeEnabledClassifier(mock)
	cmd := "rm -rf build/"

	// First call fails.
	_, ok1 := c.Analyze(context.Background(), "shell_command", cmd, "DANGEROUS")
	require.False(t, ok1)
	assert.Equal(t, 1, mock.callCount(), "first call should hit the LLM")

	// Second call with the same command should hit the LLM again —
	// failures must NOT be cached so a transient timeout doesn't poison
	// the session.
	_, ok2 := c.Analyze(context.Background(), "shell_command", cmd, "DANGEROUS")
	require.False(t, ok2)
	assert.Equal(t, 2, mock.callCount(), "failure should not be cached; second call must re-hit the LLM")
}

// ---------------------------------------------------------------------------
// Analyze — normalizer behavior on real LLM output
// ---------------------------------------------------------------------------

func TestAnalyze_NormalizesUnknownRisk(t *testing.T) {
	// The LLM returns a non-canonical risk string. The normalizer should
	// clamp it to "medium" (the conservative default).
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"extreme","recommendation":"maybe","summary":"does something"}`),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
	require.True(t, ok)
	assert.Equal(t, "medium", analysis.Risk, "non-canonical risk should default to medium")
	assert.Equal(t, "ask", analysis.Recommendation, "non-canonical recommendation should default to ask")
	assert.Equal(t, "does something", analysis.Summary)
}

func TestAnalyze_PreservesCaseInsensitiveRisk(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"CRITICAL","recommendation":"DENY","summary":"destroys everything"}`),
	}
	c := makeEnabledClassifier(mock)

	analysis, ok := c.Analyze(context.Background(), "shell_command", "rm -rf /", "DANGEROUS")
	require.True(t, ok)
	assert.Equal(t, "critical", analysis.Risk)
	assert.Equal(t, "deny", analysis.Recommendation)
}

func TestAnalyze_PromptIncludesCommandAndRisk(t *testing.T) {
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"low","recommendation":"approve","summary":"ok"}`),
	}
	c := makeEnabledClassifier(mock)

	c.Analyze(context.Background(), "shell_command", "git status --porcelain", "DANGEROUS")

	require.NotEmpty(t, mock.LastMessages)
	content := mock.LastMessages[0].Content
	assert.Contains(t, content, "git status --porcelain", "the prompt should contain the command being analyzed")
	assert.Contains(t, content, "DANGEROUS", "the prompt should contain the heuristic risk")
	assert.Contains(t, content, "shell_command", "the prompt should contain the tool name")
}

// ---------------------------------------------------------------------------
// Normalizers (unit tests)
// ---------------------------------------------------------------------------

func TestNormalizeLLMRisk_ValidValues(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"low", "low"},
		{"medium", "medium"},
		{"high", "high"},
		{"critical", "critical"},
		// Case-insensitive
		{"LOW", "low"},
		{"Medium", "medium"},
		{"HIGH", "high"},
		{"Critical", "critical"},
		// Whitespace trimmed
		{"  high  ", "high"},
		{"\tcritical\n", "critical"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeLLMRisk(tt.input))
		})
	}
}

func TestNormalizeLLMRisk_InvalidDefaultsToMedium(t *testing.T) {
	tests := []string{
		"",         // empty
		"unknown",  // not in vocabulary
		"extreme",  // not in vocabulary
		"severe",   // not in vocabulary
		"block",    // not in vocabulary
		"123",      // numeric
		"low risk", // compound (not exact match)
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, "medium", normalizeLLMRisk(input),
				"invalid risk %q should default to medium (conservative)", input)
		})
	}
}

func TestNormalizeLLMRecommendation_ValidValues(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"approve", "approve"},
		{"deny", "deny"},
		// Case-insensitive
		{"APPROVE", "approve"},
		{"Deny", "deny"},
		// Whitespace trimmed
		{"  approve  ", "approve"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeLLMRecommendation(tt.input))
		})
	}
}

func TestNormalizeLLMRecommendation_InvalidDefaultsToAsk(t *testing.T) {
	tests := []string{
		"",         // empty
		"maybe",    // not in vocabulary
		"unknown",  // not in vocabulary
		"allow",    // close but not "approve"
		"block",    // close but not "deny"
		"ask",      // explicitly "ask" — it IS the default, verify it works
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, "ask", normalizeLLMRecommendation(input),
				"invalid recommendation %q should default to ask (conservative)", input)
		})
	}
}

// ---------------------------------------------------------------------------
// FormatAnalysisForPrompt
// ---------------------------------------------------------------------------

func TestFormatAnalysisForPrompt_OK(t *testing.T) {
	analysis := SecurityLLMAnalysis{
		Risk:           "high",
		Recommendation: "ask",
		Summary:        "This command force-pushes to the remote main branch, which rewrites published history.",
	}
	got := FormatAnalysisForPrompt(analysis, true)

	assert.Contains(t, got, "LLM risk analysis: high")
	assert.Contains(t, got, "Recommendation: ask")
	assert.Contains(t, got, analysis.Summary)
	// The summary should be on its own line after the header.
	assert.True(t, strings.Contains(got, "\n"), "summary should be separated from header by a newline")
}

func TestFormatAnalysisForPrompt_NotOK(t *testing.T) {
	analysis := SecurityLLMAnalysis{
		Risk:           "high",
		Recommendation: "ask",
		Summary:        "should not appear",
	}
	got := FormatAnalysisForPrompt(analysis, false)
	assert.Equal(t, "", got, "ok=false must return empty string")
}

func TestFormatAnalysisForPrompt_EmptySummary(t *testing.T) {
	analysis := SecurityLLMAnalysis{
		Risk:           "low",
		Recommendation: "approve",
		Summary:        "", // empty
	}
	got := FormatAnalysisForPrompt(analysis, true)

	// The header line should still be present.
	assert.Contains(t, got, "LLM risk analysis: low")
	assert.Contains(t, got, "Recommendation: approve")
	// No trailing newline when summary is empty.
	assert.False(t, strings.HasSuffix(got, "\n"),
		"empty summary should not append a trailing newline")
	assert.False(t, strings.Contains(got, "\n"),
		"empty summary should produce a single-line output")
}

func TestFormatAnalysisForPrompt_WhitespaceOnlySummary(t *testing.T) {
	analysis := SecurityLLMAnalysis{
		Risk:           "medium",
		Recommendation: "ask",
		Summary:        "   \n\t  ", // whitespace only
	}
	got := FormatAnalysisForPrompt(analysis, true)

	assert.Contains(t, got, "LLM risk analysis: medium")
	assert.False(t, strings.Contains(got, "\n"),
		"whitespace-only summary should be treated as empty — no newline")
}

// ---------------------------------------------------------------------------
// Safety contract
// ---------------------------------------------------------------------------

func TestAnalyze_DisabledClassifierReturnsFalse(t *testing.T) {
	// Construct a disabled classifier via the config path, then inject
	// a mock that returns valid JSON. The disabled classifier must NOT
	// call the LLM — ok=false regardless.
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	cfg := mgr.GetConfig()
	cfg.SecurityLLMClassifier = &configuration.SecurityLLMClassifierConfig{
		Enabled: false,
	}

	classifier, err := NewSecurityLLMClassifier(cfg, nil)
	require.NoError(t, err)

	// Even if we inject a mock that would return valid JSON, the disabled
	// classifier must short-circuit before touching the client.
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"low","recommendation":"approve","summary":"safe"}`),
	}
	classifier.client = mock // inject the mock AFTER construction

	analysis, ok := classifier.Analyze(context.Background(), "shell_command", "ls", "DANGEROUS")
	assert.False(t, ok, "disabled classifier must return ok=false even with a working mock")
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)
	assert.Equal(t, 0, mock.callCount(), "disabled classifier must NOT call the LLM at all")
}

func TestAnalyze_NilClassifierReturnsFalse(t *testing.T) {
	// A nil *SecurityLLMClassifier must not panic — Analyze checks c == nil.
	var c *SecurityLLMClassifier
	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
	assert.False(t, ok)
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)
}

func TestAnalyze_NilClientReturnsFalse(t *testing.T) {
	// A classifier that's "enabled" but has a nil client (shouldn't happen
	// via the constructor, but is a valid internal state) must return false.
	c := &SecurityLLMClassifier{
		enabled: true,
		client:  nil,
		cache:   make(map[string]SecurityLLMAnalysis),
	}
	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
	assert.False(t, ok)
	assert.Equal(t, SecurityLLMAnalysis{}, analysis)
}

func TestAnalyze_ZeroTimeoutUsesDefault(t *testing.T) {
	// A classifier with timeout=0 should fall back to the default timeout
	// and still work — it must not create a context with deadline 0
	// (which would immediately expire).
	mock := &mockLLMClient{
		Response: makeChatResponse(`{"risk":"low","recommendation":"approve","summary":"ok"}`),
	}
	c := &SecurityLLMClassifier{
		client:  mock,
		cache:   make(map[string]SecurityLLMAnalysis),
		timeout: 0, // zero — should fall back to securityLLMClassifierTimeout
		enabled: true,
	}

	analysis, ok := c.Analyze(context.Background(), "shell_command", "ls", "CAUTION")
	require.True(t, ok, "zero timeout should fall back to default and succeed")
	assert.Equal(t, "low", analysis.Risk)
}
