# SP-011: Terminal Parity & Bug Fixes

**Status:** 📋 Proposed
**Depends on:** SP-003 (WebUI & Frontend Architecture)
**Priority:** High
**Effort Estimate:** ~2-3 weeks (3 phases — critical bug fixes first)

## Problem

The embedded terminal has **3 critical issues** that make it feel broken:

### Critical Issue 1: `exit` Doesn't Close the Pane

When the user types `exit` in a terminal, the backend sends a `pty_exit` event. The frontend writes `[Process exited]` to the xterm buffer but does NOT close the session, disconnect the WebSocket, or remove the tab. The dead terminal sits there forever with no way to recover.

**Current behavior (broken):**
```typescript
// TerminalPane.tsx — pty_exit handler
} else if (event.type === 'pty_exit') {
  xtermRef.current?.writeln('\r\n\x1b[90m[Process exited]\x1b[0m');
  // Nothing else happens — session stays open, tab stays, pane is dead
}
```

**Expected behavior:**
1. Show a brief "[Process exited]" message
2. Disconnect the pane's WebSocket cleanly
3. Notify parent Terminal component via new `onProcessExit` callback
4. Parent handles cleanup:
   - If secondary split pane → auto-close the split
   - If only session → auto-create a fresh session after 1.5s
   - If one of multiple tabbed sessions → close the tab, switch to next

### Critical Issue 2: Tabs + Split Panes Conflict

The terminal uses `activeSessionId` for BOTH "which tab is selected" AND "which session renders in the primary pane". When split mode is active and the user switches tabs, it changes the primary pane's session — but the secondary pane is locked to one session. With >2 sessions, only the two split panes actually work; extra tabs exist but can only swap into the primary pane position, which is confusing.

**Current architecture:**
```
Tab bar shows: [Session 1] [Session 2] [Session 3]
                     ↑ active (renders in primary pane)

Primary pane:   renders activeSessionId (switches with tab clicks)
Secondary pane: always renders secondarySessionId (locked)
```

**Problem:**
- Tab switching changes what's in the primary pane while the split is active
- Extra tabs beyond the 2 split panes have no pane to render in
- No way to assign tabs to specific panes
- User expectation: each pane should have its own tab group

**Solution: Per-Pane Session Model**
- Each split pane maintains its own session group with its own tab bar
- When unsplit, all sessions collapse back into a single group
- New state model:

```typescript
interface PaneState {
  id: string;                     // stable pane identifier
  sessions: TerminalSession[];    // tabs in this pane
  activeSessionId: string;        // which tab is visible
}
```

- Unsplit: 1 PaneState (all sessions in one tab bar)
- Split: 2 PaneStates (each with its own session list and tab bar)
- Splitting moves the current active session to the new pane
- Tabs can only be switched within their pane

### Critical Issue 3: Non-Working Buttons Should Be Removed

There are buttons in the terminal header that either don't work correctly or are confusing. Specifically:

**Zoom buttons (+) (-) work** — these control font size and are functional. Keep them.

**However**, the user reports seeing non-functional (+)(-) buttons. This is likely because:
- The split (+) button in the tab bar ("New terminal session") may appear broken when split is active
- The split buttons (⠀ ⠁) create new sessions but the interaction with tabs is confusing

**Solution:** Remove the (+) new session button from the tab bar when split mode is active, and only allow creating new sessions through the tab bar within each pane's own tab group.

## Current State

### Terminal Architecture

```
webui/src/components/Terminal.tsx (main terminal component, ~480 lines)
├── TerminalTabBar (from @sprout/ui — tab CRUD, rename, pin)
├── TerminalPane.tsx (xterm.js mount, WebSocket, events, ~500 lines)
│   ├── xterm.js Terminal instance
│   ├── FitAddon (auto-resize)
│   ├── Custom key handler (Ctrl+Shift+C/V)
│   └── WebSocket → PTY data via terminalWebSocket.ts
├── Shell picker (+ button with dropdown)
├── Zoom controls (ZoomIn/ZoomOut/Type buttons — work)
├── Clear button
└── Split management (horizontal/vertical, drag divider)

packages/ui/src/components/Terminal.tsx (simplified version, ~278 lines)
└── Same structure but simplified session management
```

### What Already Works

