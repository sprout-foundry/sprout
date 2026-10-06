import type { Message } from '@sprout/ui';
import { describe, expect, it } from 'vitest';
import { handleQueryCompleted, handleQueryStarted, markTurnStopped } from './streaming';
import type { EventHandlerContext } from '../webSocketEventHelpers';

const msg = (type: Message['type'], content: string, extra: Partial<Message> = {}): Message => ({
  id: `${type}-${content}`,
  type,
  content,
  timestamp: new Date(0),
  ...extra,
});

describe('markTurnStopped', () => {
  it("marks the turn's answer", () => {
    const out = markTurnStopped([msg('user', 'q'), msg('assistant', 'partial', { reasoning: 'thinking' })]);
    expect(out).toHaveLength(2);
    expect(out[1]).toMatchObject({ content: 'partial', stopped: true });
  });

  it('closes a turn the agent never answered with a marker-only reply', () => {
    const out = markTurnStopped([msg('assistant', 'earlier answer'), msg('user', 'q')]);
    expect(out[0].stopped).toBeUndefined();
    expect(out[2]).toMatchObject({ type: 'assistant', content: '', stopped: true });
  });
});

describe('handleQueryCompleted', () => {
  function complete(data: Record<string, unknown>, messages: Message[]) {
    let state: Record<string, unknown> = { messages, toolExecutions: [], logs: [], currentTodos: [] };
    const ctx = {
      event: { id: 'e1', type: 'query_completed', data },
      setState: (update: unknown) => {
        const patch = typeof update === 'function' ? (update as (s: typeof state) => typeof state)(state) : update;
        state = { ...state, ...(patch as object) };
      },
      activeRequestsRef: { current: 1 },
    } as unknown as EventHandlerContext;
    handleQueryCompleted(ctx);
    return state.messages as Message[];
  }

  it('marks a stopped run', () => {
    const out = complete({ query: 'q', response: '', status: 'interrupted' }, [
      msg('user', 'q'),
      msg('assistant', 'partial'),
    ]);
    expect(out[out.length - 1].stopped).toBe(true);
  });

  it('leaves a finished run unmarked', () => {
    const out = complete({ query: 'q', response: 'done' }, [msg('user', 'q'), msg('assistant', 'done')]);
    expect(out.some((m) => m.stopped)).toBe(false);
  });
});

describe('handleQueryStarted', () => {
  function start(data: Record<string, unknown>, refStart: number) {
    let state: Record<string, unknown> = { messages: [], toolExecutions: [], logs: [], currentTodos: [] };
    const ref = { current: refStart };
    const ctx = {
      event: { id: 'e1', type: 'query_started', data },
      setState: (update: unknown) => {
        const patch = typeof update === 'function' ? (update as (s: typeof state) => typeof state)(state) : update;
        state = { ...state, ...(patch as object) };
      },
      activeRequestsRef: ref,
    } as unknown as EventHandlerContext;
    handleQueryStarted(ctx);
    return { state, ref };
  }

  it('sets the request counter when a turn starts without a local send (auto-resume)', () => {
    // The wakeup/auto-resume path dispatches query_started with no preceding
    // handleSendMessage; without the counter bump the composer would offer a
    // new query and dead-end on "already in process" instead of steering.
    const { ref, state } = start({ query: 'wakeup', source: 'auto-resume' }, 0);
    expect(ref.current).toBe(1);
    expect(state.isProcessing).toBe(true);
  });

  it('clamps to 1 on a normal send (no double-count)', () => {
    const { ref } = start({ query: 'hi' }, 1);
    expect(ref.current).toBe(1);
  });

  it('does not touch the counter for a subagent turn', () => {
    const { ref } = start({ query: 'sub', subagent_depth: 1 }, 0);
    expect(ref.current).toBe(0);
  });
});
