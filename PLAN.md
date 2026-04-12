# Plan: Chat Interface Improvements

## Overview

Four areas of improvement to the chat UI — all geared toward better visibility of tool activity and improved text interaction.

---

## Issue 1 — Tool Call Inline Indicators (footnote-style links in chat)

### Problem

When the assistant executes tools (shell_command, read_file, write_file, etc.), the chat shows no visual indication that a tool ran. Only subagents get an inline activity feed. Users have no awareness of tool activity without checking the sidebar.

### Current Behavior

- Subagents: Full `SubagentActivityFeed` component rendered inline in the chat (Chat.tsx:590) with live log streaming.
- All other tools: Tool pills render **after** the assistant response streams the `[executing tool …]` stderr line (MessageSegments.tsx:118-166). These pills are clickable and link to the sidebar, but only appear as **future/in-progress** indicators during streaming — once the response is complete, the pills serve as post-hoc links.
- Tool call data flows via `tool_start` → `tool_end` WS events into `toolExecutions[]` and `messages[last].toolRefs[]`.

### Proposed Behavior

When a tool call completes (`tool_end`), insert a minimal inline footnote link into the assistant message at the point it was referenced, e.g. `shell_command [1]`. This link navigates to the matching tool execution in the sidebar Tools tab (same as clicking an existing tool pill).

### Implementation Steps

#### 1a. Track per-turn tool index on the backend

The backend already maintains `te.toolIndex` (per-turn counter, reset each `ExecuteTools` call). Wire this into the WebSocket event payload:

- **`pkg/events/events.go`** — `ToolStartEvent()`: Add `"tool_index": toolIndex` to the event map.
- **`pkg/agent/agent_events.go`** — `PublishToolStart()`: Accept a `toolIndex int` parameter and pass it through.
- **`pkg/agent/tool_executor.go`** — `ExecuteTools()` loop: The loop starts at index 0 and calls `PublishToolStart` before `executeSingleTool`. Use a local `i` counter (`for i, tc := range toolCalls`) and pass `i` to `PublishToolStart`.
- **Result**: Each `tool_start` event now includes `"tool_index": 0`, `"tool_index": 1`, etc., reset per turn.

#### 1b. Store `toolIndex` in frontend ToolExecution

- **`webui/src/types/app.ts`** — Add `toolIndex?: number` to `ToolExecution` interface.
- **`webui/src/hooks/useWebSocketEvents.ts`** — In `tool_start` handler (~line 305): read `eventData.tool_index` and store it on the `ToolExecution` object. Also include it in the `toolRef` pushed to messages: `toolIndex: eventData.tool_index ?? toolExecutions.length`.

#### 1c. Render footnote links in MessageSegments

- **`webui/src/components/MessageSegments.tsx`** — In the `tool_call` segment renderer (~line 118): Already renders a clickable pill. Modify so that:
  - While the tool is **in progress** (no matching completed toolRef): show existing pill (current behavior).
  - When the tool has **completed** (matching toolRef with `toolIndex`): render a compact footnote link instead: `[1]` or `[2]`, styled like a superscript footnote. Use the same `onClick` handler that calls `onToolRefClick(toolId)`.
