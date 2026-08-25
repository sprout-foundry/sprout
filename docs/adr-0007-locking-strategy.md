# ADR-0007: Locking strategy

## Status
Accepted (2026-07-01).

## Context
Sprout's pkg/ tree currently has 208 `sync.Mutex` / `sync.RWMutex` fields.
We need a single decision tree that future authors can apply mechanically
so we don't accumulate more one-off patterns.

## Decision

### When to use each primitive
1. **`sync.Mutex`** — default choice when goroutines need to read AND write
   shared state. 90% of cases.
2. **`sync.RWMutex`** — only when the protected data has a clear
   read-heavy access pattern (e.g. cache maps, registries) AND the read
   critical section is long enough that the atomic overhead of RLock
   pays for itself. Profile first; default to `sync.Mutex`.
3. **Channels** — when goroutines communicate via ownership transfer
   rather than sharing state (the Go proverb). Use for pipelines,
   fan-in/fan-out, request queues.
4. **`sync/atomic`** — for individual counters, flags, and pointers
   where the contention is on a single word. Never for multi-field
   invariants.

### Naming convention
- Default: `mu sync.Mutex`.
- Add a domain prefix ONLY when the struct has multiple mutexes or
  when the mutex guards a state domain that callers reference by name
  (e.g. `destructiveAppPrompterMu` in
  `pkg/agent_tools/computer_use/destructive_app_prompter.go`).
- For `sync.RWMutex`, same rule: `mu sync.RWMutex` unless disambiguating.

### Pattern catalog (existing mutexes classified)

`grep -rEn "sync\.(Mutex|RWMutex)" pkg/ --include="*.go" | grep -v _test.go`
returns **208** mutex fields across **130** files. Breakdown by pattern
(estimated from sampled file context):

| Pattern | Count | % | Description |
|---|---|---|---|
| **StateGuard** | ~120 | 58% | Standard state-guarding mutex around a struct's mutable fields. The default. |
| **CacheLock** | ~25 | 12% | Protects a cache map (LRU, hits/misses, token cache). Often `sync.RWMutex` for read-heavy access. |
| **OwnerQualified** | ~30 | 14% | Domain prefix encodes ownership — `submanagerXxxMu`, `debugLogMutex`, `destructiveAppPrompterMu`, etc. **Keep the prefix** per the naming rule. |
| **SingletonSwap** | ~15 | 7% | Package-level mutex guarding a singleton reference (e.g. `chordWatcherMu`, `pricingResolverMu`). |
| **PackageCache** | ~10 | 5% | Package-level mutex guarding a package-level cache map (e.g. `pkg/validation/validation.go::metadataMu`). |
| **ExternalSystem** | ~5 | 2% | Mutex guards a third-party SDK that isn't thread-safe (e.g. `pkg/mcp/manager.go::mutex` around an MCP client). |
| **IOLock** | ~3 | 2% | Guards IO state (file descriptor, response buffer). E.g. `pkg/agent_tools/computer_use/audit.go::mu` around the audit log file. |

Representative examples (most-used files):

| File | Field | Pattern | Notes |
|---|---|---|---|
| `pkg/agent/agent.go` | `mu`, `stateMu`, `interruptMu` | StateGuard / OwnerQualified | Multi-mutex struct; prefixes disambiguate. |
| `pkg/agent/submanager_state.go` | 13 fields | OwnerQualified | Per-submanager domain prefix is intentional. |
| `pkg/agent/subagent_runners.go` | `metricsMu`, `activeMu` | OwnerQualified | Domain prefix encodes which sub-system the metric guards. |
| `pkg/agent_api/token_utils.go` | `cacheMu` | CacheLock | RWMutex around the token-estimate LRU cache. |
| `pkg/agent_api/pricing_resolver.go` | `mu` | SingletonSwap | Renamed from `pricingResolverMu` in SP-099-2. |
| `pkg/agent_tools/computer_use/audit.go` | `mu` | IOLock | Guards the audit log file descriptor. |
| `pkg/agent_tools/computer_use/destructive_app_prompter.go` | `destructiveAppPrompterMu` | OwnerQualified | Domain prefix encodes ownership of the prompter's state. |
| `pkg/agent_tools/computer_use/panic_key_chord.go` | `watcherMu` | SingletonSwap | Renamed from `chordWatcherMu` in SP-099-2. |
| `pkg/embedding/manager.go` | `mu`, `cacheMu` | StateGuard / CacheLock | Two mutexes; `cacheMu` keeps the disambiguating prefix. |
| `pkg/embedding/shared_runtime.go` | `sharedONNXMu` | OwnerQualified | Multi-mutex candidate; prefix conveys shared-runtime ownership. |
| `pkg/mcp/manager.go` | `mutex`, `errMu` | ExternalSystem / StateGuard | MCP SDK + error-channel bookkeeping. |
| `pkg/providerregistry/registry.go` | `mu` | CacheLock | RWMutex around the provider cache map. |
| `pkg/validation/validation.go` | `metadataMu` | PackageCache | Renamed *out* of scope: domain prefix conveys "metadata cache". |

The catalog is sampled, not exhaustive. To re-classify, run the grep and
read each declaration's 5 lines of context.

### Migration rules
- New mutexes: prefer `sync.Mutex`; use `sync.RWMutex` only with profile data.
- Existing mutexes: do not rename unless the rename fits the
  naming-convention rule above. Bulk rename churn is forbidden.
- New packages: prefer `sync.Once` for one-shot init, atomic for counters,
  channels for ownership transfer.

## Consequences
- Future mutex additions go through this decision tree.
- Reviewers reject PRs that introduce a new domain prefix without justification.
- Profile data is required for `sync.RWMutex` over `sync.Mutex`.

## References
- Effective Go: https://go.dev/doc/effective_go#sharing
- The Go Memory Model: https://go.dev/ref/mem
- Project-internal: `pkg/agent/testing_state_isolation.go` for
  state-dir isolation hook (the canonical example of
  `SetTestStateDirHook` over a package-level function var).
