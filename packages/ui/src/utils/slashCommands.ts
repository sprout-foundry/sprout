export interface SlashCommand {
  name: string;
  description: string;
  isAlias?: boolean;
  aliasOf?: string;
}

export const SLASH_COMMANDS: SlashCommand[] = [
  { name: 'clear', description: 'Clear the current session' },
  { name: 'commit', description: 'Stage changes and commit' },
  { name: 'changes', description: 'List tracked file changes' },
  { name: 'compact', description: 'Compact context window' },
  { name: 'edit', description: 'Open editor to compose a query' },
  { name: 'exec', description: 'Execute a shell command' },
  { name: 'exit', description: 'Exit the agent' },
  { name: 'extend', description: 'Extend the context window' },
  { name: 'help', description: 'Show available commands and usage' },
  { name: 'index', description: 'Enable/disable workspace embedding index' },
  { name: 'init', description: 'Initialize workspace with instructions' },
  { name: 'log', description: 'Show commit history' },
  { name: 'mcp', description: 'Manage MCP server configuration' },
  { name: 'model', description: 'List or switch model' },
  { name: 'persona', description: 'List or switch active persona' },
  { name: 'provider', description: 'List or switch provider' },
  { name: 'review', description: 'Request a code review' },
  { name: 'review deep', description: 'Request a deep code review' },
  { name: 'risk-profile', description: 'Set or view the risk profile' },
  { name: 'rollback', description: 'Revert tracked changes' },
  { name: 'sessions', description: 'List or manage sessions' },
  { name: 'shell', description: 'Execute a shell command' },
  { name: 'setup', description: 'Show current configuration summary' },
  { name: 'status', description: 'Show working tree status' },
  { name: 'stats', description: 'Show token and cost statistics' },
  { name: 'subagent model', description: 'Configure subagent model' },
  { name: 'subagent persona', description: 'Manage a specific subagent persona' },
  { name: 'subagent personas', description: 'List available subagent personas' },
  { name: 'subagent provider', description: 'Configure subagent provider' },
  // Aliases
  { name: '?', description: 'Alias for /help', isAlias: true, aliasOf: 'help' },
  { name: 'h', description: 'Alias for /help', isAlias: true, aliasOf: 'help' },
  { name: 'm', description: 'Alias for /model', isAlias: true, aliasOf: 'model' },
  { name: 'p', description: 'Alias for /provider', isAlias: true, aliasOf: 'provider' },
  { name: 'q', description: 'Alias for /exit', isAlias: true, aliasOf: 'exit' },
  { name: 'x', description: 'Alias for /exit', isAlias: true, aliasOf: 'exit' },
];

/**
 * Bounded cache for getMatchingSlashCommands results.
 * Key = prefix.toLowerCase().trim(), value = sorted SlashCommand[].
 * Safe to keep indefinitely because SLASH_COMMANDS is a static constant
 * (never mutates at runtime). The cache grows at most to the number of
 * distinct non-empty prefixes the user types, which is trivially small
 * (< 100 in practice, far below any memory concern).
 */
const _matchCache = new Map<string, SlashCommand[]>();

export function getMatchingSlashCommands(prefix: string): SlashCommand[] {
  const normalized = prefix.toLowerCase().trim();
  const cached = _matchCache.get(normalized);
  if (cached !== undefined) return cached;
  const result = SLASH_COMMANDS.filter(cmd => cmd.name.toLowerCase().startsWith(normalized)).sort((a, b) => {
    // Aliases sort after real commands
    if (a.isAlias !== b.isAlias) return a.isAlias ? 1 : -1;
    return a.name.localeCompare(b.name);
  });
  _matchCache.set(normalized, result);
  return result;
}
