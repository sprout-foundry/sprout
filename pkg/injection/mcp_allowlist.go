package injection

import "strings"

// MCPToolEntrySeparator separates the server from the tool in an
// allowlist entry: `<server>__<tool>`. A double underscore is used
// because server names routinely contain single underscores
// (`my_server`), so a single one would be ambiguous.
const MCPToolEntrySeparator = "__"

// MCPToolAllowlistEntry returns the allowlist entry that would permit one
// specific tool on one server: `<server>__<tool>`. It is the entry named in
// the denial message (SP-170) so the agent reading the error can take a
// different path — either allowlisting the tool, or avoiding it.
func MCPToolAllowlistEntry(server, tool string) string {
	return server + MCPToolEntrySeparator + tool
}

// MCPToolAllowlistServerEntry returns the allowlist entry that would permit
// every tool on one server: `<server>`.
func MCPToolAllowlistServerEntry(server string) string {
	return server
}

// AllowlistedMCPTool reports whether an MCP tool is authorized by the
// user's persistent allowlist (SP-170). Matching is exact and
// case-sensitive — no globs in v1, because a glob that silently
// over-matches is worse than an explicit list.
//
// Two entry forms are recognized:
//
//   - `<server>` matches every tool on that server;
//   - `<server>__<tool>` matches exactly that tool.
//
// Matching is on whole segments, so the entry `acme` never matches the
// server `acmecorp`, and the entry `acme__read` never matches the tool
// `read_file`. An empty server or tool matches nothing: a malformed call
// must fail closed rather than fall through to a wildcard.
func AllowlistedMCPTool(entries []string, server, tool string) bool {
	if server == "" || tool == "" {
		return false
	}
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		entryServer, entryTool, hasTool := strings.Cut(entry, MCPToolEntrySeparator)
		if entryServer != server {
			continue
		}
		if !hasTool {
			// `<server>` — the whole server is trusted.
			return true
		}
		if entryTool == tool {
			return true
		}
	}
	return false
}
