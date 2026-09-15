# Design Workspace

Inventory and contract for this project's design assets. This manifest is
the first file an agent reads when `design/` exists. Keep it current as
screens and flows are added.

## Layout

| Directory  | Contents                                                                 |
| ---------- | ------------------------------------------------------------------------ |
| `tokens/`    | Design tokens, one `*.tokens.json` file per tier (W3C DTCG format: `$value` / `$type`). Tiers: `color`, `typography`, `spacing`, `sizing`, `motion`, plus project tiers. No aggregation file; consumers glob. |
| `brand/`     | `brand.md` (name, voice, palette as `{group.token}` references — never raw hex — usage rules) plus logo SVGs. |
| `icons/`     | Icon SVGs, one file per icon, plus optional `sprite.svg` of `<symbol id="icon-name">` entries. |
| `wireframes/` | One SVG per screen. File stem is the screen name. Interactive elements carry stable `id` attributes; navigation targets carry `data-nav="<screen-name>"`. |
| `screens/`   | One self-contained HTML + CSS file per screen. Inline `<style>` or workspace-relative CSS only; no external network resources. |
| `flows/`     | One mermaid `flowchart` per `.mmd` file. For screen flows, node ids are wireframe file stems; edge labels carry trigger semantics (`-- "tap Submit" -->`). |
| `feedback/`  | Human annotations, one JSON file per target. |

Screen, flow, and icon names follow the slug rule `^[a-z0-9]+(-[a-z0-9]+)*$`.

## Device frames

Declared frames used by wireframe and screen sizing (machine-parsed; keep
this exact shape — a `frames:` line followed by indented `name: WxH`
entries, plain integers):

```
frames:
  desktop: 1440x900
  mobile: 390x844
  tablet: 768x1024
```

## Screens

One line per screen: the screen name (matching a `wireframes/` file stem)
and its purpose.

```
- `login` — draft — sign-in entry point with credential recovery
- `inbox` — ready — message list with unread badges
```

## Flows

One line per flow: the flow name (matching a `flows/` file stem) and its
purpose.

```
- `sign-up` — draft — account creation from landing to first-run
```

## Status markers

Screens and flows carry one of: `draft`, `review`, `ready`.
- `draft` — in progress, open for agent iteration
- `review` — awaiting human feedback
- `ready` — approved as the source of truth for implementation

## Links

Relative links to key assets; the validator checks that these resolve.

```
- [Brand guide](brand/brand.md)
- [Color tokens](tokens/color.tokens.json)
```
