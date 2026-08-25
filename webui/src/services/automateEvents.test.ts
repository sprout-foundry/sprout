/**
 * automateEvents.test.ts — Unit tests for the automate event pub-sub service.
 *
 * Covers:
 * - subscribeAutomate / unsubscribe
 * - emitAutomate dispatches to all subscribers
 * - unsubscribed handlers are not called
 * - handler errors are swallowed (don't block other handlers)
 * - session_id filtering (consumer-side responsibility)
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { subscribeAutomate, emitAutomate, type AutomateEventHandler } from './automateEvents';

describe('automateEvents pub-sub', () => {
  beforeEach(() => {
    // Clean up any lingering subscribers between tests by creating a fresh
    // handler that would have been registered, then unsubscribing.
    // Since the module uses a module-level Set, we need to be careful.
    // The simplest approach: each test registers its own handler and
    // unsubscribes in afterEach (handled by the return value of subscribeAutomate).
  });

  it('invokes subscriber when event is emitted', () => {
    const handler = vi.fn();
    const unsub = subscribeAutomate(handler);

    emitAutomate('automate.session_started', { session_id: 'abc123', workflow: 'test' });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith('automate.session_started', {
      session_id: 'abc123',
      workflow: 'test',
    });

    unsub();
  });

  it('invokes all subscribers', () => {
    const h1 = vi.fn();
    const h2 = vi.fn();
    const unsub1 = subscribeAutomate(h1);
    const unsub2 = subscribeAutomate(h2);

    emitAutomate('automate.output_chunk', { session_id: 'x', offset: 10, chunk_len: 5 });

    expect(h1).toHaveBeenCalledTimes(1);
    expect(h2).toHaveBeenCalledTimes(1);
    expect(h1).toHaveBeenCalledWith('automate.output_chunk', { session_id: 'x', offset: 10, chunk_len: 5 });
    expect(h2).toHaveBeenCalledWith('automate.output_chunk', { session_id: 'x', offset: 10, chunk_len: 5 });

    unsub1();
    unsub2();
  });

  it('unsubscribe removes the handler', () => {
    const handler = vi.fn();
    const unsub = subscribeAutomate(handler);

    emitAutomate('automate.session_ended', { session_id: 'abc' });
    expect(handler).toHaveBeenCalledTimes(1);

    unsub();

    emitAutomate('automate.session_ended', { session_id: 'def' });
    expect(handler).toHaveBeenCalledTimes(1); // still 1, not called again
  });

  it('handler errors do not block other handlers', () => {
    const failingHandler = vi.fn(() => {
      throw new Error('boom');
    });
    const goodHandler = vi.fn();
    const unsub1 = subscribeAutomate(failingHandler);
    const unsub2 = subscribeAutomate(goodHandler);

    // Should not throw — errors are swallowed
    expect(() => {
      emitAutomate('automate.output_chunk', { session_id: 's1' });
    }).not.toThrow();

    expect(failingHandler).toHaveBeenCalledTimes(1);
    expect(goodHandler).toHaveBeenCalledTimes(1);

    unsub1();
    unsub2();
  });

  it('session_id filtering is consumer responsibility (pub-sub is transparent)', () => {
    // The pub-sub layer doesn't filter by session_id — that's the consumer's job.
    // This test verifies that all events pass through regardless of session_id.
    const handler = vi.fn();
    const unsub = subscribeAutomate(handler);

    emitAutomate('automate.output_chunk', { session_id: 's1' });
    emitAutomate('automate.output_chunk', { session_id: 's2' });
    emitAutomate('automate.output_chunk', { session_id: 's1' });

    expect(handler).toHaveBeenCalledTimes(3);

    unsub();
  });

  it('handles budget_update events', () => {
    const handler = vi.fn();
    const unsub = subscribeAutomate(handler);

    emitAutomate('automate.budget_update', {
      session_id: 's1',
      spent_usd: 2.5,
      budget_usd: 10,
      fraction: 0.25,
    });

    expect(handler).toHaveBeenCalledWith('automate.budget_update', {
      session_id: 's1',
      spent_usd: 2.5,
      budget_usd: 10,
      fraction: 0.25,
    });

    unsub();
  });

  it('does not call unsubscribed handler after emission', () => {
    const h1 = vi.fn();
    const h2 = vi.fn();
    const unsub1 = subscribeAutomate(h1);
    const unsub2 = subscribeAutomate(h2);

    unsub1();

    emitAutomate('automate.session_started', { session_id: 'x' });

    expect(h1).toHaveBeenCalledTimes(0);
    expect(h2).toHaveBeenCalledTimes(1);

    unsub2();
  });
});
