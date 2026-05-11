import { WebSocketService } from './websocket';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../utils/log', () => ({
  debugLog: jest.fn(),
}));

jest.mock('./clientSession', () => ({
  appendClientIdToUrl: jest.fn((url) => url + '?client=test'),
}));

jest.mock('./notificationBus', () => ({
  notificationBus: { notify: jest.fn() },
}));

// Get the mocked functions for assertions
const debugLog = require('../utils/log').debugLog;
const appendClientIdToUrl = require('./clientSession').appendClientIdToUrl;
const notificationBus = require('./notificationBus').notificationBus;

// Mock WebSocket
const mockSend = jest.fn();
const mockClose = jest.fn();
let mockOnOpen = null;
let mockOnClose = null;
let mockOnError = null;
let mockOnMessage = null;
let mockReadyState = 3; // Start as CLOSED
let webSocketConstructorCallCount = 0;

class MockWebSocket {
  static OPEN = 1;
  static CONNECTING = 0;
  static CLOSING = 2;
  static CLOSED = 3;

  get readyState() {
    return mockReadyState;
  }

  send = mockSend;
  close = mockClose;

  set onopen(cb) {
    mockOnOpen = cb;
  }
  get onopen() {
    return mockOnOpen;
  }

  set onclose(cb) {
    mockOnClose = cb;
  }
  get onclose() {
    return mockOnClose;
  }

  set onerror(cb) {
    mockOnError = cb;
  }
  get onerror() {
    return mockOnError;
  }

  set onmessage(cb) {
    mockOnMessage = cb;
  }
  get onmessage() {
    return mockOnMessage;
  }

  constructor(url) {
    this.url = url;
    webSocketConstructorCallCount++;
  }
}

// @ts-ignore - Mock global WebSocket
global.WebSocket = MockWebSocket;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

beforeEach(() => {
  jest.useFakeTimers();
  jest.clearAllMocks();
  jest.spyOn(Math, 'random').mockReturnValue(0.5);

  // Reset WebSocket mock state
  mockReadyState = MockWebSocket.CLOSED;
  mockOnOpen = null;
  mockOnClose = null;
  mockOnError = null;
  mockOnMessage = null;
  webSocketConstructorCallCount = 0;

  // Reset singleton instance between tests
  // @ts-ignore
  WebSocketService.instance = null;

  // Default mock for appendClientId
  appendClientIdToUrl.mockImplementation((url) => url + '?client=test');
});

afterEach(() => {
  jest.useRealTimers();
  jest.spyOn(Math, 'random').mockRestore();
});

function triggerWebSocketOpen() {
  if (mockOnOpen) {
    mockOnOpen();
  }
}

function triggerWebSocketClose(event = { code: 1000, reason: 'Normal closure' }) {
  if (mockOnClose) {
    mockOnClose(event);
  }
}

function triggerWebSocketError(event) {
  if (mockOnError) {
    mockOnError(event);
  }
}

function triggerWebSocketMessage(data) {
  if (mockOnMessage) {
    mockOnMessage({ data: JSON.stringify(data) });
  }
}

// ---------------------------------------------------------------------------
// Test Groups
// ---------------------------------------------------------------------------

