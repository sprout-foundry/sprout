import { ApiService } from '../services/api';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

const COMMAND_HISTORY_KEY = 'ledit-command-history';
const MAX_COMMAND_HISTORY = 100;

export interface CommandHistoryState {
  commands: string[];
  index: number;
  tempInput: string;
}

export async function loadCommandHistory(apiService: ApiService): Promise<string[]> {
  let commands = readLocalCommandHistory();

  try {
    const terminalService = TerminalWebSocketService.getInstance();
    const response = await apiService.getTerminalHistory(terminalService.getSessionId() || undefined);
    if (response && Array.isArray(response.history)) {
      const terminalCommands = response.history.filter((cmd) => cmd.trim());
      commands = dedupeCommands([...terminalCommands, ...commands]);
      writeLocalCommandHistory(commands);
    }
  } catch {
    // Terminal history sync is best-effort; local history is still usable.
  }

  return commands;
}

export async function saveCommandHistory(apiService: ApiService, commands: string[], command: string): Promise<CommandHistoryState> {
  const trimmedCommand = command.trim();
  const nextCommands = dedupeCommands([...commands, trimmedCommand]);
  const nextHistory: CommandHistoryState = {
    commands: nextCommands,
    index: -1,
    tempInput: ''
  };

  writeLocalCommandHistory(nextCommands);

  try {
    await apiService.addTerminalHistory(trimmedCommand);
  } catch {
    // History sync failures should not block sending commands.
  }

  return nextHistory;
}

function readLocalCommandHistory(): string[] {
  const localHistory = localStorage.getItem(COMMAND_HISTORY_KEY);
  if (!localHistory) {
    return [];
  }

  try {
    const parsed = JSON.parse(localHistory);
    if (parsed && Array.isArray(parsed.commands)) {
      return sanitizeCommands(parsed.commands);
    }
    if (Array.isArray(parsed)) {
      return sanitizeCommands(parsed);
    }
  } catch {
    return [];
  }

  return [];
}

function writeLocalCommandHistory(commands: string[]) {
  localStorage.setItem(COMMAND_HISTORY_KEY, JSON.stringify({
    commands,
    index: -1,
    tempInput: ''
  }));
}

function dedupeCommands(commands: string[]): string[] {
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

function sanitizeCommands(commands: unknown[]): string[] {
  return commands.filter((command): command is string => typeof command === 'string' && command.trim().length > 0);
}
