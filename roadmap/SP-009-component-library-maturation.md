# SP-009: Component Library Maturation — Publish & Storybook

**Status:** ✅ Implemented (Storybook + MDX docs + Chromatic; webui imports @sprout/ui)
**Depends on:** SP-003 (Webui & Frontend Architecture) — relies on the component extraction already underway  
**Priority:** Medium  
**Effort Estimate:** ~2-3 weeks (2 phases)

## Problem

The `@sprout/ui` package (`packages/ui/`) has been extracted from the webui with working build infrastructure and component implementations, but it is not yet published or documented. Two gaps block adoption by external consumers:

1. **Not published**: The package exists only as a local `file:` dependency (`"@sprout/ui": "file:../packages/ui"` in `webui/package.json`). It cannot be consumed by the Foundry cloud frontend or any other project without a local checkout.

2. **No isolated development/Docs**: Components have no Storybook or visual documentation. When modifying components in `@sprout/ui`, developers must boot the full sprout webui to see changes. There is no way to develop, test, or document components in isolation.

## Current State

### Package Structure

```
packages/ui/
├── package.json          # @sprout/ui v0.1.0, MIT license
├── vite.config.ts        # Vite library mode, ESM + CJS output
├── tsconfig.json         # Build config (no path aliases)
├── tsconfig.build.json   # Declaration-only emission for dts plugin
├── src/
│   ├── index.ts          # 86 lines, 90+ exports (types + implementations)
│   ├── components/       # 22 component files (6800 lines total)
│   ├── contexts/         # 4 context files (adapter, events, notifications)
│   ├── hooks/            # 1 hook (useMultiSelect)
│   ├── services/         # 1 service (notificationBus)
│   ├── types/            # 7 type files (adapter, editor, events, file-tree, git-types, notification)
│   └── utils/            # 10 utility files (clipboard, fuzzyMatch, log, etc.)
├── dist/                 # Build output (ESM + CJS + declarations + CSS)
└── node_modules/
```

### Component Inventory

**Full implementations (usable standalone):**

| Component | Lines | Status |
|-----------|-------|--------|
| `FileTree` | 1601 | Complete — virtualized tree, context menus, drag-drop, keyboard nav |
| `ChatPanel` | 758 | Complete — virtualized messages, streaming, tool rendering |
| `CommandInput` | 935 | Complete — multi-line, file attachments, slash commands, history |
| `GitPanel` | 605 | Complete — diff view, staging, commit, branches |
| `Terminal` | 278 | Complete — xterm.js integration, tabs, zoom |
| `TerminalPane` | 241 | Complete — terminal emulator pane |
| `CommandPalette` | 278 | Complete — fuzzy search, commands/files/symbols |
| `Sidebar` | 180 | Complete — composable sidebar sections |
| `TerminalTabBar` | 228 | Complete — tab management with context menus |
| `QueuedMessagesPanel` | 224 | Complete — editable queued prompts |
| `MessageSegments` | 180 | Complete — rich message rendering (code, tool calls, thinking) |
| `ChatMessageContextMenu` | 175 | Complete — copy, insert-at-cursor context menu |
| `ContextMenu` | 133 | Complete — positioned context menu |
| `StatusBar` | 133 | Complete — slot-based status bar |
| `NotificationItem` | 117 | Complete — animated notification |
| `GitFileSection` | 91 | Complete — git file status icons |
| `MessageContent` | 68 | Complete — Markdown with syntax highlighting |
| `LiveLog` | 61 | Complete — auto-scrolling log viewer |
| `MessageBubble` | 45 | Complete — message container with actions |
| `SelectionActionBar` | 26 | Complete — batch action bar |
| `ThemedDialog` | — | Stub — uses native browser dialogs |
| `GitSidebarPanel` | — | Stub — re-exports types only |

**Supporting files:**

