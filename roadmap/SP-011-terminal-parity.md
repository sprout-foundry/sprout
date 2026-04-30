# SP-011: Terminal Parity & Bug Fixes

**Status:** 📋 Proposed  
**Depends on:** SP-003 (Webui & Frontend Architecture)  
**Priority:** High  
**Effort Estimate:** ~3-4 weeks (4 phases — includes critical bug fixes)

## Problem

The embedded terminal has **3 critical bugs** and **6 missing features** that collectively make it feel unfinished:

### Critical Bugs

1. **`exit` doesn't close the pane** — When the user types `exit` in a terminal, the backend sends a `pty_exit` event. The frontend writes `[Process exited]` to the xterm buffer but does NOT close the session, disconnect the WebSocket, or remove the tab. The dead terminal sits there forever with no way to recover.

2. **Tabs + Split panes conflict** — The terminal uses `activeSessionId` for both "which tab is selected" and "which session renders in the primary pane". When split mode is active and the user switches tabs, it changes the primary pane's session — but the tab bar still shows all sessions, and the secondary pane is locked to one session. There is no clean mapping between tabs and panes.

3. **Zoom buttons may not work** — Two divergent implementations exist: `packages/ui/src/components/Terminal.tsx` (simplified, may have working zoom) and `webui/src/components/Terminal.tsx` (745 lines, full-featured). Need to verify and unify.

### Missing Features

4. **No terminal search** — cannot find text in scrollback
5. **No clickable file paths** — `./foo.go:12:34` in compiler output cannot be clicked to open in editor
6. **No copy-on-select** — standard in most terminals
7. **No right-click context menu** — paste, copy, search require keyboard shortcuts
8. **No reverse-i-search** — Ctrl+R for command history search
9. **Scrollback lost on refresh** — output not persisted

## Current State

### Terminal Architecture

```
webui/src/components/Terminal.tsx (745 lines — main terminal component)
├── TerminalTabBar (from @sprout/ui — tab CRUD, rename, pin)
├── TerminalPane.tsx (xterm.js mount, WebSocket, events)
│   ├── xterm.js Terminal instance
│   ├── FitAddon (auto-resize)
│   ├── ClipboardAddon (Ctrl+Shift+C/V)
│   └── WebSocket → PTY data via terminalWebSocket.ts
├── Shell picker (+ button with dropdown)
├── Zoom controls (ZoomIn/ZoomOut/Type buttons)
└── Split management (horizontal/vertical, drag divider)

packages/ui/src/components/Terminal.tsx (278 lines — simplified version)
└── Same structure but simplified session management
```

### Bug Analysis

#### Bug 1: `exit` Doesn't Close Pane

```typescript
// webui/src/components/TerminalPane.tsx:829-830
} else if (event.type === 'pty_exit') {
  xtermRef.current?.writeln('\r\n\x1b[90m[Process exited]\x1b[0m');
  // BUG: No session cleanup, no WebSocket disconnect, no tab removal
  // The terminal just sits dead with "[Process exited]" text
}
```

**Expected behavior:** On `pty_exit`, the terminal should:
1. Show a brief "Process exited" message
2. Disconnect the WebSocket cleanly
3. Mark the session as "exited" (grayed out tab, or auto-close after 2s)
4. If it's the secondary split pane, auto-close it
5. If it's the only session, show a "Start new session" prompt or restart button

#### Bug 2: Tabs + Split Conflict

```typescript
// webui/src/components/Terminal.tsx
// activeSessionId is used for BOTH tab selection AND primary pane rendering
const [activeSessionId, setActiveSessionId] = useState(...);

// Primary pane renders activeSessionId
<TerminalPane key={activeSessionId} ... />

// Secondary pane renders secondarySessionId
<TerminalPane key={secondarySessionId} ... />

// Tab bar switches activeSessionId
<TerminalTabBar onSwitch={switchSession} ... />
// switchSession = useCallback((id) => setActiveSessionId(id), []);
```

**Problem:** When using tabs + split:
- Clicking a tab changes what's in the primary pane
- The secondary pane is always the same session
- There's no way to assign specific tabs to specific panes
- If you have 3 sessions and split active: primary shows whichever tab you clicked, secondary is locked
- The tab bar shows sessions but doesn't indicate which pane they're in

**Expected behavior:** Each pane should have its own active session. The tab bar should show which pane owns which session. OR: split mode should create two independent tab bars (one per pane), like VS Code's terminal splits.

#### Bug 3: Zoom Button Divergence

