# SP-146 — Design Chats Run Independently and Work From Given Inputs

> **Status (2026-09-30):** Proposed — a note for the design workspace work
> (SP-140 series). Owner-reported problems; no code changed yet.

## Problems

1. **A design chat and a coding chat can't run at the same time.** SP-142 §3
   gates every chat in a workspace behind whichever one is running ("one
   workspace, one runner", `busyChatInWorkspace` in
   `pkg/webui/chat_sessions.go`, 409 `workspace_busy`). Design and code
   chats share the workspace, so a design request queues behind a coding
   run and vice versa. They are unrelated work and shouldn't be coupled.
2. **Design starts without enough detail, then goes looking for it.** When a
   design chat has no brief or assets, the agent searches the user's disk for
   something to work from instead of asking. It should work from what the
   user gives it.

## Proposal

### 1. Scope the runner gate by write set

The gate exists because two agents editing the same files collide. Make it
about overlapping writes rather than the workspace as a whole:

- A chat's lane declares where it writes: design lane → the design tree
  (`design/`, per SP-140-1); code lane → everything except `design/`.
- The gate blocks a new run only when its write scope overlaps a running
  chat's. Design and code then run in parallel; two code chats still queue.
- Enforce the scope in the file tools for the design lane (writes outside
  the design tree are rejected with a clear error), so "doesn't overlap" is a
  guarantee, not a convention.
- Reads stay unrestricted (a design chat reads components to extract tokens).

### 2. Inputs are given, not hunted

- The design chat starts by asking for what it needs when the request is
  thin: a brief, reference images, a Figma link, or a folder **inside the
  project** to learn from.
- Inputs arrive explicitly: attached images, a Figma link (existing MCP
  path), or a picked project folder.
- File search and listing in the design lane are confined to the workspace;
  no scanning of home, Desktop, Downloads or other projects.
- The discover-from-code path (SP-140) stays, scoped to the workspace.

## Acceptance

- A design chat and a coding chat run concurrently in one workspace; two
  coding chats still serialize.
- A design-lane write outside the design tree fails with an explanatory
  error.
- A thin design request gets clarifying questions, and no tool call in the
  design lane touches a path outside the workspace.
