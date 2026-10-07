# SP-152 — Outbound Language Guard

> **Status (2026-10-07):** Shipped.
> Related: SP-150 (per-role metrics), SP-151 (summaries are checked too).

## Problem

Open models occasionally reply in the wrong language: a Chinese or
English paragraph in an otherwise Spanish conversation, for example. The
answer may be correct, but it reads as broken and costs a retry. Nothing
checks outbound text today.

## Design

### 152a. Check every user-facing message

Before model-authored text is shown, identify its language and compare
it with the user's language:

- User language: the language the user is writing in, inferred from
  their own recent messages (same detector, majority over recent turns).
  For very short or mixed input, fall back to the configured language
  setting (account or config). There is no fixed list of supported
  languages: any language the detector recognizes is a valid target.
- Code blocks, inline code, URLs, file paths and quoted user text are
  stripped before detection; they are not prose.
- Messages below a length threshold are not judged (detection is
  unreliable on a few words).

### 152b. On mismatch

- The message is not displayed.
- Regenerate once with an explicit instruction to answer in the user's
  language. If it still mismatches, show a short templated notice in the
  user's language ("The reply came back in the wrong language — retry?")
  with the option to view the original.
- Images, screenshots and rendered previews pass through unchanged.

### 152c. Streaming

Replies stream token by token, so a wrong-language reply would be
visible before a whole-message check runs. The guard holds back the
start of each streamed reply until it has enough prose to judge (a few
dozen words, code excluded), checks it, then releases the buffer and
streams the rest live. A reply that switches language mid-stream is
checked again at completion and replaced if it fails.

### 152d. Detector

Deterministic and in-process: no external service or model call, so it
adds no latency, cost or new failure mode. It runs wherever the outbound
message is produced:

- **CLI:** in the local sprout process.
- **Hosted workspaces:** in the sprout process inside the workspace,
  before events leave it.
- **WASM build:** compiled into the browser build. Both passes must be
  pure Go with no cgo; the binary-size check covers the WASM artifact as
  well as native binaries.

Two passes:

- Unicode script detection (standard library `unicode` range tables)
  catches the common failure, wrong script entirely, cheaply and exactly.
- For same-script languages, a small trigram detector with no network or
  model download. `github.com/abadojack/whatlanggo` fits (small, pure Go,
  compiles to WASM). `github.com/pemistahl/lingua-go` is more accurate on
  short text but carries large language models, which rules it out for
  the WASM build unless restricted to a few languages. Neither is a
  dependency today; choose with a binary-size measurement.

The detector is a standalone package so other Go programs embedding
sprout can call the same check.

**WASM size impact (measured, item 152.8):** the language guard (script
pass + `whatlanggo` trigram pass, both pure Go, no cgo) adds 558,352
bytes (≈545 KB, ~0.97 %) to the stripped `sprout.wasm` artifact:
57,408,813 → 57,967,165 bytes (54.7 → 55.2 MB). `make test-wasm`
confirms the detector compiles and runs under `GOOS=js GOARCH=wasm`,
and the guard runs on the browser build's outbound path
(`cmd/wasm` → `ProcessQuery` → final-message guard + streaming
hold-back). Both the baseline and post-guard artifacts exceed the build
script's 50 MB threshold — a pre-existing warning, not introduced by
the guard.

### 152f. Default

On by default everywhere, including the CLI. A config setting turns it
off. The cost is one in-process check per reply and, rarely, one
regeneration.

### 152e. Metrics

Every mismatch is logged with model ID and role (SP-150); a per-model
mismatch rate is available in diagnostics.

## Acceptance criteria

- [ ] Detector package with table tests: correct language for prose
      across a broad sample of languages and scripts; code and URLs
      ignored; short text not judged; short/mixed input falls back to the
      configured language.
- [ ] Scripted wrong-language reply: not displayed, regenerated, final
      text matches the user's language.
- [ ] Streaming hold-back test: a wrong-language stream never reaches
      the client.
- [x] WASM build includes the detector; size impact recorded in the PR.
- [ ] Mismatch metric recorded per model and role.
- [ ] On by default in the CLI and web UI; the config setting disables it.

## Non-goals

- No translation of user input or of model output.
- No content filtering; language only.

## Open questions

- Resolved: no fixed language list; the target is the user's own writing
  language with a configured fallback (152a).
- Resolved: on by default everywhere, configurable off (152f).
- Length threshold for judging a message, tuned from the detector tests.
