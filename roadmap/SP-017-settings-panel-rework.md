# SP-017: Settings Panel Rework — Scoped Collapsible Sections

**Status:** ✅ Partially Implemented (scoped labels shipped; collapsible sections pending → see SP-101)
**Depends on:** SP-003 (Webui & Frontend Architecture), SP-013 (Agent Settings Management)  
**Priority:** Medium  
**Effort Estimate:** ~1 week

## Problem

The settings panel has 11 flat tabs with no grouping or hierarchy. Settings span four distinct scopes — session, workspace, global, and runtime — but the UI treats everything identically. A hidden layer picker exists but is poorly surfaced and most tabs ignore it.

This creates three specific problems:

1. **Scope confusion.** Users change "reasoning effort" (session-scoped) right next to "provider priority" (global-scoped) with no indication of where the value is stored or who it affects.

2. **Tab sprawl.** Several tabs are trivially thin — Security (2 fields), Performance (5 fields), OCR (3 fields), Commit & Review (4 fields). Each is a separate tab for 2–5 fields.

3. **Provider/model repetition.** Provider and model selection is meaningful at every scope (global default, workspace override, session override) but the UI only exposes it in one place.

## Current State

### Tab Inventory

| Tab | Fields | Actual Scope | Problems |
|-----|--------|-------------|----------|
| General | reasoning_effort, disable_thinking, skip_prompt, pre_write_validation, history_scope, system_prompt, editor prefs | Mixed: session + runtime | Editor prefs aren't config fields at all. Dumping ground. |
| Security | allow_orchestrator_git_write | Session | 1 field — too thin for a tab |
| Credentials | API keys (credential store) | Global | Correct, separate. Keep as-is. |
| Perf | api_timeouts (5 sub-fields) | Global | 5 fields — too thin for a tab |
| Subagents | provider/model, type catalog, parallel controls | Session overrides + global types | Complex enough to stay separate |
| Commit & Review | commit_provider/model, review_provider/model | Session overrides | 4 fields — too thin |
| OCR | pdf_ocr_enabled, pdf_ocr_provider, pdf_ocr_model | Global | 3 fields — too thin |
| MCP | servers, credentials, auto-start | Workspace | Complex enough to stay separate |
| Providers | custom providers, models, priority | Global | Complex enough to stay separate |
| Skills | skill catalog | Global | Stays separate |
| Embeddings | index status + config | Workspace | New, stays separate |

### Layer System

The backend already supports three config layers merged in priority order:

```
Global (~/.config/sprout/config.json)
  └── Workspace (.sprout/config.json)
        └── Session (in-memory overlay)
```

The UI has a small button row to switch between layers, but:
- Most users don't notice it
- Most tabs don't respect it
- It requires the user to understand the layer concept instead of the UI just writing to the right place

## Proposed Solution

### Replace 11 flat tabs with 4 collapsible sections

Each section is tied to a specific config scope. The UI writes to the correct layer automatically based on which section the user is editing. The layer picker is removed — scope is determined by *where you edit*, not by a separate toggle.

```
┌─────────────────────────────────────────────────┐
│  Settings                                 [X]   │
├─────────────────────────────────────────────────┤
│                                                 │
│  ▼ Agent                         [Session]      │
│    Provider & Model    (session override)        │
│    Behavior            (reasoning, prompts)      │
│    Subagents           (subagent config)         │
│    Skills              (skill catalog)            │
│                                                 │
│  ▼ Workspace                    [Workspace]      │
│    Provider & Model    (workspace default)        │
│    Embeddings          (index status + config)   │
│    MCP                 (servers, credentials)    │
│                                                 │
│  ▼ Environment                  [Global]         │
│    Provider & Model    (global default)           │
│    Provider Catalog    (custom providers, models) │
│    Commit & Review     (commit/review provider)   │
│    Performance         (API timeouts)             │
│    OCR                 (PDF config)               │
│                                                 │
│  ▼ Editor                       [Runtime]        │
│    Preferences         (auto-save, whitespace)    │
│    Keybindings         (hotkey config)             │
│                                                 │
├─────────────────────────────────────────────────┤
│  [Manage Credentials →]  (separate panel)        │
└─────────────────────────────────────────────────┘
```

