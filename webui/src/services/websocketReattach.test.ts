/**
 * websocketReattach.test.ts — Tests for WebSocket reattach on reconnect.
 *
 * Covers the reattach functionality of WebSocketService:
 * - trackSeq() extracts __seq from incoming events and stores highest seq per chat
 * - setActiveChatId() sets the current active chat for reattach context
 * - getLastSeq() / getActiveChatSeq() return tracked sequences
 * - connect() on reconnect with a known seq → reattach URL params
 * - connect() on initial connection → no reattach params
 * - events whose seq was already delivered are dropped
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks — must come before the import of the module under test
// ---------------------------------------------------------------------------

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
  // config/mode.ts reads this constant at module scope (adapter-installed
  // refresh listener); the mock must declare it or the import fails.
  ADAPTER_INSTALLED_EVENT: 'sprout:adapter-installed',
}));

vi.mock('./notificationBus', () => ({
  notificationBus: {
    notify: vi.fn(),
  },
}));

import { clientFetch } from './clientSession';
import { WebSocketService } from './websocket';

// ---------------------------------------------------------------------------
// Helpers — singleton management & WebSocket mock
// ---------------------------------------------------------------------------

/**
 * Reset the singleton to a fresh instance.
 * WebSocketService is not exported as default, so we access the class directly.
 * We clear the static instance and also clean up any pending timers.
 */
function resetSingleton(): WebSocketService {
  // @ts-ignore — accessing private static property for testing
  WebSocketService.instance = null;
  vi.clearAllTimers();
  return WebSocketService.getInstance();
}

