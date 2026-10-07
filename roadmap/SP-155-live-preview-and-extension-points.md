# SP-155 — Live Preview and UI Extension Points

> **Status (2026-10-07):** Partially shipped — preview pane, hosted preview embedding and the public mode registry ship; the visual-regression check for the default UI remains.
> Depends on SP-153 (dev command and port). Related: SP-147 (modes),
> SP-143 (screen kit preview).

## Problem

**Preview.** The running app is never in view while the agent works on
it:

- `register_preview_port`
  (`pkg/agent_tools/register_preview_port_handler.go`) only works when
  `WORKSPACE_ID` is set (hosted workspaces) and returns a URL rather than
  showing anything.
- `webui/src/components/LivePreview.tsx` renders single HTML/SVG files and
  design screens, not a running dev server.
- CLI users switch to a browser tab and refresh by hand.

**Composition.** Environments that embed sprout's web UI (hosted
workspaces, custom shells) can only take the whole UI or nothing. The
mode registry (`webui/src/workspaces/registry.ts`: `WORKSPACE_MODES`,
`DEFAULT_WORKSPACE_MODE`), the layout configuration
(`webui/src/config/layout.ts`) and the `@sprout/ui` package
(`packages/ui/src/index.ts`) exist, but there is no supported way to
register a mode, choose the default, or reuse views like chat, changes
and preview in a different arrangement.

## Design

### 155a. Preview pane

A preview pane in the web UI showing the project's running app:

- Local: start (or detect) the dev server from the starter manifest's
  `dev` command and port (SP-153) and embed `localhost`.
- Hosted: when the agent registers a preview port, the pane embeds the
  returned URL instead of only printing it.
- Reloads on file changes; clear states when the app is starting,
  stopped or failed, with a restart action.
- Available in Code mode as a panel and in the SP-143 screen preview slot
  where it applies.

### 155b. Mode registry extension

- A public registration API for workspace modes (id, label, icon, shell
  component, availability predicate).
- The default mode becomes configuration rather than a constant; the
  built-in default stays `code`.
- Built-in Code and Design modes register through the same API.

### 155c. Composable views

Export the chat, changes, file and preview views from `@sprout/ui` (or a
documented entry point) with typed props, so an embedding shell can
arrange them without forking the web UI. The layout configuration
accepts an embedding-supplied arrangement.

### 155d. Copy keys

Primary UI strings move to copy keys so an embedding environment can
supply its own wording. The default strings are unchanged.

## Acceptance criteria

- [ ] Preview pane shows a local dev server for a starter project (e2e).
- [ ] Preview pane embeds a registered hosted preview URL (test with a
      stub).
- [ ] A test mode registers through the public API and appears in the
      switcher; Code/Design unchanged.
- [ ] An example embedding composes chat + preview from exported views.
- [ ] Default UI is visually unchanged (visual regression).

## Non-goals

- No new built-in modes in this spec (Ship is SP-156).
- No theming beyond the existing token system.

## Open questions

- Whether composable views live in `@sprout/ui` or a new package.
- How embedding shells authenticate the preview iframe in hosted setups.
