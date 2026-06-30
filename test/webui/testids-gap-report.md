# TestIDs Gap Report — SP-087-5 Tier 2 E2E Tests

This report lists every `data-testid` that Tier 2 spec files wanted but is **not**
present in `test/webui/testids.ts`. The registry is frozen for this task; these
gaps are documented here for future remediation.

---

## `tier2-mcp-servers.spec.ts`

- `mcp-server-form` — used in `tier2-mcp-servers.spec.ts` (MCPServerForm wrapper has no testid)
- `mcp-server-name-input` — used in `tier2-mcp-servers.spec.ts` (server name `<input>` has no testid)
- `mcp-server-command-input` — used in `tier2-mcp-servers.spec.ts` (server command `<input>` has no testid)
- `mcp-server-add-button` — used in `tier2-mcp-servers.spec.ts` (submit/add button has no testid)
- `mcp-server-row` — used in `tier2-mcp-servers.spec.ts` (individual server `.crud-item` row has no testid)
- `mcp-server-delete-button` — used in `tier2-mcp-servers.spec.ts` (delete `.crud-btn.danger` has no testid)

## `tier2-background-tasks.spec.ts`

- `background-tasks-trigger` — used in `tier2-background-tasks.spec.ts` (`.background-tasks-trigger` button has no testid)
- `background-tasks-popover` — used in `tier2-background-tasks.spec.ts` (`.background-tasks-popover` has no testid)
- `background-task-item` — used in `tier2-background-tasks.spec.ts` (`.background-task-item` has no testid)
- `background-task-attach` — used in `tier2-background-tasks.spec.ts` (`.background-task-btn-attach` has no testid)
- `background-task-kill` — used in `tier2-background-tasks.spec.ts` (`.background-task-btn-kill` has no testid)

## `tier2-git-ops.spec.ts`

- `sidebar-git-tab` — used in `tier2-git-ops.spec.ts` (git icon rail tab is NOT in the registry; the registry lists it but the Sidebar only renders `sidebar-logs-tab` as a hardcoded tab — git is rendered as a platform nav item without a testid)
- `git-push-button` — used in `tier2-git-ops.spec.ts` (push button in the git panel has no testid)
- `git-remote-url` — used in `tier2-git-ops.spec.ts` (remote URL display in the git panel has no testid)

## `tier2-costs-status.spec.ts`

- `status-bar-cost` — used in `tier2-costs-status.spec.ts` (ChatStatusBarItems renders cost text but has no dedicated testid)

## `tier2-steer-input.spec.ts`

- `chat-input` — used in `tier2-steer-input.spec.ts` (chat input textarea has no testid; existing specs use `[data-testid="chat-shell"] textarea` fallback)
- `chat-send` — used in `tier2-steer-input.spec.ts` (send button has no testid; existing specs use keyboard Enter fallback)

## `tier2-multi-chat.spec.ts`

- `chat-new-button` — used in `tier2-multi-chat.spec.ts` (new chat button has no testid)
- `chat-item` — used in `tier2-multi-chat.spec.ts` (individual chat/session item in sidebar has no testid)

## `tier2-workspace-picker.spec.ts`

- `workspace-picker` — used in `tier2-workspace-picker.spec.ts` (workspace picker dropdown/modal has no testid)
- `workspace-picker-option` — used in `tier2-workspace-picker.spec.ts` (individual workspace option in the picker has no testid)

## `tier2-search-panel.spec.ts`

*(No gaps — all testids are in the registry.)*

## `tier2-markdown-viewer.spec.ts`

- `markdown-preview` — used in `tier2-markdown-viewer.spec.ts` (MarkdownPreview component has no testid)

## `tier2-binary-viewer.spec.ts`

- `binary-viewer` — used in `tier2-binary-viewer.spec.ts` (BinaryFileViewer component has no testid; `editor-image-viewer` IS in the registry and is used for image files)

## `tier2-theme-toggle.spec.ts`

- `theme-toggle` — used in `tier2-theme-toggle.spec.ts` (theme toggle/selector in editor preferences has no testid)

---

## Summary

| Spec File | Gaps |
|---|---|
| tier2-mcp-servers.spec.ts | 6 |
| tier2-background-tasks.spec.ts | 5 |
| tier2-git-ops.spec.ts | 3 |
| tier2-costs-status.spec.ts | 1 |
| tier2-steer-input.spec.ts | 2 |
| tier2-multi-chat.spec.ts | 2 |
| tier2-workspace-picker.spec.ts | 2 |
| tier2-search-panel.spec.ts | 0 |
| tier2-markdown-viewer.spec.ts | 1 |
| tier2-binary-viewer.spec.ts | 1 |
| tier2-theme-toggle.spec.ts | 1 |
| **Total** | **24** |

All 24 gaps are documented in the spec files as `test.fixme()` with inline comments
naming the missing testid and referencing SP-087-5 followup.