describe('WebSocketService - Queue Behavior', () => {
  it('queues messages when WebSocket is not OPEN', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    // WebSocket is CONNECTING, not OPEN yet
    mockReadyState = MockWebSocket.CONNECTING;

    const event = { type: 'test', data: 'hello' };
    ws.sendEvent(event);

    // Message should be queued, not sent
    expect(mockSend).not.toHaveBeenCalled();
    expect(ws.getQueuedMessageCount()).toBe(1);
  });

  it('sends messages immediately when WebSocket is OPEN', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    // Set WebSocket to OPEN
    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    const event = { type: 'test', data: 'hello' };
    ws.sendEvent(event);

    // Message should be sent immediately
    expect(mockSend).toHaveBeenCalledWith(JSON.stringify(event));
    expect(ws.getQueuedMessageCount()).toBe(0);
  });

  it('getQueuedMessageCount returns correct count', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.CONNECTING;

    // Queue 3 messages
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });
    ws.sendEvent({ type: 'msg3' });

    expect(ws.getQueuedMessageCount()).toBe(3);

    // Queue 2 more
    ws.sendEvent({ type: 'msg4' });
    ws.sendEvent({ type: 'msg5' });

    expect(ws.getQueuedMessageCount()).toBe(5);
  });

  it('drops oldest message when queue exceeds maxQueueSize (100)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.CLOSED;

    // Queue 101 messages (max is 100)
    for (let i = 0; i < 101; i++) {
      ws.sendEvent({ type: `msg${i}`, data: i });
    }

    // Should have max 100 messages
    expect(ws.getQueuedMessageCount()).toBe(100);

    // The first message (msg0) should have been dropped
    expect(debugLog).toHaveBeenCalledWith(
      expect.stringContaining('Queue full (100 messages). Dropped oldest message.'),
    );
  });

  it('clears queue on disconnect()', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.CLOSED;

    // Queue some messages
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });
    ws.sendEvent({ type: 'msg3' });

    expect(ws.getQueuedMessageCount()).toBe(3);

    ws.disconnect();

    // Queue should be cleared
    expect(ws.getQueuedMessageCount()).toBe(0);
  });

  it('preserves queue when connection drops unexpectedly (onclose without intentionalClose)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Now disconnect from server side (not intentional)
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    expect(ws.getQueuedMessageCount()).toBe(0); // Messages sent since OPEN

    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose({ code: 1006, reason: 'Abnormal closure' });

    // Queue some messages while disconnected
    ws.sendEvent({ type: 'msg3' });
    ws.sendEvent({ type: 'msg4' });

    expect(ws.getQueuedMessageCount()).toBe(2);

    // Advance timers to trigger reconnect
    jest.advanceTimersByTime(2100);

    // Queue should still have messages (preserved)
    expect(ws.getQueuedMessageCount()).toBe(2);
  });
});

describe('WebSocketService - Replay on Reconnect', () => {
  it('replays queued messages in order on reconnect (wasConnectedBefore=true)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Disconnect and queue messages
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    const events = [
      { type: 'first', data: 1 },
      { type: 'second', data: 2 },
      { type: 'third', data: 3 },
    ];

    events.forEach((e) => ws.sendEvent(e));
    expect(ws.getQueuedMessageCount()).toBe(3);

    // Trigger reconnect
    mockReadyState = MockWebSocket.OPEN;
    mockSend.mockClear();
    triggerWebSocketOpen();

    // Messages should be replayed in order
    expect(mockSend).toHaveBeenCalledTimes(3);
    expect(mockSend).toHaveBeenNthCalledWith(1, JSON.stringify(events[0]));
    expect(mockSend).toHaveBeenNthCalledWith(2, JSON.stringify(events[1]));
    expect(mockSend).toHaveBeenNthCalledWith(3, JSON.stringify(events[2]));

    // Queue should be cleared after replay
    expect(ws.getQueuedMessageCount()).toBe(0);
  });

  it('does NOT replay messages on initial connection (wasConnectedBefore=false)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    // Queue messages before initial open
    mockReadyState = MockWebSocket.CONNECTING;
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    mockSend.mockClear();

    // Initial open
    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    // Messages should NOT be sent on initial connection
    expect(mockSend).not.toHaveBeenCalled();
    expect(ws.getQueuedMessageCount()).toBe(2);
  });

  it('reports queuedMessageCount as 0 in connection_status after successful replay', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Disconnect and queue messages
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    let connectionStatus = null;
    ws.onEvent((event) => {
      if (event.type === 'connection_status') {
        connectionStatus = event;
      }
    });

    // Reconnect
    mockReadyState = MockWebSocket.OPEN;
    mockSend.mockClear();
    triggerWebSocketOpen();

    expect(connectionStatus).toBeTruthy();
    expect(connectionStatus.data).toMatchObject({
      connected: true,
      reconnected: true,
      queuedMessageCount: 0, // Queue cleared after replay
    });
  });

  it('reports queuedMessageCount correctly in disconnect connection_status', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Queue messages before disconnect
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    let connectionStatus = null;
    ws.onEvent((event) => {
      if (event.type === 'connection_status') {
        connectionStatus = event;
      }
    });

    // Disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    expect(connectionStatus).toBeTruthy();
    expect(connectionStatus.data).toMatchObject({
      connected: false,
      reconnecting: true,
      queuedMessageCount: 0, // All messages sent before disconnect
    });

    // Now test with queued messages
    ws.sendEvent({ type: 'msg3' });
    ws.sendEvent({ type: 'msg4' });

    connectionStatus = null;
    triggerWebSocketClose();

    expect(connectionStatus).toBeTruthy();
    expect(connectionStatus.data).toMatchObject({
      connected: false,
      queuedMessageCount: 2, // Messages queued while disconnected
    });
  });
});

