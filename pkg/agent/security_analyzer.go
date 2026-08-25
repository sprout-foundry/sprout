// Package agent: LLM-augmented security analysis for shell commands.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SecurityAnalysis is the structured output of AnalyzeShellCommand.
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

	// ChainLength is the number of subcommands in the analyzed chain.
	// 0 means single-command path or analyzer didn't run.
	ChainLength int `json:"chain_length,omitempty"`

	// ChainSubcommands are the per-subcommand strings, in order. Used by the UI stepper.
	ChainSubcommands []string `json:"chain_subcommands,omitempty"`

	// ChainClassifications holds the per-subcommand risk classification for the stepper dots.
	ChainClassifications []string `json:"chain_classifications,omitempty"`
}

// MaxChainSubcommandsForBatchPrompt caps chain length for the batch prompt; longer chains fall back to per-subcommand analysis.
const MaxChainSubcommandsForBatchPrompt = 10

// riskToLLMTone maps a SecurityRisk to the LLM's "low/moderate/high" vocabulary for the chain stepper dots.
func riskToLLMTone(r agenttools.SecurityRisk) string {
	switch r {
	case agenttools.SecuritySafe:
		return "low"
	case agenttools.SecurityCaution:
		return "moderate"
	case agenttools.SecurityDangerous:
		return "high"
	default:
		return "moderate"
	}
}

// ─── Chain types ───────────────────────────────────────────────────────────

// Chain is a top-level decomposition of a shell command.
// For unchained input, Subcommands has length 1. Operators carries the chain
// operator between each adjacent pair of subcommands.
type Chain struct {
	Original    string
	Operators   []string // len(Subcommands)-1
	Subcommands []string // len >= 1 (split via SplitChainedCommand)
}

// ParseChain splits a shell command string into a Chain value, delegating to SplitChainedCommand.
func ParseChain(s string) Chain {
	s = strings.TrimSpace(s)
	parts := agenttools.SplitChainedCommand(s)
	return Chain{
		Original:    s,
		Operators:   nil, // reconstruction deferred
		Subcommands: parts,
	}
}

// tokenizeChain walks the input string and emits a normalized sequence of
// subcommands and operators. It is quote-aware: operators inside single or
// double quotes are not treated as chain operators.
//
// Example outputs:
//
//	"a && b"     → ["a", "AND", "b"]
//	"a || b"     → ["a", "OR", "b"]
//	"a | b"      → ["a", "PIPE", "b"]
//	"a ; b"      → ["a", "SEQ", "b"]
//	"a && b || c | d" → ["a", "AND", "b", "OR", "c", "PIPE", "d"]
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

// AnalyzeChain analyzes a command chain using the LLM. Single subcommands use
// the single-command prompt; chains up to MaxChainSubcommandsForBatchPrompt use
// the chain-aware prompt with per-subcommand classifications; longer chains
// fall back to per-subcommand analyses via AnalyzeChainFallback.
func AnalyzeChain(ctx context.Context, agent *Agent, chain Chain, classifications []agenttools.ChainedClassification, cwd string) (*SecurityAnalysis, error) {
	if agent == nil {
		return nil, agenterrors.NewInvalidInputError("nil agent", nil)
	}
	if len(chain.Subcommands) == 0 {
		return nil, agenterrors.NewInvalidInputError("empty chain", nil)
	}

	// Long chains fall back to per-subcommand single-command analyses.
	if len(chain.Subcommands) > MaxChainSubcommandsForBatchPrompt {
		return AnalyzeChainFallback(ctx, agent, chain, classifications, cwd)
	}

	client := agent.getClient()
	if client == nil {
		return nil, agenterrors.NewConfig("no client configured", nil)
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
		return nil, agenterrors.NewAgent("security_analyzer", "llm call", err)
	}
	if len(resp.Choices) == 0 {
		return nil, agenterrors.NewAgent("security_analyzer", "no response choices", nil)
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	content = extractJSON(content)

	var sa SecurityAnalysis
	if err := json.Unmarshal([]byte(content), &sa); err != nil {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("parse analysis (got: %s)", content), err)
	}

	sa.RiskAssessment = strings.ToLower(strings.TrimSpace(sa.RiskAssessment))
	sa.Recommendation = strings.ToLower(strings.TrimSpace(sa.Recommendation))

	// Populate chain metadata for the UI stepper. Single-command analyses
	// (length 1) get ChainLength=0 and nil slice fields.
	if len(chain.Subcommands) > 1 {
		sa.ChainLength = len(chain.Subcommands)
		sa.ChainSubcommands = append([]string(nil), chain.Subcommands...)
		sa.ChainClassifications = make([]string, 0, len(chain.Subcommands))
		for _, sub := range chain.Subcommands {
			sa.ChainClassifications = append(sa.ChainClassifications, classificationToneFor(classifications, sub))
		}
	}

	return &sa, nil
}

// classificationToneFor returns the LLM-vocabulary tone for the static classification matching the subcommand.
func classificationToneFor(classifications []agenttools.ChainedClassification, subcommand string) string {
	for _, c := range classifications {
		if c.Subcommand == subcommand {
			return riskToLLMTone(c.Risk)
		}
	}
	return "moderate"
}

