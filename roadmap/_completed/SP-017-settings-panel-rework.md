# SP-017: Settings Panel Rework — Scoped Collapsible Sections

**Status:** ✅ Partially Implemented (4 collapsible sections + scoped labels shipped; provider/model-per-scope deferred → SP-101)

The settings panel had 11 flat tabs with no grouping or scope indication. Settings span four distinct scopes (session, workspace, global, runtime) but the UI treated everything identically. This spec replaced the flat tabs with 4 collapsible sections (Agent/Session, Workspace, Environment/Global, Editor/Runtime), each showing its scope as a colored badge. Thin tabs (Security, Performance, OCR, Commit & Review) were merged into parent sections. A layer picker was removed — scope is determined by which section the user edits, not by a separate toggle. Provider/model selection at all three scopes (session, workspace, global) with inherited value display was deferred to SP-101.

## Status reconciliation (2026-07-05)

Audited during CLI-UX + WebUI gap audit. `webui/src/components/settings/ProviderModelSubsection.tsx` exists as a standalone component but is not imported into `SettingsPanel.tsx` (verified by grep). The component lives, the wiring does not. Genuine ~0.25 day work to finish the SP-101-Phase 3 deferred piece.

## Key decisions

- 4 collapsible sections replace 11 flat tabs — scope is implicit from section, not explicit from a layer toggle.
- Thin tabs (Security, Performance, OCR, Commit & Review) merged into parent sections rather than kept as standalone tabs.
- Credentials kept as a separate panel (different data model — encrypted API keys, not config fields).
- Provider/model per scope deferred to SP-101 (requires `ProviderModelSubsection` component and inherited value display).
- No backend changes needed — config API and layer merge system remain unchanged.

## Artifacts

- code: `webui/src/components/settings/types.ts` — section-grouped structure replacing `SUB_TABS`
- code: `webui/src/components/SettingsPanel.tsx` — collapsible section navigation, scope badges
- code: `webui/src/components/settings/ProviderModelSubsection.tsx` — reusable provider/model picker per scope
- tests: existing config round-trip tests cover the unchanged backend API

Full specification archived — see git history for original content.
