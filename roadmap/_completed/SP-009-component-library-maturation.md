# SP-009: Component Library Maturation — Storybook + @sprout/ui

**Status:** ✅ Implemented (Storybook + MDX docs + Chromatic visual regression; webui imports @sprout/ui as monorepo sibling)

The `@sprout/ui` package (`packages/ui/`) had 22 component implementations (6,800 lines) extracted from the webui but was not documented — it existed only as a local `file:` dependency. This spec added Storybook 8 (Vite builder, React framework) for isolated component development and visual documentation. A `MockAdapter` wraps all stories in `SproutProvider` with mocked API connectivity. Stories were written for all 22 components in 3 tiers (visual components, message/chat components, UI primitives). Chromatic visual regression testing was connected and runs on every PR touching `packages/ui/`.

The original spec proposed npm-publishing `@sprout/ui` to a public registry. **That part was killed** — `@sprout/ui` is internal monorepo tooling, not a published artifact. The `file:` reference in `webui/package.json` is the correct consumption pattern; no versioned releases are produced. The `packages/ui/.npmrc` (and matching `packages/events/.npmrc`) were deleted; the README was rewritten to document the monorepo-internal consumption model rather than `npm install @sprout/ui` workflow.

## Key decisions

- **Storybook 8 with Vite builder** (matches existing build infrastructure).
- **Mock adapter pattern** — all stories wrapped in `SproutProvider` with a no-op `APIAdapter`.
- **Chromatic for visual regression** — baseline snapshots for all stories, runs on PRs.
- **Tiered story writing**: visual components first (FileTree, ChatPanel, Terminal), then message/chat, then UI primitives.
- **No npm publish.** `@sprout/ui` is monorepo-internal; the `file:` reference is correct and avoids a maintainability tax (versioning, semver, breaking-change coordination) for zero real users. External consumers fork the package.
- **Same applies to `@sprout/events`.** It's a sibling monorepo package; the `.npmrc` was deleted with the same rationale.

## Artifacts

- code: `packages/ui/.storybook/main.ts` — Storybook config (Vite builder)
- code: `packages/ui/.storybook/preview.tsx` — global decorators, mock adapter
- code: `packages/ui/src/components/*.stories.tsx` — story files for all 22 components
- code: `packages/ui/README.md` — package documentation (monorepo-internal)
- code: `packages/ui/src/index.ts` — exports map (90+ exports)
- tests: `packages/ui/src/contexts/SproutAdapterContext.test.tsx` — 19 tests for provider hooks
- removed: `packages/ui/.npmrc`, `packages/events/.npmrc` (npm publish configs)

Full specification archived — see git history for original content.
