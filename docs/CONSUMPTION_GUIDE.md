# @sprout/ui — Consumption Guide

This guide explains how to integrate `@sprout/ui` into your own React application. It covers installation, setup, and usage examples for all exported components, contexts, hooks, and utilities.

## Introduction

`@sprout/ui` is a comprehensive library of IDE-style React components originally built for the [Sprout](https://github.com/sprout-foundry/sprout) development environment. It provides production-ready components including a CodeMirror-powered code editor, xterm.js-based terminal, chat panel, file tree, Git integration, notifications, command palette, and more.

If you're building an IDE-like application, a developer tool, or any interface that needs rich code editing and terminal capabilities, `@sprout/ui` gives you a solid foundation.

## Installation

```bash
npm install @sprout/ui
```

Or with other package managers:

```bash
yarn add @sprout/ui
pnpm add @sprout/ui
```

## Peer Dependencies

`@sprout/ui` requires the following peer dependencies. Ensure they are installed in your project:

| Package | Version | Purpose |
|---------|---------|---------|
| `react` | `>=18.0.0` | React framework |
| `react-dom` | `>=18.0.0` | React DOM rendering |
| `@sprout/events` | `^0.1.0` | Event bus for cross-component communication |

```bash
npm install react react-dom @sprout/events
```

If you already have React installed, you only need:

```bash
npm install @sprout/events
```

## Importing CSS

All component styles are bundled into a single CSS file. Import it **once** at the root of your application:

```js
import '@sprout/ui/dist/style.css';
```

> **Tip:** Import this in your app's entry point (e.g., `index.js`, `main.tsx`) before any component imports so styles are available globally.

## Getting Started: Minimal App Setup

At minimum, wrap your application with `SproutProvider` and `EventsContextProvider` to enable all features:

```tsx
import React from 'react';
import { SproutProvider, EventsContextProvider, ChatPanel, Sidebar } from '@sprout/ui';
import '@sprout/ui/dist/style.css';

function App() {
  return (
    <EventsContextProvider>
      <SproutProvider adapter={null}>
        <div style={{ display: 'flex', height: '100vh' }}>
          <Sidebar
            items={[
              { id: 'chat', label: 'Chat', icon: 'chat' }
            ]}
            activeItem="chat"
            onItemSelect={(id) => console.log('Selected:', id)}
          />
          <div style={{ flex: 1 }}>
            <ChatPanel
              messages={[]}
              onSendMessage={(msg) => console.log('Message:', msg)}
            />
          </div>
        </div>
      </SproutProvider>
    </EventsContextProvider>
  );
}

export default App;
```

### Provider Options

| Provider | Required Props | Purpose |
|----------|---------------|---------|
| `EventsContextProvider` | None | Global event bus for component communication |
| `SproutProvider` | `adapter` — an `APIAdapter` object or `null` | Provides API adapter and fetch helpers via context. Pass `null` for local-only use. |
| `NotificationProvider` | None | Manages toast notification state for `NotificationStack` |

## Component Usage Examples

### ChatPanel

Display a chat interface with AI-style message bubbles:

```tsx
import { ChatPanel, Message } from '@sprout/ui';

const messages: Message[] = [
  { id: '1', role: 'user', content: 'Hello!' },
  { id: '2', role: 'assistant', content: 'Hi there! How can I help?' },
];

function Chat() {
  return (
    <ChatPanel
      messages={messages}
      onSendMessage={(message) => {
        console.log('User sent:', message);
        // Add your message to state or send to backend
      }}
    />
  );
}
```

### Terminal

Integrate a fully-functional terminal using xterm.js:

```tsx
import { TerminalPane } from '@sprout/ui';

function TerminalView() {
  // Create a terminal connection (pty, WebSocket, etc.)
  const connection = {
    onOpen: () => console.log('Terminal opened'),
    send: (data: string) => {
      // Forward data to your backend/pty
      console.log('Terminal input:', data);
    },
    onClose: () => console.log('Terminal closed'),
    onResize: (cols: number, rows: number) => {
      console.log('Terminal resized:', cols, 'x', rows);
    },
  };

  return <TerminalPane connection={connection} />;
}
```

### Editor

Use the CodeMirror 6-based code editor with language support:

```tsx
import { Editor } from '@sprout/ui';

function EditorView() {
  const code = `function fibonacci(n: number): number {
  if (n <= 1) return n;
  return fibonacci(n - 1) + fibonacci(n - 2);
}`;

  return (
    <Editor
      value={code}
      language="typescript"
      onChange={(value) => console.log('Code changed:', value)}
      theme="dark"
    />
  );
}
```

Supported languages include Go, Python, JavaScript, TypeScript, Rust, Java, C++, Ruby, PHP, SQL, YAML, JSON, HTML, CSS, Markdown, and more.

### FileTree

Display a virtualized file explorer:

```tsx
import { FileTree, FileInfo } from '@sprout/ui';

const files: FileInfo[] = [
  { path: 'src/', isDir: true, children: [
    { path: 'src/index.ts', isDir: false },
    { path: 'src/app.tsx', isDir: false },
  ]},
  { path: 'package.json', isDir: false },
  { path: 'README.md', isDir: false },
];

function FileExplorer() {
  return (
    <FileTree
      files={files}
      onFileSelect={(file) => console.log('Selected:', file.path)}
      onFileOpen={(file) => console.log('Open:', file.path)}
    />
  );
}
```

### Notifications

Show toast notifications using `NotificationProvider` and `NotificationStack`:

```tsx
import { NotificationProvider, NotificationStack, useNotifications } from '@sprout/ui';

function App() {
  return (
    <NotificationProvider>
      <MyApp />
      <NotificationStack />
    </NotificationProvider>
  );
}

function MyApp() {
  const { addNotification } = useNotifications();

  return (
    <button
      onClick={() =>
        addNotification({
          type: 'success',
          title: 'Saved',
          message: 'Changes saved successfully.',
        })
      }
    >
      Save
    </button>
  );
}
```

Supported notification types: `info`, `success`, `warning`, `error`.

### CommandPalette

Add a VS Code-style command palette with fuzzy search:

```tsx
import { useState } from 'react';
import { CommandPalette } from '@sprout/ui';

function App() {
  const [paletteOpen, setPaletteOpen] = useState(false);

  const commands = [
    { id: 'new-file', label: 'New File', icon: 'file' },
    { id: 'new-terminal', label: 'New Terminal', icon: 'terminal' },
    { id: 'settings', label: 'Settings', icon: 'settings' },
  ];

  return (
    <>
      <button onClick={() => setPaletteOpen(true)}>⌘P Command Palette</button>
      {paletteOpen && (
        <CommandPalette
          commands={commands}
          onClose={() => setPaletteOpen(false)}
          onCommand={(cmd) => console.log('Executed:', cmd)}
        />
      )}
    </>
  );
}
```

### Dialogs

Use the built-in themed dialogs for alerts, confirmations, and prompts:

```tsx
import { showThemedAlert, showThemedConfirm, showThemedPrompt } from '@sprout/ui';

// Alert (informational)
await showThemedAlert({
  title: 'Success',
  message: 'Your changes have been saved.',
});

// Confirmation dialog
const confirmed = await showThemedConfirm({
  title: 'Delete File',
  message: 'Are you sure you want to delete this file?',
});
if (confirmed) {
  // Proceed with deletion
}

// Prompt with user input
const fileName = await showThemedPrompt({
  title: 'New File',
  label: 'Enter file name:',
  defaultValue: 'untitled.txt',
});
if (fileName) {
  // Create file with the given name
}
```

### Sidebar and StatusBar

Build an IDE-style layout:

```tsx
import { Sidebar, StatusBar } from '@sprout/ui';

function IdeLayout() {
  const menuItems = [
    { id: 'files', label: 'Explorer', icon: 'folder' },
    { id: 'git', label: 'Git', icon: 'git-branch' },
    { id: 'search', label: 'Search', icon: 'search' },
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      <div style={{ display: 'flex', flex: 1 }}>
        <Sidebar
          items={menuItems}
          activeItem="files"
          onItemSelect={(id) => console.log('Sidebar:', id)}
        />
        <div style={{ flex: 1 }}>
          {/* Main content area */}
        </div>
      </div>
      <StatusBar
        cursorPosition={{ line: 10, column: 5 }}
        filesChanged={3}
        branches="main"
      />
    </div>
  );
}
```

### Git Panel

Display Git status, diffs, and commit workflow:

```tsx
import { GitSidebarPanel } from '@sprout/ui';

function GitView() {
  return (
    <GitSidebarPanel
      gitStatus={null}
      onStageFile={(file) => console.log('Stage:', file)}
      onUnstageFile={(file) => console.log('Unstage:', file)}
      onCommit={() => console.log('Commit')}
    />
  );
}
```

## Available Components

| Category | Components |
|----------|------------|
| **Panels** | `ChatPanel`, `Terminal`, `TerminalPane`, `TerminalTabBar` |
| **Editors** | `Editor` |
| **Trees** | `FileTree` |
| **Navigation** | `Sidebar`, `MenuBar`, `StatusBar`, `CommandPalette` |
| **Notifications** | `NotificationStack`, `NotificationItem` |
| **Git** | `GitSidebarPanel` |
| **Chat** | `MessageBubble`, `MessageContent`, `MessageSegments`, `ChatMessageContextMenu`, `QueuedMessagesPanel`, `CommandInput`, `SelectionActionBar` |
| **UI Primitives** | `ContextMenu`, `LiveLog`, `Skeleton`, `SkeletonText` |
| **Dialogs** | `showThemedAlert`, `showThemedConfirm`, `showThemedPrompt` |

## Contexts and Hooks

| Context | Hook | Description |
|---------|------|-------------|
| `NotificationProvider` | `useNotifications()` | Add and manage toast notifications |
| `SproutProvider` | `useSproutAdapter()`, `useSproutFetch()` | Access API adapter and fetch helpers |
| `EventsContextProvider` | `useEvents()` | Subscribe to and emit global events |

| Hook | Description |
|------|-------------|
| `useMultiSelect()` | Multi-selection state management (for `FileTree`, etc.) |
| `flattenVisibleFiles()` | Flatten visible entries from a file tree structure |

## Utilities

| Utility | Description | Example |
|---------|-------------|---------|
| `fuzzyFilter(items, query)` | Filter a list with fuzzy matching | `fuzzyFilter(['apple', 'apricot'], 'ap')` |
| `highlightMatches(item, chars)` | Get highlight indices for fuzzy matches | `highlightMatches(result.item, result.highlightChars)` |
| `fuzzyScore(item, query)` | Score a single item against a query | `fuzzyScore('apple', 'ap')` |
| `copyToClipboard(text)` | Copy text to clipboard | `copyToClipboard('Hello')` |
| `generateUUID()` | Generate a UUID v4 | `const id = generateUUID()` |
| `stripAnsiCodes(text)` | Remove ANSI escape codes | `stripAnsiCodes('\x1b[31mred\x1b[0m')` |
| `hasAnsiCodes(text)` | Check for ANSI escape codes | `hasAnsiCodes(text)` |
| `ansiToHtml(text)` | Convert ANSI codes to HTML spans | `ansiToHtml('\x1b[32mgreen\x1b[0m')` |
| `parseMessageSegments(content)` | Parse message content into renderable segments | `parseMessageSegments(msg)` |
| `detectLineEnding(content)` | Detect line ending style (CRLF, LF, CR) | `detectLineEnding(content)` |
| `getStatusInfo(statusChar)` | Map Git status chars to labels | `getStatusInfo('M')` → `"Modified"` |
| `getPersonaColor(id)` | Get color for a persona ID | `getPersonaColor('coder')` |
| `groupSubagentRuns(entries)` | Group subagent run entries | `groupSubagentRuns(log)` |

## TypeScript Support

`@sprout/ui` ships with full TypeScript type declarations in `dist/index.d.ts`. All exports are typed and available when you import from `'@sprout/ui'`.

### Key Type Exports

| Type | Description |
|------|-------------|
| `ChatProps`, `Message`, `ToolExecution`, `SubagentRun`, `SubagentActivity`, `LogEntry`, `TodoItem`, `TodoStatus`, `FileEdit`, `LiveLogLine` | Chat system |
| `TextSegment`, `ToolCallSegment`, `TodoUpdateSegment`, `ProgressSegment`, `ResultSegment`, `MessageSegment` | Message segment types |
| `EditorProps`, `EditorState`, `EditorBuffer`, `EditorPane`, `PaneLayout`, `PaneSize` | Editor |
| `FileTreeProps`, `FileInfo` | File tree |
| `TerminalProps`, `TerminalThemePack`, `CreateTerminalConnection` | Terminal |
| `TerminalSession`, `AttachableSession` | Terminal sessions |
| `CommandPaletteProps`, `PaletteMode`, `CommandDef` | Command palette |
| `StatusBarProps`, `CursorPosition` | Status bar |
| `SidebarProps` | Sidebar |
| `GitStatusData`, `GitFile`, `GitSidebarPanelProps` | Git integration |
| `Revision`, `RevisionFile`, `RevisionDetailFile` | Revision system |
| `Notification`, `NotificationType`, `NotificationData` | Notifications |
| `APIAdapter`, `PlatformNavItem` | Platform adapter |
| `SproutEvent`, `EventsProvider` | Event system |

### Using Types

```ts
import type { ChatProps, EditorProps, Message, FileInfo, TerminalProps } from '@sprout/ui';

const props: EditorProps = {
  value: 'const x = 1;',
  language: 'javascript',
  onChange: (v) => console.log(v),
};
```

## Module Formats

`@sprout/ui` is published in multiple module formats for maximum compatibility:

| Format | Path | Usage |
|--------|------|-------|
| **ES Module** | `dist/index.esm.js` | Default for modern bundlers (Vite, Webpack 5, etc.) |
| **CommonJS** | `dist/index.cjs.js` | For CommonJS environments |
| **TypeScript** | `dist/index.d.ts` | Type declarations |
| **CSS** | `dist/style.css` | Bundled styles |

Your bundler will automatically resolve the correct format based on your configuration.

## Theme Customization

Components in `@sprout/ui` use CSS custom properties (CSS variables) for styling. You can override these variables in your application to customize colors, fonts, and spacing.

To identify the available CSS variables, inspect the component styles in your browser's developer tools. Override them in a global stylesheet loaded after `@sprout/ui/dist/style.css`:

```css
/* Example: override theme colors */
:root {
  --sprout-bg: #1e1e1e;
  --sprout-fg: #d4d4d4;
  --sprout-accent: #007acc;
}
```

> **Note:** A comprehensive theming guide with all available variables is planned for a future release.

## Troubleshooting

### "Missing peer dependency" errors

If you see errors about missing peer dependencies, install the required packages:

```bash
npm install react react-dom @sprout/events
```

### CSS styles are not applied

Make sure you've imported the CSS file at the root of your application:

```js
import '@sprout/ui/dist/style.css';
```

This should be imported **before** any component usage and only **once** in your application.

### TypeScript module resolution issues

If TypeScript can't resolve `@sprout/ui`, check your `tsconfig.json`:

```json
{
  "compilerOptions": {
    "moduleResolution": "node",
    "esModuleInterop": true
  }
}
```

For monorepos or workspace setups, ensure the package is properly linked or installed in your project's `node_modules`.

### Components appear unstyled or broken

- Verify that `SproutProvider` and `EventsContextProvider` are wrapping your component tree
- Check that `@sprout/events` is installed — some components require the event bus
- Make sure you're not importing from internal paths (e.g., `@sprout/ui/dist/components/...`) — always import from `'@sprout/ui'`

### Terminal doesn't connect

`TerminalPane` requires a connection object with `onOpen`, `send`, `onClose`, and optionally `onResize` methods. Ensure your connection implementation properly forwards data to a PTY, WebSocket, or other terminal backend.

## Further Reading

- [Component Library Architecture](COMPONENT_LIBRARY.md) — How Sprout's component system is organized
- [packages/ui README](https://github.com/sprout-foundry/sprout/tree/main/packages/ui) — Full API reference, Storybook, and development setup
- [Sprout Main Repository](https://github.com/sprout-foundry/sprout) — Source code, examples, and issues
