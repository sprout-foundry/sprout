# @sprout-foundry/design

Design tokens for the Sprout ecosystem. CSS custom properties for colors, spacing, typography, and shadows — the single source of truth for theming across Sprout products.

## Usage

```css
/* Import the tokens (includes dark + light theme via :root) */
@import '@sprout-foundry/design/tokens.css';

/* Optional: CSS reset */
@import '@sprout-foundry/design/reset.css';
```

Themes are controlled via `data-theme` attribute on the root element:
- Dark (default): `<html>` (no attribute needed)
- Light: `<html data-theme="light">`

## Installation

```bash
npm install @sprout-foundry/design
```

This package is published to GitHub Packages. Configure your `.npmrc`:
```
@sprout-foundry:registry=https://npm.pkg.github.com
```
