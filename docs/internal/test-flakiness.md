# Test Flakiness Policy

A test that can fail on a clean checkout with no local changes is flaky.
Flaky tests burn CI hours: a 20-minute package timeout, a failed release
run, and a re-push cycle for a bug that lives in the test, not the code.

**Rule: fix the root cause. Re-running until green is how flakes ship.**
If a flake cannot be fixed in the current session, quarantine it
(`t.Skipf` with the reason + a roadmap entry) and keep it out of CI.

## Rules for new and changed tests

1. **No unbounded waits.** Every blocking syscall in test code or code
   under test needs a deadline. Poll-based loops must verify the event
   bits before a blocking read, and reads on borrowed file descriptors
   run with `O_NONBLOCK` for the read window (restored on exit) so they
   return `EAGAIN` instead of parking. A read that can block for the
   full `go test` timeout is a bug in the code under test, not a flake.
2. **No live network in unit tests.** No public APIs — including
   keyless ones (the DuckDuckGo fallback needs no credentials and still
   counts). Use `httptest` doubles or skip behind `SKIP_NETWORK_TESTS`.
   Filesystem result caches (`search_cache`, `url_cache`) are shared
   mutable state: unit tests must not read or depend on them.
3. **Close what you open.** Every pipe, pty, and file a test creates
   gets closed on all ends from `t.Cleanup`. A leaked reader goroutine
   won't fail the test, but it pollutes every later timeout dump and
   hides the real hang.
4. **Contract tests repeat.** A test that pins timing or deadline
   behavior must run its case in a tight loop (10+ iterations, total
   under ~1s). Single-pass timing tests cannot shake out
   kernel-timer-boundary races, and those races are platform-specific
   (macOS and Linux `poll` semantics differ).
5. **Assert against state you control.** Cross-call consistency
   assertions across live I/O (call 1 failed, call 2 hit the cache)
   are coin flips. Stub the interface, assert the handler's behavior
   against the stub.

## Pattern catalog — mechanisms that have burned CI hours

- **`ppoll` timeout quirk.** Linux `ppoll` can return -1/errno=0 when
  the timeout expires; x/sys surfaces that as `n=-1, err=nil`. A loop
  that only checks `n == 0` and `err != nil` falls through to a
  blocking read on a silent fd → the whole package runs to the 20-minute
  timeout. Guards: `continue` on `n < 0`; verify `Revents & POLLIN`
  before `Read`; `O_NONBLOCK` for the read window.
- **Network + shared cache.** A search path hitting a live API plus a
  per-query JSON cache: transient failure on call 1, success on call 2
  (which writes the cache), and a cross-call assertion on the first
  call's error → deterministic failure. Fix: stub the engine; assert
  the handler's mapping (result → pass-through, error → `IsError` +
  message).
- **Reader goroutine leaks.** A goroutine parked in `ReadString` on an
  unclosed pipe cannot be unblocked by a cancel channel — it never
  reaches the `select`. Close the write end in test cleanup; the read
  returns EOF and the goroutine exits.
- **Environment-dependent expectations.** A test that asserts on what
  happens "without an API key" is asserting on the runner's network
  state. If the assertion's truth value depends on an external service
  answering (or failing), the test belongs behind a skip or a stub.

## Diagnosing a CI hang

1. Read the `go test` timeout dump first — it names the running test
   and shows every parked goroutine's stack.
2. A parked `unix.Read`/`syscall.Read` = a missing deadline or a poll
   with no event check. A parked `io.(*pipe).read` = an unclosed pipe
   from an earlier test in the same binary (tests share one process).
3. Green on macOS, hung on Linux (or the reverse): suspect a
   platform-specific syscall quirk, not a logic change.
