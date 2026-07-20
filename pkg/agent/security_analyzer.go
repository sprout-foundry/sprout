// Package agent: LLM-augmented security analysis for shell commands.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
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

// ─── SP-124b Chain types ───────────────────────────────────────────────────

// Chain is a top-level decomposition of a shell command. SP-124b.
// For unchained input, Subcommands has length 1 (the input itself).
// Operators carries the chain operator between each adjacent pair of
// subcommands (length len(Subcommands)-1, in order). The values are
// the literal operators as they appear in the original string: "&&",
// "||", ";", or "|".
//
// Operators is best-effort: when reconstruction from the original
// string is ambiguous (operators inside quoted strings), they may be
// empty strings. Consumers should not rely on Operators for security
// decisions in Phase 1 — use Subcommands for splitting-driven logic
// and treat Operators as display metadata.
type Chain struct {
	Original    string
	Operators   []string // len(Subcommands)-1
	Subcommands []string // len >= 1 (split via SplitChainedCommand)
}

// ParseChain splits a shell command string into a Chain value. It
// delegates all splitting to pkg/agenttools.SplitChainedCommand (SP-122).
// It does NOT write a new splitter — quoting semantics must match the
// rest of the codebase.
func ParseChain(s string) Chain {
	s = strings.TrimSpace(s)
	parts := agenttools.SplitChainedCommand(s)
	return Chain{
		Original:    s,
		Operators:   nil, // reconstruction deferred — not needed for Phase 1
		Subcommands: parts,
	}
}

// tokenizeChain walks the input string and emits a normalized sequence of
// subcommands and operators. It is quote-aware: operators inside single or
// double quotes are not treated as chain operators.
//
// Example outputs:
//   "a && b"     → ["a", "AND", "b"]
//   "a || b"     → ["a", "OR", "b"]
//   "a | b"      → ["a", "PIPE", "b"]
//   "a ; b"      → ["a", "SEQ", "b"]
//   "a && b || c | d" → ["a", "AND", "b", "OR", "c", "PIPE", "d"]
func tokenizeChain(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var tokens []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	i := 0

	// Helper to flush the current subcommand token (with whitespace normalization)
	flush := func() {
		s := current.String()
		// Collapse internal whitespace
		fields := strings.Fields(s)
		if len(fields) > 0 {
			tokens = append(tokens, strings.Join(fields, " "))
		}
		current.Reset()
	}

	for i < len(input) {
		ch := input[i]

		// Handle quote state
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			current.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			current.WriteByte(ch)
			i++
			continue
		}

		// If we're inside quotes, just add the character
		if inSingleQuote || inDoubleQuote {
			current.WriteByte(ch)
			i++
			continue
		}

		// Check for operators at current position
		remaining := input[i:]
		if strings.HasPrefix(remaining, "&&") {
			flush()
			tokens = append(tokens, "AND")
			i += 2
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}
		if strings.HasPrefix(remaining, "||") {
			flush()
			tokens = append(tokens, "OR")
			i += 2
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}
		if strings.HasPrefix(remaining, "|") {
			flush()
			tokens = append(tokens, "PIPE")
			i++
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}
		if strings.HasPrefix(remaining, ";") {
			flush()
			tokens = append(tokens, "SEQ")
			i++
			// Skip trailing whitespace after operator
			for i < len(input) && (input[i] == ' ' || input[i] == '\t') {
				i++
			}
			continue
		}

		// Regular character - accumulate
		current.WriteByte(ch)
		i++
	}

	// Flush any remaining subcommand
	flush()

	return tokens
}

// chainTokensToCacheKey converts a tokenized chain to a normalized cache key.
// The prefix is prepended by the caller.
func chainTokensToCacheKey(tokens []string) string {
	return strings.Join(tokens, " | ")
}

// ChainCacheKey returns the cache key for storing/retrieving analyses
// of a shell chain. The key is normalized so that equivalent chains
// (modulo whitespace and outer trimming) collide, but distinct
// operators keep distinct keys.
func ChainCacheKey(input string) string {
	tokens := tokenizeChain(input)
	if len(tokens) == 0 {
		return "sp-124b:v1:"
	}
	return "sp-124b:v1:" + chainTokensToCacheKey(tokens)
}

// NormalizeChain returns a normalized cache key for a chain.
// It walks chain.Original to recover operators and produces distinct keys
// for "a && b" and "a || b". Chains with identical subcommands and operators
// normalize to the same key regardless of internal whitespace.
func NormalizeChain(chain Chain) string {
	if chain.Original == "" {
		return ""
	}
	tokens := tokenizeChain(chain.Original)
	if len(tokens) == 0 {
		return ""
	}
	return chainTokensToCacheKey(tokens)
}

