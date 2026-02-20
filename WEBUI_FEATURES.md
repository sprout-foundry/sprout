# Ledit WebUI Feature Documentation

> Last Updated: 2026-02-19
> Status: Core features working. Some performance improvements needed.

## Overview

The Ledit WebUI is a React-based web interface for the Ledit AI code editor. It provides a modern, responsive UI for interacting with the AI agent, editing files, managing git operations, and viewing system logs.

---

## 1. Navigation & Layout

### 1.1 Sidebar Navigation
- **Toggle Sidebar** - Button to collapse/expand the sidebar
- **View Switcher** - Four icon buttons for switching between views:
  - 💬 Chat View
  - 📝 Editor View  
  - 🔀 Git View
  - 📋 Logs View

### 1.2 Main Navigation Bar
- Appears at the top of the main content area
- Contains the same view switcher as the sidebar
- Theme toggle button (☀️/🌙)

---

## 2. Chat View

### 2.1 Chat Input
- **Text Input** - Multi-line text area for entering queries
- **Send Button** - Sends the message to the AI agent
- **Clear Button** - Clears the input field (Ctrl+C)
- **Refresh Button** - Refreshes history from terminal (Ctrl+R)

### 2.2 Chat Display
- **Message List** - Shows conversation history
- **User Messages** - Right-aligned, user-typed messages
- **AI Messages** - Left-aligned, AI-generated responses
- **Tool Execution Display** - Shows tool execution progress

### 2.3 File References
- Files can be referenced in chat using `@filename` syntax
- Clicking a file in the sidebar inserts the reference into chat

### 2.4 Chat Stats (Sidebar)
- **Queries** - Number of queries sent
- **Status** - Connection status indicator (🟢/🔴)

---

## 3. Editor View

### 3.1 File Browser (Sidebar)
- Lists all files in the project (62 files)
- File icons based on type:
  - 📄 - Regular file
  - 📁 - Directory
  - 📝 - Markdown file
  - 📜 - JavaScript
  - 📘 - TypeScript
  - 🐹 - Go
  - 🐍 - Python
- Click to open file in editor

### 3.2 Editor Tabs
- Shows open file tabs
- Close button on each tab
- Active tab indicator

### 3.3 Code Editor (CodeMirror)
- Syntax highlighting for multiple languages:
  - JavaScript/TypeScript
  - Python
  - Go
  - JSON
  - HTML
  - CSS
  - Markdown
  - PHP
- Line numbers
- Code folding
- Search/Replace (Ctrl+F)
- Auto-completion

### 3.4 Editor Toolbar
- **Toggle Line Numbers** (Ctrl+L) - Show/hide line numbers
- **Go to Line** (Ctrl+G) - Jump to specific line
- **Split Vertically** - Open file in vertical split
- **Split Horizontally** - Open file in horizontal split
- **Theme Toggle** - Switch between dark/light theme

### 3.5 Split Panes
- Support for up to 3 split panes
- Resize handles between panes
- Close split button

### 3.6 File Operations
- **Save** (Ctrl+S) - Save file to disk
- **Modified Indicator** - Shows if file has unsaved changes
- **Character Count** - Shows line and character count

---

## 4. Git View

### 4.1 Branch Information
- Current branch name
- Ahead/behind commit count

### 4.2 File Lists
- **Staged Files** - Files ready to commit
- **Modified Files** - Files with changes
- **Untracked Files** - New files not in git
- **Deleted Files** - Removed files

### 4.3 Git Actions
- **Select All** - Select all files
- **Deselect All** - Deselect all files
- **Stage Selected** - Stage selected files
- **Unstage Selected** - Unstage selected files
- **Discard Selected** - Discard changes to selected files
- **Commit** - Create commit with message

### 4.4 File Selection
- Checkbox for each file
- Shows file status (M, A, D, ??)
- Shows changes (+additions/-deletions)

---

## 5. Logs View

### 5.1 Log Display
- Timestamped log entries
- Color-coded by level:
  - ✅ Success
  - ⚠️ Warning
  - ❌ Error
  - ℹ️ Info

### 5.2 Filtering
- **Level Filter** - Filter by log level (All, Info, Success, Warning, Error)
- **Category Filter** - Filter by category (Query, Tool, File, System, Stream)
- **Search** - Search logs by text

### 5.3 Log Controls
- **Auto-scroll** - Toggle auto-scroll to latest
- **Clear** - Clear all logs
- **Total/Filtered Count** - Shows log counts

---

## 6. Terminal

### 6.1 Terminal Display
- Shows terminal output from the agent
- ANSI color support
- Scrollable output area

### 6.2 Terminal Controls
- **Expand/Collapse** - Toggle terminal size
- **Clear** - Clear terminal output
- **Status Indicator** - Shows connection status

---

## 7. Configuration (Sidebar)

### 7.1 Provider Selection
- Dropdown to select AI provider:
  - OpenAI
  - Anthropic
  - Ollama
  - DeepInfra
  - Cerebras
  - Z.AI
  - MiniMax

### 7.2 Model Selection
- Dropdown to select model for the chosen provider

### 7.3 Connection Status
- WebSocket connection indicator
- Server connection status

---

## 8. Status Bar

### 8.1 Connection Info
- WebSocket status (Live/Disconnected)
- Provider:Model display

### 8.2 Statistics
- **Tokens** - Total tokens used
- **Context** - Context usage percentage
- **Cost** - API cost estimate
- **Iterations** - Current/max iterations

### 8.3 Activity Indicators
- **📡** - Query in progress
- **📝** - Edits made

---

## 9. File Edits Panel

### 9.1 Recent Edits
- Shows files that have been edited
- Displays action type (modified, created, deleted)
- Timestamp of edits
- Lines added/deleted

---

## 10. System Features

### 10.1 Service Worker (PWA)
- Offline capability
- App caching
- Update notifications

### 10.2 Theme Support
- Dark mode (default)
- Light mode toggle
- Theme persists across sessions

### 10.3 Responsive Design
- Mobile-friendly layout
- Collapsible sidebar on mobile
- Touch-friendly controls

---

## 11. Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| Ctrl+S | Save file |
| Ctrl+L | Toggle line numbers |
| Ctrl+G | Go to line |
| Ctrl+F | Find in editor |
| Ctrl+C | Clear input (when not selecting) |
| Ctrl+R | Refresh history |

---

## 12. WebSocket Events

The WebUI communicates with the server via WebSocket:

- `connection_status` - Server connection state
- `query_started` - New query initiated
- `query_progress` - Query progress updates
- `stream_chunk` - Streaming response chunks
- `query_completed` - Query finished
- `tool_execution` - Tool execution status
- `file_changed` - File modification events
- `terminal_output` - Terminal output
- `error` - Error events
- `metrics_update` - Stats updates

---

## 13. API Endpoints

### REST API
- `GET /api/stats` - Server statistics
- `GET /api/providers` - Available AI providers
- `GET /api/files` - Project file list
- `GET /api/file?path=...` - File content
- `POST /api/query` - Send AI query
- `GET /api/git/status` - Git status
- `POST /api/git/stage` - Stage files
- `POST /api/git/unstage` - Unstage files
- `POST /api/git/discard` - Discard changes
- `POST /api/git/commit` - Create commit
- `GET /api/terminal/history` - Terminal history
- `POST /api/terminal/history` - Add to history

### WebSocket
- `WS /ws` - Real-time communication
