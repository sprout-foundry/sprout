package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// ────────────────────────────────────────────────────────────────────
// SP-124b Phase 2: chain-length cap + per-subcommand fallback.
//
// Verify the long-chain fallback path (>MaxChainSubcommandsForBatchPrompt
// subcommands) and the single-command regression guard.
// ────────────────────────────────────────────────────────────────────

// buildLongChainCmd produces a chain of n subcommands joined by " && ".
// Subcommands are intentionally varied so the per-subcommand static
// classification produces different risk levels (so the synthesis picks
// "high" rather than all-low).
func buildLongChainCmd(n int) string {
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			parts[i] = fmt.Sprintf("echo step-%d", i)
		case 1:
			parts[i] = fmt.Sprintf("ls -la dir-%d", i)
		case 2:
			parts[i] = fmt.Sprintf("cat file-%d.txt", i)
		}
	}
	return strings.Join(parts, " && ")
}

// TestAnalyzeChain_LongChainFallback verifies that when a chain exceeds
// MaxChainSubcommandsForBatchPrompt, AnalyzeChain falls back to the
// per-subcommand analysis path and synthesizes one SecurityAnalysis
// entry that:
//   - has ChainLength equal to the number of subcommands
//   - populates ChainSubcommands and ChainClassifications
//   - makes exactly N LLM calls (one per subcommand), NOT one batch call
func TestAnalyzeChain_LongChainFallback(t *testing.T) {
	const n = MaxChainSubcommandsForBatchPrompt + 1 // 11
	cmd := buildLongChainCmd(n)

	var (
		mu    sync.Mutex
		calls int
		// capture the prompts sent so we can assert the SINGLE-command
		// prompt was used (no batch table prompt)
		prompts []string
	)
	client := &mockPhase2CountingClient{
		response: `{"summary": "step ok", "modifies": "files", "risk_assessment": "low", "recommendation": "approve"}`,
		onCall: func(messages []api.Message) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			for _, m := range messages {
				if m.Role == "system" {
					prompts = append(prompts, m.Content)
				}
			}
		},
	}

	agent := &Agent{}
	agent.setClient(client, api.TestClientType)

	chain := ParseChain(cmd)
	if len(chain.Subcommands) != n {
		t.Fatalf("ParseChain produced %d subcommands, want %d", len(chain.Subcommands), n)
	}

	sa, err := AnalyzeChain(context.Background(), agent, chain, agenttools.ClassifyChainedCommand(cmd), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sa == nil {
		t.Fatal("expected non-nil SecurityAnalysis")
	}

	// Synthesis: one LLM call per subcommand.
	if calls != n {
		t.Errorf("expected %d LLM calls (one per subcommand), got %d", n, calls)
	}

	// No batch prompt was sent — every call should have used the
	// single-command prompt (no "chained" in system prompt).
	for i, p := range prompts {
		if strings.Contains(p, "chained") {
			t.Errorf("call %d used chain prompt but expected single-command prompt (long-chain fallback must not use batch prompt)", i)
		}
	}

	// Chain metadata populated.
	if sa.ChainLength != n {
		t.Errorf("ChainLength = %d, want %d", sa.ChainLength, n)
	}
	if len(sa.ChainSubcommands) != n {
		t.Errorf("ChainSubcommands length = %d, want %d", len(sa.ChainSubcommands), n)
	}
	if len(sa.ChainClassifications) != n {
		t.Errorf("ChainClassifications length = %d, want %d", len(sa.ChainClassifications), n)
	}

	// Per-subcommand classifications are valid LLM-tone strings.
	for i, c := range sa.ChainClassifications {
		if c != "low" && c != "moderate" && c != "high" {
			t.Errorf("ChainClassifications[%d] = %q, want low/moderate/high", i, c)
		}
	}

	// Subcommands preserved in order.
	for i := 0; i < n; i++ {
		if sa.ChainSubcommands[i] != chain.Subcommands[i] {
			t.Errorf("ChainSubcommands[%d] = %q, want %q", i, sa.ChainSubcommands[i], chain.Subcommands[i])
		}
	}

	// Synthesized summary mentions chain length and front-loads the first
	// 3 subcommands (per spec).
	if !strings.Contains(sa.Summary, fmt.Sprintf("%d subcommands", n)) {
		t.Errorf("synthesized summary should mention %d subcommands, got %q", n, sa.Summary)
	}
}

