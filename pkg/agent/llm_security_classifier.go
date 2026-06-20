// SP-076 — LLM-based command risk classifier.
//
// This file augments the heuristic security classifier
// (pkg/agent_tools/security_classifier.go) with an optional LLM pass that
// produces a richer risk analysis for commands the heuristic already flags
// as risky. The two-tier pipeline is:
//
//	heuristic ClassifyToolCall (always runs)
//	    │
//	    ├─ SAFE ──────────────────────── auto-run (no LLM call)
//	    ├─ CRITICAL / hard-block ─────── reject (no LLM call)
//	    └─ CAUTION / DANGEROUS ─────────┐
//	                                   ▼
//	          allowlist / unsafe-shell / session-elevated?
//	                   │
//	                   ├─ yes ─── auto-run (no LLM call)
//	                   └─ no ────► LLM classifier (HERE)
//	                                   │  {risk, recommendation, summary}
//	                                   ▼
//	                      analysis appended to the approval prompt
//
// # Safety contract (read this before editing)
//
//  1. This classifier runs ONLY AFTER the heuristic and the allowlist checks.
//     Callers must gate invocations: only call Analyze when the heuristic
//     result is CAUTION/DANGEROUS AND the command is not allowlisted AND not
//     in unsafe-shell/session-elevated mode AND not hard-blocked.
//
//  2. The LLM's `recommendation` is ADVISORY. It is surfaced in the prompt
//     text so the user can make a more informed decision. It does NOT change
//     whether a prompt is shown, and it does NOT auto-run a command.
//
//  3. Hard-block (Critical tier) is NEVER sent to the LLM. The heuristic
//     already rejected it; this classifier never sees it.
//
//  4. Any LLM error, timeout, or unparseable response degrades gracefully to
//     "no analysis available" (ok=false). The heuristic gate runs unchanged —
//     the LLM can never wedge or weaken the command-approval path.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// securityLLMClassifierTimeout is the default bound on an LLM risk-analysis
// call. Bounded so the approval path can never hang waiting on the LLM; on
// timeout the classifier returns "no analysis" and the heuristic runs alone.
const securityLLMClassifierTimeout = 5 * time.Second

// securityLLMClassifierPrompt is the system/user prompt sent to the LLM. It
// instructs the model to analyze what a shell command does and return strict
// JSON. The actual command/verdict are interpolated into the user message
// (see buildAnalysisMessages).
const securityLLMClassifierPrompt = `You are a security risk classifier for shell commands run inside a software development agent.

You will be given:
- TOOL: the tool invoking the command (usually "shell_command").
- HEURISTIC RISK: the verdict of a fast heuristic classifier (one of SAFE, CAUTION, DANGEROUS). Use it as context, not as a constraint — your job is to explain what the command actually does.
- COMMAND: the exact shell command string to analyze.

Analyze what the command actually does when executed, then respond with ONLY a JSON object (no markdown fences, no prose before or after) with exactly these keys:

{
  "risk": "<low|medium|high|critical>",
  "recommendation": "<approve|ask|deny>",
  "summary": "<one or two plain-language sentences explaining what this command does, written for a non-expert user who must decide whether to approve it>"
}

Rules for the fields:
- "risk": how dangerous the command's real effect is.
    low      — read-only or trivially safe (e.g. ls, cat, npm test).
    medium   — modifies files within the workspace, reversible (e.g. npm install, mkdir).
    high     — modifies files outside the workspace, network fetch+execute, force-push, deletes data, or is otherwise hard to reverse.
    critical — destroys data system-wide, wipes disks, escalates privileges on the host, or could brick the machine (e.g. rm -rf /, mkfs, :(){ :|:& };:).
- "recommendation": what the user should do.
    approve — safe to run without asking.
    ask     — the user should review before running.
    deny    — should never be run.
- "summary": 1-2 sentences in plain language. Name the concrete effect (e.g. "deletes the node_modules directory", "force-pushes to the main branch"). Do NOT restate the command verbatim; explain what it DOES. Do not use bullet points or lists.

Output ONLY the JSON object. No backticks, no code fences, no commentary.`

