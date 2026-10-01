import type { SproutEvent } from '../types/events';

const FILE_MODIFYING_TOOLS = [
  'shell_command',
  'write_file',
  'edit_file',
  'write_structured_file',
  'patch_structured_file',
];

/**
 * Whether an event means files in the workspace may have changed: a file
 * write, create or delete (editor saves and the agent's file tools), a file
 * changing on disk, or a completed tool that can modify files (shell commands
 * can run sed, mv, git checkout…). Git's own actions are not included — the
 * git panel refreshes after those itself.
 */
export function changesWorkspaceFiles(event: SproutEvent | undefined): boolean {
  if (!event) return false;
  const data = event.data as Record<string, unknown> | undefined;
  if (event.type === 'file_changed') {
    const action = String(data?.action || '');
    return ['write', 'edit', 'created', 'deleted'].includes(action);
  }
  if (event.type === 'file_content_changed') return true;
  if (event.type === 'tool_end') {
    if (data?.status === 'failed') return false;
    return FILE_MODIFYING_TOOLS.includes(String(data?.tool_name || ''));
  }
  return false;
}
