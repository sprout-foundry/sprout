# @sprout/ui

Shared component library for Sprout. Contains reusable UI primitives (buttons, trees, terminals, editors, etc.) with no application-specific coupling.

**Full documentation**: [`docs/COMPONENT_LIBRARY.md`](../../docs/COMPONENT_LIBRARY.md)

## Quick Start

```bash
npm run build         # Build the library
npm test              # Run tests
npm run storybook     # Start Storybook on :6006
```

## Adding a Component

See [docs/COMPONENT_LIBRARY.md](../../docs/COMPONENT_LIBRARY.md) for the primitive vs composite rubric and step-by-step guide.

## Import Direction

```
webui  ──>  @sprout/ui   ✅
@sprout/ui  ──>  webui   ❌  (never)
```