interface MockWebSocketInstance {
  url: string;
  readyState: number;
  onopen: ((event: Event) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  onerror: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  send: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
}

// WebSocket readyState constants (numeric values, independent of global WebSocket mock)
const WS_CONNECTING = 0;
const WS_OPEN = 1;
const WS_CLOSED = 3;

/**
 * Create a mock WebSocket constructor. Returns the mock instance so tests
 * can trigger events (onopen, onmessage, onclose, onerror).
 *
 * IMPORTANT: To simulate incoming messages, trigger `mock.onmessage?(event)`
 * because WebSocketService assigns its handler to `this.ws.onmessage` in connect().
 *
 * The mock includes static CONNECTING/OPEN/CLOSED constants because the
 * production code references them (e.g., `WebSocket.OPEN`). After
 * `vi.stubGlobal('WebSocket', MockWebSocket)`, these need to still work.
 */
function createMockWebSocket() {
  const instances: MockWebSocketInstance[] = [];

  const MockWebSocket = vi.fn(function (this: MockWebSocketInstance, url: string) {
    const instance = {
      url,
      readyState: WS_CONNECTING,
      onopen: null as ((event: Event) => void) | null,
      onclose: null as ((event: CloseEvent) => void) | null,
      onerror: null as ((event: Event) => void) | null,
      onmessage: null as ((event: MessageEvent) => void) | null,
      send: vi.fn(),
      close: vi.fn(),
    } as MockWebSocketInstance;
    instances.push(instance);
    return instance;
  }) as unknown as typeof WebSocket;

  // Mirror real WebSocket static properties so that code referencing
  // WebSocket.OPEN, WebSocket.CLOSED, etc. still works after stubbing.
  (MockWebSocket as any).CONNECTING = WS_CONNECTING;
  (MockWebSocket as any).OPEN = WS_OPEN;
  (MockWebSocket as any).CLOSED = WS_CLOSED;

  return { MockWebSocket, instances };
}

// ---------------------------------------------------------------------------
// Tests: Seq Tracking (trackSeq via onmessage, setActiveChatId, getLastSeq,
// getActiveChatSeq)
// ---------------------------------------------------------------------------

describe('seq tracking', () => {
  let service: WebSocketService;
  let mockWs: MockWebSocketInstance;

  beforeEach(() => {
    vi.useFakeTimers();

    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    service = resetSingleton();
    service.connect();
    mockWs = instances[0];
    // Simulate connection open: update readyState AND fire the handler
    mockWs.readyState = WS_OPEN;
    mockWs.onopen?.(new Event('open'));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('tracks __seq per chat_id from incoming events', () => {
    // Simulate incoming event with __seq for chat-a
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 5, data: { chat_id: 'chat-a' } }),
      }),
    );
    expect(service.getLastSeq('chat-a')).toBe(5);

    // Simulate incoming event with __seq for chat-b
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 10, data: { chat_id: 'chat-b' } }),
      }),
    );
    expect(service.getLastSeq('chat-b')).toBe(10);
  });

  it('keeps only the highest __seq per chat_id', () => {
    // First event seq 3
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 3, data: { chat_id: 'chat-a' } }),
      }),
    );
    expect(service.getLastSeq('chat-a')).toBe(3);

    // Second event seq 7 (higher, should update)
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 7, data: { chat_id: 'chat-a' } }),
      }),
    );
    expect(service.getLastSeq('chat-a')).toBe(7);

    // Third event seq 5 (lower, should NOT update)
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 5, data: { chat_id: 'chat-a' } }),
      }),
    );
    expect(service.getLastSeq('chat-a')).toBe(7);
  });

  it('ignores events without __seq', () => {
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', data: { chat_id: 'chat-a' } }),
      }),
    );
    expect(service.getLastSeq('chat-a')).toBeUndefined();
  });

  it('ignores events with non-numeric __seq', () => {
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 'not-a-number', data: { chat_id: 'chat-a' } }),
      }),
    );
    expect(service.getLastSeq('chat-a')).toBeUndefined();
  });

  it('falls back to activeChatId when event has no data.chat_id', () => {
    service.setActiveChatId('chat-fallback');

    // Event with __seq but no data.chat_id — should use activeChatId
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 42 }),
      }),
    );
    expect(service.getLastSeq('chat-fallback')).toBe(42);
  });

  it('does not track when no chat_id or activeChatId is available', () => {
    service.setActiveChatId(null);

    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 99 }),
      }),
    );
    expect(service.getLastSeq('any-chat')).toBeUndefined();
    expect(service.getActiveChatSeq()).toBeUndefined();
  });

  it('ignores pong events (returns before trackSeq)', () => {
    service.setActiveChatId('chat-a');

    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'pong', __seq: 100 }),
      }),
    );
    expect(service.getActiveChatSeq()).toBeUndefined();
  });

  it('ignores ping events from server (sends pong, returns before trackSeq)', () => {
    service.setActiveChatId('chat-a');

    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'ping', __seq: 100 }),
      }),
    );
    expect(service.getActiveChatSeq()).toBeUndefined();
  });

  it('setActiveChatId + getActiveChatSeq work together', () => {
    service.setActiveChatId('chat-123');

    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 10, data: { chat_id: 'chat-123' } }),
      }),
    );

    expect(service.getActiveChatSeq()).toBe(10);
  });

  it('getActiveChatSeq returns undefined when no active chat', () => {
    service.setActiveChatId(null);
    expect(service.getActiveChatSeq()).toBeUndefined();
  });

  it('getActiveChatSeq returns undefined when active chat has no tracked seq', () => {
    service.setActiveChatId('chat-no-seq');
    expect(service.getActiveChatSeq()).toBeUndefined();
  });

  it('getLastSeq returns undefined for unknown chat_id', () => {
    expect(service.getLastSeq('unknown')).toBeUndefined();
  });

  it('tracks seq for multiple different chats independently', () => {
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 1, data: { chat_id: 'chat-a' } }),
      }),
    );
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 2, data: { chat_id: 'chat-b' } }),
      }),
    );
    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 3, data: { chat_id: 'chat-c' } }),
      }),
    );

    expect(service.getLastSeq('chat-a')).toBe(1);
    expect(service.getLastSeq('chat-b')).toBe(2);
    expect(service.getLastSeq('chat-c')).toBe(3);
  });

  it('prefers data.chat_id from event over activeChatId', () => {
    service.setActiveChatId('active-chat');

    mockWs.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 8, data: { chat_id: 'other-chat' } }),
      }),
    );

    expect(service.getLastSeq('other-chat')).toBe(8);
    expect(service.getLastSeq('active-chat')).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Tests: Reattach on reconnect (connect() with wasConnectedBefore=true)
// ---------------------------------------------------------------------------
// NOTE: We test the reattach logic in connect() directly by:
// 1. Establishing a first connection (sets wasConnectedBefore=true via onopen)
// 2. Tracking seq via onmessage
// 3. Simulating disconnect (readyState=CLOSED)
// 4. Calling service.connect() directly and awaiting it
// This avoids timer/microtask synchronization issues with runOnlyPendingTimersAsync
// while still exercising the exact same code path that the reconnect timer would take.
// ---------------------------------------------------------------------------

