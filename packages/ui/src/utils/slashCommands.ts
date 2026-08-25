export interface SlashCommand {
  name: string;
  description: string;
  isAlias?: boolean;
  aliasOf?: string;
  /** True for server-returned ARGUMENT candidates (not command names) — the
   *  autocomplete renders these without the leading "/". Defaults to false. */
  isArgument?: boolean;
}

// Mirror of pkg/agent_commands/commands.go NewCommandRegistry().
// Real commands sorted alphabetically, aliases grouped after (also sorted).
export const SLASH_COMMANDS: SlashCommand[] = [
  { name: 'changes', description: 'Show file changes tracked in the current session' },
  { name: 'clear', description: 'Clear the current session' },
  { name: 'codegraph', description: 'Code intelligence graph: build, stats, dead-code' },
  { name: 'commit', description: 'Stage changes and commit' },
  { name: 'compact', description: 'LLM-summarize the middle of the conversation, preserving the opening task anchor and the recent causal chain' },
  { name: 'context', description: 'Show or set the context mode (full|low_context) — takes effect next session' },
  { name: 'custom', description: 'Manage custom OpenAI-compatible providers' },
  { name: 'edit', description: 'Open $EDITOR to compose or edit a query' },
  { name: 'exec', description: 'Execute a shell command directly (also use !<command> as shortcut)' },
  { name: 'exit', description: 'Exit the interactive session' },
  { name: 'fork', description: 'Fork the conversation at a user message breakpoint' },
  { name: 'help', description: 'Show help information and available slash commands' },
  { name: 'index', description: 'Enable/disable workspace embedding index' },
  { name: 'info', description: 'Quick overview of live agent state (model, context, cost, persona)' },
  { name: 'init', description: 'Generate or improve AGENTS.md with intelligent codebase analysis' },
  { name: 'keys', description: 'Manage API credentials for providers' },
  { name: 'log', description: 'Show recent change history from all sessions' },
  { name: 'max-context', description: 'Show or set the max context token cap for cost control (0 = no cap)' },
  { name: 'mcp', description: 'Manage MCP (Model Context Protocol) servers - add, remove, list, test' },
  { name: 'model', description: 'List available models and select which model to use' },
  { name: 'persona', description: 'List, activate, and enable/disable personas' },
  { name: 'provider', description: 'Show current provider status and switch providers' },
  { name: 'recall', description: 'Search past sessions for relevant context' },
  { name: 'review', description: 'Perform AI-powered code review on staged Git changes' },
  { name: 'review-deep', description: 'Perform deep evidence-based code review on staged Git changes' },
  { name: 'rewind', description: 'Rewind conversation to a previous turn' },
  { name: 'risk-profile', description: 'Show or change the shell-command risk profile (readonly|cautious|default|permissive|unrestricted)' },
  { name: 'rollback', description: 'Rollback changes by revision ID (use /log to see available revisions)' },
  { name: 'search', description: 'Search across saved sessions by content' },
  { name: 'sessions', description: 'Show and load previous conversation sessions' },
  { name: 'settings', description: 'Browse and change settings interactively' },
  { name: 'setup', description: 'Show persisted configuration (provider defaults, subagent config, skills, MCP, warnings)' },
  { name: 'shell', description: 'Generate shell scripts from natural language descriptions with full environmental context' },
  { name: 'skill', description: 'Install, update, remove, list, enable, or disable skills' },
  { name: 'status', description: 'Detailed runtime status (tools, tokens, vision, change tracking, file changes)' },
  { name: 'subagent-model', description: 'Configure subagent model' },
  { name: 'subagent-persona', description: 'Alias for /persona' },
  { name: 'subagent-personas', description: 'Alias for /persona list' },
  { name: 'subagent-provider', description: 'Configure subagent provider' },
  { name: 'tools', description: 'Show or toggle per-tool invocation detail visibility' },
  { name: 'transcript', description: 'Capture a JSON snapshot of the current conversation (subcommands: preview, markdown, diff)' },
  { name: 'usage', description: 'Show visual usage dashboard with bar charts' },
  { name: 'verbose', description: 'Show or set output verbosity (compact|default|verbose)' },
  // Aliases (mirrors Go RegisterAlias calls)
  { name: '?', description: 'Alias for /help', isAlias: true, aliasOf: 'help' },
  { name: 'c', description: 'Alias for /commit', isAlias: true, aliasOf: 'commit' },
  { name: 'cg', description: 'Alias for /codegraph', isAlias: true, aliasOf: 'codegraph' },
  { name: 'ch', description: 'Alias for /changes', isAlias: true, aliasOf: 'changes' },
  { name: 'cl', description: 'Alias for /clear', isAlias: true, aliasOf: 'clear' },
  { name: 'cp', description: 'Alias for /compact', isAlias: true, aliasOf: 'compact' },
  { name: 'e', description: 'Alias for /edit', isAlias: true, aliasOf: 'edit' },
  { name: 'h', description: 'Alias for /help', isAlias: true, aliasOf: 'help' },
  { name: 'i', description: 'Alias for /index', isAlias: true, aliasOf: 'index' },
  { name: 'key', description: 'Alias for /keys', isAlias: true, aliasOf: 'keys' },
  { name: 'm', description: 'Alias for /model', isAlias: true, aliasOf: 'model' },
  { name: 'new', description: 'Alias for /clear', isAlias: true, aliasOf: 'clear' },
  { name: 'p', description: 'Alias for /provider', isAlias: true, aliasOf: 'provider' },
  { name: 'q', description: 'Alias for /exit', isAlias: true, aliasOf: 'exit' },
  { name: 'r', description: 'Alias for /review', isAlias: true, aliasOf: 'review' },
  { name: 'rb', description: 'Alias for /rollback', isAlias: true, aliasOf: 'rollback' },
  { name: 'resume', description: 'Alias for /sessions', isAlias: true, aliasOf: 'sessions' },
  { name: 'rw', description: 'Alias for /rewind', isAlias: true, aliasOf: 'rewind' },
  { name: 's', description: 'Alias for /search', isAlias: true, aliasOf: 'search' },
  { name: 'st', description: 'Alias for /status', isAlias: true, aliasOf: 'status' },
  { name: 'stats', description: 'Alias for /usage', isAlias: true, aliasOf: 'usage' },
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