- **`webui/src/components/Chat.css`** — Add `.segment-tool-footnote` styles: small monospace, muted color, slightly raised (superscript or vertical-align), clickable with cursor:pointer. Keep `user-select: none` on the footnote element itself (it's a UI control).

#### 1d. Tool pill vs footnote logic

The `claimMatchingToolRef` function already tries to match tool refs. The key distinction is: if a tool call is found in `toolRefs` **and** a corresponding `ToolExecution` exists with status `completed` or `error`, treat it as a "done" footnote. If status is `started`/`running`, keep the current pill style.

- Add a lookup function or prop to `MessageSegments` that can check the tool completion status (pass `toolExecutions` or a `Map<string, ToolExecution>` down, or use a callback `getToolStatus: (toolId: string) => string | undefined`).

#### Files Changed
- `pkg/events/events.go`
- `pkg/agent/agent_events.go`
- `pkg/agent/tool_executor.go`
- `webui/src/types/app.ts`
- `webui/src/hooks/useWebSocketEvents.ts`
- `webui/src/components/MessageSegments.tsx`
- `webui/src/components/Chat.tsx` (pass toolExecutions/status lookup to MessageSegments)
- `webui/src/components/Chat.css`

---

## Issue 2 — Context Menu Preventing Text Selection/Copying

### Problem

Right-clicking any text in a chat message shows the custom context menu (Copy message / Copy code / Insert at cursor). This prevents the native browser context menu, which is the standard way users access "Copy" after selecting text. The custom "Copy message" copies the **entire** message, not just a selection. Users cannot right-click → Copy on a text selection.

### Current Behavior

- **`ChatMessageContextMenu.tsx`** line 87: `handleContextMenu` calls `e.preventDefault()` on every `contextmenu` event within the chat container that has a matching `[data-message-content]` ancestor. This kills the native browser menu.
- The custom menu offers "Copy message" (full message text), "Copy code block" (if in `<pre>`), "Insert at cursor".
- The `message-bubble` and assistant text are not marked `user-select: none` — **text selection itself works** (highlight + Ctrl+C works). The problem is purely the right-click context menu being hijacked.

### Proposed Behavior

- If the user has **text selected** (window.getSelection().toString() is non-empty), **do not** show the custom context menu. Let the native browser context menu appear, which provides "Copy selection" natively.
- If the user right-clicks with **no selection**, show the existing custom menu as-is.

### Implementation Steps

#### 2a. Guard on selection

- **`webui/src/components/ChatMessageContextMenu.tsx`** — In `handleContextMenu` (~line 82-93), before showing the custom menu, check for an active text selection:
  ```ts
  const selection = window.getSelection()?.toString()?.trim();
  if (selection && selection.length > 0) return; // Let native context menu handle it
  ```
  Place this check **after** the `container.contains()` guard and **before** the `e.preventDefault()` call. This preserves the custom menu for no-selection right-clicks while restoring native Copy on selections.

#### Files Changed
- `webui/src/components/ChatMessageContextMenu.tsx` (1-3 lines)

---

## Issue 3 — Rolling Tool Call History in Sidebar (with per-query grouping)

### Problem

When a new query starts (`query_started` event), **all previous tool executions are cleared** (`toolExecutions: []` in `useWebSocketEvents.ts`:157). Users lose the ability to review tool calls from earlier turns in the conversation.

### Current Behavior

- `useWebSocketEvents.ts` line 157: `toolExecutions: []` on `query_started`.
- The ContextPanel Tools tab shows a flat list of all `toolExecutions`. No grouping by query/turn.
- `PerChatState` saves/restores `toolExecutions` when switching chats, but within a session the list resets each query.

### Proposed Behavior

- **Don't clear** `toolExecutions` on `query_started`. Instead, keep a rolling history for the entire session.
- Group tool calls by query turn. The **current** turn's tools are expanded (as today). Prior turns are **collapsed** into a single "Turn N — K tools" row that expands on click to show the individual tool executions from that turn.
- Each tool inside a prior turn expands/collapses individually (same as current behavior).

### Implementation Steps

#### 3a. Add `queryId` to tool executions

We need a way to group tool calls by query turn. The simplest approach: use the existing `queryCount` (incremented per query) or introduce a `queryId`.

- **`webui/src/types/app.ts`** — Add `queryId?: number` to `ToolExecution`. This is the `queryCount` value at the time the tool was created.
- **`webui/src/hooks/useWebSocketEvents.ts`** — In `query_started` handler: stop clearing `toolExecutions`. Instead, just increment `queryCount` (already done). In `tool_start` handler: set `queryId: prev.queryCount` on the new `ToolExecution` object. Keep the `subagentActivities: []` clear (that's display-only and per-turn makes sense). Keep `fileEdits: []` clear for freshness.
- Remove the `toolExecutions: [],` line from `query_started`.

#### 3b. Group toolExecutions by `queryId` in ContextPanel

- **`webui/src/components/ContextPanel.tsx`** — In `renderToolsTab()` (~line 1078):
  - Group `toolExecutions` by `queryId`. The latest `queryId` = current turn (expanded by default). All prior `queryIds` are collapsed into accordion headers.
  - Render structure:
    ```
    [▾] Current turn — 3 tools      ← auto-expanded
        ├── shell_command [completed] 2.1s
        ├── read_file [completed] 0.3s
        └── write_file [completed] 1.1s
    [▶] Previous turn — 5 tools     ← collapsed, click to expand
    [▶] Turn 3 — 2 tools            ← collapsed
    ```
  - Track which query groups are expanded in a `Set<number>` (similar to existing `expandedTools` state). Default: only the latest queryId is expanded.
  - Clicking a toggle header adds/removes the `queryId` from the expanded set.
  - Each individual tool within an expanded group works exactly as today (expand/collapse, highlight-able from pills).

#### 3c. Cap rolling history

- Add a cap: keep last N tool executions (e.g., 200) to prevent unbounded memory. When the cap is exceeded, trim the oldest entries. Apply in `tool_start` handler or as a derived value.
- Keep the cap configurable but start with a reasonable default. This avoids memory issues in long sessions.

#### Files Changed
- `webui/src/types/app.ts` (add `queryId`)
- `webui/src/hooks/useWebSocketEvents.ts` (remove clear, add `queryId` to tool_start, add cap)
- `webui/src/components/ContextPanel.tsx` (grouped rendering in `renderToolsTab`)

---

## Issue 4 — Better Subagent Visibility in Sidebar

### Problem

When a subagent is running, the Subagents tab in the sidebar shows basic card information but lacks the detailed real-time output that the inline `SubagentActivityFeed` in the chat provides. Users have to look at the chat area to see what a subagent is actually doing.

### Current Behavior

- **Subagents tab** (`ContextPanel.tsx:1132-1267`): Shows subagent cards grouped by `toolExecution` → associated activities. Displays:
  - Persona, status icon, duration
  - Prompt preview
  - Stats (update count, task count for parallel)
  - Activity list (chronological items from `subagentActivities`)
  - Auto-scroll for active subagents (liveActivityListRef)
- **`LiveLog` component** (`Chat.tsx:247-276`): Only used in the **inline** `ActiveSubagentCard` (Chat.tsx:278-314), which is part of `SubagentActivityFeed`. Not used in the sidebar's Subagents tab.

### Proposed Behavior

- Port the `LiveLog` component (or equivalent) into the sidebar's Subagents tab so real-time subagent output is visible directly in the sidebar.
- Show the live output **inside each active subagent card** in the Subagents tab, auto-scrolling to the latest line (with user scroll-lock override, same pattern as `LiveLog`).
- Consolidate: the inline `SubagentActivityFeed` in the chat can become a minimal summary/pill (since the sidebar now has full detail), or remain for users who don't have the sidebar open.

### Implementation Steps

#### 4a. Extract `LiveLog` as a shared component

- **`webui/src/components/LiveLog.tsx`** (new file) — Move the `LiveLog` component out of `Chat.tsx` into its own file for reuse. It's currently defined at Chat.tsx:247-276. Props: `lines: string[]`, `className?: string`.
- Keep the existing `LiveLog` behavior: auto-scroll unless user has scrolled up; max-height with overflow-y scroll; monospace font.
- **`webui/src/components/Chat.tsx`** — Import `LiveLog` from the new shared module instead of defining inline.

#### 4b. Integrate `LiveLog` into the Subagents tab cards

- **`webui/src/components/ContextPanel.tsx`** — In `renderSubagentsTab()`, for each active (`status === 'started' || status === 'running'`) subagent card:
  - Derive `outputLines` from the associated `subagentActivities`. This is already done — `subagentRuns` computation at lines 846-905 extracts activity lines per subagent.
  - After rendering the existing card header/prompt/stats, add a `LiveLog` component showing the latest output lines (last 50 for active, similar to `ActiveSubagentCard` in Chat.tsx:310).
  - Style consistently with the sidebar theme (dark/light aware, use CSS variables).

#### 4c. Add live "current step" indicator

- In addition to the log, show a "NOW" indicator (already partially implemented at ContextPanel.tsx ~line 1215) for the most recent activity, with a pulsing dot or highlight to indicate live activity.
- Ensure `auto-scroll` works correctly — the activity list container should scroll to bottom when new events arrive, unless the user has manually scrolled up.

#### 4d. (Optional) Simplify inline chat subagent feed

- Once the sidebar has full subagent detail, the inline `SubagentActivityFeed` in the chat can be simplified to a single compact pill/indicator per active subagent (persona + "in progress" + duration) instead of the full expandable log. This reduces chat clutter.
- This is optional and can be a follow-up. The priority is getting live output into the sidebar.

#### Files Changed
- `webui/src/components/LiveLog.tsx` (new, extracted from Chat.tsx)
- `webui/src/components/Chat.tsx` (extract LiveLog, simplify if desired)
- `webui/src/components/ContextPanel.tsx` (add LiveLog to subagent cards, tweak activity display)
- `webui/src/components/ContextPanel.css` or shared CSS (LiveLog styles)

---

## Implementation Order & Dependencies

```
Issue 2 (context menu)     ← Smallest change, no dependencies, quick win
Issue 3 (rolling history)   ← Medium scope, self-contained
Issue 1 (footnote links)    ← Medium scope, needs backend change for tool_index
Issue 4 (subagent sidebar)  ← Largest scope, benefits from running after 1+3
```

**Recommended order**: 2 → 3 → 1 → 4

Issue 2 is a 3-line fix. Issue 3 sets up the `queryId` grouping that pairs naturally with the footnote links in Issue 1. Issue 4 is independent but largest.