// TestAnalyzeChain_MaxLengthBoundary verifies the boundary case:
//   - At MaxChainSubcommandsForBatchPrompt (10), uses BATCH prompt (1 LLM call)
//   - At MaxChainSubcommandsForBatchPrompt + 1 (11), uses FALLBACK (N LLM calls)
func TestAnalyzeChain_MaxLengthBoundary(t *testing.T) {
	const batchLen = MaxChainSubcommandsForBatchPrompt // 10
	const fallbackLen = batchLen + 1                   // 11

	t.Run("batch path at max length", func(t *testing.T) {
		var mu sync.Mutex
		var calls int
		var usedBatchPrompt bool
		client := &mockPhase2CountingClient{
			response: `{"summary": "chain", "modifies": "", "risk_assessment": "moderate", "recommendation": "review"}`,
			onCall: func(messages []api.Message) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				for _, m := range messages {
					if m.Role == "system" && strings.Contains(m.Content, "chained") {
						usedBatchPrompt = true
					}
				}
			},
		}
		agent := &Agent{}
		agent.setClient(client, api.TestClientType)

		cmd := buildLongChainCmd(batchLen)
		chain := ParseChain(cmd)
		sa, err := AnalyzeChain(context.Background(), agent, chain, agenttools.ClassifyChainedCommand(cmd), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Errorf("batch path should make exactly 1 LLM call, got %d", calls)
		}
		if !usedBatchPrompt {
			t.Error("batch path should use the chain-aware prompt")
		}
		if sa.ChainLength != batchLen {
			t.Errorf("ChainLength = %d, want %d", sa.ChainLength, batchLen)
		}
		if len(sa.ChainSubcommands) != batchLen {
			t.Errorf("ChainSubcommands length = %d, want %d", len(sa.ChainSubcommands), batchLen)
		}
	})

	t.Run("fallback path above max length", func(t *testing.T) {
		var mu sync.Mutex
		var calls int
		client := &mockPhase2CountingClient{
			response: `{"summary": "step ok", "modifies": "", "risk_assessment": "low", "recommendation": "approve"}`,
			onCall: func(messages []api.Message) {
				mu.Lock()
				defer mu.Unlock()
				calls++
			},
		}
		agent := &Agent{}
		agent.setClient(client, api.TestClientType)

		cmd := buildLongChainCmd(fallbackLen)
		chain := ParseChain(cmd)
		_, err := AnalyzeChain(context.Background(), agent, chain, agenttools.ClassifyChainedCommand(cmd), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != fallbackLen {
			t.Errorf("fallback path should make %d LLM calls (one per subcommand), got %d", fallbackLen, calls)
		}
	})
}

