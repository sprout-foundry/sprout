# SP-165 — `sprout mcp add` for HTTP Servers

Status: Proposed (2026-10-19). Split from a 17-item user nit list. Touches
`cmd/mcp_add.go`, `cmd/mcp_add_servers.go`, and the MCP server type in
`pkg/mcp/types.go`.

## Problem

The user's report:

> sprout mcp add doesn't appear to work for web-based mcp servers, it appears
> to require the stdio integration. I had no chance to plug in a url, just a
> command and using a url in place of a command didn't work

Confirmed in the code. The top-level menu offers "Custom MCP Server (stdio or
http)" (`cmd/mcp_add.go`), whose label promises both. The flow it dispatches
to, `setupCustomMCPServer` (`cmd/mcp_add_servers.go`), prompts for:

1. server name,
2. **command** (required — empty is an error),
3. command arguments,
4. environment variables,
5. working directory,
6. auto-start,
7. timeout.

There is no URL prompt and no type selection. It builds
`mcp.MCPServerConfig{Command: command, ...}` with `Type` left empty. A
web-based MCP server is unrepresentable through this path.

The `http-generic` template *does* exist (`pkg/mcp/registry.go`) and the
template path *does* prompt for a URL when `Type == "http"` and the template
URL is empty (`cmd/mcp_add.go`). But the top-level menu **filters both
generics out**:

```go
if t.ID == "http-generic" || t.ID == "stdio-generic" {
    continue
}
```

So the only reachable custom path is the one with no URL support. The menu
label is a lie.

## Design

### 165a. The custom flow asks what kind of server it is

`setupCustomMCPServer` gains a type picker as its first question — stdio or
HTTP — and branches:

- **stdio**: the existing prompts (command, args, env, workdir).
- **HTTP**: URL, headers/auth, env if needed. No command prompt.

The type is recorded on `MCPServerConfig.Type`, which the config struct
already carries and the rest of the system already honors.

### 165b. The menu label matches the flow

Either the label stops promising HTTP, or the flow supports it. 165a makes
the flow support it, so the label becomes true. The two generic templates
stay filtered from the main menu (the custom flow subsumes them), but the
custom flow's label and behavior agree.

### 165c. Validation

An HTTP server with no URL is rejected at entry, the same way a stdio server
with no command is today. A URL that is not a URL is rejected with a message
naming the problem — the user's "using a url in place of a command didn't
work" should become "that looks like a URL; did you mean an HTTP server?"
rather than a confusing failure downstream.

### 165d. `mcp test` covers both types

`sprout mcp test` must exercise an HTTP server the same way it exercises a
stdio one. If it already does (the picker shows `srv.Type` as the detail,
suggesting type-awareness), confirm with a test; if not, fix it. A server
that can be added but not tested is half a feature.

### 165e. Non-interactive path

If `sprout mcp add` has a flag-driven or JSON path, it must accept a URL and
type too — otherwise scripted setup cannot create an HTTP server. Check
whether such a path exists; if it does not, note it as a follow-up rather
than inventing one here.

## Acceptance criteria

- [ ] `sprout mcp add` → custom → HTTP prompts for a URL and writes a server
      with `Type: "http"` and the URL set (test).
- [ ] The same flow → stdio behaves as today (test — no regression).
- [ ] An HTTP server with no URL, or a malformed URL, is rejected with a
      message naming the problem (test).
- [ ] `sprout mcp test` connects to an HTTP server added this way (test).
- [ ] The menu label and the flow agree (no label promising a capability the
      flow lacks).

## Non-goals

- No change to the template registry beyond confirming the generics stay
  filtered.
- No OAuth flow for HTTP MCP servers — bearer/header auth only, matching what
  `MCPServerConfig` carries today.
- No change to how HTTP servers are transported at runtime.

## Open questions

- Whether to un-filter `http-generic`/`stdio-generic` and make them the
  canonical paths instead of the custom flow. The custom flow is more
  discoverable; the templates are more structured. Leaning: keep them
  filtered, make the custom flow complete.
- Whether the type picker should be a question or inferred from the first
  input (a URL-looking string implies HTTP). A question is unambiguous;
  inference is fewer keystrokes. Leaning question.
