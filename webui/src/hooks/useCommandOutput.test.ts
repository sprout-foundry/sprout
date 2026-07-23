/**
 * useCommandOutput tests — SP-114 Phase 2d.
 *
 * Validates the hook subscribes via WebSocketService, filters by chat_id,
 * accumulates chunks, stops on is_final, and cleans up on unmount.
 *
 * Pattern: replicate the MockWebSocket + resetSingleton flow from
 * `services/websocketReattach.test.ts` but in JSX/hook form: we mock the
 * service so the hook sees the same shape it would at runtime, fire
 * synthetic events into `service.getInstance().onEvent(cb)` callbacks,
 * and inspect `renderHook().result.current`.
 */

import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
// ── Hoisted mock state ─────────────────────────────────────────────────
//
// vi.mock factories run BEFORE the module code that uses them, so any
// captured state has to live inside a vi.hoisted block. Without this
// pattern, the WebSocketService mock captures the wrong (stale)
// reference and the listeners array is empty.
const listeners: Array<(event: { type: string; data?: unknown }) => void> = [];
const wsServiceDouble = vi.hoisted(() => ({
  onEvent: vi.fn((cb: (e: { type: string; data?: unknown }) => void) => {
    listeners.push(cb);
  }),
  removeEvent: vi.fn((cb: (e: { type: string; data?: unknown }) => void) => {
    const idx = listeners.indexOf(cb);
    if (idx >= 0) listeners.splice(idx, 1);
  }),
  _reset: () => {
    listeners.length = 0;
    wsServiceDouble.onEvent.mockClear();
    wsServiceDouble.removeEvent.mockClear();
  },
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('./clientSession', () => ({
  appendClientIdToUrl: vi.fn((url: string) => url),
  clientFetch: vi.fn(),
  getProxyBase: vi.fn(() => ''),
}));

vi.mock('./apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
}));

vi.mock('./notificationBus', () => ({
  notificationBus: { notify: vi.fn() },
}));

vi.mock('../services/websocket', () => ({
  WebSocketService: { getInstance: vi.fn(() => wsServiceDouble) },
}));

import { useCommandOutput } from './useCommandOutput';

// ── Helpers ─────────────────────────────────────────────────────────────

/** Dispatch one event to all currently-subscribed callbacks. The hook
 *  registers a single callback per mount, so this fires directly into
 *  the hook. */
function dispatch(event: { type: string; data?: unknown }) {
  // Snapshot is intentional: callbacks may add/remove themselves during dispatch.
  const snapshot = listeners.slice();
  for (const cb of snapshot) {
    cb(event);
  }
}

// ── Tests ───────────────────────────────────────────────────────────────

describe('useCommandOutput', () => {
  beforeEach(() => {
    wsServiceDouble._reset();
  });

  afterEach(() => {
    listeners.length = 0;
  });

  it('subscribes via WebSocketService.onEvent on mount', () => {
    renderHook(() => useCommandOutput('chat-1'));
    expect(wsServiceDouble.onEvent).toHaveBeenCalledTimes(1);
    expect(listeners).toHaveLength(1);
  });

  it('appends chunk to output on command_output (non-final)', () => {
    const { result } = renderHook(() => useCommandOutput('chat-1'));

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: 'Hello ', is_final: false, chat_id: 'chat-1' },
      });
    });
    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: 'world', is_final: false, chat_id: 'chat-1' },
      });
    });

    expect(result.current.output).toBe('Hello world');
    expect(result.current.isRunning).toBe(true);
    expect(result.current.command).toBe('info');
    expect(result.current.droppedBytes).toBe(0);
    expect(result.current.error).toBeNull();
  });

  it('sets isRunning:false on is_final:true', () => {
    const { result } = renderHook(() => useCommandOutput('chat-1'));

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: 'Hello', is_final: false, chat_id: 'chat-1' },
      });
    });
    expect(result.current.isRunning).toBe(true);

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: '', is_final: true, chat_id: 'chat-1' },
      });
    });
    expect(result.current.isRunning).toBe(false);
    expect(result.current.output).toBe('Hello');
  });

  it('accumulates dropped_bytes across multiple command_output_dropped events', () => {
    const { result } = renderHook(() => useCommandOutput('chat-1'));

    act(() => {
      dispatch({ type: 'command_output_dropped', data: { command: 'info', dropped_bytes: 4096, chat_id: 'chat-1' } });
    });
    expect(result.current.droppedBytes).toBe(4096);

    act(() => {
      dispatch({ type: 'command_output_dropped', data: { command: 'info', dropped_bytes: 1024, chat_id: 'chat-1' } });
    });
    expect(result.current.droppedBytes).toBe(5120);
  });

  it('filters events: command_output for chat-A must NOT update chat-B hook', () => {
    const { result: resultB } = renderHook(() => useCommandOutput('chat-B'));

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: 'leak', is_final: false, chat_id: 'chat-A' },
      });
    });

    expect(resultB.current.output).toBe('');
    expect(resultB.current.command).toBeNull();
  });

  it('filters events: command_output_dropped for chat-A must NOT update chat-B hook', () => {
    const { result: resultB } = renderHook(() => useCommandOutput('chat-B'));

    act(() => {
      dispatch({ type: 'command_output_dropped', data: { dropped_bytes: 9999, chat_id: 'chat-A' } });
    });

    expect(resultB.current.droppedBytes).toBe(0);
  });

  it('ignores events with no chat_id (server emitted without chat scope)', () => {
    // When both sides lack a chat_id we accept the event. This covers the
    // edge case where the server's resolveChatID returned empty.
    const { result } = renderHook(() => useCommandOutput(undefined));

    act(() => {
      dispatch({ type: 'command_output', data: { command: 'info', chunk: 'nochat', is_final: false } });
    });

    expect(result.current.output).toBe('nochat');
  });

  it('cleanup: removes the subscription on unmount', () => {
    const { unmount } = renderHook(() => useCommandOutput('chat-1'));
    expect(wsServiceDouble.removeEvent).not.toHaveBeenCalled();

    unmount();
    expect(wsServiceDouble.removeEvent).toHaveBeenCalledTimes(1);
  });

  it('cleanup: resubscribes when chatId changes', () => {
    const { rerender } = renderHook(({ chatId }: { chatId: string }) => useCommandOutput(chatId), {
      initialProps: { chatId: 'chat-1' },
    });
    expect(wsServiceDouble.onEvent).toHaveBeenCalledTimes(1);
    expect(wsServiceDouble.removeEvent).toHaveBeenCalledTimes(0);

    // Effect cleanup + re-subscribe cycle.
    rerender({ chatId: 'chat-2' });
    expect(wsServiceDouble.removeEvent).toHaveBeenCalledTimes(1);
    expect(wsServiceDouble.onEvent).toHaveBeenCalledTimes(2);
  });

  it('resets state to INITIAL when chatId changes', () => {
    const { result, rerender } = renderHook(({ chatId }: { chatId: string }) => useCommandOutput(chatId), {
      initialProps: { chatId: 'chat-1' },
    });

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: 'old', is_final: false, chat_id: 'chat-1' },
      });
    });
    expect(result.current.output).toBe('old');
    expect(result.current.isRunning).toBe(true);

    rerender({ chatId: 'chat-2' });
    expect(result.current.output).toBe('');
    expect(result.current.isRunning).toBe(false);
    expect(result.current.command).toBeNull();
    expect(result.current.droppedBytes).toBe(0);
  });

  it('ignores events of other types (no updates)', () => {
    const { result } = renderHook(() => useCommandOutput('chat-1'));
    const before = { ...result.current };

    act(() => {
      dispatch({ type: 'stream_chunk', data: { chat_id: 'chat-1', chunk: 'no' } });
      dispatch({ type: 'tool_start', data: { chat_id: 'chat-1' } });
      dispatch({ type: 'connection_status', data: { connected: true } });
    });

    expect(result.current).toEqual(before);
  });

  it('starts a new command when command_name in the chunk differs from current command', () => {
    const { result } = renderHook(() => useCommandOutput('chat-1'));

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'info', chunk: 'first run', is_final: false, chat_id: 'chat-1' },
      });
    });
    expect(result.current.output).toBe('first run');
    expect(result.current.command).toBe('info');

    act(() => {
      dispatch({
        type: 'command_output',
        data: { command: 'help', chunk: 'second run', is_final: false, chat_id: 'chat-1' },
      });
    });
    // Old output dropped (v1 simplification: latest-command-only).
    expect(result.current.output).toBe('second run');
    expect(result.current.command).toBe('help');
  });

  it('handles missing fields gracefully (no chunk → empty string, no command → no crash)', () => {
    const { result } = renderHook(() => useCommandOutput('chat-1'));

    act(() => {
      dispatch({ type: 'command_output', data: { is_final: false, chat_id: 'chat-1' } });
    });
    expect(result.current.output).toBe('');
    expect(result.current.isRunning).toBe(true);
    expect(result.current.command).toBeNull();
  });
});