// TestAnalyzeShellCommand_ChainLengthZeroForSingleCommand verifies the
// single-command regression guard: AnalyzeShellCommand on a 1-subcommand
// input must produce ChainLength=0 and nil ChainSubcommands / ChainClassifications.
// This is the contract that suppresses the WebUI stepper + CLI stepper for
// single-command paths.
func TestAnalyzeShellCommand_ChainLengthZeroForSingleCommand(t *testing.T) {
	client := &mockSecurityAnalyzerClient{
		response: `{"summary": "Lists files", "modifies": "current directory", "risk_assessment": "low", "recommendation": "approve"}`,
	}
	agent := &Agent{}
	agent.setClient(client, api.TestClientType)

	sa, err := AnalyzeShellCommand(context.Background(), agent, "ls -la", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sa == nil {
		t.Fatal("expected non-nil SecurityAnalysis")
	}
	if sa.ChainLength != 0 {
		t.Errorf("ChainLength for single command = %d, want 0", sa.ChainLength)
	}
	if sa.ChainSubcommands != nil {
		t.Errorf("ChainSubcommands for single command should be nil, got %v", sa.ChainSubcommands)
	}
	if sa.ChainClassifications != nil {
		t.Errorf("ChainClassifications for single command should be nil, got %v", sa.ChainClassifications)
	}
}

// TestAnalyzeShellCommand_ChainLengthForChained verifies that
// AnalyzeShellCommand populates chain metadata for normal (non-fallback)
// chains. This is the path the WebUI stepper and CLI stepper consume.
func TestAnalyzeShellCommand_ChainLengthForChained(t *testing.T) {
	client := &mockSecurityAnalyzerClient{
		response: `{"summary": "Commits and pushes", "modifies": ".git/", "risk_assessment": "moderate", "recommendation": "review"}`,
	}
	agent := &Agent{}
	agent.setClient(client, api.TestClientType)

	sa, err := AnalyzeShellCommand(context.Background(), agent, "git add -A && git commit -m 'wip' && git push", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sa == nil {
		t.Fatal("expected non-nil SecurityAnalysis")
	}
	if sa.ChainLength != 3 {
		t.Errorf("ChainLength = %d, want 3", sa.ChainLength)
	}
	if len(sa.ChainSubcommands) != 3 {
		t.Errorf("ChainSubcommands length = %d, want 3", len(sa.ChainSubcommands))
	}
	if len(sa.ChainClassifications) != 3 {
		t.Errorf("ChainClassifications length = %d, want 3", len(sa.ChainClassifications))
	}
}

// TestRiskToLLMTone verifies the mapping from SecurityRisk to the LLM's
// low/moderate/high vocabulary used for ChainClassifications.
func TestRiskToLLMTone(t *testing.T) {
	cases := []struct {
		risk agenttools.SecurityRisk
		want string
	}{
		{agenttools.SecuritySafe, "low"},
		{agenttools.SecurityCaution, "moderate"},
		{agenttools.SecurityDangerous, "high"},
		{agenttools.SecurityRisk(99), "moderate"}, // unknown -> moderate
	}
	for _, c := range cases {
		got := riskToLLMTone(c.risk)
		if got != c.want {
			t.Errorf("riskToLLMTone(%v) = %q, want %q", c.risk, got, c.want)
		}
	}
}

// ── Mock client for Phase 2 tests ──────────────────────────────────

type mockPhase2CountingClient struct {
	response string
	onCall   func(messages []api.Message)
}

func (m *mockPhase2CountingClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	if m.onCall != nil {
		m.onCall(messages)
	}
	return &api.ChatResponse{
		Choices: []api.ChatChoice{
			{
				Message: api.Message{
					Role:    "assistant",
					Content: m.response,
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

func (m *mockPhase2CountingClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return m.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}
func (m *mockPhase2CountingClient) CheckConnection() error                              { return nil }
func (m *mockPhase2CountingClient) SetDebug(debug bool)                                {}
func (m *mockPhase2CountingClient) SetModel(model string) error                       { return nil }
func (m *mockPhase2CountingClient) GetModel() string                                   { return "test" }
func (m *mockPhase2CountingClient) GetProvider() string                               { return "test" }
func (m *mockPhase2CountingClient) GetModelContextLimit() (int, error)                { return 4096, nil }
func (m *mockPhase2CountingClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) { return nil, nil }
func (m *mockPhase2CountingClient) SupportsVision() bool                               { return false }
func (m *mockPhase2CountingClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return m.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}
func (m *mockPhase2CountingClient) GetLastTPS() float64                          { return 100.0 }
func (m *mockPhase2CountingClient) GetAverageTPS() float64                        { return 100.0 }
func (m *mockPhase2CountingClient) GetTPSStats() map[string]float64               { return nil }
func (m *mockPhase2CountingClient) ResetTPSStats()                                {}
func (m *mockPhase2CountingClient) SupportsConversationalVision() bool            { return false }
func (m *mockPhase2CountingClient) VisionCapabilities() api.VisionCapabilities     { return api.VisionCapabilities{} }
func (m *mockPhase2CountingClient) GetVisionModel() string                        { return "" }

var _ api.ClientInterface = (*mockPhase2CountingClient)(nil)
