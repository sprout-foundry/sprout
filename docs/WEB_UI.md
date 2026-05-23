# Web UI

`sprout` includes a built-in **React-based Web UI** that launches automatically alongside the terminal interface when you run `sprout` or `sprout agent` in interactive mode.

## Features

- **AI Chat Interface** — Real-time streaming conversation with the AI agent, with interactive prompts and tool output rendered inline
- **Code Editor** — CodeMirror-based editor with syntax highlighting, multiple tabs, split views, and unsaved change detection
- **Integrated Terminal** — Full terminal session via WebSocket, with command history and PTY support
- **File Browser** — Browse and navigate your workspace files; click to open in the editor
- **Git Integration** — Stage/unstage files, view diffs, commit with AI-generated messages, AI-powered deep code review
- **Search & Replace** — Workspace-wide search with case-sensitive, whole-word, and regex options
- **Change History** — Browse changelogs, view file revisions with diffs, and rollback changes
- **Settings Panel** — Configure providers, models, MCP servers, skills, and other settings
- **Memory Management** — View, create, edit, and delete persistent memories
- **Provider Catalog** — Browse available providers and models in settings
- **Command Palette** — `Ctrl+Shift+P` for fast navigation (Go to File, toggle views, etc.)
- **Multi-Instance Support** — Multiple `sprout` sessions share a single Web UI server
- **Session Management** — Save and restore chat sessions
- **Image Upload** — Upload images for AI vision analysis
- **Themes** — Multiple dark and light editor themes (Atom One Dark, Dracula, Solarized, etc.)
- **PWA Support** — Installable as a Progressive Web App on desktop and mobile
- **Responsive & Mobile-Friendly** — Collapsible sidebar, touch-friendly controls
- **Customizable Hotkeys** — Keyboard shortcuts configurable in Settings

## Accessing the Web UI

When you start `sprout` in interactive mode, the Web UI is available at `http://localhost:56000` (or the next available port). The terminal displays the URL on startup.

The Web UI binds to `127.0.0.1` (localhost) only — not directly accessible from other machines. See [SSH Tunneling](#ssh-tunneling-remote-web-ui-access) for remote access.

```bash
# Start with Web UI (default)
sprout

# Disable the Web UI
sprout --no-web-ui
sprout agent --no-web-ui "Analyze this code"

# Custom port
sprout agent --web-port 8080

# Daemon mode — keep the Web UI running without an interactive prompt
sprout agent -d
```

## SSH Tunneling (Remote Web UI Access)

The Web UI binds to `127.0.0.1` (localhost only) for security. To access it from your local browser when `sprout` runs on a remote server, use SSH port forwarding.

### Quick Start

```bash
# Forward local port 56000 to the same port on the remote server
ssh -L 56000:127.0.0.1:56000 user@remote-server
# Then open http://localhost:56000 in your local browser
```

### Common Scenarios

```bash
# Tunnel in the background
ssh -fN -L 56000:127.0.0.1:56000 user@remote-server
# Kill when done: kill $(lsof -t -i:56000)

# Remote sprout on a custom port
ssh -L 56000:127.0.0.1:8080 user@remote-server

# Different local port (avoid conflicts)
ssh -L 9090:127.0.0.1:56000 user@remote-server

# Jump host / bastion
ssh -J bastion.example.com -L 56000:127.0.0.1:56000 user@internal-server

# Attach to existing tmux session and start sprout
ssh -t -L 56000:127.0.0.1:56000 user@remote-server "tmux attach -t sprout"
```

### Tips

- The tunnel only works while the SSH connection is alive (unless you used `-f`)
- Make sure `sprout` is running on the remote server before opening the URL (or use `--daemon` mode)
- You can simplify frequent tunnels with an SSH config entry in `~/.ssh/config`:

```ssh-config
Host sprout-remote
    HostName remote-server.example.com
    User youruser
    LocalForward 56000 127.0.0.1:56000
```

Then: `ssh -fN sprout-remote`

## Development

See [webui/DEVELOPMENT.md](../webui/DEVELOPMENT.md) for the Web UI development setup and architecture details.

For the reusable multi-language language-server integration design, see [LSP Architecture Plan](./LSP_ARCHITECTURE.md).
