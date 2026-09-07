/**
 * useAvailableShells — tests for the available-shells fetch + selection state hook
 * extracted from Terminal.tsx during SP-075-extension.
 */
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('../services/notificationBus', () => ({
  notificationBus: { notify: vi.fn() },
}));

const { getAvailableShellsMock, gateMock } = vi.hoisted(() => ({
  getAvailableShellsMock: vi.fn(),
  gateMock: vi.fn(),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      getAvailableShells: getAvailableShellsMock,
    }),
  },
}));

vi.mock('../services/nativeTerminal', () => ({
  nativeTerminalGate: gateMock,
}));

import { notificationBus } from '../services/notificationBus';
import { useAvailableShells } from './useAvailableShells';

// ---------------------------------------------------------------------------
// Harness — runs the hook and surfaces state to the test
// ---------------------------------------------------------------------------

interface CapturedState {
  availableShells: ReturnType<typeof useAvailableShells>['availableShells'];
  shellsLoaded: boolean;
  selectedShell: string | null;
  setSelectedShell: ReturnType<typeof useAvailableShells>['setSelectedShell'];
}

function Harness({ onState }: { onState: (state: CapturedState) => void }): null {
  const result = useAvailableShells();
  onState(result);
  return null;
}

let container: HTMLDivElement;
let root: Root;
let lastState: CapturedState | null = null;
const stateCallback = (s: CapturedState) => {
  lastState = s;
};

beforeEach(() => {
  lastState = null;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  // Default: gate inactive → the fetch path (pre-existing behavior).
  gateMock.mockResolvedValue({ active: false, reason: 'native-terminal-disabled' });
});

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

describe('useAvailableShells', () => {
  describe('initial state', () => {
    it('starts with empty shells, not loaded, no selection', () => {
      getAvailableShellsMock.mockReturnValue(new Promise(() => {})); // never resolves

      act(() => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });

      expect(lastState?.availableShells).toEqual([]);
      expect(lastState?.shellsLoaded).toBe(false);
      expect(lastState?.selectedShell).toBeNull();
    });
  });

  describe('successful fetch', () => {
    it('populates availableShells and marks loaded=true', async () => {
      const shells = [
        { name: 'bash', path: '/bin/bash', default: true },
        { name: 'zsh', path: '/bin/zsh', default: false },
      ];
      getAvailableShellsMock.mockResolvedValue({ shells });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });

      // After the microtask settles
      await act(async () => {
        await Promise.resolve();
      });

      expect(lastState?.availableShells).toEqual(shells);
      expect(lastState?.shellsLoaded).toBe(true);
    });

    it('auto-selects the default shell when one is available', async () => {
      const shells = [
        { name: 'zsh', path: '/bin/zsh', default: false },
        { name: 'bash', path: '/bin/bash', default: true },
      ];
      getAvailableShellsMock.mockResolvedValue({ shells });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(lastState?.selectedShell).toBe('bash');
    });

    it('falls back to first shell when none flagged default', async () => {
      const shells = [
        { name: 'fish', path: '/usr/bin/fish', default: false },
        { name: 'zsh', path: '/bin/zsh', default: false },
      ];
      getAvailableShellsMock.mockResolvedValue({ shells });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(lastState?.selectedShell).toBe('fish');
    });

    it('handles an empty shells array', async () => {
      getAvailableShellsMock.mockResolvedValue({ shells: [] });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(lastState?.availableShells).toEqual([]);
      expect(lastState?.shellsLoaded).toBe(true);
      expect(lastState?.selectedShell).toBeNull();
    });
  });

  describe('failed fetch', () => {
    it('marks loaded=true and notifies on error', async () => {
      const error = new Error('boom');
      getAvailableShellsMock.mockRejectedValue(error);

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(lastState?.shellsLoaded).toBe(true);
      expect(lastState?.availableShells).toEqual([]);
      expect(vi.mocked(notificationBus.notify)).toHaveBeenCalledWith(
        'warning',
        'Terminal',
        expect.stringContaining('boom'),
      );
    });
  });

  describe('setSelectedShell', () => {
    it('updates selectedShell locally without re-fetching', async () => {
      const shells = [{ name: 'bash', path: '/bin/bash', default: true }];
      getAvailableShellsMock.mockResolvedValue({ shells });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(lastState?.selectedShell).toBe('bash');

      act(() => {
        lastState?.setSelectedShell('zsh');
      });

      expect(lastState?.selectedShell).toBe('zsh');
      // Still called only once (initial fetch)
      expect(getAvailableShellsMock).toHaveBeenCalledTimes(1);
    });
  });

  describe('native terminal gate active (studio shell)', () => {
    it('never fetches shells and provides the synthetic native entry', async () => {
      gateMock.mockResolvedValue({ active: true, reason: 'active' });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(getAvailableShellsMock).not.toHaveBeenCalled();
      expect(notificationBus.notify).not.toHaveBeenCalled();
      expect(lastState?.shellsLoaded).toBe(true);
      expect(lastState?.availableShells).toEqual([{ name: 'native', default: true }]);
      expect(lastState?.selectedShell).toBe('native');
    });

    it('fetches normally when the gate rejects (getCapabilities failure)', async () => {
      gateMock.mockResolvedValue({ active: false, reason: 'getCapabilities-rejected' });
      const shells = [{ name: 'bash', path: '/bin/bash', default: true }];
      getAvailableShellsMock.mockResolvedValue({ shells });

      await act(async () => {
        root.render(createElement(Harness, { onState: stateCallback }));
      });
      await act(async () => {
        await Promise.resolve();
      });

      expect(getAvailableShellsMock).toHaveBeenCalledTimes(1);
      expect(lastState?.selectedShell).toBe('bash');
    });
  });
});