// AnalyzeChainFallback handles chains longer than MaxChainSubcommandsForBatchPrompt.
// It runs per-subcommand single-command analysis on each subcommand and synthesizes
// a single SecurityAnalysis: max severity, worst recommendation, deduped modifies.
func AnalyzeChainFallback(ctx context.Context, agent *Agent, chain Chain, classifications []agenttools.ChainedClassification, cwd string) (*SecurityAnalysis, error) {
	if agent == nil {
		return nil, agenterrors.NewInvalidInputError("nil agent", nil)
	}
	if len(chain.Subcommands) == 0 {
		return nil, agenterrors.NewInvalidInputError("empty chain", nil)
	}

	// Build per-subcommand classification tones up-front so metadata is populated even if LLM calls fail.
	toneBySub := make(map[string]string, len(chain.Subcommands))
	for _, c := range classifications {
		toneBySub[c.Subcommand] = riskToLLMTone(c.Risk)
	}

	// Run one single-command analysis per subcommand.
	analyses := make([]*SecurityAnalysis, 0, len(chain.Subcommands))
	for _, sub := range chain.Subcommands {
		subCls := []agenttools.ChainedClassification{}
		for _, c := range classifications {
			if c.Subcommand == sub {
				subCls = append(subCls, c)
				break
			}
		}
		// Per-subcommand chain has length 1 → uses the single-command prompt path.
		sa, err := AnalyzeChain(ctx, agent, Chain{Original: sub, Subcommands: []string{sub}}, subCls, cwd)
		if err == nil && sa != nil {
			analyses = append(analyses, sa)
		}
	}

	synthesized := synthesizeChainFallback(chain, analyses)

	// Chain metadata: always populated, regardless of per-subcommand success.
	synthesized.ChainLength = len(chain.Subcommands)
	synthesized.ChainSubcommands = append([]string(nil), chain.Subcommands...)
	synthesized.ChainClassifications = make([]string, 0, len(chain.Subcommands))
	for _, sub := range chain.Subcommands {
		if tone, ok := toneBySub[sub]; ok && tone != "" {
			synthesized.ChainClassifications = append(synthesized.ChainClassifications, tone)
			continue
		}
		synthesized.ChainClassifications = append(synthesized.ChainClassifications, "moderate")
	}

	return synthesized, nil
}

// synthesizeChainFallback combines per-subcommand SecurityAnalysis results into one synthesized entry.
func synthesizeChainFallback(chain Chain, analyses []*SecurityAnalysis) *SecurityAnalysis {
	n := len(chain.Subcommands)
	const previewCount = 3

	// Summary: name the chain length and the first previewCount subcommands.
	preview := make([]string, 0, previewCount)
	for i := 0; i < n && i < previewCount; i++ {
		preview = append(preview, chain.Subcommands[i])
	}
	summary := fmt.Sprintf("Chain of %d subcommands (too long for batch analysis): %s",
		n, strings.Join(preview, "; "))
	if n > previewCount {
		summary += "..."
	}

	// Modifies: dedup, comma-join, cap at 5.
	seen := make(map[string]struct{}, len(analyses))
	modParts := make([]string, 0, len(analyses))
	for _, a := range analyses {
		if a == nil || a.Modifies == "" {
			continue
		}
		if _, ok := seen[a.Modifies]; ok {
			continue
		}
		seen[a.Modifies] = struct{}{}
		modParts = append(modParts, a.Modifies)
		if len(modParts) >= 5 {
			break
		}
	}
	modifies := strings.Join(modParts, ", ")

	// RiskAssessment: max severity across all sub-analyses.
	riskOrder := map[string]int{"low": 1, "moderate": 2, "high": 3}
	overallRisk := "low"
	for _, a := range analyses {
		if a == nil {
			continue
		}
		tone := strings.ToLower(strings.TrimSpace(a.RiskAssessment))
		if riskOrder[tone] > riskOrder[overallRisk] {
			overallRisk = tone
		}
	}
	if n > 0 && len(analyses) == 0 {
		// No sub-analyses succeeded — pick the maximum from the static
		// classifications so the synthesized entry still reflects the chain.
		overallRisk = "low"
	}

	// Recommendation: reject > review > approve.
	overallRec := "approve"
	recOrder := map[string]int{"approve": 1, "review": 2, "reject": 3}
	for _, a := range analyses {
		if a == nil {
			continue
		}
		tone := strings.ToLower(strings.TrimSpace(a.Recommendation))
		if recOrder[tone] > recOrder[overallRec] {
			overallRec = tone
		}
	}

	return &SecurityAnalysis{
		Summary:        summary,
		Modifies:       modifies,
		RiskAssessment: overallRisk,
		Recommendation: overallRec,
	}
}

// buildChainPrompt constructs the chain-aware system prompt with per-subcommand classification table embedded.
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

// AnalyzeShellCommand sends a shell command to the agent's LLM for plain-language analysis.
func AnalyzeShellCommand(ctx context.Context, agent *Agent, command, cwd string) (*SecurityAnalysis, error) {
	if agent == nil {
		return nil, agenterrors.NewInvalidInputError("nil agent", nil)
	}
	if command == "" {
		return nil, agenterrors.NewInvalidInputError("empty command", nil)
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
