# SP-011: Terminal Parity

**Status:** 📋 Proposed  
**Depends on:** SP-003 (Webui & Frontend Architecture)  
**Priority:** Medium  
**Effort Estimate:** ~2-3 weeks (3 phases)

## Problem

The embedded terminal (`TerminalPane.tsx` via xterm.js) provides basic shell access but lacks several features that developers consider essential in a modern terminal emulator. Users coming from VS Code's integrated terminal, iTerm2, or Windows Terminal will notice paper cuts immediately:

1. **No terminal search** — cannot find text in scrollback
2. **No clickable file paths** — `./foo.go:12:34` shown in compiler output cannot be clicked to open in editor
3. **No copy-on-select** — standard in most terminals
4. **No right-click context menu** — paste, copy selection, and search all require keyboard shortcuts
5. **No reverse-i-search** — Ctrl+R for command history search
6. **Scrolled-back output lost on refresh** — scrollback buffer not persisted

## Current State

### Terminal Architecture

```
Terminal.tsx (tab management, zoom, shell picker)
└── TerminalPane.tsx (xterm.js mount, WebSocket data pipe)
    ├── xterm.js Terminal instance
    ├── FitAddon (auto-resize)
    ├── ClipboardAddon (copy/paste via Ctrl+Shift+C/V)
    └── WebSocket → PTY data channel
```

### Features Already Implemented

| Feature | Implementation | Location |
|---------|---------------|----------|
| Tabbed sessions | TerminalTabBar + TerminalManager | Terminal.tsx |
| Session persistence | Session IDs in localStorage | Terminal.tsx |
| Shell picker | Dynamic shell detection | Terminal.tsx |
| Split panes | Horizontal/vertical with resize | Terminal.tsx |
| Font zoom | 8-32px, Ctrl+/Ctrl-, persisted | Terminal.tsx |
| Clear | Per-pane clear button | TerminalPane.tsx |
| WebSocket reconnect | Exponential backoff, health ping | terminalWebSocket.ts |
| WASM shell fallback | Cloud mode, no Go backend | wasmShell.ts |
| Selection copy | Ctrl+Shift+C/V via @xterm/addon-clipboard | TerminalPane.tsx |

### Missing Features Inventory

| Feature | xterm.js Support | Effort | User Impact |
|---------|-----------------|--------|-------------|
| Terminal search | `@xterm/addon-search` | Low | High |
| Clickable file paths | Custom plugin | Medium | High |
| Copy-on-select | Custom plugin | Low | Medium |
| Right-click context menu | Custom React component | Low | Medium |
| Reverse-i-search | Custom plugin | Medium | Medium |
| Scrollback persistence | Custom (IndexedDB/localStorage) | Medium | Medium |
| Word selection (double-click) | Custom plugin | Low | Low |
| Line selection (triple-click) | Custom plugin | Low | Low |

## Proposed Solution

### Phase 1: Search & Links (Week 1)

#### T1.1: Terminal Search (Ctrl+Shift+F)

Install and wire `@xterm/addon-search`:

```typescript
// TerminalPane.tsx additions
import { SearchAddon } from '@xterm/addon-search';

// Add search state
const [searchVisible, setSearchVisible] = useState(false);
const [searchTerm, setSearchTerm] = useState('');
const [searchResults, setSearchResults] = useState<{ count: number; index: number }>();

// Search bar UI (positioned above terminal, like VS Code)
// - Text input with next/prev buttons
// - Match counter (3 of 15)
// - Case-sensitive toggle
// - Regex toggle
// - Close on Escape
```

Keybindings:
- `Ctrl+Shift+F` — open search bar
- `Enter` — next match
- `Shift+Enter` — previous match
- `Escape` — close search bar

#### T1.2: Clickable File Paths

Create a custom web links plugin that detects file path patterns and makes them clickable:

```typescript
// webui/src/extensions/terminalFilePaths.ts

// Patterns to detect:
const FILE_PATTERNS = [
  /(\.{0,2}\/[^\s:]+):(\d+)(?::(\d+))?/,     // ./foo.go:12:34
  /([A-Za-z0-9_-]+\.[a-z]{1,10}):(\d+)/,       // foo.go:12
  /(\/[^\s:]+\/[^\s:]+):(\d+)(?::(\d+))?/,     // /abs/path/file.go:12:34
];

// On click: parse match, construct file path + line + col,
// dispatch custom event to open in editor
```

Implementation via `Terminal.registerLinkProvider()` — xterm.js native link provider API. Clicking opens the file in the editor at the correct line/column.

