import { ApiService } from '../services/api';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

const MAX_COMMAND_HISTORY = 100;

export interface CommandHistoryState {
  commands: string[];
  index: number;
  tempInput: string;
}

export async function loadCommandHistory(apiService: ApiService): Promise<string[]> {
  try {
    const terminalService = TerminalWebSocketService.getInstance();
    const response = await apiService.getTerminalHistory(terminalService.getSessionId() || undefined);
    if (response && Array.isArray(response.history)) {
      return dedupeCommands(response.history);
    }
  } catch {
    // Terminal history sync is best-effort; command input can still function without persisted history.
  }

  return [];
}

export async function saveCommandHistory(apiService: ApiService, commands: string[], command: string): Promise<CommandHistoryState> {
  const trimmedCommand = command.trim();
  const nextCommands = dedupeCommands([...commands, trimmedCommand]);
  const nextHistory: CommandHistoryState = {
    commands: nextCommands,
    index: -1,
    tempInput: ''
  };

  try {
    await apiService.addTerminalHistory(trimmedCommand);
  } catch {
    // History sync failures should not block sending commands.
  }

  return nextHistory;
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
