# SP-076 — LLM-Based Command Risk Classifier

## Status
Proposed

## Problem

The heuristic security classifier (`pkg/agent_tools/security_classifier.go`)
relies on prefix matching and a catch-all CAUTION default. It over-flags
common dev commands (`npm test`, `rm -rf node_modules`, `go build`) as
CAUTION/DANGEROUS, producing approval-prompt fatigue. Users learn to
auto-approve everything, which defeats the security gate for the rare
genuinely-dangerous command.

An embedding-based classifier was attempted and rejected (see AGENTS.md
"Security Risk Classification"): embeddings measure semantic similarity,
but command risk is structural (`rm -rf node_modules` vs `rm -rf /etc` are
nearly identical vectors with opposite risk).

## Goal

Add an **LLM-based classifier** that produces a richer risk analysis for
commands the heuristic already flags as risky, **only when** the command is
not already allowed (allowlisted / unsafe-shell-mode / session-elevated).
The LLM output informs the user's approval decision — it does **not** bypass
the gate on its own (the LLM cannot downgrade a heuristic block to auto-run).

## Design

### Two-tier pipeline (unchanged heuristic + new LLM augmentation)

```
shell_command invocation
        │
        ▼
heuristic ClassifyToolCall (existing, always runs)
        │
   ┌────┴────────────────────┐
   │ SAFE → auto-run         │
   │ CRITICAL/hard-block →   │
   │   reject (no LLM call)  │
   └────┬────────────────────┘
        │ CAUTION or DANGEROUS
        ▼
allowlist / unsafe-shell / session-elevated?
        │
   ┌────┴────────────┐
   │ yes → auto-run  │
   │ no  ↓           │
   └────┬────────────┘
        ▼
LLM classifier (NEW) — only here
        │  produces {risk, recommendation, summary}
        ▼
present analysis alongside the approval prompt
(the recommendation can suggest approve/ask/deny but does NOT auto-run)
```

### The LLM call

- **When**: heuristic result is CAUTION or DANGEROUS, AND command is not
  allowlisted, AND not in unsafe-shell / session-elevated mode. Hard-block
  (Critical tier) never reaches the LLM — it's rejected outright.
- **Input**: the command string, the tool name, and the heuristic verdict.
- **Output**: structured JSON:
  ```json
  {
    "risk": "low|medium|high|critical",
    "recommendation": "approve|ask|deny",
    "summary": "Plain-language explanation of what this command does, in one or two sentences, written for the user who must decide whether to approve it."
  }
  ```
- **Provider/model**: reuse the same resolution as `pkg/spec` (default
  provider/model via `configuration.ResolveProviderModel`). Configurable
  later via a dedicated `security_llm_provider`/`security_llm_model` if
  the user wants a cheaper/faster model for this gate.
- **Timeout**: bounded (default 5s). On timeout or error, fall back to the
  heuristic result with no LLM analysis (the gate must never hang on the
  LLM call).

### Safety contract

1. The LLM classifier runs **after** the heuristic and **after** the
   allowlist check — it never fires for commands that would auto-run or
   hard-block.
2. The LLM's `recommendation` is **advisory**. It is surfaced in the
   approval prompt text (e.g. *"LLM analysis: high risk — this command
   force-pushes to main. Recommendation: ask."*). It does **not** change
   whether a prompt is shown or auto-run a command. The user still decides.
3. Hard-block (Critical tier) is never sent to the LLM.
4. Any LLM error/timeout degrades gracefully to "no analysis" — the
   existing heuristic gate still runs unchanged.

### Files

- `pkg/agent/llm_security_classifier.go` — the classifier: prompt
  construction, LLM call (via `factory.CreateProviderClient`), JSON
  parsing with markdown-fence stripping (mirror `pkg/spec/extractor.go`).
- `pkg/agent/llm_security_classifier_test.go` — tests with a mock client.
- Integration: one call site in `pkg/agent/risk_prompt.go` (the
  `highRiskApprovedForCommand` path / `buildSecurityPrompt`) or
  `tool_security.go` — wherever the approval prompt text is assembled,
  augment it with the LLM analysis when available.

### Open questions (resolve during implementation)

- Should the LLM analysis be shown for **every** CAUTION prompt, or only
  DANGEROUS? (Default: both — the cost is one LLM call per prompt, and
  CAUTION prompts are exactly where fatigue is worst.)
- Should the analysis be cached per-command for the session? (Default: yes,
  a small LRU keyed on the command string, to avoid re-analyzing repeated
  commands.)
- Config flag name and default (on/off). (Default: on, with
  `security_llm_classifier.enabled` config to disable.)
