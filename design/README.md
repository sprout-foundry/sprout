# Design Workspace

Inventory and contract for the Sprout WebUI design assets. This manifest is
the first file an agent reads when `design/` exists. The tree was seeded by
distilling the visual language from the shipped implementation (`webui/src/`
— App.css design-system root plus component CSS and component structure), so
components, wireframes, and flows mirror what the app actually renders.
Components are the primary unit; screens compose them. Keep this file
current as screens and flows are added.

## Layout

| Directory  | Contents                                                                 |
| ---------- | ------------------------------------------------------------------------ |
| `tokens/`    | Design tokens, one `*.tokens.json` file per tier (W3C DTCG format: `$value` / `$type`). Tiers: `color`, `typography`, `spacing`, `sizing`, `radius`, `shadow`, `motion`. No aggregation file; consumers glob. |
| `brand/`     | `brand.md` (name, voice, palette as `{group.token}` references — never raw hex — usage rules). |
| `icons/`     | Icon SVGs, one file per icon, plus optional `sprite.svg`. (Not yet populated; the UI uses lucide-react inline.) |
| `wireframes/` | One SVG per screen. File stem is the screen name. Interactive elements carry stable `id` attributes; navigation targets carry `data-nav="<screen-name>"`. A screen is a *composition* of `components/` — its layout and placement, not the component internals. |
| `components/` | One SVG per reusable component, in its key variants and states, with `{group.token}` refs. File stem is the component name. The composable layer beneath screens: change a component once and every screen that composes it follows. |
| `screens/`   | One self-contained HTML + CSS file per screen. (Not yet populated.) |
| `runtime/`   | Fixed screen-kit assets copied in by the scaffold (SP-143): device chrome `chrome.css` + base documents `base/phone.html` / `base/desktop.html`. Referenced by screens, never hand-edited. |
| `flows/`     | One mermaid `flowchart` per `.mmd` file. For screen flows, node ids are wireframe file stems; edge labels carry trigger semantics (`-- "tap Submit" -->`). |
| `feedback/`  | Human annotations, one JSON file per target. |

Screen, flow, and icon names follow the slug rule `^[a-z0-9]+(-[a-z0-9]+)*$`.

## Device frames

Declared frames used by wireframe and screen sizing (machine-parsed — a
`frames:` line followed by indented `name: WxH` entries, plain integers).
All current wireframes target the desktop frame; the app also ships
tablet/mobile layouts (breakpoint at 769px) for future frames.

frames:
  desktop: 1440x900
  tablet: 768x1024
  mobile: 390x844

## Screens

One line per screen: the screen name (matching a `wireframes/` file stem)
and its purpose.

- `app-shell` — draft — Code-mode default: header bar, sidebar (mode switcher, icon rail, section pane), chat workspace, terminal strip
- `chat` — draft — chat surface: message timeline, tool timeline bar, composer with model selector, queue, and status
- `editor` — draft — editor surface: tab bar, CodeMirror pane, unified diff pane, status footer
- `git-panel` — draft — sidebar git section: status, staging, commit message, history
- `settings` — draft — settings: providers, models, theme, UI scale
- `command-palette` — draft — ⌘K overlay: files, symbols, actions
- `design-flows` — draft — Design mode: flow canvas with wireframe nodes and the design rail
- `design-screens` — draft — Design mode: screens browser with detail pane
- `mobile-sessions` — draft — the screen kit's phone dogfood (SP-143 143.7): session list with data-nav rows into `mobile-session`
- `mobile-session` — draft — phone session detail with declared states (ready/loading/error) and a data-nav back link

## Components

One line per component: the component name (matching a `components/` file
stem) and its purpose. Components are the composable layer beneath screens —
the vocabulary every screen is built from.

