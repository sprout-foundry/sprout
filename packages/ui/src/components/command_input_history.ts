import { debugLog } from '../utils/log';

const MAX_COMMAND_HISTORY = 100;
const STORAGE_KEY = 'sprout:chat-history';

export interface CommandHistoryState {
  commands: string[];
  index: number;
  tempInput: string;
}

/**
 * Minimal interface for API operations needed by command history.
 * The host application provides this via the CommandInput component.
 */
export interface CommandHistoryApi {
  getChatHistory?: () => Promise<string[]>;
}

export function dedupeCommands(commands: string[]): string[] {
  const seen = new Set<string>();
  return commands.filter((cmd) => {
    if (seen.has(cmd)) return false;
    seen.add(cmd);
    return true;
  });
}

export function createEmptyState(): CommandHistoryState {
  return { commands: [], index: -1, tempInput: '' };
}

export async function loadCommandHistory(api?: CommandHistoryApi | null): Promise<CommandHistoryState> {
  try {
    // Try to load from localStorage first
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      const commands = JSON.parse(stored) as string[];
      return { commands: dedupeCommands(commands), index: -1, tempInput: '' };
    }
    // Fall back to API if available
    if (api?.getChatHistory) {
      const commands = await api.getChatHistory();
      return { commands: dedupeCommands(commands), index: -1, tempInput: '' };
    }
  } catch (err) {
    debugLog('[CommandHistory] Failed to load:', err);
  }
  return createEmptyState();
}

export function persistCommandHistory(commands: string[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(dedupeCommands(commands).slice(0, MAX_COMMAND_HISTORY)));
  } catch (err) {
    debugLog('[CommandHistory] Failed to persist:', err);
  }
}