#### T1.3: Copy-on-Select

```typescript
// TerminalPane.tsx
// On selection change, if user preference enabled, copy to clipboard
terminal.onSelectionChange(() => {
  if (copyOnSelect && terminal.hasSelection()) {
    navigator.clipboard.writeText(terminal.getSelection());
  }
});
```

### Phase 2: Context Menu & History (Week 2)

#### T2.1: Right-Click Context Menu

React component positioned at cursor:

```typescript
// Options when right-clicking in terminal:
// - Paste             (Ctrl+Shift+V)
// - Copy Selection    (if text selected, Ctrl+Shift+C)
// - Search            (Ctrl+Shift+F)
// - Clear Terminal
// - Split Pane
// - Separator
// - Select All
```

Reuses existing `ContextMenu` component from `@sprout/ui`.

#### T2.2: Reverse-i-search (Ctrl+R)

```typescript
// Custom plugin that intercepts Ctrl+R
// Shows prompt: (reverse-i-search)`query': 
// Filters PTY output to search command history
// Up/Down navigates matches
// Enter executes selected command
// Escape cancels
```

This is more complex — requires either sending `Ctrl+R` to the PTY (letting the shell handle it) and capturing the output, or implementing client-side history. Start with passthrough to the shell (bash/zsh handle Ctrl+R natively) and intercept the display.

### Phase 3: Persistence & Polish (Week 3)

#### T3.1: Scrollback Persistence

```typescript
// On page hide / terminal unmount:
// 1. Serialize terminal buffer to string (terminal.buffer.active)
// 2. Store in IndexedDB keyed by session ID
// 3. On restore: write scrollback to terminal before reconnecting

// Storage strategy:
// - IndexedDB 'terminal-scrollback' store
// - Key: session ID
// - Value: { content: string, timestamp: number, cursorRow: number }
// - Cleanup: remove entries older than 24 hours
// - Max size: 500KB per session (truncate oldest lines)
```

#### T3.2: Double/Triple Click Selection

- **Double-click**: Select word under cursor (configurable word separators)
- **Triple-click**: Select entire line

xterm.js supports this natively via `wordSeparator` option and selectLine plugin. Low effort.

#### T3.3: Selection Highlight

When text is selected in the terminal, highlight all other occurrences of the same text (similar to editor word highlighting). Optional, medium effort.

## Implementation Phases

### Phase 1: Search & Links (Week 1)

**New files:**
- `webui/src/components/TerminalSearchBar.tsx` — search UI component
- `webui/src/components/TerminalSearchBar.css` — search bar styling
- `webui/src/extensions/terminalFilePaths.ts` — file path link detection

**Modified files:**
- `webui/packages/ui/src/components/TerminalPane.tsx` — wire SearchAddon, link provider
- `packages/ui/src/components/Terminal.tsx` — add Ctrl+Shift+F hotkey
- `packages/ui/package.json` — add `@xterm/addon-search` dependency

### Phase 2: Context Menu & History (Week 2)

**New files:**
- `webui/src/components/TerminalContextMenu.tsx` — right-click menu

**Modified files:**
- `packages/ui/src/components/TerminalPane.tsx` — context menu integration, copy-on-select

### Phase 3: Persistence & Polish (Week 3)

**New files:**
- `webui/src/services/terminalScrollback.ts` — IndexedDB scrollback persistence

**Modified files:**
- `packages/ui/src/components/TerminalPane.tsx` — scrollback save/restore
- `packages/ui/src/components/Terminal.tsx` — double/triple click config

## Success Criteria

| Metric | Target |
|--------|--------|
| Terminal search | Ctrl+Shift+F opens search, finds text in scrollback |
| Clickable file paths | `./foo.go:12` opens in editor at line 12 |
| Copy-on-select | Selected text auto-copies (when enabled) |
| Right-click menu | Shows paste/copy/search/clear options |
| Scrolled-back persistence | Output preserved across page refresh |
| Build | `make build-all` passes |

## Files Reference

| File | Action |
|------|--------|
| `packages/ui/src/components/TerminalPane.tsx` | Modify: SearchAddon, link provider, context menu |
| `packages/ui/src/components/Terminal.tsx` | Modify: hotkeys, zoom integration |
| `webui/src/components/TerminalSearchBar.tsx` | Create: search UI |
| `webui/src/extensions/terminalFilePaths.ts` | Create: file path link detection |
| `webui/src/components/TerminalContextMenu.tsx` | Create: right-click menu |
| `webui/src/services/terminalScrollback.ts` | Create: IndexedDB persistence |