describe('reattach on reconnect', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    (clientFetch as ReturnType<typeof vi.fn>).mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  /**
   * Simulate a real WebSocket disconnect: set readyState to CLOSED (3),
   * then fire the onclose handler. The CLOSED readyState is critical
   * because connect() has an early return when readyState is OPEN or
   * CONNECTING — without it, the second connect() would bail out.
   * Also clears onclose/onerror handlers to prevent reconnect timer from firing.
   */
  function simulateDisconnect(mock: MockWebSocketInstance) {
    mock.readyState = WS_CLOSED;
    mock.onclose = null;
    mock.onerror = null;
  }

  it('adds reattach params on reconnect when query is active', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();

    // First connection — establishes wasConnectedBefore
    service.connect();
    const firstMock = instances[0];
    firstMock.onopen?.(new Event('open'));
    expect(firstMock.url).not.toContain('reattach');

    // Set up state for reattach
    service.setActiveChatId('chat-abc');

    // Simulate receiving events with seq during first connection
    firstMock.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 10, data: { chat_id: 'chat-abc' } }),
      }),
    );

    // Simulate disconnect (clear handlers to prevent timer-based reconnect)
    simulateDisconnect(firstMock);

    // Set up clientFetch mock BEFORE calling connect()
    (clientFetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ active: true, chat_id: 'chat-abc' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    // Call connect() directly (same code path as the reconnect timer)
    await service.connect();

    // Second connection should have reattach params
    const reconnectMock = instances[1];
    expect(reconnectMock).toBeDefined();
    expect(reconnectMock!.url).toContain('reattach=chat-abc');
    expect(reconnectMock!.url).toContain('after_seq=10');
  });

  it('does NOT add reattach params on initial connection', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();
    service.setActiveChatId('chat-abc');

    await service.connect();
    const mock = instances[0];
    mock.onopen?.(new Event('open'));

    expect(mock.url).not.toContain('reattach');
    expect(mock.url).not.toContain('after_seq');
    // clientFetch should NOT have been called (wasConnectedBefore is false)
    expect(clientFetch).not.toHaveBeenCalled();
  });

  it('does NOT add reattach params when activeChatId is null', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();

    // First connection
    await service.connect();
    const firstMock = instances[0];
    firstMock.onopen?.(new Event('open'));

    // activeChatId is null (default)

    simulateDisconnect(firstMock);

    // No clientFetch mock needed — activeChatId is null, so it won't be called
    await service.connect();

    const reconnectMock = instances[1];
    expect(reconnectMock).toBeDefined();
    expect(reconnectMock!.url).not.toContain('reattach');
    expect(clientFetch).not.toHaveBeenCalled();
  });

  it('does NOT add reattach params when no seq has been tracked', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();

    // First connection
    await service.connect();
    const firstMock = instances[0];
    firstMock.onopen?.(new Event('open'));

    service.setActiveChatId('chat-no-seq');
    // No messages received, so no seq tracked

    simulateDisconnect(firstMock);

    await service.connect();

    const reconnectMock = instances[1];
    expect(reconnectMock).toBeDefined();
    expect(reconnectMock!.url).not.toContain('reattach');
    expect(clientFetch).not.toHaveBeenCalled();
  });

  it('sends after_seq=0 when last tracked seq is 0', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();

    // First connection
    await service.connect();
    const firstMock = instances[0];
    firstMock.onopen?.(new Event('open'));

    service.setActiveChatId('chat-zero');
    // Track seq=0 (the first event from the server)
    firstMock.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', __seq: 0, data: { chat_id: 'chat-zero' } }),
      }),
    );
    expect(service.getLastSeq('chat-zero')).toBe(0);

    simulateDisconnect(firstMock);

    // Mock clientFetch — query is active
    (clientFetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ active: true, chat_id: 'chat-zero' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await service.connect();

    const reconnectMock = instances[1];
    expect(reconnectMock).toBeDefined();
    expect(reconnectMock!.url).toContain('reattach=chat-zero');
    expect(reconnectMock!.url).toContain('after_seq=0');
  });

  it('reattaches without a status round-trip, even after the run ended', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();

    await service.connect();
    const firstMock = instances[0];
    firstMock.onopen?.(new Event('open'));

    service.setActiveChatId('chat-done');
    firstMock.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', data: { chat_id: 'chat-done', __seq: 5 } }),
      }),
    );

    simulateDisconnect(firstMock);
    await service.connect();

    // The server keeps the run's buffer after it ends, so the tail of a run
    // that finished during the outage still comes back.
    const reconnect = instances[1];
    expect(reconnect!.url).toContain('reattach=chat-done');
    expect(reconnect!.url).toContain('after_seq=5');
    expect(clientFetch).not.toHaveBeenCalled();
  });

  it('follows the highest seq across reconnects', async () => {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);

    const service = resetSingleton();

    await service.connect();
    const firstMock = instances[0];
    firstMock.onopen?.(new Event('open'));
    service.setActiveChatId('chat-retry');
    firstMock.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', data: { chat_id: 'chat-retry', __seq: 5 } }),
      }),
    );

    simulateDisconnect(firstMock);
    await service.connect();
    const reconnect1 = instances[1];
    expect(reconnect1!.url).toContain('after_seq=5');
    reconnect1.onopen?.(new Event('open'));
    reconnect1.onmessage?.(
      new MessageEvent('message', {
        data: JSON.stringify({ type: 'stream_chunk', data: { chat_id: 'chat-retry', __seq: 8 } }),
      }),
    );

    simulateDisconnect(reconnect1);
    await service.connect();
    expect(instances[2]!.url).toContain('reattach=chat-retry');
    expect(instances[2]!.url).toContain('after_seq=8');
  });
});

