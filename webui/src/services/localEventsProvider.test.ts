// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { LocalEventsProvider } from './localEventsProvider';
import { WebSocketService } from './websocket';

// ---------------------------------------------------------------------------
// Mocks — the mock factory must be self-contained (no references to
// outer-scope variables that haven't been initialised when the factory
// is hoisted by vi.mock).
// ---------------------------------------------------------------------------

const mockInstance = {
  connect: vi.fn().mockResolvedValue(undefined),
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
};

vi.mock('./websocket', () => ({
  WebSocketService: {
    getInstance: vi.fn(() => mockInstance),
  },
}));

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

beforeEach(() => {
  // Reset all mock call counts but don't reset the getInstance implementation
  vi.clearAllMocks();
  // Restore the getInstance implementation after clearAllMocks nukes it
  WebSocketService.getInstance.mockImplementation(() => mockInstance);
  // Restore default return values
  mockInstance.isConnected.mockReturnValue(true);
  mockInstance.getQueuedMessageCount.mockReturnValue(0);
  mockInstance.flushQueuedMessages.mockReturnValue(0);
});

describe('LocalEventsProvider', () => {
  it('delegates connect() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    provider.connect();
    expect(mockInstance.connect).toHaveBeenCalledTimes(1);
  });

  it('delegates disconnect() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    provider.disconnect();
    expect(mockInstance.disconnect).toHaveBeenCalledTimes(1);
  });

  it('delegates onEvent() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    const cb = vi.fn();
    provider.onEvent(cb);
    expect(mockInstance.onEvent).toHaveBeenCalledWith(cb);
  });

  it('delegates removeEvent() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    const cb = vi.fn();
    provider.removeEvent(cb);
    expect(mockInstance.removeEvent).toHaveBeenCalledWith(cb);
  });

  it('delegates sendEvent() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    const event = { type: 'test', data: { key: 'value' } };
    provider.sendEvent(event);
    expect(mockInstance.sendEvent).toHaveBeenCalledWith(event);
  });

  it('delegates isConnected() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    mockInstance.isConnected.mockReturnValue(true);
    expect(provider.isConnected()).toBe(true);
    mockInstance.isConnected.mockReturnValue(false);
    expect(provider.isConnected()).toBe(false);
  });

  it('delegates onReconnect() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    const cb = vi.fn();
    provider.onReconnect(cb);
    expect(mockInstance.onReconnect).toHaveBeenCalledWith(cb);

    provider.onReconnect(null);
    expect(mockInstance.onReconnect).toHaveBeenCalledWith(null);
  });

  it('delegates freeze() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    provider.freeze();
    expect(mockInstance.freeze).toHaveBeenCalledTimes(1);
  });

  it('delegates resume() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    provider.resume();
    expect(mockInstance.resume).toHaveBeenCalledTimes(1);
  });

  it('delegates resetAndReconnect() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    provider.resetAndReconnect();
    expect(mockInstance.resetAndReconnect).toHaveBeenCalledTimes(1);
  });

  it('delegates getQueuedMessageCount() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    mockInstance.getQueuedMessageCount.mockReturnValue(5);
    expect(provider.getQueuedMessageCount()).toBe(5);
  });

  it('delegates flushQueuedMessages() to WebSocketService', () => {
    const provider = new LocalEventsProvider();
    mockInstance.flushQueuedMessages.mockReturnValue(3);
    expect(provider.flushQueuedMessages()).toBe(3);
  });

  it('calls WebSocketService.getInstance() for each method call', () => {
    const provider = new LocalEventsProvider();

    provider.connect();
    provider.disconnect();
    provider.sendEvent({ type: 'test' });

    // Each method call triggers getInstance()
    expect(WebSocketService.getInstance).toHaveBeenCalledTimes(3);
  });

  it('implements EventsProvider interface (all methods present)', () => {
    const provider = new LocalEventsProvider();

    expect(typeof provider.connect).toBe('function');
    expect(typeof provider.disconnect).toBe('function');
    expect(typeof provider.onEvent).toBe('function');
    expect(typeof provider.removeEvent).toBe('function');
    expect(typeof provider.sendEvent).toBe('function');
    expect(typeof provider.isConnected).toBe('function');
    expect(typeof provider.onReconnect).toBe('function');
    expect(typeof provider.freeze).toBe('function');
    expect(typeof provider.resume).toBe('function');
    expect(typeof provider.resetAndReconnect).toBe('function');
    expect(typeof provider.getQueuedMessageCount).toBe('function');
    expect(typeof provider.flushQueuedMessages).toBe('function');
  });
});
