// Package agent: LLM-augmented security analysis for shell commands.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// SecurityAnalysis is the structured output of AnalyzeShellCommand. SP-124.
// Returned as part of BrokerDecision (Phase 1) and surfaced in the WebUI
// approval dialog (Phase 2).
type SecurityAnalysis struct {
	// Summary is a one-sentence plain-language description of what the
	// command does. Required.
	Summary string `json:"summary"`
	// Modifies lists files, directories, or system resources the command
	// touches (e.g. "No local files; executes arbitrary code from URL").
	Modifies string `json:"modifies"`
	// RiskAssessment is one of "low", "moderate", "high" — the LLM's own
	// assessment. Independent of (often overrides) the static classifier.
	RiskAssessment string `json:"risk_assessment"`
	// Recommendation is one of "approve", "review", "reject".
	Recommendation string `json:"recommendation"`
}

// AnalyzeShellCommand sends a shell command (and optional cwd context) to
// the agent's configured LLM for plain-language analysis. Bounded by ctx
// — must return within the deadline (2s in production). On timeout, error,
// or unparseable response, returns nil to signal "no analysis available"
// — the caller falls through to the static-classifier prompt.
//
// The analysis is non-blocking relative to the user's approval prompt:
// the prompt renders immediately; the analysis, if it arrives in time,
// is attached as supplementary context. (The current implementation in
// Phase 1 actually IS blocking — see approval_broker.go integration — but
// the timeout is tight enough that the user experience is non-blocking.)
func AnalyzeShellCommand(ctx context.Context, agent *Agent, command, cwd string) (*SecurityAnalysis, error) {
	if agent == nil {
		return nil, fmt.Errorf("nil agent")
	}
	if command == "" {
		return nil, fmt.Errorf("empty command")
	}
	client := agent.getClient()
	if client == nil {
		return nil, fmt.Errorf("no client configured")
	}

	systemPrompt := `You are a security analyzer for shell commands. Analyze the given command and respond with ONLY a JSON object matching:
{"summary": "<one sentence plain-language summary>", "modifies": "<files or resources the command touches>", "risk_assessment": "low" | "moderate" | "high", "recommendation": "approve" | "review" | "reject"}

Focus on: data destruction, data exfiltration, privilege escalation, unrecoverable operations. Don't be alarmist.`

	userPrompt := fmt.Sprintf("Command: %s\nWorking directory: %s", command, cwd)

	// Use the configured model for analysis. Phase 1 bias is toward
	// cheap/fast models since this is a simple classification task.
	model := agent.GetModel()

	msgs := []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	resp, err := client.SendChatRequest(ctx, msgs, nil, model, false)
	if err != nil {
		return nil, fmt.Errorf("llm call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)

	// Extract JSON from the response (some models wrap in markdown code fences)
	content = extractJSON(content)

	var sa SecurityAnalysis
	if err := json.Unmarshal([]byte(content), &sa); err != nil {
		return nil, fmt.Errorf("parse analysis: %w (got: %s)", err, content)
	}

	// Normalize values
	sa.RiskAssessment = strings.ToLower(strings.TrimSpace(sa.RiskAssessment))
	sa.Recommendation = strings.ToLower(strings.TrimSpace(sa.Recommendation))

	return &sa, nil
}

// extractJSON returns the first balanced {...} substring in s, after
// stripping a leading or trailing markdown code fence. A robust fallback
// for LLM output that may wrap JSON in ```json fences or surround it
// with prose — a model returning {"summary": "..."} anywhere in the
// response will yield that exact JSON object back.
//
// Returns the input unchanged when no JSON object is present, so callers
// can pass the result directly to json.Unmarshal — an empty/no-JSON
// response surfaces as a parse error and falls back to the static
// classifier (SP-124 contract: analyzer failures are never blocking).
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// First try stripping a single-line ``` fence with no newline
	// separator (rare, but some compact formatters do this).
	if strings.HasPrefix(s, "```") && strings.HasSuffix(s, "```") && len(s) >= 6 {
		inner := strings.TrimSpace(s[3 : len(s)-3])
		if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
			return inner
		}
	}
	// Standard case: bare `{` scanning handles fences, leading prose,
	// trailing prose, and any other wrapper. Find the first `{` and last
	// `}` (greedy match — multi-line JSON works because `LastIndex`
	// scans from the end of the string).
	if start := strings.Index(s, "{"); start != -1 {
		if end := strings.LastIndex(s, "}"); end != -1 && end > start {
			return s[start : end+1]
		}
	}
	return s
}
