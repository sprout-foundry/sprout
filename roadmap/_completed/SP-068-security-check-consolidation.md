# SP-068: Security Check Consolidation

**Status:** ✅ Implemented (2026-06-09; single resolver, single broker, sprout explain)

A single tool call was judged by two independent risk taxonomies running in sequence (static classifier: SAFE/CAUTION/DANGEROUS; persona cascade: Low/Medium/High/Critical), plus four special-case gates and a workspace policy. This produced double prompts, reasoning hazards, and no single answer to "why was this gated?" This spec consolidated everything into one risk scale, one resolver, and one approval broker. The anti-double-prompt plumbing (`HasUserApproval`, `consumeShellCommandApproval`) was deleted.

## Key decisions

- Canonical risk scale is `Low / Medium / High / Critical` (the richer of the two existing scales). Static classifier maps: SAFE→Low, CAUTION→Medium, DANGEROUS→High, critical-op→Critical.
- `ResolveToolRisk` runs all inputs (classifier, persona, git-gate, fs-tier, workspace policy) and takes the most restrictive result — one function answers "what happens if the agent runs X?"
- Phase 2 shipped behind a `unified_risk_resolver` config flag (default off) for one release with shadow-mode logging comparing old-vs-new decisions before flipping the default.
- `ApprovalBroker.Request(assessment)` owns surface selection (WebUI vs CLI), timeout, fallback, and 4-option outcome — Gate 1 and Gate 2 call sites collapse to one broker call.
- `sprout explain '<command>'` prints the full `RiskAssessment` with canonical level, contributing sources, and the exact rule that set the level.

## Artifacts

- code: `pkg/agent/risk_assessment.go` — `RiskAssessment` value with canonical level, sources, reason string, `IsHardBlock` flag
- code: `pkg/agent/risk_prompt.go` — unified resolver and approval broker
- code: `pkg/agent/tool_security.go` — consolidated tool security checks
- code: `pkg/agent_tools/security_classifier.go` — static classifier (now feeds into unified resolver)
- tests: `pkg/agent/approval_broker_test.go` — broker behavior and regression tests

Full specification archived — see git history for original content.