| Feature | Status | Notes |
|---------|--------|-------|
| Tabbed sessions | ✅ Works | Rename, pin, close tabs |
| Shell picker | ✅ Works | Dynamic shell detection, dropdown |
| Split panes | ⚠️ Partially | Creates split, but conflicts with tabs |
| Font zoom | ✅ Works | ZoomIn/ZoomOut/Reset buttons |
| Clear | ✅ Works | Per-pane clear button |
| WebSocket reconnect | ✅ Works | Exponential backoff, freeze/resume |
| WASM fallback | ✅ Works | Cloud mode browser shell |
| Selection copy | ✅ Works | Ctrl+Shift+C/V + right-click context menu |
| `exit` handling | ❌ Broken | Shows message but doesn't clean up |
| Tab/split coexistence | ❌ Broken | Conflicting session-to-pane mapping |

## Proposed Solution

### Phase 1: Critical Bug Fixes (Days 1-4)

#### P1.1: Handle `pty_exit` — Auto-Close or Restart

**New prop on TerminalPane:**
```typescript
interface TerminalPaneProps {
  // ... existing props ...
  /** Called when the PTY process exits (user typed exit, shell crashed, etc.) */
  onProcessExit?: () => void;
}
```

**TerminalPane.tsx changes:**
```typescript
// In the WebSocket event handler:
} else if (event.type === 'pty_exit') {
  xtermRef.current?.writeln('\r\n\x1b[90m[Process exited]\x1b[0m');
  setPaneConnected(false);
  onConnectionChangeRef.current?.(false);
  
  // Close the WebSocket connection cleanly
  const service = terminalWSRef.current;
  if (service) {
    service.closeSession();
    service.disconnect();
    terminalWSRef.current = null;
  }
  
  // Notify parent after a brief delay so the user sees the exit message
  setTimeout(() => {
    onProcessExitRef.current?.();
  }, 1000);
}
```

**Terminal.tsx changes:**
```typescript
const handlePaneExit = useCallback((paneId: string) => {
  // If secondary split pane exited, auto-close the split
  if (secondaryPaneId === paneId) {
    closeSecondaryPane();
    return;
  }
  
  // If primary pane's session exited:
  const pane = panesRef.current.find(p => p.id === paneId);
  if (!pane) return;
  
  if (pane.sessions.length <= 1 && panesRef.current.length <= 1) {
    // Last session in last pane — auto-restart after 1.5s
    const newId = `pane-${++paneIdCounter.current}`;
    const newSession: TerminalSession = { id: newId, name: 'Session 1', is_pinned: false };
    
    // Brief delay so user sees the exit message
    setTimeout(() => {
      setSessions([newSession]);
      setActiveSessionId(newId);
    }, 1500);
  } else if (pane.sessions.length <= 1) {
    // Last session in this pane, but other pane exists — close this pane
    closePane(paneId);
  } else {
    // Multiple sessions in this pane — close the exited one, switch to next
    const currentSessions = pane.sessions;
    const activeIdx = currentSessions.findIndex(s => s.id === pane.activeSessionId);
    const remaining = currentSessions.filter(s => s.id !== pane.activeSessionId);
    const nextActive = remaining[Math.min(activeIdx, remaining.length - 1)];
    updatePane(paneId, { sessions: remaining, activeSessionId: nextActive.id });
  }
}, []);
```

#### P1.2: Fix Tabs + Split — Per-Pane Session Model

Replace the current flat session model with a per-pane model:

**New state:**
```typescript
interface PaneState {
  id: string;                      // "primary" | "secondary"
  sessions: TerminalSession[];     // tabs belonging to this pane
  activeSessionId: string;         // currently visible tab
}

// State:
const [panes, setPanes] = useState<PaneState[]>([
  { id: 'primary', sessions: [{ id: 's-1', name: 'Session 1', is_pinned: false }], activeSessionId: 's-1' }
]);
const [splitDirection, setSplitDirection] = useState<SplitDirection>('none');
```

**Rendering:**
```tsx
{panes.map((pane, paneIndex) => (
  <div key={pane.id} className="terminal-pane-wrapper" style={splitStyleForPane(paneIndex)}>
    {/* Per-pane tab bar */}
    <TerminalTabBar
      sessions={pane.sessions}
      activeSessionId={pane.activeSessionId}
      onSwitch={(id) => switchSessionInPane(pane.id, id)}
      onCreate={() => addSessionToPane(pane.id)}
      onClose={(id) => closeSessionInPane(pane.id, id)}
      onRename={(id, name) => renameSessionInPane(pane.id, id, name)}
      onTogglePin={(id) => togglePinInPane(pane.id, id)}
    />
    {/* Active session terminal */}
    <TerminalPane
      key={pane.activeSessionId}
      isActive={isExpanded}
      isConnected={isConnected}
      showCloseButton={panes.length > 1}
      onClose={() => closePane(pane.id)}
      onProcessExit={() => handlePaneExit(pane.id, pane.activeSessionId)}
      preferredShell={getSessionShell(pane.activeSessionId)}
      fontSize={fontSize}
    />
  </div>
))}
```