describe('duplicate suppression', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  function connected() {
    const { MockWebSocket, instances } = createMockWebSocket();
    vi.stubGlobal('WebSocket', MockWebSocket);
    const service = resetSingleton();
    service.connect();
    const mock = instances[0];
    mock.readyState = WS_OPEN;
    mock.onopen?.(new Event('open'));
    const received: unknown[] = [];
    service.onEvent((e) => received.push(e));
    const send = (frame: unknown) => mock.onmessage?.(new MessageEvent('message', { data: JSON.stringify(frame) }));
    return { service, received, send };
  }

  it('delivers an event once when a replay repeats a seq the live stream already sent', () => {
    const { received, send } = connected();
    const chunk = (seq: number) => ({ type: 'stream_chunk', data: { chat_id: 'c', chunk: `t${seq}`, __seq: seq } });

    send(chunk(1));
    send(chunk(2));
    send({ type: 'chat_run_restored', data: { chat_id: 'c', after_seq: 1, last_seq: 3, gap: false } });
    send(chunk(2));
    send(chunk(3));

    const chunks = received.filter((e) => (e as { type: string }).type === 'stream_chunk');
    expect(chunks.map((e) => (e as { data: { chunk: string } }).data.chunk)).toEqual(['t1', 't2', 't3']);
  });

  it('keeps per-chat seqs apart and passes events without a seq', () => {
    const { received, send } = connected();
    send({ type: 'stream_chunk', data: { chat_id: 'a', __seq: 7 } });
    send({ type: 'stream_chunk', data: { chat_id: 'b', __seq: 7 } });
    send({ type: 'metrics_update', data: {} });
    send({ type: 'metrics_update', data: {} });
    expect(received).toHaveLength(4);
  });

  it("forgets a chat's seqs when the server restarted its buffer", () => {
    const { service, received, send } = connected();
    send({ type: 'stream_chunk', data: { chat_id: 'c', __seq: 50 } });
    send({ type: 'chat_run_restored', data: { chat_id: 'c', after_seq: 50, last_seq: 2, gap: true } });
    expect(service.getLastSeq('c')).toBeUndefined();
    send({ type: 'stream_chunk', data: { chat_id: 'c', __seq: 50 } });
    expect(received.filter((e) => (e as { type: string }).type === 'stream_chunk')).toHaveLength(2);
  });

  it('markApplied reports server-snapshot events the stream already delivered', () => {
    const { service, send } = connected();
    send({ type: 'tool_start', data: { chat_id: 'c', __seq: 4 } });
    expect(service.markApplied({ type: 'tool_start', data: { chat_id: 'c', __seq: 4 } } as never)).toBe(false);
    expect(service.markApplied({ type: 'tool_end', data: { chat_id: 'c', __seq: 5 } } as never)).toBe(true);
    expect(service.markApplied({ type: 'tool_end', data: { chat_id: 'c', __seq: 5 } } as never)).toBe(false);
  });
});
