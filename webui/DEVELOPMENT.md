# Sprout Web UI Development Setup

## Prerequisites

1. **Go 1.25.0+** — Backend language
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

### Option 2: Development Mode (Separate servers)

```bash
# Terminal 1: Start the Go backend
./sprout agent --web-port 56000

# Terminal 2: Start the Vite dev server
cd webui
npm install
npm run dev
# Vite dev server runs on http://localhost:3000
# It proxies API/WebSocket calls to the backend on port 56000
```

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