### Section Definitions

#### 1. Agent (session scope)

Everything that controls how the agent behaves in *this session*. Writes to the session overlay.

| Subsection | Fields | Source |
|-----------|--------|--------|
| Provider & Model | `last_used_provider`, provider model override | New — currently only in chat input dropdown |
| Behavior | `reasoning_effort`, `disable_thinking`, `skip_prompt`, `enable_pre_write_validation`, `history_scope`, `system_prompt_text`, `allow_orchestrator_git_write` | Merges General + Security tabs |
| Subagents | `subagent_provider`, `subagent_model`, `subagent_types`, `subagent_max_parallel`, `subagent_parallel_enabled` | Keeps existing SubagentSettingsTab |
| Skills | `skills` catalog | Keeps existing SkillsSettingsTab |

#### 2. Workspace (workspace scope)

Everything tied to the *current project directory*. Writes to `.sprout/config.json` in the project root.

| Subsection | Fields | Source |
|-----------|--------|--------|
| Provider & Model | workspace-level provider/model default | New — currently only via manual config edit |
| Embeddings | `embedding_index.*` + live status + rebuild | Keeps existing EmbeddingSettingsTab |
| MCP | `mcp.*` servers, credentials, auto-start | Keeps existing MCPSettingsTab |

#### 3. Environment (global scope)

Infrastructure configuration. Writes to `~/.config/sprout/config.json`.

| Subsection | Fields | Source |
|-----------|--------|--------|
| Provider & Model | `last_used_provider` default, `provider_models`, `provider_priority` | Extracts from Providers tab |
| Provider Catalog | `custom_providers` | Keeps existing ProviderSettingsTab |
| Commit & Review | `commit_provider`, `commit_model`, `review_provider`, `review_model` | Merges CommitReviewSettingsTab |
| Performance | `api_timeouts.*` | Merges PerformanceSettingsTab |
| OCR | `pdf_ocr_enabled`, `pdf_ocr_provider`, `pdf_ocr_model` | Merges OcrSettingsTab |

#### 4. Editor (runtime scope)

UI preferences stored in localStorage, not in config.json. These never merge across layers.

| Subsection | Fields | Source |
|-----------|--------|--------|
| Preferences | auto-save, format-on-save, whitespace rendering | Extracts from GeneralSettingsTab |
| Keybindings | hotkey config | New — API already exists |

### Credentials — Separate Panel

The credentials panel manages a credential store (encrypted API keys), not config fields. It has a different data model and different UX patterns. It stays as a separate top-level button/link below the sections, not inside any collapsible section.

### Inherited Value Display

Each subsection field shows where its current value comes from:

- **Own value**: normal display (user set it at this layer)
- **Inherited value**: show the resolved value with a subtle "inherited from Global" or "inherited from Workspace" indicator, plus an "Override" button that clears the field for editing

Example in the Agent > Provider & Model subsection:
```
Provider:  openrouter         ← inherited from Workspace
Model:     gpt-5              ← overridden for this session
[Reset to workspace default]
```

This teaches users the layer model through use rather than through a separate toggle.

### Scope Badge

Each section header shows its scope as a badge:

```
▼ Agent                    Session
▼ Workspace               Workspace
▼ Environment              Global
▼ Editor                   Runtime
```

The badge uses distinct colors:
- Session: blue (ephemeral)
- Workspace: green (project-specific)
- Global: orange (affects everything)
- Runtime: gray (UI-only, not persisted)

## Implementation Steps

### Step 1: Data Model

Replace `SUB_TABS` in `types.ts` with a section-grouped structure:

