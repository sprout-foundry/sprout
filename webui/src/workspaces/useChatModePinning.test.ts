/**
 * Per-mode conversation pinning.
 *
 * Covers the pin store (write/read, corrupt-storage tolerance), the hook
 * (mode-switch restore rules + pin recording), and the boot-path decision
 * (persisted design mode → design pin / fresh, never the cross-mode
 * "most recent non-empty" fallback).
 */

import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { INSTANCE_PID_STORAGE_KEY } from '../constants/app';
import {
  chatModePinStorageKey,
  decideBootRestore,
  readChatModePins,
  useChatModePinning,
  writeChatModePin,
} from './useChatModePinning';
import { workspaceModeStorageKey } from './useWorkspaceMode';

const pinKey = chatModePinStorageKey();
const modeKey = workspaceModeStorageKey();

function setPersistedMode(mode: 'code' | 'design' | null) {
  if (mode === null) window.localStorage.removeItem(modeKey);
  else window.localStorage.setItem(modeKey, JSON.stringify(mode));
}

function baseOptions(overrides: Partial<Parameters<typeof useChatModePinning>[0]> = {}) {
  return {
    mode: 'code' as const,
    activeChatId: 'chat-code',
    onSwitchSession: vi.fn(),
    onFreshSession: vi.fn().mockResolvedValue(null),
    ...overrides,
  };
}

beforeEach(() => {
  window.localStorage.removeItem(pinKey);
  setPersistedMode(null);
});

describe('pin store', () => {
  it('writes and reads per-mode pins independently', () => {
    writeChatModePin('design', 'sess-d');
    writeChatModePin('code', 'sess-c');
    expect(readChatModePins()).toEqual({ code: 'sess-c', design: 'sess-d' });
  });

  it('replaces the pin for a mode on subsequent writes', () => {
    writeChatModePin('design', 'old');
    writeChatModePin('design', 'new');
    expect(readChatModePins().design).toBe('new');
  });

  it('tolerates corrupt storage (degrades to the empty map, never throws)', () => {
    window.localStorage.setItem(pinKey, '{not json');
    expect(readChatModePins()).toEqual({});
    // A write after corruption recovers a well-formed map.
    writeChatModePin('design', 'sess-d');
    expect(readChatModePins().design).toBe('sess-d');
  });

  it('ignores malformed pin values', () => {
    window.localStorage.setItem(pinKey, JSON.stringify({ code: 42, design: 'sess-d' }));
    expect(readChatModePins()).toEqual({ design: 'sess-d' });
  });

  it('is scoped per instance + UI context like the persisted mode', () => {
    window.localStorage.setItem(INSTANCE_PID_STORAGE_KEY, 'pid-42');
    try {
      expect(chatModePinStorageKey()).toBe('sprout:webui:chatModePin:v1:pid-42:local');
      expect(workspaceModeStorageKey()).toBe('sprout:webui:workspaceMode:v1:pid-42:local');
    } finally {
      window.localStorage.removeItem(INSTANCE_PID_STORAGE_KEY);
    }
  });
});