| File | Purpose |
|------|---------|
| `command_input_history.ts` | Command history state management + persistence |
| `terminalConstants.ts` | Font size defaults |
| `git-constants.ts` | Git status color mappings |

### Build Output

- **ESM**: `dist/index.esm.js`
- **CJS**: `dist/index.cjs.js`
- **Types**: `dist/index.d.ts` (via vite-plugin-dts)
- **CSS**: `dist/style.css` (single bundled file)
- **Exports map**: `package.json` exports for `.` and `./dist/style.css`

### Tests

One test file: `src/contexts/SproutAdapterContext.test.tsx` — covers `SproutProvider`, `useSproutAdapter()`, `useSproutFetch()` hooks (19 tests passing).

### Webui Integration

```json
// webui/package.json
"@sprout/ui": "file:../packages/ui"
```

The webui imports components directly:
```typescript
import { MessageBubble, MessageContent, ChatMessageContextMenu, LiveLog, 
         SelectionActionBar, TerminalTabBar } from '@sprout/ui';
import { showThemedPrompt, showThemedConfirm } from '@sprout/ui';
import '@sprout/ui/dist/style.css';
```

## Proposed Solution

### Phase 1: Publish @sprout/ui to npm

**Goal:** Make the package installable by any project.

#### P1.1: Prepare for Publishing

- **Verify build**: Ensure `npm run build` produces clean output with no errors
- **Version strategy**: Use `0.x.y` for initial releases (breaking changes expected)
- **README**: Minimal README with installation, usage example, and badge
- **Changelog**: Add `CHANGELOG.md` to track releases
- **CI check**: Add `npm run build && npm run type-check` to CI pipeline

#### P1.2: npm Publishing

- **Registry**: npmjs.com under `@sprout` org scope
- **Access**: `public` (already set in `publishConfig`)
- **Automation**: GitHub Actions workflow for publish on tag (`@sprout/ui@*`)
- **Pre-publish checks**: `prepublishOnly` script already runs `npm run build`

#### P1.3: Update Webui Dependency

```json
// webui/package.json (after publish)
"@sprout/ui": "^0.1.0"
```

Replace `file:../packages/ui` reference with versioned npm dependency. Keep monorepo structure for development but install from npm in CI.

#### P1.4: CLI for Foundry Integration

Document how Foundry (or any project) consumes the package:

```bash
npm install @sprout/ui
```

```typescript
import { FileTree, ChatPanel, Terminal, StatusBar } from '@sprout/ui';
import '@sprout/ui/dist/style.css';
```

### Phase 2: Storybook

**Goal:** Isolated component development and visual documentation.

#### P2.1: Setup Storybook

Install Storybook 8 in `packages/ui/`:

```bash
cd packages/ui
npx storybook@latest init
```

Configuration:
- **Builder**: Vite (matches existing build)
- **Framework**: React
- **TypeScript**: Full support via existing tsconfig

#### P2.2: Mock Adapter for Stories

Create a `MockAdapter` that implements `APIAdapter`:

```typescript
// packages/ui/.storybook/mocks/MockAdapter.ts
export const mockAdapter: APIAdapter = {
  name: 'mock',
  fetch: async (input, init) => new Response(JSON.stringify({})),
  getWebSocketURL: () => null,
  requiresBackendHealthCheck: false,
  fileOpsViaAPI: false,
  showOnboarding: false,
  supportsSSH: false,
  supportsInstances: false,
  supportsLocalTerminal: false,
  supportsSettings: false,
};
```

Wrap all stories in `SproutProvider` with the mock adapter.

#### P2.3: Write Stories (Priority Order)

**Tier 1 — Visual components (most value):**
- `StatusBar.stories.tsx` — Show different slot configurations
- `FileTree.stories.tsx` — Various tree sizes, selection states, context menus
- `Terminal.stories.tsx` — Terminal with different themes
- `GitPanel.stories.tsx` — Different git states (clean, dirty, merge conflict)
- `CommandPalette.stories.tsx` — Commands, files, symbols modes

