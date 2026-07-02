# SP-084: Export Sessions to Shareable Markdown / HTML

**Status:** ✅ Implemented (2026-06-30; sprout export CLI + WebUI export button)

Users needed a way to export completed sprout sessions as human-readable formats for sharing, archiving, or blogging. The existing training export only produced ShareGPT/OpenAI/Alpaca JSONL. This spec shipped `sprout export` as a top-level CLI command supporting Markdown (GFM), self-contained HTML, and lossless JSON formats, plus a WebUI export button in the session-detail view. API keys are redacted by default using the existing `pkg/secretdetect` integration.

## Key decisions

- `sprout export` is a top-level subcommand (not nested under `sprout session export`) — matches `sprout sessions` by analogy and is shorter to type for a frequent operation.
- Markdown output uses GFM with tool calls collapsed into `<details>` blocks to keep the conversation readable while preserving full context.
- HTML output is a single self-contained file with embedded CSS — no external asset dependencies, opens cleanly in any browser.
- Secret redaction is on by default using `pkg/secretdetect` (same redactor as the training export pipeline).
- `--latest` and `--all` flags provide convenience for common workflows (export most recent session, or export all sessions in a directory tree).

## Artifacts

- code: `pkg/export/session_export.go` — core export logic: `ExportMarkdown`, `ExportHTML`, `ExportJSON`
- code: `cmd/export.go` — CLI subcommand with flag parsing (`--format`, `--output`, `--latest`, `--all`)
- code: `pkg/webui/export_api.go` — HTTP handler serving WebUI downloads
- tests: `pkg/export/session_export_test.go` — round-trip tests for all three formats

Full specification archived — see git history for original content.