// SecurityLLMAnalysis is the structured output from the LLM risk classifier.
type SecurityLLMAnalysis struct {
	Risk           string // "low" | "medium" | "high" | "critical"
	Recommendation string // "approve" | "ask" | "deny"
	Summary        string // plain-language explanation of what the command does, 1-2 sentences
}

// SecurityLLMClassifier calls the configured LLM to produce a risk analysis
// for shell commands flagged as risky by the heuristic classifier. It runs
// ONLY after the heuristic and allowlist checks — never for auto-run or
// hard-blocked commands. The analysis is advisory: it informs the user's
// approval decision but does NOT bypass the gate.
type SecurityLLMClassifier struct {
	client  api.ClientInterface
	cache   map[string]SecurityLLMAnalysis // session-scoped, keyed on command string
	mu      sync.Mutex
	timeout time.Duration
	enabled bool
}

// NewSecurityLLMClassifier builds a classifier from the app configuration.
//
// It mirrors pkg/spec/extractor.go's NewSpecExtractor: resolve the
// provider/client via configuration.ResolveProviderModel +
// factory.CreateProviderClient. If the client can't be created (no provider
// configured, bad credentials, etc.) it returns a classifier with
// enabled=false — NOT an error — so the security gate keeps working without
// the LLM. The feature is also disabled when the config flag
// security_llm_classifier.enabled is false (the flag defaults to true; see
// Config.IsSecurityLLMClassifierEnabled).
func NewSecurityLLMClassifier(cfg *configuration.Config, logger *utils.Logger) (*SecurityLLMClassifier, error) {
	// Config flag opt-out (default-on).
	if cfg != nil && !cfg.IsSecurityLLMClassifierEnabled() {
		return &SecurityLLMClassifier{enabled: false}, nil
	}

	// Resolve provider/model. Honor an optional per-classifier override
	// (security_llm_classifier.provider / .model) before falling back to the
	// global default — lets users point this gate at a cheaper/faster model.
	explicitProvider := ""
	explicitModel := ""
	if cfg != nil && cfg.SecurityLLMClassifier != nil {
		explicitProvider = strings.TrimSpace(cfg.SecurityLLMClassifier.Provider)
		explicitModel = strings.TrimSpace(cfg.SecurityLLMClassifier.Model)
	}

	clientType, model, err := configuration.ResolveProviderModel(cfg, explicitProvider, explicitModel)
	if err != nil {
		// Degrade gracefully — the gate must keep working.
		if logger != nil {
			logger.LogProcessStep(fmt.Sprintf("[security-llm] disabled: failed to resolve provider/model: %v", err))
		}
		return &SecurityLLMClassifier{enabled: false}, nil
	}

	client, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		// Degrade gracefully — no client means "no analysis available", not an
		// error. The caller proceeds with the heuristic verdict alone.
		if logger != nil {
			logger.LogProcessStep(fmt.Sprintf("[security-llm] disabled: failed to create client (%s): %v", clientType, err))
		}
		return &SecurityLLMClassifier{enabled: false}, nil
	}

	resolvedModel := strings.TrimSpace(client.GetModel())
	if resolvedModel == "" {
		resolvedModel = "<provider default>"
	}
	if logger != nil {
		logger.LogProcessStep(fmt.Sprintf("[info] Security LLM classifier using provider/model: %s | %s", clientType, resolvedModel))
	}

	return &SecurityLLMClassifier{
		client:  client,
		cache:   make(map[string]SecurityLLMAnalysis),
		timeout: securityLLMClassifierTimeout,
		enabled: true,
	}, nil
}