Two files manage zoom independently:
- `packages/ui/src/components/Terminal.tsx`: lines 78, 113-118, 176-177 (simpler, uses `FONT_SIZE_STORAGE_KEY`)
- `webui/src/components/Terminal.tsx`: lines 212-241 (more complex, same storage key via `ledit-terminal-font-size`)

Since the webui imports `TerminalTabBar` from `@sprout/ui` but renders its own `Terminal.tsx`, the packages/ui version is likely not used. Need to verify zoom buttons actually work in the running app.

### Features Already Implemented

| Feature | Implementation | Location |
|---------|---------------|----------|
| Tabbed sessions | TerminalTabBar | Terminal.tsx |
| Session persistence | Session IDs in localStorage | Terminal.tsx |
| Shell picker | Dynamic shell detection | Terminal.tsx |
| Split panes | Horizontal/vertical with drag | Terminal.tsx |
| Font zoom UI | ZoomIn/ZoomOut/Type buttons | Terminal.tsx |
| Clear | Per-pane clear button | TerminalPane.tsx |
| WebSocket reconnect | Exponential backoff | terminalWebSocket.ts |
| WASM shell fallback | Cloud mode | wasmShell.ts |
| Selection copy | Ctrl+Shift+C/V | TerminalPane.tsx |

## Proposed Solution

### Phase 0: Critical Bug Fixes (Days 1-3)

#### P0.1: Handle `pty_exit` — Auto-Close or Restart

```typescript
// TerminalPane.tsx — on pty_exit event
} else if (event.type === 'pty_exit') {
  xtermRef.current?.writeln('\r\n\x1b[90m[Process exited]\x1b[0m');
  setPaneConnected(false);
  
  // Notify parent terminal that this session exited
  // Pass the exit code if available (data?.exit_code)
  onProcessExit?.(id, data?.exit_code);
}
```

```typescript
// Terminal.tsx — handle process exit
const handleProcessExit = useCallback((sessionId: string) => {
  // If secondary split pane exited, auto-close the split
  if (secondarySessionIdRef.current === sessionId) {
    closeSecondaryPane();
    return;
  }
  
  // If primary pane's session exited:
  if (sessionsRef.current.length <= 1) {
    // Last session — show restart prompt or auto-restart after 2s
    // Replace with a new session spawned after a brief delay
    const id = `pane-${++paneIdCounter.current}`;
    setSessions([{ id, name: 'Session 1', is_pinned: false }]);
    setActiveSessionId(id);
  } else {
    // Multiple sessions — close the exited one, switch to another
    closeSession(sessionId);
  }
}, [closeSecondaryPane, closeSession]);
```

**New prop on TerminalPane:**
- `onProcessExit?: (sessionId: string, exitCode?: number) => void`

#### P0.2: Fix Tabs + Split Conflict

Redesign the session-to-pane mapping. Two options:

**Option A (Recommended): Per-Pane Tab Bars**
- Each pane gets its own tab bar
- Split creates two independent terminal groups
- Sessions cannot be shared between panes
- Simpler mental model, matches VS Code behavior

**Option B: Session Assignment**
- One tab bar, but each tab shows which pane it's in (badge: "1" or "2")
- Dragging a tab to a pane assigns it there
- More complex but flexible

Go with **Option A** — it's simpler and matches what users expect:

```typescript
// New state model:
interface PaneGroup {
  id: string;
  sessions: TerminalSession[];
  activeSessionId: string;
}

const [panes, setPanes] = useState<PaneGroup[]>([
  { id: 'pane-1', sessions: [...], activeSessionId: 'pane-1' }
]);

// Each pane renders its own TerminalTabBar
// Split adds a new PaneGroup
// Unsplit merges PaneGroups or closes one
```

#### P0.3: Verify & Fix Zoom Buttons

Test that ZoomIn (+) / ZoomOut (-) buttons in the header actually change the xterm font size. If broken:
- Verify `fontSize` prop is passed correctly to `TerminalPane`
- Verify `TerminalPane` applies `fontSize` to xterm.js `Terminal` options
- Check `terminal.options.fontSize = fontSize` is called on updates
- Ensure the font size is persisted to localStorage

### Phase 1: Search & Links (Week 1)

#### T1.1: Terminal Search (Ctrl+Shift+F)

Install and wire `@xterm/addon-search`:

```typescript
// TerminalPane.tsx additions
import { SearchAddon } from '@xterm/addon-search';

const searchAddon = new SearchAddon();
terminal.loadAddon(searchAddon);

// Search bar positioned above terminal pane (VS Code style)
// - Text input with next/prev buttons
// - Match counter (3 of 15)
// - Case-sensitive toggle
// - Regex toggle
// - Close on Escape
```

