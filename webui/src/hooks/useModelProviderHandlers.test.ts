// @ts-nocheck

/**
 * useModelProviderHandlers.test.ts — covers the optimistic stats sync.
 *
 * The dropdown's onChange handlers must mirror the new provider/model into
 * state.stats so that ChatStatusBarItems (which reads stats.provider/model
 * from the chat status bar) updates at the same instant as the settings
 * sidebar dropdowns. Without this mirroring, the status bar lags behind the
 * dropdown until the backend's next metrics_update arrives.
 */

import { act, createElement, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// Hoisted mock for useEvents.sendEvent so each test can inspect what was sent
const mocks = vi.hoisted(() => ({
  sendEvent: vi.fn(),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

vi.mock('../contexts/EventsContext', () => ({
  useEvents: () => ({
    sendEvent: mocks.sendEvent,
    onEvent: vi.fn(),
    removeEvent: vi.fn(),
    connect: vi.fn(),
    disconnect: vi.fn(),
    freeze: vi.fn(),
    resume: vi.fn(),
    isConnected: vi.fn().mockReturnValue(true),
    onReconnect: vi.fn(),
    resetAndReconnect: vi.fn(),
    getQueuedMessageCount: vi.fn().mockReturnValue(0),
    flushQueuedMessages: vi.fn().mockReturnValue(0),
  }),
}));

import { useModelProviderHandlers } from './useModelProviderHandlers';

// Minimal AppState shape — the hook only reads provider and stats, so the
// fixture mirrors only those fields plus model (a parallel to provider).
function makeInitialState() {
  return {
    provider: 'anthropic',
    model: 'claude-3-5-sonnet',
    stats: {
      provider: 'anthropic',
      model: 'claude-3-5-sonnet',
      total_tokens: 1234,
      total_cost: 0.0456,
    },
  };
}

let container: HTMLDivElement;
let root: Root;
let captured: {
  state: ReturnType<typeof makeInitialState>;
  handlers: ReturnType<typeof useModelProviderHandlers> | null;
};

function TestComponent() {
  // Mimic the prod call site: a stateful container that reacts to the hook's
  // setState updater. Using a ref-backed object means setState mutations are
  // visible across renders without forcing a re-render hop per assertion.
  const stateRef = useRef(makeInitialState());

  const setState: any = (updater: any) => {
    const partial = updater(stateRef.current);
    stateRef.current = { ...stateRef.current, ...partial };
    captured.state = stateRef.current;
  };

  const handlers = useModelProviderHandlers({
    state: stateRef.current,
    setState,
  });
  captured.handlers = handlers;
  captured.state = stateRef.current;
  return createElement('div');
}

function renderHook() {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root.render(createElement(TestComponent));
  });
}

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  captured = { state: makeInitialState(), handlers: null };
  mocks.sendEvent.mockReset();
});

afterEach(() => {
  if (root) act(() => root.unmount());
  if (container) container.remove();
});

describe('handleModelChange', () => {
  it('mirrors the new model into state.stats', () => {
    renderHook();
    act(() => {
      captured.handlers!.handleModelChange('claude-3-7-sonnet');
    });

    expect(captured.state.model).toBe('claude-3-7-sonnet');
    expect(captured.state.stats.model).toBe('claude-3-7-sonnet');
  });

  it('preserves non-model fields on state.stats (cost, tokens)', () => {
    renderHook();
    act(() => {
      captured.handlers!.handleModelChange('claude-3-7-sonnet');
    });

    expect(captured.state.stats.total_tokens).toBe(1234);
    expect(captured.state.stats.total_cost).toBe(0.0456);
    // Provider stays — only model was changed.
    expect(captured.state.stats.provider).toBe('anthropic');
  });

  it('sends the model_change event with the current provider', () => {
    renderHook();
    act(() => {
      captured.handlers!.handleModelChange('claude-3-7-sonnet');
    });

    expect(mocks.sendEvent).toHaveBeenCalledWith({
      type: 'model_change',
      data: { provider: 'anthropic', model: 'claude-3-7-sonnet' },
    });
  });
});

describe('handleProviderChange', () => {
  it('mirrors the new provider into state.stats', () => {
    renderHook();
    act(() => {
      captured.handlers!.handleProviderChange('openai');
    });

    expect(captured.state.provider).toBe('openai');
    expect(captured.state.stats.provider).toBe('openai');
  });

  it('preserves non-provider fields on state.stats (cost, tokens, model)', () => {
    renderHook();
    act(() => {
      captured.handlers!.handleProviderChange('openai');
    });

    expect(captured.state.stats.total_tokens).toBe(1234);
    expect(captured.state.stats.total_cost).toBe(0.0456);
    // The previous model stays on stats — provider switch alone does not
    // imply the model has changed yet (the backend may resolve a default).
    expect(captured.state.stats.model).toBe('claude-3-5-sonnet');
  });

  it('sends the provider_change event', () => {
    renderHook();
    act(() => {
      captured.handlers!.handleProviderChange('openai');
    });

    expect(mocks.sendEvent).toHaveBeenCalledWith({
      type: 'provider_change',
      data: { provider: 'openai' },
    });
  });

  it('updates pendingProviderChangeRef when supplied', () => {
    const pendingRef = { current: false };
    const pendingValueRef = { current: null as string | null };

    function TestComponentWithRefs() {
      const stateRef = useRef(makeInitialState());
      const setState: any = (updater: any) => {
        stateRef.current = { ...stateRef.current, ...updater(stateRef.current) };
        captured.state = stateRef.current;
      };
      captured.handlers = useModelProviderHandlers({
        state: stateRef.current,
        setState,
        pendingProviderChangeRef: pendingRef,
        pendingProviderChangeValueRef: pendingValueRef,
      });
      return createElement('div');
    }

    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    act(() => root.render(createElement(TestComponentWithRefs)));

    act(() => {
      captured.handlers!.handleProviderChange('openai');
    });

    expect(pendingRef.current).toBe(true);
    expect(pendingValueRef.current).toBe('openai');
  });
});
