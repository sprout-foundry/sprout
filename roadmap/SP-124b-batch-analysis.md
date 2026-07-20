# SP-124b: Batch Security Analysis for Chained Commands

**Status:** 🔵 Proposed
**Created:** 2026-07-19
**Parent:** [SP-124 — LLM-Augmented Security Analysis](./SP-124-llm-security-analysis.md) (Phases 1–3 shipped 2026-07-19)
**Effort:** Phase 1 (~1–2 days), Phase 2 (~1 day)

## Relationship to SP-124

SP-124 Phases 1–3 ship single-command LLM analysis (backend + WebUI panel + CLI picker). SP-124's "Future considerations" lists four follow-ups; this spec elevates the first one — **batch analysis for chained commands** — to a tracked sub-SP. It is one of potentially several `SP-124b-*` follow-ups if the user demand warrants.

Why it's first:
- Smallest scope (the existing analyzer is the codebase; we're adding a wrapper)
- Highest UX leverage: chained commands are exactly the regime where the static classifier is weakest
- All plumbing exists (cache, broker, renderers) — Phase 1 is mostly orchestration

## Problem

The SP-124 analyzer takes a single command string and asks the LLM "what does this do?". The static security classifier (`pkg/agent_tools/security_classifier.go`) already does its own chain-aware splitting for the gate: `classifyChainedCommand` calls `SplitChainedCommand` (quote-aware splitter on `&&`, `||`, `;`, `|`), classifies each subcommand independently via `classifySingleCommand`, and returns the max risk. The static gate is therefore not blind to chains.

What's missing is the **LLM analysis surface**: when a chained command has at least one CAUTION- or DANGEROUS-rated subcommand, only the CAUTION/DANGEROUS subcommand currently reaches the LLM (the analyzer only sees what the gate flagged), so the analysis loses the chain context that makes the chain risky as a whole.

### Concrete failure

```
git status && git add -A && git commit -m "wip" && git push
```

Each subcommand is `Safe`-classified; the gate produces `SecuritySafe` and SP-124's `AnalyzeShellCommand` is never even called. The chain is destructive-and-irreversible and the user never sees an analysis. Even when a chain has exactly one CAUTION subcommand, the LLM analysis only sees that one subcommand — it can't flag that the chain as a *whole* matters more than the one subcommand (e.g., `cd /tmp && curl ... | bash` — the gate flags the pipe-to-shell as CAUTION, but the LLM is asked only about the pipe-to-shell fragment and doesn't see the `cd /tmp`).

## Design

### Core concept: chain-aware batch analysis

When the input string contains one or more chain operators (`&&`, `||`, `;`, `|` at the top level — not inside a subshell), the analyzer:

1. Splits the string into a `Chain` value: ordered list of subcommand strings
2. Runs the existing per-subcommand static gate (preserves SP-124 risk level — no behavior change for the gate)
3. Sends **one** LLM call with both:
   - The full command string
   - The per-subcommand static classifications (low/medium/high + matched patterns)
4. Caches by a **normalized cache key** (see §"Caching")
5. Renders the analysis panel with extra context fields (`chain_length`, per-subcommand risk badges)

A single subcommand behaves exactly as it does today — no chain, no batch prompt, no schema change.

### The chain input type

```go
// pkg/agent/security_analyzer.go (additive to SP-124 types)

// Chain is a top-level decomposition of a shell command. For unchained
// input it has Subcommands length 1 (the input itself). The split is
// done by the static classifier's quote-aware splitter
// `pkg/agent_tools.SplitChainedCommand` so the LLM side and the gate
// side agree on what counts as a chain boundary — operators inside
// `(...)`, `$(...)`, `${...}`, and quoted strings are preserved with
// the containing subcommand.
type Chain struct {
    Original    string   // unparsed original input
    Operators   []string // "&" | "&&" | "||" | "|" between subcommands (length len(Subcommands)-1)
    Subcommands []string // n >= 1
}
```

Parser implementation:
- **Reuse `pkg/agent_tools.SplitChainedCommand`.** Do NOT write a new splitter; that path has tests and known quoting behavior already.
- The local `Chain` type adds operators metadata (which `SplitChainedCommand` doesn't return), reconstructed by re-scanning the original string at split positions. If reconstruction is fragile, fall back to `Operators: nil` (consumers don't strictly need it for Phase 1).

### Per-subcommand classifications

The static gate already returns per-subcommand `SecurityRisk` via `classifyChainedCommand` (`pkg/agent_tools/security_classifier.go:438`). For the LLM prompt we additionally need per-subcommand `Reasoning` and `Category`, which the public type currently doesn't carry. Phase 1 adds:

```go
// pkg/agent_tools/security_classifier.go (additive)

// ChainedClassification is a per-subcommand classification result.
// The existing []SecurityRisk return type stays for backwards compat;
// new code uses this richer type.
type ChainedClassification struct {
    Subcommand string       // the subcommand text
    Risk       SecurityRisk
    Reasoning  string       // human-readable why
    Category   RiskCategory // see RiskCategory type
}

func ClassifyChainedCommand(cmd string) []ChainedClassification // thin wrapper around classifyChainedCommand
```

`ClassifyShellCommand` (the entry point in SP-124 today) is extended to also return `[]ChainedClassification` on a new sibling function; the existing return type is preserved.

### Prompt change

Replace the SP-124 system prompt with a chain-aware version when `len(Subcommands) > 1`:

```
You are a security analyzer. Analyze this *chained* shell command.

The chain has {N} subcommands. For each, the static gate has classified:
{subcommand_classification_table}

Provide:
1. A one-sentence summary of what the WHOLE CHAIN does (not any single step).
2. What files, directories, or system resources the chain modifies.
3. Per-subcommand risk if materially different from the static classification.
4. A chain-level risk assessment (low / moderate / high) — this is the
   risk of the CHAIN AS A WHOLE, not the sum of its parts.
5. A recommendation (approve / review / reject).

Watch for patterns the per-subcommand gate misses:
- Trust escalation: `cd /tmp && curl ... | bash`
- Destructive sequencing: `git rm ... && git push` (no recovery window)
- Exfiltration: `cat ~/.ssh/id_rsa | base64 | curl -d @- ...`
- Transient state assumptions: `... && rm -rf ...` where the writer doesn't check

Be specific. Don't be alarmist. Most chains are fine.

Chain:
{command}

Working directory: {cwd}
```

For `Subcommands: [s]` (length 1), the existing SP-124 prompt is used unchanged.

### Caching (choice: cache by normalized chain)

The cache key is a normalization of the chain, not the raw string. The normalization:
1. Collapse runs of whitespace within each subcommand to a single space
2. Trim leading/trailing whitespace from each subcommand
3. Preserve all operators and argument tokens verbatim (no alias expansion, no glob expansion)
4. Join with ` | ` (so `a && b` and `a &&  b` collide, but `a && b` and `a || b` don't)

Why normalize: `git status  &&  git push` and `git status && git push` should share the same analysis. Token-level diff is a follow-up if needed; this is the minimum useful cache.

Cache hit semantics: a hit returns the stored analysis and *re-uses* the original-string rendering through the unchanged `SecurityAnalysisView`. Cached analyses don't get a per-subcommand UI expansion (the cache stores the final panel state, not the raw inputs).

### Where this plugs in

| Layer | File | Change |
|-------|------|--------|
| Classifier (existing) | `pkg/agent_tools/security_classifier.go` | Add `ChainedClassification` type + `ClassifyChainedCommand` wrapper. Existing `classifyChainedCommand` stays; no behavior change. |
| Analyzer | `pkg/agent/security_analyzer.go` | Add `Chain` type + parser that delegates to `pkg/agent_tools.SplitChainedCommand`. Add `AnalyzeChain(ctx, chain, []ChainedClassification) (*SecurityAnalysis, error)`. Existing `AnalyzeShellCommand` becomes a thin wrapper that calls `AnalyzeChain` after parsing. |
| Cache | Same file | Cache key uses normalized chain string (see "Caching"); value gains optional `ChainLength int` and `ChainSubcommands []string` fields. |
| Broker | `pkg/agent/approval_broker.go` | No API change — `BrokerDecision.SecurityAnalysis` already carries the analysis struct; Phase 2 adds `ChainLength int` to `extras`. |
| CLI | `pkg/utils/security_analysis_view.go` | Add optional `ChainSubcommands []string` field for the per-subcommand badge rendering. |
| WebUI | `webui/src/components/SecurityApprovalDialog.tsx` | Render the chain as a horizontal stepper when `chain_length > 1`, with a per-subcommand risk dot. |

### Cross-references

- **SP-122 (shipped):** owns the static gate's chain classification (`classifyChainedCommand`, `SplitChainedCommand`, `safeRmRfPrefixes`). SP-124b consumes the gate's output; it does NOT touch the gate itself.
- **SP-124 phases 1–3 (shipped):** own the single-command LLM analyzer, cache, broker plumbing, and renderer surfaces. SP-124b extends the analyzer entry point; the renderers are extended in Phase 2.

### Out of scope (deferred to potential SP-124c/124d)

- Persistent per-user / per-workspace chain-decision learning (SP-124 future item #2)
- Policy-driven auto-approval of low-risk chains (SP-124 future item #3)
- Per-subcommand analysis emitted **independently** when the gate fires (we share one LLM call for the chain)
- Cross-platform chain-operator differences (Windows `&` vs `&&`)

## Decisions

- **One LLM call per chain, not per subcommand.** The batch prompt explicitly includes the static gate's per-subcommand classification so the LLM doesn't have to re-derive risk for already-classified subcommands. Latency cost stays roughly constant.
- **Normalize for cache key, not for rendering.** The user always sees the original string in the approval panel. Only the cache lookup is lossy.
- **No new hard-block logic.** Chains stay classified by their highest-severity subcommand (existing behavior). Batch analysis just gives the user a more informed prompt.
- **Chain limit.** Chains longer than **10 subcommands** get the per-subcommand fallback (Phase 2) rather than the batch prompt. 10 is generous — anything longer is almost certainly a script that should be in a file.

## Implementation phases

### Phase 1: Backend batch analysis (~1–2 days)

- [ ] Add `ChainedClassification` struct + `ClassifyChainedCommand(cmd string) []ChainedClassification` to `pkg/agent_tools/security_classifier.go`. Populate `Subcommand`, `Risk`, `Reasoning`, `Category` for each part.
- [ ] Add `Chain` struct + `ParseChain(s string) Chain` to `pkg/agent/security_analyzer.go`. **Delegate the actual splitting to `pkg/agent_tools.SplitChainedCommand`** — do not reimplement.
- [ ] Add `AnalyzeChain(ctx, chain, []ChainedClassification) (*SecurityAnalysis, error)` that uses the chain-aware prompt when `len(chain.Subcommands) > 1` and the SP-124 single-command prompt otherwise.
- [ ] Switch cache key to normalized chain (with regression test that existing single-command cache keys still hit).
- [ ] Embed per-subcommand `ChainedClassification` table in the batch prompt payload (one entry per subcommand, with risk + 1-line reasoning).
- [ ] Tests:
  - `ParseChain` for `cmd1 && cmd2`, `(a && b) | c`, `'echo a && echo b'`, `cmd1 | cmd2 && cmd3` (delegates to `SplitChainedCommand`)
  - `ClassifyChainedCommand` populates per-subcommand reasoning/category, not just `[]SecurityRisk`
  - Prompt is selected correctly for `len(Subcommands) == 1` vs `> 1`
  - Cache hit on normalization (`a && b` ↔ `a  &&  b`)
  - Cache miss on operator change (`a && b` vs `a || b`)
  - One LLM call per chain analysis (verified via mock provider)
  - Static gate behavior unchanged for single commands (regression: every existing `security_classifier_test.go` test still passes)

### Phase 2: Per-subcommand fallback + UI surfacing (~1 day)

- [ ] Add chain-length limit (10) → fall back to per-subcommand analyses combined into a synthesized batch analysis entry
- [ ] Add `ChainSubcommands []string` to `pkg/utils.SecurityAnalysisView`
- [ ] Render chain stepper in `SecurityApprovalDialog.tsx` when `chain_length > 1`
- [ ] Render per-subcommand risk dots (low/med/high colored circles) on the stepper
- [ ] Tests:
  - >10 subcommands selects fallback path
  - CLI prompt renders >3 subcommands as collapsed "(N more...)" with expand affordance (cap terminal noise)
  - WebUI stepper renders all subcommands
  - Single-command path is visually unchanged (regression guard)

### Acceptance criteria

- A chained command with 2+ subcommands of mixed risk triggers ONE LLM call (counted via metric or test mock), not N.
- The chain-aware prompt is sent when `Subcommands` length > 1, SP-124 prompt is sent when = 1 (verified by mock provider capturing prompts).
- Cache hit rate improves for repeated chains with whitespace variation (test asserts this on a synthetic workload).
- `make build-all` clean.
- `go test ./...` clean, including new tests in `pkg/agent` and `pkg/console` and `pkg/utils`.
- Working tree behavior unchanged for any single-command path; manual smoke of `git status && git push` in the CLI confirms the new analysis surfaces.

## Risks & mitigations

| Risk | Mitigation |
|------|------------|
| `ParseChain` splits inside subshells incorrectly | Test cases intentionally cover `(a && b) | c` and `$(cmd && echo)`. Start with a small parser, prefer reuse of the existing tokenizer. |
| Prompt gets too long for very wide chains | Phase 2's 10-subcommand cap + per-subcommand fallback. |
| Cache key normalization collides distinct commands | Strict on operators, lenient on whitespace only. Add fuzz test asserting no false-positive collides in a corpus of N synthetic chains. |
| LLM analysis latency grows with chain length | One call, not N — wall-clock cost roughly constant. The static classifications carry most of the work. |
| Backward compat with the SP-124 cache from prior sessions | Cache entries from SP-124 sessions are keyed on raw string. Cache key versioned: SP-124 keys are invalid; SP-124b keys are written under a new prefix. |
