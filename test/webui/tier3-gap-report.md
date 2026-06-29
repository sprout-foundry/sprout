# TestIDs Gap Report — SP-087-6 Tier 3 Edge Case Tests

This report lists every `data-testid` that Tier 3 spec files wanted but is **not**
present in `test/webui/testids.ts`. The registry is frozen for this task; these
gaps are documented here for future remediation.

All affected tests are written as `test.fixme()` with the missing testid list
included in the test description. They will become runnable once the missing
testids are added to the registry.

---

## `tier3-empty-workspace.spec.ts`

- `file-tree-empty` — empty-state indicator rendered in the file tree panel when the workspace has no files (NOT in registry)
- `editor-empty` — empty state in the editor pane when no file is open (NOT in registry; `editor` IS in the registry)

## `tier3-empty-session-list.spec.ts`

- `chat-sessions-empty` — empty-state CTA rendered in the sidebar session list when no chats exist (NOT in registry)
- `chat-item` — individual chat/session item in the sidebar (NOT in registry; **also documented in tier2-multi-chat gap report**)

## `tier3-large-session.spec.ts`

- `chat-message` — individual message element in the chat list (NOT in registry; existing specs use `messageList.locator('> *')`)
- `chat-message-user` — user message role wrapper (NOT in registry)
- `chat-message-assistant` — assistant message role wrapper (NOT in registry)

## `tier3-long-paths.spec.ts`

- `file-tree` — the file tree panel container (NOT in registry; existing specs use `[class*="file-tree"]` fallback)
- `file-tree-item` — individual file/folder item in the file tree (NOT in registry; existing specs use `[class*="file-tree-item"]` fallback)

## `tier3-special-chars.spec.ts`

- `file-tree` — the file tree panel container (NOT in registry; **shared with tier3-long-paths**)
- `file-tree-item` — individual file/folder item in the file tree (NOT in registry; **shared with tier3-long-paths**)

## `tier3-network-failure.spec.ts`

- `chat-input` — chat input textarea (NOT in registry; existing specs use `[data-testid="chat-shell"] textarea` fallback; **also documented in tier2-steer-input gap report**)
- `chat-send` — send button (NOT in registry; existing specs use keyboard Enter fallback; **also documented in tier2-steer-input gap report**)
- `disconnected-overlay` — the DisconnectedOverlay component shown when the WebSocket drops (NOT in registry; existing specs use `[class*="disconnected-overlay"]` fallback)

---

## Summary

| Spec File | Tier3-only Gaps | Shared with Tier 2 |
|---|---|---|
| tier3-empty-workspace.spec.ts | 2 | 0 |
| tier3-empty-session-list.spec.ts | 1 | 1 (`chat-item`) |
| tier3-large-session.spec.ts | 3 | 0 |
| tier3-long-paths.spec.ts | 2 | 0 |
| tier3-special-chars.spec.ts | 2 (shared with tier3-long-paths) | 0 |
| tier3-network-failure.spec.ts | 1 | 2 (`chat-input`, `chat-send`) |
| **Unique testids** | **9** | **3** |
| **Total unique gaps** | **12** | |

All 11 gaps are documented in the spec files as `test.fixme()` with the missing
testid list embedded in the test name (mirroring the Tier 2 `test/webui/testids-gap-report.md`
pattern). When the testids are added to `test/webui/testids.ts`, these tests can be
unwrapped back to plain `test()` calls.
