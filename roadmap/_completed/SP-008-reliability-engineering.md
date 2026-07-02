# SP-008: Reliability Engineering — Concurrency & Observability

**Status:** ✅ Shipped (Tracks A + B complete 2026-06)

Two reliability tracks that grew out of an audit showing sprout's runtime
correctness depended on convention rather than enforcement. Track A
(Concurrency) added channel-pattern guidelines, a locking audit, and a
default `-race` mode for the test suite so any future regression surfaces
immediately. Track B (Observability) introduced typed errors
(`pkg/errors/types.go`: `TransientError`, `RateLimitError`,
`SecurityViolationError`, `ContextOverflowError`, `AuthError`), structured
logging (`pkg/logging/structured.go`), a `fmt.Printf` audit-and-replace
pass, and a unified retry classification (`ClassifyError` + `RetryAction`).
Followup work that builds on this (deeper observability, SP-094/SP-099) is
tracked as separate specs.

## Key decisions

- **`-race` is now the default** in `make test-unit`, not opt-in. The cost
  (~2x test runtime) is worth catching regressions at write time.
- **Typed errors at boundaries, raw errors at source.** Wrap with
  `fmt.Errorf("...: %w", err)` at handler boundaries so callers can
  `errors.As`; internal helpers return raw errors.
- **`ClassifyError` lives at the agent boundary, not the provider layer.**
  Providers return domain errors; the agent decides whether to retry,
  surface to the user, or abort.
- **Structured logging uses a `LogContext` interface**, not a global logger,
  so contexts (request ID, chat ID) thread through naturally.

## Artifacts

- code: `pkg/errors/types.go` — typed error hierarchy
- code: `pkg/logging/structured.go` — structured logger
- code: `pkg/agent/retry.go` — `ClassifyError`, `RetryAction`, `handleToolError`
- tests: `pkg/agent/concurrency_test.go` — race detector coverage
- build: `make test-unit` runs with `-race` by default (commit `f5fe3dd7`)

Full specification archived — see git history for original content.