```typescript
export interface SettingsSection {
  id: string;
  label: string;
  scope: 'session' | 'workspace' | 'global' | 'runtime';
  defaultExpanded?: boolean;
  subsections: SettingsSubsection[];
}

export interface SettingsSubsection {
  id: string;
  label: string;
  /** React component name to render (resolved in SettingsPanel) */
  component: string;
}
```

### Step 2: Navigation Rewrite

Replace the tab bar in `SettingsPanel.tsx` with collapsible section headers. Each header toggles its subsections. Multiple sections can be open simultaneously (accordion mode optional — default all collapsed except Agent).

### Step 3: Component Wiring

All existing tab components stay unchanged — they render subsection content. The only change is *where* they render (inside a section instead of as a standalone tab).

Tab components that merge into parent sections:
- `SecuritySettingsTab.tsx` → content moves to Agent > Behavior subsection
- `PerformanceSettingsTab.tsx` → content moves to Environment > Performance subsection
- `OcrSettingsTab.tsx` → content moves to Environment > OCR subsection
- `CommitReviewSettingsTab.tsx` → content moves to Environment > Commit & Review subsection

These files can either be deleted (content inlined) or kept as subsection renderers.

### Step 4: Provider & Model Per Section

Add a new `ProviderModelSubsection` component used in all three scoped sections (Agent, Workspace, Environment). Each instance reads/writes to its own layer:

- **Agent (session)**: overrides for the current conversation
- **Workspace**: project-level defaults in `.sprout/config.json`
- **Environment**: global defaults in `~/.config/sprout/config.json`

Show inherited values when not overridden at the current layer.

### Step 5: Remove Layer Picker

Delete the session/workspace/global toggle buttons. The scope is now determined by which section the user is editing. This simplifies the UI and eliminates confusion.

### Step 6: CSS

Add collapsible section styles to `SettingsPanel.css`:
- Section headers with chevron toggle + scope badge
- Smooth expand/collapse animation
- Subsection indentation
- Inherited value styling (subtle background tint + "inherited" label)

## Files Changed

| File | Action |
|------|--------|
| `webui/src/components/settings/types.ts` | Replace SUB_TABS with SECTION_GROUPS structure |
| `webui/src/components/SettingsPanel.tsx` | Rewrite navigation — collapsible sections, remove tab bar and layer picker |
| `webui/src/components/SettingsPanel.css` | Add section/subsection styles, collapse animations, scope badges |
| `webui/src/components/settings/ProviderModelSubsection.tsx` | **New** — reusable provider/model picker per scope |
| `webui/src/components/settings/SecuritySettingsTab.tsx` | Content merges into Agent > Behavior; file deleted or kept as renderer |
| `webui/src/components/settings/PerformanceSettingsTab.tsx` | Content merges into Environment > Performance |
| `webui/src/components/settings/OcrSettingsTab.tsx` | Content merges into Environment > OCR |
| `webui/src/components/settings/CommitReviewSettingsTab.tsx` | Content merges into Environment > Commit & Review |
| `webui/src/components/settings/GeneralSettingsTab.tsx` | Split: editor prefs → Editor section, behavior → Agent section |
| All other tab components | No changes — rendered as subsections |

## Success Criteria

| Criterion | Target |
|-----------|--------|
| Navigation | 4 collapsible sections replace 11 flat tabs |
| Scope clarity | Every section shows its scope badge; every field shows inheritance |
| Provider/model | Available at all 3 scopes (session, workspace, global) |
| Layer picker | Removed — scope determined by section, not toggle |
| Credentials | Accessible but separate from config sections |
| Build | `make build-all` passes |
| No regressions | All existing config fields remain editable |

## Migration Notes

- No backend changes required — the config API (`GET/PUT /api/settings`) and layer merge system remain the same
- No config file format changes — existing `config.json` files are compatible
- The credential store API is unchanged
- This is purely a frontend navigation/UX restructuring
