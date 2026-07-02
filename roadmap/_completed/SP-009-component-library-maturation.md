# SP-009: Component Library Maturation — Storybook + @sprout/ui

**Status:** ✅ Implemented (Storybook + MDX docs + Chromatic visual regression; webui imports @sprout/ui)

The `@sprout/ui` package (`packages/ui/`) had 22 component implementations (6,800 lines) extracted from the webui but was not published or documented — it existed only as a local `file:` dependency. This spec added Storybook 8 (Vite builder, React framework) for isolated component development and visual documentation. A `MockAdapter` wraps all stories in `SproutProvider` with mocked API connectivity. Stories were written for all 22 components in 3 tiers (visual components, message/chat components, UI primitives). Chromatic visual regression testing was connected and runs on every PR touching `packages/ui/`. The package has a README, CHANGELOG, and build/type-check CI steps.

## Key decisions

- Storybook 8 with Vite builder (matches existing build infrastructure).
- Mock adapter pattern — all stories wrapped in `SproutProvider` with a no-op `APIAdapter`.
- Chromatic for visual regression — baseline snapshots for all stories, runs on PRs.
- Tiered story writing: visual components first (FileTree, ChatPanel, Terminal), then message/chat, then UI primitives.
- Package remains in monorepo structure — npm publishing deferred (webui still uses `file:` reference in CI).

## Artifacts

- code: `packages/ui/.storybook/main.ts` — Storybook config (Vite builder)
- code: `packages/ui/.storybook/preview.tsx` — global decorators, mock adapter
- code: `packages/ui/src/components/*.stories.tsx` — story files for all 22 components
- code: `packages/ui/README.md` — package documentation
- code: `packages/ui/CHANGELOG.md` — release tracking
- code: `packages/ui/src/index.ts` — exports map (90+ exports)
- tests: `packages/ui/src/contexts/SproutAdapterContext.test.tsx` — 19 tests for provider hooks

Full specification archived — see git history for original content.