- `button` — draft — action buttons: primary/secondary/ghost/icon in default, hover, disabled states
- `input` — draft — text field with label: default, focus, error
- `badge` — draft — status pills: semantic washes, agent state (idle/running/blocked), counts
- `select` — draft — dropdown (model selector, mode switcher): closed chip + portaled open panel
- `tab` — draft — tab bar: active/inactive, modified dot + close, overflow
- `toggle` — draft — switch + checkbox for settings rows
- `sidebar` — draft — navigation macro: pinned header (brandmark, mode switcher), icon rail, section pane, resize; expanded 288px / collapsed 48px
- `chat-message` — draft — timeline blocks: user bubble, agent block, tool-event row, subagent depth indent, todo list
- `composer` — draft — message input: placeholder, model selector, send, queue row
- `file-tree-row` — draft — explorer row: indent, chevron, icon, dirty dot, active wash
- `panel-section` — draft — collapsible section (git panel, settings groups): open/closed
- `token-card` — draft — design token row: group header, swatch/value row, copied state
- `status-bar` — draft — bottom strip of state/branch/model/ctx segments
- `empty-state` — draft — placeholder for absent content: icon, title, hint, optional action

## Composition

How each screen composes components. This is the load-bearing map: a screen
wireframe shows placement, the component spec shows the part. Edit the part.

| Screen             | Components (in order of placement)                                            |
| ------------------ | ----------------------------------------------------------------------------- |
| `app-shell`        | `sidebar` · `chat-message` (empty → `empty-state`) · `composer` · `status-bar` |
| `chat`             | `chat-message` (×n, incl. subagent/todo) · `composer` · `status-bar`           |
| `editor`           | `tab` · `panel-section` (diff) · `badge` (dirty) · `status-bar`                |
| `git-panel`        | `panel-section` (×4) · `input` (message) · `button` (commit) · `badge`         |
| `settings`         | `tab` (sections) · `select` (providers/models) · `toggle` · `input` · `button` |
| `command-palette`  | `input` (query) · `file-tree-row` (results) · `button` (actions)               |
| `design-flows`     | `tab` (rail) · `panel-section` (node detail) · `badge` (status)                |
| `design-screens`   | `tab` (rail) · `file-tree-row` (screen list) · `token-card` (detail pane)      |
| `mobile-sessions`  | `sidebar` (phone session rail, collapsed to a list) · `chat-message` teaser rows |
| `mobile-session`   | `chat-message` (detail) · `badge` (state pane: ready/loading/error)            |

## Flows

One line per flow: the flow name (matching a `flows/` file stem) and its
purpose.

- `navigation` — draft — Code-mode navigation map between the main surfaces
- `design-mode` — draft — mode switching between Code and Design, design rail navigation, file handoff back to Code
- `agent-turn` — draft — one chat turn end to end: send, tool execution, todos, changes, review/commit (non-screen flow; node ids are generic)
- `mobile-screens` — draft — the SP-143 143.7 dogfood pair: phone session list ↔ detail via the runtime's data-nav swap

## Status markers

Screens, components, and flows carry one of: `draft`, `review`, `ready`.
- `draft` — in progress, open for agent iteration
- `review` — awaiting human feedback
- `ready` — approved as the source of truth for implementation

This pass seeded the tree from the implementation, so every screen, component,
and flow is `draft`: it mirrors the app but has not yet been approved as the
forward-looking source of truth.

## Git contract

The design tree is versioned by the workspace repository like any other
source file. Two repository-level lines make that work; `design_validate`
reports a `fix` finding with the exact line to append when either is
missing, and the scaffold appends them without touching existing rules.

`.gitattributes` — mark wireframe, icon, and logo SVGs as HTML so a
rendered PR diff is readable markup (`append`, never replace):

```
design/**/*.svg diff=html
```

`.gitignore` — render PNGs and critique scratch are never sources. Only
`design/.cache/` is ignored; `design/generated/` is deliberately left
alone — committing generated artifacts (with their provenance hashes) is
the project's choice:

```
design/.cache/
```

## Links

Relative links to key assets; the validator checks that these resolve.

- [Brand guide](brand/brand.md)
- [Color tokens](tokens/color.tokens.json)
- [Typography tokens](tokens/typography.tokens.json)
- [Spacing tokens](tokens/spacing.tokens.json)
- [Sizing tokens](tokens/sizing.tokens.json)
- [Radius tokens](tokens/radius.tokens.json)
- [Shadow tokens](tokens/shadow.tokens.json)
- [Motion tokens](tokens/motion.tokens.json)