**Split behavior:**
- `toggleSplit(direction)`:
  - If currently unsplit → create secondary pane, move current active session to it, keep remaining sessions in primary
  - If currently split → merge all sessions back to primary pane, close secondary
  - If switching directions → just change direction, keep both panes

- `addSessionToPane(paneId)` → create new session in specific pane
- `closeSessionInPane(paneId, sessionId)` → close within that pane's scope
- `switchSessionInPane(paneId, sessionId)` → switch tab within pane

#### P1.3: Remove Non-Functional Buttons During Split

**Changes to the tab bar area in Terminal.tsx:**
- When split is active, each pane's tab bar gets its own (+) create button
- Remove the global (+) shell picker from the tab bar row when split is active
- Each pane's (+) button creates sessions in that pane only
- The (+) new session button in the tab bar only appears for the pane that owns it

### Phase 2: Polish & UX Improvements (Week 2)

#### P2.1: Exited Session Visual Feedback

Add CSS for exited/waiting state:
```css
.terminal-tab.exited {
  opacity: 0.5;
  font-style: italic;
}
```

Show a "Session ended. Starting new session..." message before auto-restart.

#### P2.2: Verify Zoom Buttons Are Correctly Visible

Confirm that ZoomIn (+) / ZoomOut (-) buttons:
- Are visible in the terminal header
- Correctly change xterm font size
- Persist to localStorage across reloads
- The font size indicator shows current size on hover

If any are broken, fix the data flow from Terminal → TerminalPane → xterm.

#### P2.3: Clean Up `packages/ui` Terminal Duplication

The `packages/ui/src/components/Terminal.tsx` is a simplified duplicate with its own session management. Evaluate:
- If it's used anywhere (storybook, examples) → update it to match the new per-pane model
- If it's not used → remove it to avoid confusion

### Phase 3: Missing Features (Week 3, optional)

#### P3.1: Terminal Search (Ctrl+Shift+F)
- Install `@xterm/addon-search`
- Add search bar above terminal pane
- Match counter, case-sensitive/regex toggles

#### P3.2: Clickable File Paths
- Detect patterns like `./foo.go:12:34` in terminal output
- Use `Terminal.registerLinkProvider()` for click handling
- Dispatch event to open file in editor at line/col

#### P3.3: Copy-on-Select
- Auto-copy selected text to clipboard

#### P3.4: Scrollback Persistence
- Save terminal buffer to IndexedDB on unmount
- Restore on reconnect

## Implementation Plan

### Phase 1: Critical Bug Fixes — MUST DO FIRST

**Modified files:**
- `webui/src/components/TerminalPane.tsx` — handle `pty_exit`, add `onProcessExit` prop, clean up WebSocket on exit
- `webui/src/components/Terminal.tsx` — replace flat session model with per-pane model, handle pane exits, clean up split/tab interaction
- `webui/src/components/Terminal.css` — exited session styles, per-pane tab bar layout

### Phase 2: Polish

**Modified files:**
- `webui/src/components/Terminal.tsx` — zoom verification, button cleanup
- `webui/src/components/Terminal.css` — zoom button styling fixes if needed

### Phase 3: Missing Features (optional)

**New files:**
- `webui/src/components/TerminalSearchBar.tsx` + CSS — search UI
- `webui/src/extensions/terminalFilePaths.ts` — clickable file path detection

**Modified files:**
- `webui/src/components/TerminalPane.tsx` — wire search addon, link provider, copy-on-select

## Success Criteria

| Metric | Target |
|--------|--------|
| `exit` handling | Dead sessions auto-close or auto-restart after 1.5s delay |
| Split pane exit | Secondary pane auto-closes when its process exits |
| Tabs + split | Per-pane tab bars that work independently |
| Non-working buttons | Removed or fixed |
| Zoom buttons | Font size reliably changes when clicking +/- |
| Build | `make build-all` passes |
| Terminal search | (Phase 3) Ctrl+Shift+F finds text in scrollback |
| Clickable paths | (Phase 3) `./foo.go:12` opens in editor |

## Files Reference

| File | Action |
|------|--------|
| `webui/src/components/Terminal.tsx` | Major: per-pane session model, exit handling |
| `webui/src/components/TerminalPane.tsx` | Major: pty_exit handling, onProcessExit callback |
| `webui/src/components/Terminal.css` | Modify: per-pane layout, exited session styles |
| `packages/ui/src/components/Terminal.tsx` | Evaluate: update or remove duplicate |