# Leduit Web UI Development Setup

## Prerequisites

1. **Go backend (ledit)** - The main application
2. **Node.js & npm** - For the React development server
3. **Modern web browser** - Chrome, Firefox, Safari, or Edge

## Development Mode

### Running the Backend and Frontend

**Option 1: Production Mode (Recommended for testing)**
```bash
# Build and run ledit with embedded web UI
cd /path/to/ledit
go build -o ledit
./ledit agent
# Web UI will be available at http://localhost:54321
```

**Option 2: Development Mode (Separate servers)**
```bash
# Terminal 1: Start the Go backend
cd /path/to/ledit
./ledit agent --web-port 54321

# Terminal 2: Start the React development server
cd webui
npm install
npm start
# React dev server will run on http://localhost:3000
# It will proxy API/WebSocket calls to the backend on port 54321
```

## Architecture

### Backend (Go)
- **Port**: 54321 (default, auto-finds if occupied)
- **WebSocket Endpoints**:
  - `/ws` - Main event WebSocket
  - `/terminal` - Terminal WebSocket
- **HTTP API**:
  - `/api/stats` - Statistics
  - `/api/query` - Send query
  - `/api/files` - File browser
  - `/api/git/*` - Git operations
  - `/api/terminal/*` - Terminal operations

### Frontend (React)
- **Development Port**: 3000
- **Production**: Served by Go backend
- **Proxies**: All `/api/*` calls and WebSocket connections to backend

## Current Issues & Fixes

### ✅ Fixed Issues
1. **LogsView Crash** - Null pointer exception on substring()
2. **Activity Log JSON Display** - Raw JSON objects showing instead of formatted text
3. **Context Percentage Display** - Showing "%" instead of "N/A" or actual percentage
4. **File Explorer Error Messages** - Raw JSON parsing errors
5. **Webpack Events Pollution** - Development events cluttering logs
6. **WebSocket Connection** - Development mode WebSocket URL configuration
7. **Terminal UX** - Added clear backend connection status message and helpful instructions
8. **File Explorer UX** - Improved error message with exact command to start backend
9. **Status Bar Layout** - Fixed truncation issues with flex-wrap and better spacing
10. **Development Environment** - Added .env configuration and proxy setup

### 🔄 Known Issues (Work in Progress)

#### Activity Log (Sidebar)
- **Issue**: Activity Log in sidebar shows raw JSON strings instead of formatted text
- **Expected**: Should show formatted log entries with icons (like the main Logs view)
- **Note**: Main Logs view works correctly - only sidebar Activity Log is affected
- **File**: `webui/src/components/Sidebar.tsx` (lines 309-395)
- **Status**: Parsing code added but not executing - likely TypeScript compilation or prop passing issue

#### Layout Issues
- **Issue**: Various CSS layout and responsiveness problems
- **Files**: `webui/src/App.css`, `webui/src/components/*.css`

#### Model/Provider Selection
- **Issue**: Shows "unknown" for model and provider
- **Expected**: Should fetch available models from backend or show placeholder
- **File**: `webui/src/components/Sidebar.tsx`

## Configuration Files

### `.env` (Web UI)
```
REACT_APP_WS_URL=ws://localhost:54321
REACT_APP_TERMINAL_WS_URL=ws://localhost:54321
REACT_APP_API_URL=http://localhost:54321
```

### `package.json` proxy
```json
{
  "proxy": "http://localhost:54321"
}
```

## Testing

### Manual Testing Checklist
- [ ] Backend starts and serves web UI
- [ ] Development mode (npm start) connects to backend
- [ ] WebSocket connections work in both modes
- [ ] File browser loads directory tree
- [ ] Terminal connects and accepts input
- [ ] Chat view sends and receives messages
- [ ] Logs view displays events correctly
- [ ] All view switches work (Chat, Editor, Git, Logs)
- [ ] Status indicators show correct information
- [ ] Responsive layout works on different screen sizes

## Troubleshooting

### WebSocket connection fails
- Check if backend is running on the expected port
- Verify environment variables in `.env`
- Check browser console for error messages

### API calls return HTML instead of JSON
- Backend is not running or not responding on the proxy port
- Check that proxy configuration in `package.json` matches backend port

### File explorer not loading
- Backend API not accessible
- Check `/api/files` endpoint in browser network tab
- Verify backend has file system access

### Terminal input disabled
- Terminal WebSocket not connected
- Backend terminal service not started
- Check browser console for WebSocket errors

## Development Workflow

1. Make changes to React code
2. Development server hot-reloads automatically
3. For backend changes, restart the Go server
4. Test in production mode before committing
5. Run `npm run build` to create production bundle
6. Built files go to `webui/build/` and are embedded in Go binary
