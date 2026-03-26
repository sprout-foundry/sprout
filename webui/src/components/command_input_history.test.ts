// @ts-nocheck

import { loadCommandHistory, saveCommandHistory } from './command_input_history';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

jest.mock('../services/terminalWebSocket', () => ({
  TerminalWebSocketService: {
    getInstance: jest.fn(),
  },
}));

describe('command_input_history', () => {
  afterEach(() => {
    jest.restoreAllMocks();
    jest.clearAllMocks();
  });

  it('loads history only from the terminal session', async () => {
    const getSessionId = jest.fn().mockReturnValue('session-123');
    (TerminalWebSocketService.getInstance as jest.Mock).mockReturnValue({ getSessionId });

    const apiService = {
      getTerminalHistory: jest.fn().mockResolvedValue({
        history: [' first ', '', 'second', 'first', 'third '],
        count: 5,
      }),
    } as any;

    await expect(loadCommandHistory(apiService)).resolves.toEqual(['second', 'first', 'third']);
    expect(apiService.getTerminalHistory).toHaveBeenCalledWith('session-123');
  });

  it('returns empty history when no terminal session exists', async () => {
    (TerminalWebSocketService.getInstance as jest.Mock).mockReturnValue({
      getSessionId: jest.fn().mockReturnValue(null),
    });

    const apiService = {
      getTerminalHistory: jest.fn().mockResolvedValue({
        history: [],
        count: 0,
      }),
    } as any;

    await expect(loadCommandHistory(apiService)).resolves.toEqual([]);
    expect(apiService.getTerminalHistory).toHaveBeenCalledWith(undefined);
  });

  it('keeps command sending independent from terminal history persistence', async () => {
    const apiService = {
      addTerminalHistory: jest.fn().mockRejectedValue(new Error('history unavailable')),
    } as any;

    await expect(saveCommandHistory(apiService, ['older'], 'new command')).resolves.toEqual({
      commands: ['older', 'new command'],
      index: -1,
      tempInput: '',
    });

    expect(apiService.addTerminalHistory).toHaveBeenCalledWith('new command');
  });
});
