import type { ToolExecution } from '@sprout/ui';

const TOOL_LABELS: Record<string, string> = {
  shell_command: 'Shell',
  read_file: 'Read',
  write_file: 'Write',
  edit_file: 'Edit',
  list_directory: 'List',
  search_files: 'Search',
  web_search: 'Web search',
  fetch_url: 'Fetch',
  TodoWrite: 'Plan',
  TodoRead: 'Plan',
  view_history: 'History',
  rollback_changes: 'Rollback',
  analyze_ui_screenshot: 'Screenshot',
  analyze_image_content: 'Image',
};

// The argument that names what a call acted on, in order of preference.
const TARGET_KEYS = ['command', 'path', 'file_path', 'directory', 'pattern', 'query', 'url'];

export interface ToolCallView {
  label: string;
  /** What the call acted on: the command, file, pattern… */
  target?: string;
  /** The command line, for tools whose call is one. */
  command?: string;
  /** Arguments beyond the target, pretty-printed; absent when the target says it all. */
  otherArgs?: string;
}

function humanize(name: string): string {
  const words = name.replace(/_/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

function parseArgs(raw: string | undefined): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

export function describeToolCall(tool: ToolExecution): ToolCallView {
  const label = TOOL_LABELS[tool.tool] ?? humanize(tool.tool || 'Tool');
  const args = parseArgs(tool.arguments);
  if (!args) {
    // Unparseable arguments: the agent's one-line summary names the target.
    const summary = tool.message?.startsWith(tool.tool) ? tool.message.slice(tool.tool.length).trim() : tool.message;
    return { label, target: summary || undefined, otherArgs: tool.arguments || undefined };
  }
  const key = TARGET_KEYS.find((k) => typeof args[k] === 'string' && (args[k] as string).trim() !== '');
  const target = key ? (args[key] as string) : undefined;
  const rest = Object.fromEntries(Object.entries(args).filter(([k]) => k !== key));
  return {
    label,
    target,
    command: key === 'command' ? target : undefined,
    otherArgs: Object.keys(rest).length > 0 ? JSON.stringify(rest, null, 2) : undefined,
  };
}
