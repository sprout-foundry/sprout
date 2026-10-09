name: MCP Setup
description: Procedural guide for adding, configuring, and troubleshooting MCP (Model Context Protocol) servers in Sprout. Activate when the user asks about MCP setup, wants to connect an external tool/service, or needs to debug a failing MCP server.
---

# MCP Server Setup & Troubleshooting

You are Sprout's MCP integration specialist. Use this skill when the user wants to add, configure, test, or fix MCP server connections. MCP servers give the agent additional tools (GitHub, filesystem, database, browser automation, etc.).

## Key Facts

- **Hot-reload works.** Servers added via the webui settings panel or the `mcp_refresh` tool start immediately — no restart needed.
- **Everything runs through the `mcp_refresh` tool.** Its operations: `list`, `refresh`, `add`, `remove`, `set-credential`, `remove-credential`, `login`, `logout`, `oauth-status`. Do NOT search the filesystem for MCP infrastructure — there is nothing on disk to find. Use `mcp_refresh list` to see state.
- **Server types.** `stdio` (subprocess: `command` + `args`) and `http` (remote Streamable HTTP: `url`). Sprout speaks the Streamable HTTP transport correctly (Accept headers, SSE responses, session IDs).
- **CLI slash command** `/mcp` exists in the CLI with subcommands `add | remove | list | test` (no `tools` subcommand — use `mcp_refresh list` / the webui for tool listings).

---

## 1. Quick Reference: MCP Configuration

| Item | Value |
|------|-------|
| Config file | `~/.config/sprout/config.json` → `mcp.servers` |
| Webui API | `POST /api/settings/mcp/servers` (add), `PUT` (update), `DELETE` (remove) |
| Agent tool | `mcp_refresh` — list / refresh / add / remove / set-credential / login / logout / oauth-status |
| CLI command | `/mcp` — add, remove, list, test |
| Server types | `stdio` (subprocess), `http` (remote Streamable HTTP) |
| Secrets storage | Credential backend (never plaintext in config) |

---

## 2. Adding an MCP Server

### Via the agent tool (recommended)

```
mcp_refresh { "operation": "add", "name": "github", "type": "stdio",
              "command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"],
              "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "<token>" } }
```
The server starts immediately; secret env values are auto-migrated to the credential store.

### Via `/mcp` slash command (CLI)
```
/mcp add                    # interactive prompts for name, command, args, env
/mcp list                   # show configured servers + status
/mcp remove <name>          # stop + remove a server
/mcp test <name>            # test the connection
```

### Common Server Recipes

**GitHub (PAT-based, local subprocess):**
```json
{ "name": "github", "type": "stdio", "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-github"],
  "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "<your-pat>" }, "auto_start": true }
```

**GitHub (OAuth, remote — requires Copilot seat):**
```json
{ "name": "github-remote", "type": "http", "url": "https://api.githubcopilot.com/mcp/", "auto_start": true }
```
Then: `mcp_refresh { "operation": "login", "name": "github-remote" }` — a browser opens; approve; done.

**Figma (OAuth, remote — Dev Mode MCP):**
```json
{ "name": "figma", "type": "http", "url": "https://mcp.figma.com/mcp", "auto_start": true }
```
Then: `mcp_refresh { "operation": "login", "name": "figma" }`.

**Figma desktop (local server, no auth — requires the Figma desktop app running):**
```json
{ "name": "figma-local", "type": "http", "url": "http://127.0.0.1:3845/mcp", "auto_start": true }
```

**Filesystem access:**
```json
{ "name": "filesystem", "type": "stdio", "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"], "auto_start": true }
```

**PostgreSQL:**
```json
{ "name": "postgres", "type": "stdio", "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres", "postgresql://user:pass@host:5432/db"], "auto_start": true }
```

---

## 3. Authentication

Pick the pattern that matches the server:

| Server auth style | What to do |
|---|---|
| **OAuth (Figma, Linear, Notion, GitHub remote...)** | Add the http server, then `mcp_refresh login` (browser flow). `oauth-status` checks state; `logout` clears it. Tokens refresh automatically; a hard 401 means re-login. |
| **Bearer token (most PAT-style APIs)** | Store the token: `mcp_refresh set-credential` with `env_var: FIGMA_TOKEN` (any `*_TOKEN` / `*_API_KEY` / `API_KEY` / `ACCESS_TOKEN` name works). It is sent as `Authorization: Bearer <value>` automatically. |
| **Custom header (X-Figma-Token, X-API-Key...)** | Add the server with a `headers` map: `"headers": { "X-Figma-Token": "{{credential:<server>/X-Figma-Token}}" }`, then `mcp_refresh set-credential` with the same env var name. Header names with hyphens map 1:1 to headers. |
| **GitHub PAT (header name `GITHUB_PERSONAL_ACCESS_TOKEN`)** | Stored credentials under that env var name map to `Authorization: Bearer` automatically. |

Never put a plaintext secret in `headers` or `env` in a persisted payload — use `set-credential` so the value goes to the credential store and the config carries only a placeholder.