**Tier 2 — Message/Chat components:**
- `MessageBubble.stories.tsx` — Different message types
- `MessageContent.stories.tsx` — Markdown, code blocks, links
- `MessageSegments.stories.tsx` — Tool calls, thinking, reasoning
- `ChatPanel.stories.tsx` — Message list with streaming

**Tier 3 — UI primitives:**
- `ContextMenu.stories.tsx` — Menu positioning, items
- `NotificationItem.stories.tsx` — Types, animations
- `SelectionActionBar.stories.tsx` — Different counts
- `CommandInput.stories.tsx` — Empty, editing, with attachments

#### P2.4: Chromatic / Visual Regression

- Connect to Chromatic for visual regression testing
- Run on every PR that touches `packages/ui/`
- Baseline snapshots for all stories

#### P2.5: Documentation Pages

- Write MDX docs pages for complex components (FileTree, ChatPanel, GitPanel)
- Document the `SproutProvider` context and adapter pattern
- Document CSS theming/customization approach

## Implementation Phases

### Phase 1: Publish (Week 1)

**New files:**
- `packages/ui/CHANGELOG.md`
- `packages/ui/README.md`
- `.github/workflows/publish-ui.yml` — Publish workflow

**Modified files:**
- `webui/package.json` — Replace `file:` with versioned dependency
- `.github/workflows/build.yml` — Add `@sprout/ui` build + type-check step

### Phase 2: Storybook (Weeks 2-3)

**New files:**
- `packages/ui/.storybook/main.ts` — Storybook config (Vite builder)
- `packages/ui/.storybook/preview.tsx` — Global decorators, mock adapter
- `packages/ui/.storybook/mocks/MockAdapter.ts` — Mock API adapter
- `packages/ui/src/components/*.stories.tsx` — One story file per component
- `packages/ui/src/components/*.mdx` — Documentation for complex components

**Modified files:**
- `packages/ui/package.json` — Add storybook devDependencies and scripts

## Success Criteria

| Metric | Target |
|--------|--------|
| `npm install @sprout/ui` | Works from any project |
| `npm run build` in `packages/ui` | Clean, zero errors |
| Storybook stories | All 22 components have at least 1 story |
| Visual regression | Chromatic baseline for all stories |
| CI | `@sprout/ui` build/type-check runs on every PR |
| Webui | Installs `@sprout/ui` from npm (not `file:`) in CI |

## Open Questions

1. **Private or public npm package?** — Currently `publishConfig.access: "public"`. If this should be private, update to `"restricted"`.
2. **Monorepo tooling?** — Should we adopt turborepo/nx for managing the webui + ui package build order, or keep the current Makefile approach? → Start with Makefile, migrate if needed.
3. **Foundry consumption** — Does Foundry need to import from npm or from a git submodule? → npm is cleaner, defer to Foundry team.
4. **CSS-in-JS vs. CSS modules** — Current approach bundles all CSS into `dist/style.css`. Should components support CSS modules for tree-shaking? → Future scope, current approach is fine for v0.x.

## Files Reference

| File | Action |
|------|--------|
| `packages/ui/package.json` | Modify: add storybook deps, finalize version |
| `packages/ui/vite.config.ts` | Existing: build configuration |
| `packages/ui/src/index.ts` | Existing: exports map |
| `packages/ui/src/components/*.tsx` | Existing: 22 component implementations |
| `packages/ui/.storybook/main.ts` | Create: Storybook config |
| `packages/ui/.storybook/preview.tsx` | Create: decorators, mock adapter |
| `packages/ui/README.md` | Create: package documentation |
| `packages/ui/CHANGELOG.md` | Create: release tracking |
| `.github/workflows/publish-ui.yml` | Create: npm publish workflow |
| `.github/workflows/build.yml` | Modify: add ui build check |
| `webui/package.json` | Modify: versioned npm dependency |
