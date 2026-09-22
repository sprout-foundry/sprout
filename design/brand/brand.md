# Sprout Brand

Brand guide for the Sprout WebUI. Palette values are referenced as
`{group.token}` references into `design/tokens/` — never raw hex — so the
guide and the token set cannot drift apart.

## Name

Sprout — the agent's workbench. The name carries the voice: software that
grows with you, tended deliberately.

## Voice

- Clarity first. Organic vocabulary is seasoning, not a costume.
- The organic register — grow, seed, cultivate, shape, prune, branch — is
  used sparingly and only where it fits naturally.
- External design reference is framed as learning, analysis, and
  inspiration. Never "steal like an artist" register.
- Product copy is English only.

## Palette

Brand hues resolve through the `brand` group; theme-dependent pairs are
shown as dark / light.

- Brand teal — {color.dark.brand.teal} (constant across themes)
- Brand frost — {color.dark.brand.frost} (dark) / {color.light.brand.frost} (light, sprout green)
- Active cyan — {color.dark.brand.active-cyan} (dark) / {color.light.brand.active-cyan} (light)
- Brand navy — {color.dark.brand.navy}

## Usage rules

- Surfaces that want the Sprout brand color — rather than an Atom One
  accent — resolve through the `brand` tokens, never through raw values.
- `brand.active-cyan` is the canonical active rail / underline hue:
  sidebar nav items, editor tab accents, active-mode indicators.
- Brand teal is invariant across themes; the frost leaf accent flips from
  cyan (dark) to sprout green (light) so the logo reads on light
  backgrounds.
- The UI chrome itself is theme-driven (`bg`, `border`, `text`, `accent`
  groups); brand tokens are the accent layer on top, not a second theme.
- Status tints (`bg.error`, `bg.success`, `bg.warning`, `bg.info`) are the
  only sanctioned colored surfaces for state; each is a 12% accent tint.