// AnalyzeChain analyzes a command chain using the LLM. When the chain has
// exactly one subcommand, it uses the SP-124 single-command prompt.
// When the chain has multiple subcommands, it uses the chain-aware prompt
// that includes per-subcommand static classifications.
func AnalyzeChain(ctx context.Context, agent *Agent, chain Chain, classifications []agenttools.ChainedClassification, cwd string) (*SecurityAnalysis, error) {
	if agent == nil {
		return nil, fmt.Errorf("nil agent")
	}
	if len(chain.Subcommands) == 0 {
		return nil, fmt.Errorf("empty chain")
	}

	client := agent.getClient()
	if client == nil {
		return nil, fmt.Errorf("no client configured")
	}

	var systemPrompt string
	if len(chain.Subcommands) == 1 {
		systemPrompt = `You are a security analyzer for shell commands. Analyze the given command and respond with ONLY a JSON object matching:
{"summary": "<one sentence plain-language summary>", "modifies": "<files or resources the command touches>", "risk_assessment": "low" | "moderate" | "high", "recommendation": "approve" | "review" | "reject"}

Focus on: data destruction, data exfiltration, privilege escalation, unrecoverable operations. Don't be alarmist.`
	} else {
		systemPrompt = buildChainPrompt(chain, classifications)
	}

	model := agent.GetModel()
	userPrompt := fmt.Sprintf("Chain:\n%s\n\nWorking directory: %s", chain.Original, cwd)

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
	content = extractJSON(content)

	var sa SecurityAnalysis
	if err := json.Unmarshal([]byte(content), &sa); err != nil {
		return nil, fmt.Errorf("parse analysis: %w (got: %s)", err, content)
	}

	sa.RiskAssessment = strings.ToLower(strings.TrimSpace(sa.RiskAssessment))
	sa.Recommendation = strings.ToLower(strings.TrimSpace(sa.Recommendation))

	return &sa, nil
}

// buildChainPrompt constructs the chain-aware system prompt with per-subcommand
// classification table embedded. SP-124b.
func buildChainPrompt(chain Chain, classifications []agenttools.ChainedClassification) string {
	var buf strings.Builder

	buf.WriteString(`You are a security analyzer for shell commands. Analyze this *chained* shell command.

`)

	buf.WriteString(fmt.Sprintf("The chain has %d subcommands. For each, the static gate has classified:\n", len(chain.Subcommands)))

	for i, sub := range chain.Subcommands {
		risk := "SAFE"
		reasoning := ""
		category := "unknown"

		for _, c := range classifications {
			if c.Subcommand == sub {
				risk = c.Risk.String()
				reasoning = c.Reasoning
				category = string(c.Category)
				break
			}
		}

		displaySub := sub
		if len(displaySub) > 60 {
			displaySub = displaySub[:57] + "..."
		}

		buf.WriteString(fmt.Sprintf("  %d. [%s] %s — %s (%s)\n", i+1, risk, displaySub, reasoning, category))
	}

	buf.WriteString(`

Provide:
1. A one-sentence summary of what the WHOLE CHAIN does (not any single step).
2. What files, directories, or system resources the chain modifies.
3. Per-subcommand risk if materially different from the static classification.
4. A chain-level risk assessment (low / moderate / high) — this is the
   risk of the CHAIN AS A WHOLE, not the sum of its parts.
5. A recommendation (approve / review / reject).

Watch for patterns the per-subcommand gate misses:
- Trust escalation: cd /tmp && curl ... | bash
- Destructive sequencing: git rm ... && git push (no recovery window)
- Exfiltration: cat ~/.ssh/id_rsa | base64 | curl -d @- ...
- Transient state assumptions: ... && rm -rf ... where the writer doesn't check

Be specific. Don't be alarmist. Most chains are fine.

Respond with ONLY a JSON object matching:
{"summary": "<one sentence summary>", "modifies": "<files or resources>", "risk_assessment": "low" | "moderate" | "high", "recommendation": "approve" | "review" | "reject"}
`)

	return buf.String()
}

// AnalyzeShellCommand sends a shell command (and optional cwd context) to
// the agent's configured LLM for plain-language analysis.
// Bounded by ctx — must return within the deadline (2s in production).
func AnalyzeShellCommand(ctx context.Context, agent *Agent, command, cwd string) (*SecurityAnalysis, error) {
	if agent == nil {
		return nil, fmt.Errorf("nil agent")
	}
	if command == "" {
		return nil, fmt.Errorf("empty command")
	}

	chain := ParseChain(command)
	classifications := agenttools.ClassifyChainedCommand(command)
	return AnalyzeChain(ctx, agent, chain, classifications, cwd)
}

// extractJSON returns the first balanced {...} substring in s, after
// stripping a leading or trailing markdown code fence.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") && strings.HasSuffix(s, "```") && len(s) >= 6 {
		inner := strings.TrimSpace(s[3 : len(s)-3])
		if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
			return inner
		}
	}
	if start := strings.Index(s, "{"); start != -1 {
		if end := strings.LastIndex(s, "}"); end != -1 && end > start {
			return s[start : end+1]
		}
	}
	return s
}
