import { describe, expect, it, vi } from 'vitest';
import { handleAskUserRequest } from './approvals';
import type { EventHandlerContext } from './webSocketEventHelpers';

/** Minimal context for the approvals handlers — only the fields they read. */
function makeCtx(
  eventData: Record<string, unknown>,
  currentRequestId?: string,
): EventHandlerContext & {
  setState: ReturnType<typeof vi.fn>;
} {
  const setState = vi.fn();
  return {
    event: { type: 'ask_user_request', data: eventData },
    setState,
    activeRequestsRef: { current: 0 },
    activeChatIdRef: { current: 'chat-1' },
    apiService: { getStats: vi.fn() },
    pendingProviderRef: { current: '' },
    pendingProviderChangeRef: { current: false },
    pendingProviderChangeValueRef: { current: null },
    connectionTimeoutRef: { current: null },
    lastConnectionStateRef: { current: true },
    setState,
  } as unknown as EventHandlerContext & { setState: ReturnType<typeof vi.fn> };
}

describe('handleAskUserRequest — multi-window dismiss', () => {
  it('clears the dialog when a responded status arrives for the shown request', () => {
    const ctx = makeCtx({ request_id: 'ask-1', status: 'responded' });
    // Simulate the dialog currently being shown.
    ctx.setState.mockImplementation((fn: (prev: unknown) => unknown) => {
      fn({ askUserRequest: { requestId: 'ask-1', question: 'q' } });
    });
    handleAskUserRequest(ctx as EventHandlerContext);
    expect(ctx.setState).toHaveBeenCalledTimes(1);
  });

  it('does not clear a dialog for a different request id', () => {
    const ctx = makeCtx({ request_id: 'ask-1', status: 'responded' });
    let observed: unknown;
    ctx.setState.mockImplementation((fn: (prev: unknown) => unknown) => {
      observed = fn({ askUserRequest: { requestId: 'ask-other', question: 'q' } });
    });
    handleAskUserRequest(ctx as EventHandlerContext);
    // The updater must have returned the previous state unchanged.
    expect(observed).toEqual({ askUserRequest: { requestId: 'ask-other', question: 'q' } });
  });

  it('still sets a fresh question when no status is present', () => {
    const ctx = makeCtx({ request_id: 'ask-2', question: 'Proceed?' });
    handleAskUserRequest(ctx as EventHandlerContext);
    expect(ctx.setState).toHaveBeenCalledTimes(1);
    const updater = ctx.setState.mock.calls[0][0] as (prev: unknown) => unknown;
    const next = updater({ askUserRequest: null, chatSessions: [], logs: [] }) as {
      askUserRequest: { requestId: string };
    };
    expect(next.askUserRequest.requestId).toBe('ask-2');
  });

  it('clears the dialog on cancelled status (pre-existing contract)', () => {
    const ctx = makeCtx({ request_id: 'ask-3', status: 'cancelled' });
    handleAskUserRequest(ctx as EventHandlerContext);
    expect(ctx.setState).toHaveBeenCalledTimes(1);
    const updater = ctx.setState.mock.calls[0][0] as (prev: unknown) => unknown;
    expect(updater({ askUserRequest: { requestId: 'x' } })).toEqual({ askUserRequest: null });
  });
});
