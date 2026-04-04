import { type ApiService } from '../services/api';

const MAX_COMMAND_HISTORY = 100;
const STORAGE_KEY = 'ledit:chat-history';

export interface CommandHistoryState {
  commands: string[];
  index: number;
  tempInput: string;
}

export async function loadCommandHistory(_apiService: ApiService): Promise<string[]> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed)) {
        return dedupeCommands(parsed);
      }
    }
  } catch {
    // localStorage unavailable or corrupted — start with empty history
  }
  return [];
}

export function persistCommandHistory(commands: string[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(commands));
  } catch {
    // Storage quota exceeded or unavailable — ignore, in-memory history still works
  }
}

// Kept for compatibility but no longer used for the primary save path
export async function saveCommandHistory(
  _apiService: ApiService,
  commands: string[],
  command: string,
): Promise<CommandHistoryState> {
  const trimmedCommand = command.trim();
  const nextCommands = dedupeCommands([...commands, trimmedCommand]);
  persistCommandHistory(nextCommands);
  return {
    commands: nextCommands,
    index: -1,
    tempInput: '',
  };
}

export function dedupeCommands(commands: string[]): string[] {
  const unique = new Set<string>();
  const ordered: string[] = [];

  commands.forEach((command) => {
    const trimmed = command.trim();
    if (!trimmed) {
      return;
    }
    if (unique.has(trimmed)) {
      const existingIndex = ordered.indexOf(trimmed);
      if (existingIndex >= 0) {
        ordered.splice(existingIndex, 1);
      }
    } else {
      unique.add(trimmed);
    }
    ordered.push(trimmed);
  });

  return ordered.slice(-MAX_COMMAND_HISTORY);
}