#### T1.2: Clickable File Paths

```typescript
// webui/src/extensions/terminalFilePaths.ts
// Patterns to detect:
const FILE_PATTERNS = [
  /(\.{0,2}\/[^\s:]+):(\d+)(?::(\d+))?/,     // ./foo.go:12:34
  /([A-Za-z0-9_-]+\.[a-z]{1,10}):(\d+)/,       // foo.go:12
  /(\/[^\s:]+\/[^\s:]+):(\d+)(?::(\d+))?/,     // /abs/path/file.go:12:34
];
// On click: dispatch custom event to open in editor at line/col
// Via Terminal.registerLinkProvider()
```

#### T1.3: Copy-on-Select

```typescript
terminal.onSelectionChange(() => {
  if (copyOnSelect && terminal.hasSelection()) {
    navigator.clipboard.writeText(terminal.getSelection());
  }
});
```

### Phase 2: Context Menu & History (Week 2)

#### T2.1: Right-Click Context Menu

```typescript
// Options: Paste, Copy Selection, Search, Clear, Split Pane, Select All
// Reuses ContextMenu from @sprout/ui
```

#### T2.2: Reverse-i-search (Ctrl+R)

Passthrough to shell (bash/zsh handle Ctrl+R natively). Client-side display enhancement in future iteration.

### Phase 3: Persistence & Polish (Week 3)

#### T3.1: Scrollback Persistence via IndexedDB

- Serialize terminal buffer on unmount
- Store keyed by session ID in IndexedDB
- Restore on reconnect
- Max 500KB per session, 24h auto-cleanup

#### T3.2: Double/Triple Click Selection

- Double-click: select word
- Triple-click: select line
- Via xterm.js `wordSeparator` option

## Implementation Phases

### Phase 0: Bug Fixes (Days 1-3) — MUST DO FIRST

**Modified files:**
- `webui/src/components/TerminalPane.tsx` — handle `pty_exit`, add `onProcessExit` prop
- `webui/src/components/Terminal.tsx` — per-pane tab bars, `handleProcessExit`, verify zoom
- `webui/src/components/Terminal.css` — exited session styling (grayed tab)

### Phase 1: Search & Links (Week 1)

**New files:**
- `webui/src/components/TerminalSearchBar.tsx` — search UI
- `webui/src/components/TerminalSearchBar.css`
- `webui/src/extensions/terminalFilePaths.ts` — file path link detection

**Modified files:**
- `webui/src/components/TerminalPane.tsx` — wire SearchAddon, link provider
- `webui/src/components/Terminal.tsx` — Ctrl+Shift+F hotkey
- `packages/ui/package.json` — add `@xterm/addon-search`

### Phase 2: Context Menu & History (Week 2)

**New files:**
- `webui/src/components/TerminalContextMenu.tsx`

**Modified files:**
- `webui/src/components/TerminalPane.tsx` — context menu, copy-on-select

### Phase 3: Persistence & Polish (Week 3)

**New files:**
- `webui/src/services/terminalScrollback.ts` — IndexedDB persistence

**Modified files:**
- `webui/src/components/TerminalPane.tsx` — scrollback save/restore
- `webui/src/components/Terminal.tsx` — double/triple click config

## Success Criteria

| Metric | Target |
|--------|--------|
| `exit` handling | Dead sessions auto-close or show restart prompt |
| Tabs + split | Per-pane tab bars that work independently |
| Zoom buttons |font size reliably changes when clicking +/- |
| Terminal search | Ctrl+Shift+F finds text in scrollback |
| Clickable file paths | `./foo.go:12` opens in editor |
| Copy-on-select | Auto-copies selected text |
| Right-click menu | Paste/copy/search/clear options |
| Scrollback persistence | Output preserved across refresh |
| Build | `make build-all` passes |

## Files Reference

| File | Action |
|------|--------|
| `webui/src/components/Terminal.tsx` | Major: bug fixes, per-pane tab bars |
| `webui/src/components/TerminalPane.tsx` | Major: pty_exit handling, search, links, context menu |
| `webui/src/components/Terminal.css` | Modify: exited session styles |
| `webui/src/components/TerminalSearchBar.tsx` | Create: search UI |
| `webui/src/extensions/terminalFilePaths.ts` | Create: file path links |
| `webui/src/components/TerminalContextMenu.tsx` | Create: right-click menu |
| `webui/src/services/terminalScrollback.ts` | Create: IndexedDB persistence |
