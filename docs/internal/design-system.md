# Design System Rules

The webui uses a token-driven design system rooted in `webui/src/App.css` (canonical, both themes) and mirrored in `packages/ui/.storybook/tokens.css` (Storybook isolation).

## Token catalogue

**Surfaces** — `--bg-primary`, `--bg-secondary`, `--bg-tertiary`, `--bg-elevated`, `--bg-surface`, `--bg-hover` (alias for elevated).
**Status-tinted surfaces** — `--bg-error`, `--bg-success`, `--bg-warning`, `--bg-info` (12% accent-tinted via `color-mix`).
**Text** — `--text-primary`, `--text-secondary`, `--text-tertiary`, `--text-muted`.
**Accents** — `--accent-primary`, `--accent-secondary`, `--accent-success`, `--accent-warning`, `--accent-error`, `--accent-info`.
**Accent foregrounds** — `--accent-fg` (white on accent surfaces), `--accent-warning-fg` (#1a1a2e dark text on amber), `--accent-on-primary`.
**Borders** — `--border-subtle`, `--border-default`, `--border-strong`, `--border-focus`.
**Brand** — `--brand-teal`, `--brand-frost`, `--brand-active-cyan`, `--brand-navy`.

## Hard rules

1. **No raw hex/rgba in CSS or inline `style={{}}`.** Use a token. Exceptions: pure black/white scrims, HTML preview iframes, file-type/language icons.
2. **No hex fallbacks on defined tokens.** Write `var(--accent-primary)`, not `var(--accent-primary, #6366f1)`.
3. **Status-tinted backgrounds use `color-mix`, not literal rgba.**
4. **Text on colored background uses matching foreground token.** On `--accent-warning` → `var(--accent-warning-fg)`.
5. **Every interactive element gets `:focus-visible`.** Pattern: `outline: none; box-shadow: 0 0 0 2px var(--accent-primary)`.
6. **Don't `@media (prefers-color-scheme: dark)` for theming.** Use `:root[data-theme='light']`.
7. **`transition: all` is an anti-pattern.** List specific properties.
8. **No hardcoded white-alpha inset highlights** on themable surfaces. Use `color-mix`.

## Adding a new token

Add to **both** `webui/src/App.css` and `packages/ui/.storybook/tokens.css`.

## Shared-package CSS (`packages/ui/src/components/*.css`)

Tokens resolved by the consumer's stylesheet. Fallbacks allowed: `var(--accent-primary, #61afef)`.

## Verification

```bash
git diff origin/main -- 'webui/src/**/*.css' 'packages/ui/src/**/*.css' \
  | grep -E '^\+.*(#[0-9a-fA-F]{3,6}|rgba\([0-9])' \
  | grep -vE 'rgba\(0, 0, 0|var\(--'
```
