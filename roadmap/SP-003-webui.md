# SP-003: Webui & Frontend Architecture

**Status:** ✅ Active (under active development)  
**Location:** `webui/src/`, `packages/ui/`  
**Size:** ~106K lines TypeScript/React  
**Stack:** React 18, TypeScript, CodeMirror 6, Vite, CRA (legacy)

## Current State

 dual-mode web application running the sprout agent with a rich IDE-like interface. Supports **local mode** (Go backend on localhost) and **cloud mode** (CloudAdapter to Foundry platform). Recently began extracting components into `@sprout/ui` package.

## Architecture

### Dual Mode System

```typescript
// webui/src/config/mode.ts
export const mode: SproutMode =
  process.env.REACT_APP_SPROUT_MODE === 'cloud' ? 'cloud' : 'local';

// Feature flags — adapter overrides build-time defaults
export const isCloud: boolean           // mode === 'cloud'
export const supportsSSH: boolean      // adapter?.supportsSSH ?? true  (local-only)
export const supportsInstances: boolean // adapter?.supportsSSH ?? isCloud
export const supportsLocalTerminal: boolean // adapter?.allows ?? !isCloud
export const supportsSettings: boolean  // adapter?.allows ?? !isCloud
```

### API Adapter Pattern

```typescript
interface APIAdapter {
  name: string
  fetch(input, init): Promise<Response>
  getWebSocketURL(): string | null
  requiresBackendHealthCheck: boolean
  fileOpsViaAPI: boolean
  showOnboarding: boolean
  supportsSSH: boolean
  supportsInstances: boolean
  supportsLocalTerminal: boolean
  supportsSettings: boolean
  platformNavItems?: PlatformNavItem[]
}
```

- **Local mode:** No adapter installed; `clientFetch` talks to Go backend directly
- **Cloud mode:** `CloudAdapter` installed; routes `/api/*` to Foundry platform, WASM handles files locally

### Component Hierarchy (key components)

```
App.tsx (2376 lines)
├── AppContent.tsx (1275 lines)
│   ├── EditorPane.tsx (2604 lines) — CodeMirror 6 with extensions
│   ├── ContextPanel.tsx (1827 lines) — Chat + Tools + Git sidebar
│   │   ├── Chat.tsx
│   │   ├── GitSidebarPanel.tsx
│   │   ├── SearchView.tsx
│   │   └── ReviewWorkspaceTab.tsx
│   ├── LocationSwitcher.tsx (1883 lines) — SSH, instances, file browser
│   ├── FileTree.tsx (1599 lines) — File system browser
│   ├── Terminal.tsx — xterm.js terminal
│   └── StatusBar.tsx — Cursor position, git branch, encoding
├── Sidebar.tsx — Navigation, settings access, platform nav
├── SettingsPanel.tsx (2019 lines) — Global/Workspace/Session config
├── EditorManagerContext.tsx — Buffer/pane/split management
└── CommandPalette.tsx — Fuzzy command palette
```

### State Management

- **No external state library** — pure React context + hooks
- **EditorManagerContext:** Buffer persistence, pane management, split layouts
- **useWebSocketEvents:** Central event handler bridging agent events → React state
- **Chat sessions:** Per-chat agent instances with independent state

### Event System

WebSocket events from Go backend → `useWebSocketEvents` hook → React state updates:

| Event | Purpose |
|-------|---------|
| `agent_message` | Streaming assistant response |
| `tool_start` / `tool_end` | Tool execution tracking |
| `subagent_activity` | Subagent output streaming |
| `agent_metrics` | Token/cost updates |
| `git_status` | Git status refresh |

### @sprout/ui Package (`packages/ui/`)

Recently extracted as a buildable npm library:

- **Vite library mode** with ESM + CJS output
- **Leaf components** extracted: StatusBar (improved with slot API), NotificationStack (props-based), NotificationItem (CSS animation fix)
- **Stub components:** Editor, Terminal, FileTree, GitPanel, ChatPanel, Sidebar, CommandPalette (types only, implementations removed)
- **Goal:** Reusable component library for both local webui and cloud Foundry frontend

### Cloud Integration

- **CloudAdapter:** Routes API calls to Foundry platform backend
- **WASM shell:** In-browser terminal + file ops for cloud mode (no Go backend needed)
- **Synthetic responses:** Returns empty/synthetic data for cloud-inapplicable endpoints
- **Chat proxy:** Translates webui chat format → Foundry chat format
- **Git proxy:** Rewrites `/api/git/*` → `/api/proxy/git/*`
- **Platform nav:** Tasks, billing, team pages (cloud-only routes)

## Large Files Needing Attention

| File | Lines | Concern |
|------|-------|---------|
| `EditorPane.tsx` | 2604 | Well above 500-line target |
| `App.tsx` | 2376 | Above target |
| `SettingsPanel.tsx` | 2019 | Above target |
| `LocationSwitcher.tsx` | 1883 | Above target |
| `ContextPanel.tsx` | 1827 | Above target |

## Open Work (from TODO.md)

- CLOUD-ADAPTER: Phase 4 items remaining — SproutProvider context, EditorManagerContext extraction, ApiService decomposition, component prop-based refactoring

## Key Files

| File | Purpose |
|------|---------|
| `webui/src/App.tsx` | Root component, mode detection, adapter installation |
| `webui/src/config/mode.ts` | Feature flags for local/cloud mode |
| `webui/src/services/apiAdapter.ts` | APIAdapter interface + install/get |
| `webui/src/services/cloudAdapter.ts` | CloudAdapter implementation |
| `webui/src/services/wasmShell.ts` | WASM-based shell for cloud mode |
| `webui/src/services/api.ts` | ApiService class (2000+ lines) |
| `webui/src/hooks/useWebSocketEvents.ts` | Event bridge |
| `webui/src/components/EditorPane.tsx` | CodeMirror 6 editor |
| `webui/src/contexts/EditorManagerContext.tsx` | Buffer management |
| `packages/ui/vite.config.ts` | UI library build config |
| `packages/ui/src/index.ts` | UI library exports |