// Analyze calls the LLM to produce a risk analysis for the given command.
// Returns (analysis, ok). ok=false when: classifier disabled, LLM error,
// timeout, or unparseable response. Callers MUST treat ok=false as
// "no analysis available" and proceed with the heuristic verdict alone —
// never block or fail the command on an LLM error.
//
// The result is cached for the session keyed on the command string so that
// repeated identical commands don't re-incur an LLM call. Failures are never
// cached — a transient timeout shouldn't poison the analysis for the rest of
// the session.
func (c *SecurityLLMClassifier) Analyze(ctx context.Context, toolName string, command string, heuristicRisk string) (SecurityLLMAnalysis, bool) {
	// 1. Disabled or no client → no analysis. Never an error for the caller.
	if c == nil || !c.enabled || c.client == nil {
		return SecurityLLMAnalysis{}, false
	}

	// 2. Session cache (keyed on command string).
	c.mu.Lock()
	if cached, ok := c.cache[command]; ok {
		c.mu.Unlock()
		return cached, true
	}
	c.mu.Unlock()

	// 3. Build the prompt + messages.
	messages := buildAnalysisMessages(toolName, command, heuristicRisk)

	// 4. Bounded timeout so the approval path can't hang on the LLM.
	timeout := c.timeout
	if timeout <= 0 {
		timeout = securityLLMClassifierTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 5. Call the LLM (non-streaming, no tools, no reasoning, no thinking-mode).
	chatResponse, err := c.client.SendChatRequest(callCtx, messages, nil, "", false)
	if err != nil {
		return SecurityLLMAnalysis{}, false
	}
	if chatResponse == nil || len(chatResponse.Choices) == 0 {
		return SecurityLLMAnalysis{}, false
	}
	raw := strings.TrimSpace(chatResponse.Choices[0].Message.Content)
	if raw == "" {
		return SecurityLLMAnalysis{}, false
	}

	// 6. Parse JSON, with markdown-fence stripping fallback (mirror
	// pkg/spec/extractor.go): if the direct parse fails, retry on the
	// substring between the first '{' and the last '}'.
	var parsed struct {
		Risk           string `json:"risk"`
		Recommendation string `json:"recommendation"`
		Summary        string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start < 0 || end <= start {
			return SecurityLLMAnalysis{}, false
		}
		if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
			return SecurityLLMAnalysis{}, false
		}
	}

	analysis := SecurityLLMAnalysis{
		Risk:           normalizeLLMRisk(parsed.Risk),
		Recommendation: normalizeLLMRecommendation(parsed.Recommendation),
		Summary:        strings.TrimSpace(parsed.Summary),
	}

	// 7. Cache success and return. (Failures above returned without caching.)
	c.mu.Lock()
	c.cache[command] = analysis
	c.mu.Unlock()

	return analysis, true
}

// buildAnalysisMessages assembles the single user message that asks the LLM
// to analyze the command. The system/user instructions live in
// securityLLMClassifierPrompt; this injects the concrete command + verdict.
func buildAnalysisMessages(toolName string, command string, heuristicRisk string) []api.Message {
	user := fmt.Sprintf(
		"%s\n\nTOOL: %s\nHEURISTIC RISK: %s\nCOMMAND: %s",
		securityLLMClassifierPrompt, toolName, heuristicRisk, command,
	)
	return []api.Message{
		{Role: "user", Content: user},
	}
}

// normalizeLLMRisk clamps the LLM's risk string to the canonical vocabulary.
// Unknown/empty values default to "medium" — conservative: we don't want a
// malformed LLM answer to silently downgrade a genuinely-risky command to
// "low", nor inflate a safe one to "critical".
func normalizeLLMRisk(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "critical":
		return "critical"
	default:
		return "medium"
	}
}

// normalizeLLMRecommendation clamps the recommendation to approve/ask/deny.
// Unknown/empty values default to "ask" — conservative.
func normalizeLLMRecommendation(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "approve":
		return "approve"
	case "deny":
		return "deny"
	default:
		return "ask"
	}
}

// FormatAnalysisForPrompt renders the LLM analysis as a human-readable block
// to append to the approval prompt. Returns "" if ok is false (so callers can
// always pass the result straight through; an empty string means "no analysis"
// and the prompt is left unchanged).
//
// Example output when ok:
//
//	LLM risk analysis: high — Recommendation: ask
//	This command force-pushes to the remote main branch, which rewrites published history and can discard others' commits.
func FormatAnalysisForPrompt(analysis SecurityLLMAnalysis, ok bool) string {
	if !ok {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("LLM risk analysis: ")
	sb.WriteString(analysis.Risk)
	sb.WriteString(" — Recommendation: ")
	sb.WriteString(analysis.Recommendation)
	if strings.TrimSpace(analysis.Summary) != "" {
		sb.WriteString("\n")
		sb.WriteString(analysis.Summary)
	}
	return sb.String()
}
