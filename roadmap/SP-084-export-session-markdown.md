# SP-084: Export Sessions to Shareable Markdown / HTML

**Status:** 📋 Spec
**Date:** 2026-06-27
**Depends on:** SP-020 (Trace/Dataset Mode — same session metadata; the training export pipeline at `pkg/training/export.go` is a related but separate concern)
**Priority:** Medium (high user value; the existing training export only emits ShareGPT/OpenAI/Alpaca JSONL for fine-tuning, not human-readable formats)
**Effort Estimate:** ~1–2 days

## Problem

A user finishes a useful sprout session — a debugging session, a refactor walk-through, a tutorial-style Q&A — and wants to share it with a colleague, paste it into a blog post, or save it as a personal note. Currently:

- `pkg/training/export.go` exports sessions as **ShareGPT / OpenAI fine-tuning / Alpaca JSONL**. These are *training-data* formats, not human-readable.
- There is no way to export a session as Markdown, HTML, or plain text.
- Copy-paste from the WebUI loses structure (tool calls collapse into the assistant message, timestamps are dropped, code blocks lose their fences).

This is a recurring workflow: the agent produces something useful, the user wants to keep it outside sprout. Forcing them to manually clean up a copy-paste is friction.

## Goals

1. CLI: `sprout export <session-id> [--format markdown|html|json] [--output <path>]`.
2. CLI: `sprout export --latest` — convenience flag for the most recent session in the current working directory.
3. CLI: `sprout export --all` — export every session in the current working directory to a directory tree.
4. WebUI: Session-detail view gains an "Export" button with the same format options.
5. Markdown output is GitHub-flavored, renders cleanly in any Markdown viewer (GitHub, VS Code preview, Obsidian).
6. HTML output is a self-contained single file with embedded CSS — opens cleanly in any browser, no asset dependencies.

## Design

### Output formats

| Format | Use case | Structure |
|--------|----------|-----------|
| `markdown` (default) | Sharing in chat, blog post, git repo | GFM with fenced code blocks, table of contents, optional metadata header |
| `html` | Email attachment, archive | Self-contained single file, embedded CSS, collapsible tool-call sections |
| `json` | Programmatic consumption; lossless round-trip | Full session JSON with `sprout_session_export` schema wrapper |

### Markdown structure

```markdown
---
session_id: 1778897738
name: "Migrate embedding index"
working_directory: /home/u/proj
last_updated: 2026-06-15T10:30:00-05:00
total_cost: 0.42
total_tokens: 12345
turns: 18
tools_used: [edit_file, shell_command, fetch_url]
---

# Migrate embedding index

> Session started 2026-06-15. 18 turns, 12,345 tokens, $0.42.

## Table of contents
- [Turn 1](#turn-1)
- [Turn 2](#turn-2)
- ...

---

<a id="turn-1"></a>
### Turn 1 — 2026-06-15T10:30:12-05:00

**User:**

> Help me migrate the embedding index after the schema change.

**Assistant:**

I'll start by inspecting the current schema.

<details>
<summary>Tool call: <code>read_file</code></summary>

```json
{"path": "pkg/embedding/schema.go"}
```

</details>

Then in `pkg/embedding/schema.go` I see...
```

Code blocks fenced with the language detected by content (Go/TS/Python/shell) where possible, otherwise `text`. Image references preserved as `![alt](path)`. Tool calls collapsed into `<details>` blocks to keep the conversation readable.

### HTML structure

Same content as Markdown, wrapped in a self-contained HTML document:

- Embedded CSS (`<style>` block) with the sprout design tokens for theming consistency.
- Collapsible tool-call sections (`<details>`/`<summary>`).
- Sticky table of contents in a sidebar.
- One file, no external assets.

### CLI

