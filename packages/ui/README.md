# @sprout/ui

> Reusable React UI components for [Sprout IDE](https://github.com/sprout-foundry/sprout)

[![npm version](https://img.shields.io/npm/v/@sprout/ui)](https://www.npmjs.com/package/@sprout/ui)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A comprehensive collection of IDE-style React components including a code editor, terminal, chat panel, file explorer, Git integration, notifications, and more. Built with CodeMirror 6, xterm.js, and react-virtuoso for performance.

## Installation

```bash
npm install @sprout/ui
```

### Peer Dependencies

Ensure the following peer dependencies are installed in your project:

| Package | Version |
|---------|---------|
| `react` | `>=18.0.0` |
| `react-dom` | `>=18.0.0` |
| `@sprout/events` | `^0.1.0` |

### CSS

Don't forget to import the bundled styles:

```tsx
import '@sprout/ui/dist/style.css';
```

## Usage

### Chat Panel

```tsx
import { ChatPanel } from '@sprout/ui';
import '@sprout/ui/dist/style.css';

function App() {
  return (
    <ChatPanel
      messages={[]}
      onSendMessage={(message) => console.log(message)}
    />
  );
}
```

### Terminal

```tsx
import { TerminalPane, TerminalTabBar } from '@sprout/ui';
import '@sprout/ui/dist/style.css';

function TerminalView() {
  const connection = {
    onOpen: () => console.log('Terminal opened'),
    send: (data: string) => console.log(data),
    onClose: () => console.log('Terminal closed'),
  };

  return <TerminalPane connection={connection} />;
}
```

### Notifications

```tsx
import { NotificationProvider, useNotifications } from '@sprout/ui';
import '@sprout/ui/dist/style.css';

function App() {
  return (
    <NotificationProvider>
      <MyPage />
      <NotificationStack />
    </NotificationProvider>
  );
}

function MyPage() {
  const { addNotification } = useNotifications();

  return (
    <button onClick={() => addNotification({
      type: 'success',
      title: 'Saved',
      message: 'Changes saved successfully.'
    })}>
      Save
    </button>
  );
}
```

### Fuzzy Search

```tsx
import { fuzzyFilter, highlightMatches } from '@sprout/ui';

const items = ['apple', 'banana', 'apricot', 'blueberry'];
const results = fuzzyFilter(items, 'ap');

results.forEach((result) => {
  const highlighted = highlightMatches(result.item, result.highlightChars);
  console.log(`${highlighted} (score: ${result.score})`);
});
```

## Components

| Component | Description |
|-----------|-------------|
| `ChatPanel` | AI chat panel with message display |
| `CommandInput` | Chat command input with history and autocomplete |
| `CommandPalette` | VS Code-style command palette with fuzzy search |
| `ContextMenu` | Right-click context menus |
| `Editor` | CodeMirror-based code editor with language support |
| `FileTree` | Virtualized file explorer with multi-select |
| `GitSidebarPanel` | Git status, diff staging, and commit workflow panel |
| `LiveLog` | Real-time streaming log viewer |
| `MessageBubble` | Individual chat message bubble with persona styling |
| `MessageContent` | Chat message content with Markdown rendering |
| `MessageSegments` | Structured message segment rendering |
| `ChatMessageContextMenu` | Context menu for chat messages |
| `MenuBar` | Application menu bar |
| `NotificationStack` | Toast notification stack |
| `NotificationItem` | Individual notification toast |
| `QueuedMessagesPanel` | Pending message queue display |
| `SelectionActionBar` | Multi-select action toolbar |
| `Sidebar` | IDE sidebar navigation |
| `Skeleton`, `SkeletonText` | Loading placeholders |
| `StatusBar` | IDE status bar with cursor position display |
| `Terminal` | xterm.js-based terminal component |
| `TerminalPane` | Terminal with connection management |
| `TerminalTabBar` | Tab bar for managing multiple terminal sessions |

### Dialogs

```tsx
import { showThemedAlert, showThemedConfirm, showThemedPrompt } from '@sprout/ui';

await showThemedAlert({ title: 'Error', message: 'Something went wrong.' });
const confirmed = await showThemedConfirm({ title: 'Delete', message: 'Are you sure?' });
const answer = await showThemedPrompt({ title: 'Name', label: 'Enter your name', defaultValue: '' });
```

## Contexts

| Context | Hooks |
|---------|-------|
| `NotificationProvider` | `useNotifications()` — Add and manage notifications |
| `SproutProvider` | `useSproutAdapter()`, `useSproutFetch()` — API adapter and fetch helpers |
| `EventsContextProvider` | `useEvents()` — Global event bus integration |

## Hooks

| Hook | Description |
|------|-------------|
| `useMultiSelect` | Multi-selection state management for file trees |
| `flattenVisibleFiles` | Flatten visible entries from a file tree structure |

## Utilities

| Utility | Description |
|---------|-------------|
| `generateUUID()` | Generate a UUID v4 string |
| `copyToClipboard()` | Copy text to the system clipboard |
| `fuzzyScore()` | Calculate fuzzy matching score |
| `fuzzyFilter()` | Filter a list using fuzzy matching |
| `highlightMatches()` | Get highlight indices for fuzzy matches |
| `stripAnsiCodes()` | Remove ANSI escape codes from terminal strings |
| `hasAnsiCodes()` | Check if a string contains ANSI escape codes |
| `ansiToHtml()` | Convert ANSI escape codes to HTML spans |
| `debugLog()` | Conditional debug logging utility |
| `getStatusInfo()` | Map Git status characters to human-readable labels |
| `groupSubagentRuns()` | Group subagent run entries for display |
| `getPersonaColor()` | Get color for a persona identifier |
| `parseMessageSegments()` | Parse message content into renderable segments |
| `detectLineEnding()` | Detect line ending style (CRLF, LF, CR) |

### Command History

| Utility | Description |
|---------|-------------|
| `createEmptyState()` | Create an empty command history state |
| `dedupeCommands()` | Remove duplicate commands from history |
| `loadCommandHistory()` | Load command history from storage |
| `persistCommandHistory()` | Persist command history to storage |

## Types

Key exported types for TypeScript integration:

| Type | Source |
|------|--------|
| `ChatProps`, `Message`, `ToolExecution`, `SubagentRun` | Chat system |
| `EditorProps`, `EditorState`, `EditorBuffer`, `PaneLayout` | Editor |
| `FileTreeProps`, `FileInfo` | File tree |
| `TerminalProps`, `TerminalThemePack`, `TerminalSession` | Terminal |
| `CommandPaletteProps`, `PaletteMode`, `CommandDef` | Command palette |
| `StatusBarProps` | Status bar |
| `GitStatusData`, `GitFile`, `Revision` | Git integration |
| `TextSegment`, `ToolCallSegment`, `TodoUpdateSegment`, `ProgressSegment`, `ResultSegment`, `MessageSegment` | Message content segment types |
| `APIAdapter` | Platform adapter interface |
| `SproutEvent`, `EventsProvider` | Event system |

## Build & Development

```bash
cd packages/ui

npm run build            # Build with Vite (ESM + CJS + type declarations)
npm run test             # Run tests with Vitest
npm run type-check       # TypeScript type checking
npm run storybook        # Start Storybook dev server on port 6006
npm run build-storybook  # Build static Storybook
```

### Build Output

| Output | Format |
|--------|--------|
| `dist/index.esm.js` | ES module |
| `dist/index.cjs.js` | CommonJS |
| `dist/index.d.ts` | TypeScript declarations |
| `dist/style.css` | Bundled styles |

## Project Structure

```
packages/ui/
├── src/
│   ├── components/       # React components
│   ├── contexts/         # React contexts and providers
│   ├── hooks/            # Custom React hooks
│   ├── services/         # Shared services (e.g., notificationBus)
│   ├── types/            # TypeScript type definitions
│   ├── utils/            # Utility functions
│   └── index.ts          # Public API entry point
├── .storybook/           # Storybook configuration
├── vite.config.ts        # Vite library build configuration
├── tsconfig.json         # TypeScript configuration
└── package.json
```

## License

[MIT](https://opensource.org/licenses/MIT)

## Contributing

This package is part of the [Sprout monorepo](https://github.com/sprout-foundry/sprout). See the main repository for contribution guidelines.
