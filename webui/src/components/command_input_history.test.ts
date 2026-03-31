// @ts-nocheck

import { loadCommandHistory, saveCommandHistory } from './command_input_history';

describe('command_input_history', () => {
  afterEach(() => {
    jest.restoreAllMocks();
    jest.clearAllMocks();
  });

  it('loads history from the api service', async () => {
    const apiService = {
      getTerminalHistory: jest.fn().mockResolvedValue({
        history: [' first ', '', 'second', 'first', 'third '],
        count: 5,
      }),
    } as any;

    await expect(loadCommandHistory(apiService)).resolves.toEqual(['second', 'first', 'third']);
    expect(apiService.getTerminalHistory).toHaveBeenCalled();
  });

  it('returns empty history when api call fails', async () => {
    const apiService = {
      getTerminalHistory: jest.fn().mockRejectedValue(new Error('API unavailable')),
    } as any;

    await expect(loadCommandHistory(apiService)).resolves.toEqual([]);
    expect(apiService.getTerminalHistory).toHaveBeenCalled();
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
