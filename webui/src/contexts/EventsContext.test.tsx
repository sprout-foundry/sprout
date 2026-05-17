// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import type { EventsProvider, SproutEvent, SproutEventCallback } from '../types/events';
import { EventsContextProvider, useEvents } from './EventsContext';

// ---------------------------------------------------------------------------
// Mock EventsProvider
// ---------------------------------------------------------------------------

function createMockEventsProvider(overrides: Partial<EventsProvider> = {}): EventsProvider {
  return {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onEvent: vi.fn(),
    removeEvent: vi.fn(),
    sendEvent: vi.fn(),
    isConnected: vi.fn().mockReturnValue(true),
    onReconnect: vi.fn(),
    freeze: vi.fn(),
    resume: vi.fn(),
    resetAndReconnect: vi.fn(),
    getQueuedMessageCount: vi.fn().mockReturnValue(0),
    flushQueuedMessages: vi.fn().mockReturnValue(0),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;
let latestProvider: EventsProvider | undefined;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete globalThis.IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  latestProvider = undefined;
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function TestConsumer() {
  latestProvider = useEvents();
  return createElement('div', { 'data-testid': 'consumer' });
}

function renderProvider(provider: EventsProvider = createMockEventsProvider()) {
  act(() => {
    root.render(createElement(EventsContextProvider, { provider }, createElement(TestConsumer)));
  });
}

const ctx = () => latestProvider;

function requireCtx(): EventsProvider {
  const v = latestProvider;
  if (v === undefined) {
    throw new Error('Expected provider to be defined in test');
  }
  return v;
}

// ---------------------------------------------------------------------------
// Tests: useEvents hook
// ---------------------------------------------------------------------------

describe('useEvents', () => {
  it('throws an error when used outside of EventsContextProvider', () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    expect(() => {
      act(() => {
        root.render(createElement(TestConsumer));
      });
    }).toThrow('useEvents() must be used within an EventsContextProvider');

    consoleSpy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Tests: EventsContextProvider
// ---------------------------------------------------------------------------

describe('EventsContextProvider', () => {
  it('provides the events provider to children', () => {
    const provider = createMockEventsProvider();
    renderProvider(provider);

    expect(ctx()).toBe(provider);
  });

  it('renders children correctly', () => {
    renderProvider();

    expect(container.querySelector('[data-testid="consumer"]')).not.toBeNull();
  });

  it('returns the exact provider instance (reference equality)', () => {
    const provider = createMockEventsProvider();
    renderProvider(provider);

    expect(ctx()).toBe(provider);
  });

  it('context value is stable across rerenders with the same provider', () => {
    const provider = createMockEventsProvider();
    renderProvider(provider);

    const firstResult = ctx();

    act(() => {
      root.render(createElement(EventsContextProvider, { provider }, createElement(TestConsumer)));
    });

    const secondResult = ctx();
    expect(secondResult).toBe(firstResult);
  });

  it('context value updates when provider prop changes', () => {
    const firstProvider = createMockEventsProvider();
    const secondProvider = createMockEventsProvider();

    renderProvider(firstProvider);
    expect(ctx()).toBe(firstProvider);

    act(() => {
      root.render(createElement(EventsContextProvider, { provider: secondProvider }, createElement(TestConsumer)));
    });

    expect(ctx()).toBe(secondProvider);
  });

  it('inner EventsContextProvider overrides outer', () => {
    const outerProvider = createMockEventsProvider();
    const innerProvider = createMockEventsProvider();

    act(() => {
      root.render(
        createElement(
          EventsContextProvider,
          { provider: outerProvider },
          createElement(EventsContextProvider, { provider: innerProvider }, createElement(TestConsumer)),
        ),
      );
    });

    expect(ctx()).toBe(innerProvider);
  });

  it('provider methods are accessible', () => {
    const provider = createMockEventsProvider();
    renderProvider(provider);

    const p = requireCtx();
    expect(typeof p.connect).toBe('function');
    expect(typeof p.disconnect).toBe('function');
    expect(typeof p.onEvent).toBe('function');
    expect(typeof p.removeEvent).toBe('function');
    expect(typeof p.sendEvent).toBe('function');
    expect(typeof p.isConnected).toBe('function');
    expect(typeof p.onReconnect).toBe('function');
    expect(typeof p.freeze).toBe('function');
    expect(typeof p.resume).toBe('function');
    expect(typeof p.resetAndReconnect).toBe('function');
    expect(typeof p.getQueuedMessageCount).toBe('function');
    expect(typeof p.flushQueuedMessages).toBe('function');
  });

  it('consumer can call provider methods', () => {
    const provider = createMockEventsProvider();
    renderProvider(provider);

    const p = requireCtx();

    // Exercise all methods
    p.connect();
    p.disconnect();
    p.onEvent(vi.fn());
    p.removeEvent(vi.fn());
    p.sendEvent({ type: 'test' });
    p.isConnected();
    p.onReconnect(vi.fn());
    p.freeze();
    p.resume();
    p.resetAndReconnect();
    p.getQueuedMessageCount();
    p.flushQueuedMessages();

    expect(provider.connect).toHaveBeenCalled();
    expect(provider.disconnect).toHaveBeenCalled();
    expect(provider.onEvent).toHaveBeenCalled();
    expect(provider.removeEvent).toHaveBeenCalled();
    expect(provider.sendEvent).toHaveBeenCalledWith({ type: 'test' });
    expect(provider.isConnected).toHaveBeenCalled();
    expect(provider.onReconnect).toHaveBeenCalled();
    expect(provider.freeze).toHaveBeenCalled();
    expect(provider.resume).toHaveBeenCalled();
    expect(provider.resetAndReconnect).toHaveBeenCalled();
    expect(provider.getQueuedMessageCount).toHaveBeenCalled();
    expect(provider.flushQueuedMessages).toHaveBeenCalled();
  });

  it('displayName is set', () => {
    expect(EventsContextProvider.displayName).toBe('EventsContextProvider');
  });
});
