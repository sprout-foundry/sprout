# Sprout Web UI Development Setup

## Prerequisites

1. **Go 1.26+** — Backend language
2. **Node.js 22+** — Frontend build tooling
3. **Modern web browser** — Chrome, Firefox, Safari, or Edge

## Development Mode

### Option 1: Production Mode (Recommended for testing)

```bash
# Build Go binary with embedded web UI
make build-all

# Run with Web UI
./sprout agent
# Web UI available at http://localhost:56000
```

### Option 2: Hot-Reload UI Development (Separate servers)

Run the real backend and the Vite dev server side by side. UI edits
(`webui/src/**`) apply instantly in the browser via HMR — no rebuild, no
restart. Only Go changes require rebuilding the binary.

```bash
# Terminal 1: Start the Go backend
go build -o sprout . && ./sprout agent --daemon --web-port 56000

# Terminal 2: Start the Vite dev server
cd webui
npm install
npm run dev
# UI available at http://localhost:3000
# Vite proxies /api and /ws (including /terminal) to the backend on :56000
```

Working without an LLM provider (API keys, network)? Use the mock-LLM
backend — a real sprout server whose agent returns canned responses:

```bash
./sprout serve                       # alias for: agent --mock-llm --daemon
# or with an explicit port:
./sprout agent --mock-llm --daemon --web-port 56000
```

Chat turns stream realistically (query → stream chunks → completion), so
chat UI work needs no provider credentials.

**Backend on a different port or host?** Point the proxy at it:

```bash
SPROUT_DEV_BACKEND_URL=http://localhost:56001 npm run dev
```

**One command, free ports:** the e2e stack helper starts both processes,
picks free ports, and tears both down on Ctrl-C:

```bash
npx tsx test/webui/start-stack.mjs
# Prints the Vite URL and writes .ports.json
```

**Gotchas:**

- Changes under `packages/ui/src` need a rebuild before the webui sees
  them: `cd packages/ui && npm run build` (webui type-checks against
  `packages/ui/dist`).
- The embedded UI in the binary is untouched by dev-mode edits — run
  `make build-all` when you want the served-at-:56000 UI to catch up.
- Session create/modify APIs return 403 `shared_mode` in daemon mode; the
  standard stack is a shared-agent setup. Multi-chat happy paths can't be
  exercised here (pin shared-mode UX in e2e instead).

## Architecture

### Backend (Go)
- **Port**: 56000 (default, auto-finds if occupied)
- **WebSocket Endpoints**:
  - `/ws` — Main event WebSocket
  - `/terminal` — Terminal WebSocket
- **HTTP API**: `/api/*` — Full REST API (see [WEBUI_PROTOCOL.md](../docs/WEBUI_PROTOCOL.md))

### Frontend (React + Vite)
- **Development Port**: 3000
- **Production**: Served by Go backend (embedded in binary via `pkg/webui/static/`)
- **Proxies**: All `/api/*` calls and WebSocket connections to backend

## Build Commands

```bash
# Build everything (React UI + Go binary)
make build-all

# Build just the React UI (outputs to webui/build/)
cd webui && npm run build

# Build just the Go binary
go build -o sprout .

# Prepare tree-sitter grammars (needed before first build)
make prepare-grammars
```

## Testing

### Manual Testing Checklist
- [ ] Backend starts and serves web UI at http://localhost:56000
- [ ] Dev mode (`npm run dev`) connects to backend
- [ ] WebSocket connections work in both modes
- [ ] File browser loads directory tree
- [ ] Terminal connects and accepts input
- [ ] Chat view sends and receives messages
- [ ] All view switches work (Chat, Editor, Git, Logs)
- [ ] Status indicators show correct information

## Troubleshooting

### WebSocket connection fails
- Check if backend is running on port 56000
- Verify browser console for error messages

### API calls return HTML instead of JSON
- Backend is not running or not responding
- Check that Vite proxy configuration matches backend port

### File explorer not loading
- Backend API not accessible at `/api/files`
- Verify backend has file system access to workspace
