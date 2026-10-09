# @sprout-foundry/design

Design tokens for the Sprout ecosystem. CSS custom properties for colors, spacing, typography, shadows, and motion — the single source of truth for theming across Sprout products.

## Usage

```css
/* Import the tokens (includes dark + light theme via :root) */
@import "@sprout-foundry/design/tokens.css";

/* Optional: CSS reset */
@import "@sprout-foundry/design/reset.css";
```

Themes are controlled via `data-theme` attribute on the root element:

- Dark (default): `<html>` (no attribute needed)
- Light: `<html data-theme="light">`

## Brand tokens

Each brand token has **one meaning in both themes** — a name never changes
hue when the theme flips:

| Token                 | Meaning                                                            |
| --------------------- | ------------------------------------------------------------------ |
| `--brand-teal`        | Brand teal (`#14b8c8`), constant across themes.                    |
| `--brand-frost`       | The light-cyan frost accent (`#68e3ee` dark, `#0891b2` light).     |
| `--brand-sprout`      | The sprout green (`#1ba03d`) — the brand's green leaf hue.         |
| `--brand-active-cyan` | Canonical "active rail / underline" hue (sidebar nav, editor tab). |
| `--brand-navy`        | Brand navy ink (`#10212d`).                                        |

`--brand-frost` is the frost accent in both themes; the green is addressable
as `--brand-sprout`, so a surface that wants the sprout green binds to that
token rather than relying on a theme-dependent meaning of `--brand-frost`.

## Motion tokens

Easing curves and a shared duration scale (defined in the base `:root`):

| Token             | Value                          |
| ----------------- | ------------------------------ |
| `--ease-out`      | `cubic-bezier(0, 0, 0.2, 1)`   |
| `--ease-in-out`   | `cubic-bezier(0.4, 0, 0.2, 1)` |
| `--duration-fast` | `120ms`                        |
| `--duration-base` | `180ms`                        |
| `--duration-slow` | `320ms`                        |

Durations are bare durations, so a callsite composes them with an easing
curve:

```css
.card {
  transition: background var(--duration-fast) var(--ease-out);
}
```

Under `@media (prefers-reduced-motion: reduce)` the duration tokens are
zeroed (`0ms`), so those transitions resolve instantly without the callsite
changing. The easing curves are left intact — a zero duration makes them
moot.

## Installation

```bash
npm install @sprout-foundry/design
```

This package is published to GitHub Packages. Configure your `.npmrc`:

```
@sprout-foundry:registry=https://npm.pkg.github.com
```
