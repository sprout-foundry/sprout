# Changelog

All notable changes to `@sprout/ui` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-05-30

### Added

- Slash command autocomplete component.
- Per-turn cost and token display for assistant messages.
- Depth badge on tool execution cards.
- Per-depth resource usage in subagent activity feed.
- Visual depth indicator for nested subagent runs.
- Read status and mark-all-read for notifications.
- ARIA live regions for command palette results.
- Storybook MDX documentation for FileTree, ChatPanel, GitPanel.
- Chromatic visual regression testing workflow.
- NPM publish GitHub Actions workflow.

### Changed

- Migrated hardcoded `rgba()` colors to CSS custom properties for theme support.
- Migrated hardcoded hex colors to `color-mix()` with CSS variables.
- Updated semantic CSS color tokens for improved theming.

### Deprecated

- `Terminal` component in `@sprout/ui` — use the webui-local Terminal instead.

## [0.1.0] - 2025-07-09

### Added

- Initial package setup for `@sprout/ui`.
- IDE-style components: `Editor` (CodeMirror 6), `Terminal` (xterm.js), `FileTree`, `Sidebar`, `MenuBar`, `StatusBar`.
- Chat components: `ChatPanel`, `CommandInput`, `MessageBubble`, `MessageContent`, `MessageSegments`, `ChatMessageContextMenu`, `QueuedMessagesPanel`, `LiveLog`.
- Git integration: `GitSidebarPanel` for status display, diff staging, and commit workflow.
- Notification system: `NotificationProvider`, `NotificationStack`, `NotificationItem`, and `notificationBus` service.
- Command palette with fuzzy search (`CommandPalette`).
- Context menus (`ContextMenu`) and themed dialog helpers (`showThemedAlert`, `showThemedConfirm`, `showThemedPrompt`).
- Multi-select support with `useMultiSelect` hook and `SelectionActionBar` component.
- `Skeleton` and `SkeletonText` loading placeholder components.
- `TerminalTabBar` for managing multiple terminal sessions.
- Utility exports: `generateUUID`, `copyToClipboard`, `fuzzyScore`, `fuzzyFilter`, `highlightMatches`, `stripAnsiCodes`, `ansiToHtml`, `hasAnsiCodes`, `debugLog`, `getStatusInfo`, `groupSubagentRuns`, `getPersonaColor`, `parseMessageSegments`, `detectLineEnding`.
- Command history utilities: `createEmptyState`, `dedupeCommands`, `loadCommandHistory`, `persistCommandHistory`.
- Shared type definitions for adapter, API responses, editor, file-tree, git, revision, terminal, chat, message-segments, and events.
- `SproutProvider` context with `useSproutAdapter` and `useSproutFetch` hooks for API integration.
- `EventsContextProvider` with `useEvents` hook for global event bus integration.
- Vite-based library build producing ESM, CJS, and TypeScript declaration outputs.
- Storybook integration for component development and documentation.
- Vitest test infrastructure with jsdom environment.
- `react-virtuoso` integration for performant virtualized lists in `FileTree` and log viewers.
- Language support via `@codemirror/lang-*` packages for Go, Python, JavaScript, TypeScript, Rust, Java, C++, SQL, YAML, HTML, CSS, JSON, PHP, and Ruby.
- Emmet support via `@emmetio/codemirror6-plugin`.
- Minimap support via `@replit/codemirror-minimap`.
- Relative line numbers and color highlighting via `@uiw/codemirror-extensions-*`.
- CodeMirror merge/lint/search capabilities exposed through the Editor component.