describe('WebSocketService - Exponential Backoff', () => {
  it('first retry delay is approximately base delay (2000ms + jitter)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    const initialConstructorCount = webSocketConstructorCallCount;
    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Force disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    // First reconnect attempt scheduled
    jest.advanceTimersByTime(2500); // 2000 + 500 (jitter with random=0.5)

    // New WebSocket should have been created
    expect(webSocketConstructorCallCount).toBeGreaterThan(initialConstructorCount);
  });

  it('second retry delay is approximately 2x base (4000ms + jitter)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // First disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    // First reconnect attempt
    jest.advanceTimersByTime(2500);
    const countAfterFirstReconnect = webSocketConstructorCallCount;
    triggerWebSocketOpen();
    mockReadyState = MockWebSocket.OPEN;

    // Second disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    // Second reconnect attempt - should be longer
    jest.advanceTimersByTime(4500); // 4000 + 500 (jitter)

    // New WebSocket should have been created
    expect(webSocketConstructorCallCount).toBeGreaterThan(countAfterFirstReconnect);
  });

  it('fifth retry delay is approximately 2^4 * base = 32000ms capped at 30000', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    const initialConstructorCount = webSocketConstructorCallCount;
    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Trigger 5 reconnect attempts
    for (let i = 0; i < 5; i++) {
      mockReadyState = MockWebSocket.CLOSED;
      triggerWebSocketClose();

      // Advance past the reconnect delay
      jest.advanceTimersByTime(31000); // Cap is 30000 + 500 jitter
      triggerWebSocketOpen();
      mockReadyState = MockWebSocket.OPEN;
    }

    // Should have attempted to reconnect 5 times
    expect(webSocketConstructorCallCount).toBeGreaterThanOrEqual(initialConstructorCount + 5);
  });

  it('verifies reconnect timer fires and calls connect()', () => {
    const ws = WebSocketService.getInstance();

    const connectSpy = jest.spyOn(ws, 'connect');

    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    connectSpy.mockClear();

    // Advance to trigger reconnect
    jest.advanceTimersByTime(2500);

    expect(connectSpy).toHaveBeenCalled();
  });
});

describe('WebSocketService - flushQueuedMessages', () => {
  it('flushQueuedMessages sends all queued messages when connected', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Queue messages (simulate reconnect scenario)
    mockReadyState = MockWebSocket.CLOSED;
    ws.sendEvent({ type: 'msg1', data: 1 });
    ws.sendEvent({ type: 'msg2', data: 2 });
    ws.sendEvent({ type: 'msg3', data: 3 });

    expect(ws.getQueuedMessageCount()).toBe(3);

    // Now connect (but don't trigger onopen to avoid auto-replay)
    mockReadyState = MockWebSocket.OPEN;

    // Manually flush
    const sentCount = ws.flushQueuedMessages();

    expect(sentCount).toBe(3);
    expect(mockSend).toHaveBeenCalledTimes(3);
    expect(mockSend).toHaveBeenCalledWith(JSON.stringify({ type: 'msg1', data: 1 }));
    expect(mockSend).toHaveBeenCalledWith(JSON.stringify({ type: 'msg2', data: 2 }));
    expect(mockSend).toHaveBeenCalledWith(JSON.stringify({ type: 'msg3', data: 3 }));

    // Queue should be empty
    expect(ws.getQueuedMessageCount()).toBe(0);
  });

  it('flushQueuedMessages returns 0 when not connected', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.CLOSED;

    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    expect(ws.getQueuedMessageCount()).toBe(2);

    // Not connected - flush should do nothing
    const sentCount = ws.flushQueuedMessages();

    expect(sentCount).toBe(0);
    expect(mockSend).not.toHaveBeenCalled();
    expect(ws.getQueuedMessageCount()).toBe(2); // Queue unchanged
  });

  it('flushQueuedMessages clears the queue after sending', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    // Queue messages
    mockReadyState = MockWebSocket.CLOSED;
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    expect(ws.getQueuedMessageCount()).toBe(2);

    // Connect and flush
    mockReadyState = MockWebSocket.OPEN;
    ws.flushQueuedMessages();

    expect(ws.getQueuedMessageCount()).toBe(0);
  });
});

