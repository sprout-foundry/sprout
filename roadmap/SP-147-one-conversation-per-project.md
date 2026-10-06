# SP-147 — One Conversation Per Project; Mode Selects Tools, Not Chats

> **Status (2026-10-02):** Accepted. Supersedes the per-mode chat model of
> SP-142 §1–2 and closes SP-146 (withdrawn — its parallel-run gate is not
> built and its premise, separate design/code chats, is reversed here).
> SP-142's lane *server* fields (`chatSession.Mode`) remain as metadata; the
> two-lane client behavior (per-mode pins, per-mode fresh chats) is removed.

## Decision

Design · Code · Ship are stages of one project sharing **one conversation**.
The mode is a lens the active conversation runs under: it selects which
tools are advertised, which instructions the agent gets, and which surface
the UI shows. It does not select or own a chat.

## Why

- The two-chat model produced the observed bug class: pins orphaned on
  repo switch, cross-mode clicks ejecting the user to the wrong lane,
  design chats invisible from Code, sends racing mode switches.
- Handoff between design and code was never built (no "implement this
  screen" bridge beyond shared files), so the separation carried no
  benefit — only cost.
- One conversation means one runner; the workspace-wide gate
  (`busyChatInWorkspace`) stops being reachable from the UI in practice.

## Tradeoffs accepted

- **No parallel design/code runs in one workspace.** SP-146's scoped
  write-set gate would have allowed them; it dies with this decision.
  Revisit only if single-conversation serialization is observed to hurt.
- **Mode switches keep one history.** The design chat sees the code
  conversation. This is the point (continuity), not a leak to patch.

## Mechanics

1. **One chat per client context.** The chat list is the chat list; no
   per-mode ownership, no per-mode pins, no per-mode boot restore.
2. **Mode switch keeps the active chat.** Switching Design↔Code never
   creates, pins, or switches sessions (SP-142's `useChatModePinning`
   boot/pin logic is reduced to: remember the current chat per repo,
   restore it on boot).
3. **Mode shapes the agent, not the session.** Design mode prepends the
   designer instructions (the persona append is a system-prompt fragment,
   not a persona swap); Code mode uses the standard instructions. The
   mode tag travels with each request so the server can gate tools per
   request, not per session.
4. **Design chat label.** Chats started in Design mode keep a `design`
   origin tag for display; it carries no lane semantics.

## Non-goals

- Per-mode chat history separation (reversed by this spec).
- Cross-agent negotiation or agent-to-agent messaging (SP-142 non-goal,
  unchanged).
- Multi-conversation fan-out for parallel work streams (revisit with
  evidence; the scoped-write gate sketch in SP-146 §1 is the starting
  point if it returns).
