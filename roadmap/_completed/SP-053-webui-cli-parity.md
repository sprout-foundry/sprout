# SP-053: WebUI CLI Parity — Persona/Depth, Live Tools, Cost Footer

**Status:** ✅ Implemented (persona badges + depth indent in chat, live tool timeline bar, cost/model in status bar)

The CLI had three rounds of polish (spinner+footer+timeline, persona collapse, depth-aware subagent UI) while the WebUI remained flat — creating concrete fidelity gaps. This spec closed three gaps: (1) chat messages now show persona badges and depth-based indent (consuming the `subagent_depth` + `active_persona` metadata from SP-051's event decoration), (2) a `ToolTimelineBar` component renders up to 4 most-recent in-flight or just-completed tool executions with spinners/checks/durations in the chat footer, replacing the generic "Processing..." skeleton, and (3) the status bar shows provider/model/cost/tokens when chat is active via a `ChatStatusBarItems` component. The persona color map was lifted into `@sprout/ui` (`personaColors.ts`) as a single source of truth shared by CLI and WebUI.

## Key decisions

- Persona color map lifted into `@sprout/ui` (`personaColors.ts`) — single source of truth for CLI and WebUI.
- `Message` type extended with optional `persona` and `subagentDepth` fields — backwards-compatible (existing callers unaffected).
- Depth indent: `marginLeft: depth * 12px` on the message container; persona badge rendered to the left.
- `ToolTimelineBar` shows up to 4 tools, completed tools fade after 3 seconds, errors stick until next tool starts.
- Cost color thresholds match CLI: yellow >$1, red >$5.
- `ChatStatusBarItems` renders via the `StatusBar`'s existing `rightItems` slot — no schema change to the shared component.

## Artifacts

- code: `packages/ui/src/utils/personaColors.ts` — shared persona color map + `getPersonaColor()`
- code: `packages/ui/src/types/chat.ts` — `Message` type extended with `persona` + `subagentDepth`
- code: `packages/ui/src/components/MessageBubble.tsx` — renders persona badge + depth indent
- code: `webui/src/components/chat/ToolTimelineBar.tsx` — live tool timeline component
- code: `webui/src/components/chat/ChatStatusBarItems.tsx` — provider/model/cost/tokens display
- code: `webui/src/components/chat/SubagentActivityFeed.tsx` — imports persona colors from `@sprout/ui`
- tests: `packages/ui/src/components/MessageBubble.test.tsx` — persona+depth rendering tests
- tests: `webui/src/components/chat/ToolTimelineBar.test.tsx` — tool timeline state tests

Full specification archived — see git history for original content.
