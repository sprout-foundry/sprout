package agent

import (
	"strconv"
	"strings"
	"testing"
)

// TestHandleSubagentBudgetExceeded tests the token-budget-exceeded handler.
// Regression for the recent change that switched the token count away from
// scraping a dead stdout token key and onto the structured
// SubagentResult.TokensUsed field.
func TestHandleSubagentBudgetExceeded(t *testing.T) {
	const partialOutput = "Partial work output line from subagent"
	const budgetBump = "250000"

	tests := []struct {
		name           string
		resultMap      map[string]string
		result         *SubagentResult
		wantEmpty      bool
		wantTokens     string // exact token string that must appear in output
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "budget not exceeded returns empty regardless of result",
			resultMap: map[string]string{
				"budget_exceeded": "false",
			},
			result:    &SubagentResult{TokensUsed: 12345},
			wantEmpty: true,
		},
		{
			name: "exceeded with TokensUsed reports the real count",
			resultMap: map[string]string{
				"budget_exceeded": "true",
			},
			result:         &SubagentResult{TokensUsed: 12345},
			wantTokens:     "12345",
			wantNotContain: []string{"unknown"},
		},
		{
			name: "exceeded with nil result falls back to unknown",
			resultMap: map[string]string{
				"budget_exceeded": "true",
			},
			result:     nil,
			wantTokens: "unknown",
		},
		{
			name: "exceeded with TokensUsed zero falls back to unknown",
			resultMap: map[string]string{
				"budget_exceeded": "true",
			},
			result:     &SubagentResult{TokensUsed: 0},
			wantTokens: "unknown",
		},
		{
			name: "exceeded with TokensUsed and stdout plus budget number",
			resultMap: map[string]string{
				"budget_exceeded": "true",
				"stdout":          partialOutput,
				"token_budget":    budgetBump,
			},
			result:     &SubagentResult{TokensUsed: 12345},
			wantTokens: "12345",
			wantContains: []string{
				"SUBAGENT_TOKEN_BUDGET_EXCEEDED",
				partialOutput,
				// The budget number in the message is the configured
				// DefaultSubagentTokenBudget constant, not a value read
				// from resultMap["token_budget"].
				strconv.Itoa(DefaultSubagentTokenBudget),
			},
			wantNotContain: []string{"unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAgent(t)
			defer a.Shutdown()

			got := handleSubagentBudgetExceeded(a, tt.resultMap, tt.result)

			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty string, got %q", got)
				}
				return
			}

			if got == "" {
				t.Fatalf("expected non-empty error string, got empty")
			}
			if !strings.Contains(got, tt.wantTokens) {
				t.Errorf("output %q does not contain expected token string %q", got, tt.wantTokens)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("output %q does not contain expected substring %q", got, want)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(got, notWant) {
					t.Errorf("output %q should not contain %q", got, notWant)
				}
			}
		})
	}
}