```
$ sprout export --latest --output ./sessions.md
Exported session 1778897738 ("Migrate embedding index") to ./sessions.md (18 turns, 4.2 KB).

$ sprout export --all --output ./exports/
Exported 14 sessions to ./exports/:
  - migrate-embedding-2026-06-15.md
  - fix-onnx-2026-06-12.md
  - ...

$ sprout export 1778897738 --format html --output ./session.html
Exported session 1778897738 to ./session.html (28 KB, self-contained).
```

Flags:
- `--format <markdown|html|json>` — output format (default: markdown)
- `--output <path>` — file path (for single-session export) or directory (for `--all`)
- `--latest` — most recent session in current working directory
- `--all` — every session in current working directory
- `--include-tool-calls` (default) / `--no-tool-calls` — collapse tool calls into summaries
- `--include-cost` (default) / `--no-cost` — include cost/tokens metadata
- `--secret-redaction` (default) — redact API keys / tokens (use `pkg/secretdetect`)

### WebUI

The session-detail view (where the user is currently reading a conversation) gets an "Export" button in the header. Clicking opens a small dialog with format radio buttons + an "Export" button that calls `/api/sessions/<id>/export?format=markdown` and triggers a browser download.

For batch: the session list sidebar gains a "Export all" link that exports every session in the working directory as a zip of markdown files.

### Implementation

- `pkg/export/session_export.go` (new) — core export logic. Pure functions: `ExportMarkdown(session) string`, `ExportHTML(session) string`, `ExportJSON(session) []byte`.
- `cmd/export.go` (new) — CLI subcommand using the core.
- `pkg/webui/export_api.go` (new) — HTTP handler serving the WebUI downloads.
- Reuses `pkg/secretdetect` for redaction (same as the existing training export).

### Tests

- `pkg/export/session_export_test.go`: round-trip — parse exported Markdown back into messages and verify equality. Verify HTML is self-contained (no external `<link>`/`<script>` references). Verify JSON is lossless.
- `cmd/export_test.go`: CLI flag parsing, `--latest` resolves the right session, `--all` enumerates correctly.
- `pkg/webui/export_api_test.go`: HTTP contract, format query param, content-type, content-disposition.
- `webui/src/components/ExportDialog.test.tsx`: format selection, button enables only on selection, download triggers correctly.

### Phase plan

| Phase | Scope |
|-------|-------|
| 1 | `pkg/export/` core — Markdown + HTML + JSON, round-trip tests, secret redaction. |
| 2 | `cmd/export.go` CLI subcommand with flag parsing. |
| 3 | `pkg/webui/export_api.go` HTTP endpoint. |
| 4 | WebUI Export button + dialog. |

## Success Criteria

- `sprout export --latest` produces a Markdown file that renders correctly on GitHub and in `glow`/`mdcat`.
- `sprout export --latest --format html` produces a self-contained HTML file that opens in any browser with no external asset requests.
- `sprout export --latest --format json` round-trips: parsing the JSON reproduces the original session.
- Exported files have API keys / tokens redacted by default (`pkg/secretdetect` integration).
- WebUI Export button downloads the file in the chosen format.
- All tests green; `make build-all` clean.

## Risks

- **Markdown rendering differences** between viewers. Mitigation: stick to GFM core features; document any platform-specific quirks in the spec's testing notes.
- **HTML size** — long sessions can produce HTML files >1MB. Mitigation: the file is gzipped at HTTP layer; the CLI output is plain. Add a `--max-turns <N>` flag for the rare case a user wants to truncate.
- **Secret redaction false negatives** — `pkg/secretdetect` isn't perfect. Mitigation: it's the same redactor used by the existing training export; matching that baseline is acceptable for v1.

## Open Questions

1. Should `sprout export` be a top-level subcommand (`sprout export ...`) or under `sprout session export ...`? **Recommendation:** top-level — matches `sprout sessions` (which lists) by analogy; shorter to type for a frequent operation.
2. Should HTML support a `--theme dark|light|auto` flag? **Recommendation:** yes, default `auto` (respects `prefers-color-scheme`). The dark theme uses the existing `--bg-primary`/`--text-primary` tokens from the design system.