**Header rules, in priority order:** (1) explicit `headers` map wins; (2) a credential named `AUTHORIZATION` or `GITHUB_PERSONAL_ACCESS_TOKEN` → Bearer; (3) a credential whose name contains a hyphen (X-API-Key...) → a header of the same name; (4) any other token-shaped credential (`*_TOKEN`, `*_API_KEY`...) → Bearer; (5) an OAuth login (when present) → Bearer.

---

## 4. Verifying a Server Works

```
mcp_refresh { "operation": "list" }          # status + running + per-credential "set"/"missing"
```

If a server shows stopped or errored, check:
1. **Command exists** (stdio): is `npx`/the binary on PATH?
2. **Credentials resolve** (http): `list` shows `credentials: { NAME: "set" | "missing" }`.
3. **Auth style matches** (http): 401 → wrong auth pattern for this server; try §3's OAuth row or a `headers` entry.
4. **Network reachable** (http): `curl -sS -o /dev/null -w '%{http_code}' <url>` from the shell tool.
5. **Timeout**: raise `timeout` (default 30s) for slow starters.

---

## 5. Troubleshooting

### Server won't start
- `mcp_refresh list` for the error status
- stdio: run the command manually to see startup errors (`npx -y <pkg>`)
- `max_restarts` exceeded → the server is disabled; re-add it or restart sprout to reset

### Tools not appearing
- `mcp_refresh list` — is the server running?
- Running with 0 tools → the initialize handshake failed; check the server's own logs
- The tool cache refreshes automatically on add/update/remove; `mcp_refresh refresh` forces a reload

### 401 / auth failures on HTTP servers
- Does the server require OAuth? → `mcp_refresh login`
- Is the credential stored? → `mcp_refresh list` shows per-credential status
- Is the header name right? Some services want a custom header (§3) rather than Bearer
- After changing credentials, `mcp_refresh refresh` reconnects the server

### Auto-discovery
- With `GITHUB_PERSONAL_ACCESS_TOKEN` in the environment and `mcp.auto_discover: true`, sprout auto-configures a GitHub server (stdio, PAT-based)

---

## 6. Configuration Reference

### MCPServerConfig fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique identifier for the server |
| `type` | No (default `stdio`) | `stdio` for subprocess, `http` for remote |
| `command` | stdio only | Executable to run (e.g. `npx`, `node`, `python`) |
| `args` | stdio only | Arguments passed to the command |
| `url` | http only | Endpoint URL of the remote MCP server |
| `env` | No | Environment variables (secrets auto-extracted to credential store) |
| `headers` | http only | HTTP headers sent on every request; values may be `{{credential:server/NAME}}` placeholders |
| `credentials` | No | Credential placeholder map (env var name → `{{credential:...}}`) — managed via set-credential |
| `working_dir` | No | Working directory for subprocess |
| `timeout` | No (default 30s) | Connection/initialization timeout |
| `auto_start` | No (default false) | Start automatically when MCP initializes |
| `max_restarts` | No (default 3) | Max restart attempts before disabling |

### MCP global config (`config.json` → `mcp`)

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | true | Master switch for MCP |
| `auto_start` | false | Start all `auto_start: true` servers on init |
| `auto_discover` | true | Auto-configure GitHub server from env PAT |
| `timeout` | 30s | Default timeout for all servers |
| `servers` | {} | Map of server configs keyed by name |

---

## 7. Response Patterns

**User: "Set up Figma MCP"**
```
→ Figma's remote MCP is OAuth-only — a PAT will NOT work (the PAT is for the REST API, not MCP).
→ mcp_refresh add: { "operation": "add", "name": "figma", "type": "http", "url": "https://mcp.figma.com/mcp" }
→ mcp_refresh { "operation": "login", "name": "figma" } — tell the user a browser window is opening.
→ Verify: mcp_refresh { "operation": "oauth-status", "name": "figma" }, then mcp_refresh { "operation": "list" }.
→ If the user runs the Figma desktop app, offer the local alternative (http://127.0.0.1:3845/mcp, no auth).
```

**User: "Add a GitHub MCP server"**
```
→ PAT path: the stdio recipe from §2 (GITHUB_PERSONAL_ACCESS_TOKEN env).
→ Remote/OAuth path (Copilot seat): https://api.githubcopilot.com/mcp/ + login.
```

**User: "My MCP server isn't working"**
```
→ mcp_refresh list — check status and credential status
→ 401 on http → §3 auth-pattern table; OAuth servers need login, not tokens
→ stopped stdio → command/PATH check
```

**User: "How do I connect to a database?"**
```
→ Recommend the appropriate MCP server (postgres, mysql, etc.)
→ Provide the config recipe
→ Warn about credentials — use the credential store, not plaintext
```

**User: "Can I use a remote HTTP MCP server?"**
```
→ Yes — type "http" + url; Streamable HTTP is fully supported (JSON and SSE responses, sessions).
→ If it needs OAuth: add, then login.
```
