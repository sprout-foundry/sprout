# SP-170 — MCP Tool Authorization

Status: Proposed (2026-10-30).

## Problem

MCP tools are the only tools in Sprout with no authorization step. `executeMCPTool`
is reached as a special case *after* `ExecuteTool` reports an unknown tool, so it
never enters the registry and never reaches the approval broker — every built-in
tool's security gate lives inside its handler. The result is that
`mcp_github_delete_repo` runs with no confirmation while the equivalent built-in
would be risk-classified and could be denied.

This matters most in the case SP-169 exists for: content we did not write reaches
the model, the model is steered into calling a destructive MCP tool, and nothing
asks a human.

## What comparable tools do

The ecosystem has converged on **default-ask plus an explicit allowlist**, keyed by
server and by tool. Claude Code expresses this as `mcp__<server>__<tool>` rules in
`permissions.allow`/`ask`/`deny`, with `mcp__<server>__*` for a whole server. Cline,
Cursor and VS Code reach the same place via per-tool auto-approve toggles or a
"trust this server" choice at install. None auto-approve MCP tools by default.

Two details worth borrowing:

- **Deny propagates more broadly than allow.** A built-in `Bash` deny also stops
  Cowork's `mcp__workspace__bash`; a `Bash` allow does not carry over. Fail-closed
  rules should be the permissive ones.
- **Server-level rules come first.** Users think in servers ("I trust this GitHub
  server"); per-tool rules are the exception.

## What we do not do in v1

**Tool annotations are not used.** The MCP spec defines `readOnlyHint`,
`destructiveHint`, `idempotentHint` and `openWorldHint`, with pessimistic defaults
(absent = non-read-only, destructive, non-idempotent, open-world). They are the
natural signal for "auto-approve reads, confirm writes", but they cannot carry a
security property:

- Sprout does not capture them — `mcp.MCPTool` has no annotations field — so using
  them would need a prerequisite change to the tool-discovery path.
- The spec is explicit that annotations are **hints**, and clients **must** treat
  them as untrusted unless the server is trusted. A server can claim
  `readOnlyHint: true` and delete files.

An annotation can therefore only lower friction for a tool that is *already*
allowlisted, where no friction remains. The allowlist already encodes the user's
trust decision; layering a second, self-declared signal on top adds a way to get
that decision wrong without adding safety. Dropped rather than deferred — this is
a decision, not a backlog item.

## Design

### Gate placement

MCP calls are authorized in the agent's tool-dispatch path, before
`executeMCPTool` reaches the manager — not inside the MCP client, which is shared
with non-agent callers and has no notion of a session or a human.

Both entry points are gated:

- direct `mcp_<server>_<tool>` calls, and
- the `mcp_tools` meta-command's `action=call`.

Discovery actions (`list`, `describe`) are not gated: they run no server-side tool.

### Decision

1. If the tool matches the allowlist → run.
2. Otherwise, if an interactive surface can answer → prompt, offering
   *once* / *always allow this tool* / *always allow this server* / *deny*.
3. Otherwise (autonomous run, no surface) → deny, with guidance naming the tool and
   the command that would allow it.

Rule 3 is a deliberate behaviour change, following SP-168's precedent: a run with no
human must not silently execute an ungated third-party action. It is called out in
the CHANGELOG because an existing workflow using MCP tools will start failing until
its tools are allowlisted.

### Allowlist

`Config.ApprovedMCPTools []string`, persisted like `ApprovedShellCommands`.

Entries are `<server>` for a whole server or `<server>__<tool>` for one tool. The
double underscore is the separator because server names may contain single
underscores; matching is on whole segments, so `acme` never matches `acmecorp`.

Matching is case-sensitive and exact — no globs in v1. A glob that silently
over-matches is worse than an explicit list.

### Identity

`executeMCPTool` derives the server by `SplitN(toolName, "_", 2)` after stripping
`mcp_`, which mis-parses a server whose name contains an underscore (`my_server`
becomes server `my`, tool `server_read`). The allowlist makes this a correctness
issue rather than a cosmetic one: a rule for `my` would match a different server.

Resolution must therefore be server-aware — match against the configured server
names, longest first — and an ambiguous name must fail closed rather than guess.
This is a prerequisite for the allowlist, not an optional cleanup.

## Phasing

1. **Gate and allowlist.** ✅ Shipped 2026-10-30 (`db7674c77`) — config field,
   allowlist matching, both entry points plus the `fetch_url` GitHub route, the
   four-way prompt, deny when no surface can answer.
2. **Server-aware identity.** ✅ Shipped 2026-10-30 (`13cea775e`) — resolve by
   registered server name, longest first; refuse to guess when nothing matches.

Complete. Phase 3 (annotations) was dropped rather than deferred — see "What we do
not do in v1" for why.

## Testing

- A non-allowlisted tool with no surface is denied, and the error names the tool.
- A non-allowlisted tool with a surface prompts; each of the four answers does what
  it says, and *always allow* persists.
- An allowlisted tool runs without prompting, in both entry points.
- `list`/`describe` are never gated.
- Server-level entries cover their tools; a tool entry does not cover its siblings.
- `acme` does not match `acmecorp`.
- Ambiguous identity fails closed.

## Compatibility

- MCP tools in interactive sessions gain a prompt until allowlisted — noisy once,
  then quiet.
- MCP tools in autonomous runs are denied until allowlisted. This is the breaking
  part; it is the point of the change.
- Discovery is unaffected, so `sprout mcp list` and `mcp test` still work.
