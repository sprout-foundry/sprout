---
name: MCP Setup
description: Procedural guide for adding, configuring, and troubleshooting MCP (Model Context Protocol) servers in Sprout. Activate when the user asks about MCP setup, wants to connect an external tool/service, or needs to debug a failing MCP server.
---

# MCP Server Setup & Troubleshooting

You are Sprout's MCP integration specialist. Use this skill when the user wants to add, configure, test, or fix MCP server connections. MCP servers give the agent additional tools (GitHub, filesystem, database, browser automation, etc.).

## Key Fact: Hot-Reload Works

MCP servers added through the webui settings panel **start immediately** — no restart needed. The agent's tool list is refreshed automatically after add/update/delete. If adding via config file directly (editing `config.json`), use `/mcp` to trigger a reload.

---

## 1. Quick Reference: MCP Configuration

| Item | Value |
|------|-------|
| Config file | `~/.config/sprout/config.json` → `mcp.servers` |
| Webui API | `POST /api/settings/mcp/servers` (add), `PUT` (update), `DELETE` (remove) |
| CLI command | `/mcp` — interactive management |
| Server types | `stdio` (subprocess), `http` (remote SSE/streamable) |
| Secrets storage | Credential backend (not plaintext in config) |

---

## 2. Adding an MCP Server

### Via Webui (recommended — hot-reloads instantly)

```
POST /api/settings/mcp/servers
{
  "name": "github",
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-github"],
  "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "<token>" },
  "auto_start": true
}
```
The server starts immediately. No restart needed.

### Via `/mcp` slash command (interactive)
```
/mcp add                    # interactive prompts for name, command, args, env
/mcp list                   # show configured servers + status
/mcp remove <name>          # stop + remove a server
```

### Common Server Recipes

**GitHub (PAT-based, local subprocess):**
```json
{
  "name": "github",
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-github"],
  "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "<your-pat>" },
  "auto_start": true,
  "max_restarts": 3
}
```

**GitHub (OAuth, remote — requires Copilot seat):**
```json
{
  "name": "github-remote",
  "type": "http",
  "url": "https://api.githubcopilot.com/mcp/",
  "auto_start": true
}
```

**Filesystem access:**
```json
{
  "name": "filesystem",
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"],
  "auto_start": true
}
```

**PostgreSQL:**
```json
{
  "name": "postgres",
  "type": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres", "postgresql://user:pass@host:5432/db"],
  "auto_start": true
}
```

**HTTP/SSE remote server:**
```json
{
  "name": "remote-tools",
  "type": "http",
  "url": "https://my-server.example.com/mcp",
  "auto_start": true
}
```

---

## 3. Verifying a Server Works

After adding a server, verify it's connected and exposing tools:

```
/mcp list                    # Check status: "running" vs "stopped" vs "error"
/mcp tools                   # List tools exposed by all running MCP servers
```

If the server shows as stopped or error, check:
1. **Command exists**: Is `npx` or the binary on PATH?
2. **Credentials valid**: For GitHub, does the PAT have the right scopes?
3. **Network reachable**: For HTTP servers, can you curl the URL?
4. **Timeout**: Increase `timeout` (default 30s) for slow-starting servers

---

## 4. Troubleshooting

### Server won't start
- Check `/mcp list` for the error status
- Verify the `command` binary exists: `which npx` or `which <binary>`
- For `npx`-based servers, run the command manually to see startup errors:
  ```
  npx -y @modelcontextprotocol/server-github
  ```
- Check `max_restarts` — if exceeded, the server is disabled. Restart sprout or re-add the server to reset.

### Tools not appearing
- Run `/mcp list` to see server status
- If the server is running but has 0 tools, the server process started but its `initialize` handshake failed. Check the server's own logs.
- The tool cache refreshes automatically when servers are added/updated/removed via the webui. For a manual refresh, add/remove any server (which triggers the refresh), or restart sprout.

### Credential errors
- Secrets are stored in the credential backend, not plaintext in config.json
- The `env` field in config shows masked values — the real values are injected at runtime
- To update credentials: `PUT /api/settings/mcp/servers/<name>/credentials`

### Auto-discovery
- If `GITHUB_PERSONAL_ACCESS_TOKEN` is set in the environment AND `mcp.auto_discover` is true, sprout auto-configures a GitHub server
- This only triggers for PAT-based (local) servers, not remote OAuth

---

## 5. Configuration Reference

### MCPServerConfig fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique identifier for the server |
| `type` | No (default `stdio`) | `stdio` for subprocess, `http` for remote |
| `command` | stdio only | Executable to run (e.g. `npx`, `node`, `python`) |
| `args` | stdio only | Arguments passed to the command |
| `url` | http only | Endpoint URL of the remote MCP server |
| `env` | No | Environment variables (secrets auto-extracted to credential store) |
| `working_dir` | No | Working directory for subprocess |
| `timeout` | No (default 30s) | Connection/initialization timeout |
| `auto_start` | No (default false) | Start automatically when MCP initializes |
| `max_restarts` | No (default 3) | Max restart attempts before disabling |

### MCP global config (`config.json` → `mcp`)

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | false | Master switch for MCP |
| `auto_start` | false | Start all `auto_start: true` servers on init |
| `auto_discover` | false | Auto-configure GitHub server from env PAT |
| `timeout` | 30s | Default timeout for all servers |
| `servers` | {} | Map of server configs keyed by name |

---

## 6. Response Patterns

**User: "Add a GitHub MCP server"**
```
→ Ask for their GitHub PAT (or check if GITHUB_PERSONAL_ACCESS_TOKEN is set)
→ POST to /api/settings/mcp/servers with the stdio GitHub recipe
→ Confirm it's running: /mcp list
→ Show available tools: /mcp tools
```

**User: "My MCP server isn't working"**
```
→ /mcp list — check the status
→ If stopped: check command/PATH, credentials, network
→ If error: read the error message, suggest fix
→ If running but no tools: check server handshake, try /mcp reload
```

**User: "How do I connect to a database?"**
```
→ Recommend the appropriate MCP server (postgres, mysql, etc.)
→ Provide the config recipe
→ Warn about credentials — use env vars or credential store, not plaintext
```

**User: "Can I use a remote HTTP MCP server?"**
```
→ Yes — set type to "http" and provide the url
→ Show the HTTP recipe
→ Note: no subprocess management needed, just connectivity
```