describe('WebSocketService - Connection Status Events', () => {
  it('connection_status on close includes queuedMessageCount', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Queue messages while connected
    ws.sendEvent({ type: 'msg1' });

    // Disconnect and queue more
    mockReadyState = MockWebSocket.CLOSED;
    ws.sendEvent({ type: 'msg2' });
    ws.sendEvent({ type: 'msg3' });

    let connectionStatus = null;
    ws.onEvent((event) => {
      if (event.type === 'connection_status') {
        connectionStatus = event;
      }
    });

    triggerWebSocketClose();

    expect(connectionStatus).toBeTruthy();
    expect(connectionStatus.data).toMatchObject({
      connected: false,
      reconnecting: true,
      queuedMessageCount: 2,
    });
  });

  it('connection_status on open includes queuedMessageCount (0 after replay)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen(); // Initial connection

    // Disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    // Queue messages
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });
    ws.sendEvent({ type: 'msg3' });

    let connectionStatus = null;
    ws.onEvent((event) => {
      if (event.type === 'connection_status') {
        connectionStatus = event;
      }
    });

    // Reconnect (replay happens here)
    mockReadyState = MockWebSocket.OPEN;
    mockSend.mockClear();
    triggerWebSocketOpen();

    expect(connectionStatus).toBeTruthy();
    expect(connectionStatus.data).toMatchObject({
      connected: true,
      reconnected: true,
      queuedMessageCount: 0, // After replay
    });

    // Verify messages were actually replayed
    expect(mockSend).toHaveBeenCalledTimes(3);
  });

  it('connection_status on initial open includes queuedMessageCount', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    // Queue messages before initial open
    mockReadyState = MockWebSocket.CONNECTING;
    ws.sendEvent({ type: 'msg1' });
    ws.sendEvent({ type: 'msg2' });

    let connectionStatus = null;
    ws.onEvent((event) => {
      if (event.type === 'connection_status') {
        connectionStatus = event;
      }
    });

    // Initial open (no replay)
    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    expect(connectionStatus).toBeTruthy();
    expect(connectionStatus.data).toMatchObject({
      connected: true,
      reconnected: false,
      queuedMessageCount: 2, // Not replayed on initial connection
    });
  });
});

describe('WebSocketService - Miscellaneous Queue Tests', () => {
  it('handles mixed sending (some queued, some immediate)', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    // Send while connected - immediate
    ws.sendEvent({ type: 'immediate1' });
    expect(mockSend).toHaveBeenCalledTimes(1);

    // Disconnect
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    // Queue while disconnected
    ws.sendEvent({ type: 'queued1' });
    ws.sendEvent({ type: 'queued2' });

    expect(ws.getQueuedMessageCount()).toBe(2);

    // Reconnect
    mockReadyState = MockWebSocket.OPEN;
    mockSend.mockClear();
    triggerWebSocketOpen();

    // Queued messages should be sent
    expect(mockSend).toHaveBeenCalledTimes(2);

    // Send again while connected - immediate
    ws.sendEvent({ type: 'immediate2' });
    expect(mockSend).toHaveBeenCalledTimes(3);
  });

  it('handles empty queue flush', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    const sentCount = ws.flushQueuedMessages();

    expect(sentCount).toBe(0);
    expect(mockSend).not.toHaveBeenCalled();
  });

  it('handles queue overflow gracefully', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    mockReadyState = MockWebSocket.CLOSED;

    // Queue exactly 100 messages
    for (let i = 0; i < 100; i++) {
      ws.sendEvent({ type: `msg${i}` });
    }

    expect(ws.getQueuedMessageCount()).toBe(100);

    // Add one more - should drop oldest
    ws.sendEvent({ type: 'msg101' });

    expect(ws.getQueuedMessageCount()).toBe(100);
    expect(debugLog).toHaveBeenCalledWith(
      expect.stringContaining('Queue full (100 messages). Dropped oldest message.'),
    );
  });
});

describe('WebSocketService - freeze() and resume() queue preservation', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    jest.spyOn(Math, 'random').mockReturnValue(0.5);
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it('preserves queue across freeze() then resume() with replay', () => {
    const ws = WebSocketService.getInstance();
    ws.connect();

    // Initial connection
    mockReadyState = MockWebSocket.OPEN;
    triggerWebSocketOpen();

    // Disconnect and queue messages
    mockReadyState = MockWebSocket.CLOSED;
    triggerWebSocketClose();

    ws.sendEvent({ type: 'frozen_msg1' });
    ws.sendEvent({ type: 'frozen_msg2' });

    expect(ws.getQueuedMessageCount()).toBe(2);

    // Freeze should preserve queue
    ws.freeze();

    expect(ws.getQueuedMessageCount()).toBe(2);
    expect(ws.isConnected()).toBe(false);

    // Resume triggers reconnect
    mockReadyState = MockWebSocket.OPEN;
    ws.resume();
    triggerWebSocketOpen();

    // Messages should be replayed after resume → reconnect
    expect(mockSend).toHaveBeenCalled();
    expect(ws.getQueuedMessageCount()).toBe(0);
  });
});