describe('useChatModePinning — mode-switch restore', () => {
  it('does not restore on mount (boot restore belongs to the init path)', () => {
    writeChatModePin('code', 'pin-c');
    const options = baseOptions({ mode: 'code', activeChatId: 'other' });
    renderHook(() => useChatModePinning(options));
    expect(options.onSwitchSession).not.toHaveBeenCalled();
    expect(options.onFreshSession).not.toHaveBeenCalled();
  });

  it("switches to that mode's pin on a mode change", () => {
    writeChatModePin('design', 'pin-d');
    const options = baseOptions({ mode: 'code', activeChatId: 'chat-code' });
    const { rerender } = renderHook(
      ({ mode }: { mode: 'code' | 'design' }) => useChatModePinning({ ...options, mode }),
      {
        initialProps: { mode: 'code' as const },
      },
    );
    rerender({ mode: 'design' });
    expect(options.onSwitchSession).toHaveBeenCalledWith('pin-d');
    expect(options.onFreshSession).not.toHaveBeenCalled();
  });

  it('starts a fresh design conversation when there is no pin: create → switch → pin', async () => {
    writeChatModePin('code', 'pin-c'); // a code pin exists — it must NOT be used
    const options = baseOptions({
      mode: 'code',
      activeChatId: 'chat-code',
      onFreshSession: vi.fn().mockResolvedValue('fresh-design'),
    });
    const { rerender } = renderHook(
      ({ mode }: { mode: 'code' | 'design' }) => useChatModePinning({ ...options, mode }),
      {
        initialProps: { mode: 'code' as const },
      },
    );
    rerender({ mode: 'design' });
    await waitFor(() => expect(options.onFreshSession).toHaveBeenCalledTimes(1));
    // The fresh session must become the ACTIVE chat: without the switch the
    // first send would re-pin the still-active Code session (contamination).
    expect(options.onSwitchSession).toHaveBeenCalledWith('fresh-design');
    // ...and the fresh session is recorded as the design pin.
    await waitFor(() => expect(readChatModePins().design).toBe('fresh-design'));
  });

  it('a failed fresh creation switches and pins nothing', async () => {
    const options = baseOptions({
      mode: 'code',
      activeChatId: 'chat-code',
      onFreshSession: vi.fn().mockResolvedValue(null),
    });
    const { rerender } = renderHook(
      ({ mode }: { mode: 'code' | 'design' }) => useChatModePinning({ ...options, mode }),
      {
        initialProps: { mode: 'code' as const },
      },
    );
    rerender({ mode: 'design' });
    await waitFor(() => expect(options.onFreshSession).toHaveBeenCalledTimes(1));
    await act(async () => {}); // let the async restore settle
    expect(options.onSwitchSession).not.toHaveBeenCalled();
    expect(readChatModePins().design).toBeUndefined();
  });

  it('does nothing for Code with no code pin (current behavior)', () => {
    writeChatModePin('design', 'pin-d'); // only a design pin exists
    const options = baseOptions({ mode: 'design', activeChatId: 'chat-d' });
    const { rerender } = renderHook(
      ({ mode }: { mode: 'code' | 'design' }) => useChatModePinning({ ...options, mode }),
      {
        initialProps: { mode: 'design' as const },
      },
    );
    rerender({ mode: 'code' });
    expect(options.onSwitchSession).not.toHaveBeenCalled();
    expect(options.onFreshSession).not.toHaveBeenCalled();
  });

  it("switches back to the Code pin, so code→design→code restores each mode's chat", () => {
    writeChatModePin('code', 'pin-c');
    writeChatModePin('design', 'pin-d');
    const options = baseOptions({ mode: 'code', activeChatId: 'pin-c' });
    let activeId: string | null = 'pin-c';
    const { rerender } = renderHook(
      ({ mode }: { mode: 'code' | 'design' }) => useChatModePinning({ ...options, mode, activeChatId: activeId }),
      { initialProps: { mode: 'code' as const } },
    );

    rerender({ mode: 'design' });
    expect(options.onSwitchSession).toHaveBeenLastCalledWith('pin-d');

    // The switch landed on the design pin; going back restores code's.
    activeId = 'pin-d';
    rerender({ mode: 'code' });
    expect(options.onSwitchSession).toHaveBeenLastCalledWith('pin-c');
  });

  it('skips the switch when the pin is already the active chat', () => {
    writeChatModePin('design', 'pin-d');
    const options = baseOptions({ mode: 'code', activeChatId: 'pin-d' });
    const { rerender } = renderHook(
      ({ mode }: { mode: 'code' | 'design' }) => useChatModePinning({ ...options, mode }),
      {
        initialProps: { mode: 'code' as const },
      },
    );
    rerender({ mode: 'design' });
    expect(options.onSwitchSession).not.toHaveBeenCalled();
  });
});

describe('useChatModePinning — pin recording', () => {
  it('switchSession pins the session for the current mode, then delegates', () => {
    const options = baseOptions({ mode: 'design', activeChatId: 'chat-d' });
    const { result } = renderHook(() => useChatModePinning(options));
    act(() => {
      result.current.switchSession('sess-2');
    });
    expect(options.onSwitchSession).toHaveBeenCalledWith('sess-2');
    expect(readChatModePins().design).toBe('sess-2');
  });

  it('recordSend pins the active session for the current mode', () => {
    const options = baseOptions({ mode: 'design', activeChatId: 'chat-d' });
    const { result } = renderHook(() => useChatModePinning(options));
    act(() => {
      result.current.recordSend();
    });
    expect(readChatModePins().design).toBe('chat-d');
    expect(options.onSwitchSession).not.toHaveBeenCalled();
  });

  it('recordSend is a no-op when no session is active yet', () => {
    const options = baseOptions({ mode: 'design', activeChatId: null });
    const { result } = renderHook(() => useChatModePinning(options));
    act(() => {
      result.current.recordSend();
    });
    expect(readChatModePins()).toEqual({});
  });

  it('pinSession records without switching', () => {
    const options = baseOptions({ mode: 'code', activeChatId: 'chat-c' });
    const { result } = renderHook(() => useChatModePinning(options));
    act(() => {
      result.current.pinSession('created-1');
    });
    expect(readChatModePins().code).toBe('created-1');
    expect(options.onSwitchSession).not.toHaveBeenCalled();
  });
});

describe('decideBootRestore — the init-path branch', () => {
  it('persisted design mode with a pin restores that pin', () => {
    setPersistedMode('design');
    writeChatModePin('design', 'boot-d');
    expect(decideBootRestore()).toEqual({ isDesignMode: true, designPin: 'boot-d' });
  });

  it('persisted design mode without a pin stays fresh (no cross-mode fallback)', () => {
    setPersistedMode('design');
    writeChatModePin('code', 'boot-c'); // a code pin exists — irrelevant to design
    expect(decideBootRestore()).toEqual({ isDesignMode: true, designPin: null });
  });

  it('persisted code mode keeps the existing behavior', () => {
    setPersistedMode('code');
    writeChatModePin('design', 'boot-d');
    expect(decideBootRestore()).toEqual({ isDesignMode: false, designPin: null });
  });

  it('an unset mode keeps the existing behavior', () => {
    writeChatModePin('design', 'boot-d');
    expect(decideBootRestore()).toEqual({ isDesignMode: false, designPin: null });
  });
});
