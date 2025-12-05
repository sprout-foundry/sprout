package agent

import (
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TokenUsage captures key token metrics for each turn
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCost    float64
}

// TurnEvaluation captures the inputs, outputs, and tool activity for each iteration
type TurnEvaluation struct {
	Iteration         int
	Timestamp         time.Time
	UserInput         string
	AssistantContent  string
	ToolCalls         []api.ToolCall
	ToolResults       []api.Message
	ToolLogs          []string
	TokenUsage        TokenUsage
	CompletionReached bool
	FinishReason      string
	ReasoningSnippet  string
	GuardrailTrigger  string
}