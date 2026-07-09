// Diagnostic-logging tests for sendRawInput / reprInput.
//
// Why this file exists:
//   We're investigating why keyboard input from the user doesn't reach the
//   WebSocket when running interactive apps like vim in the embedded terminal.
//   To narrow down whether the bug lives in xterm's onData path or in the
//   WS service's sendRawInput gate, we instrumented both layers with
//   debugLog calls. These tests pin the logging behavior so future refactors
//   don't silently drop the diagnostics.

import { debugLog } from '../utils/log';
import { reprInput, TerminalWebSocketService } from './terminalWebSocket';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
  setMinLevel: vi.fn(),
  getMinLevel: vi.fn(() => 0),
  Levels: { debug: 0, info: 1, success: 2, warn: 3, error: 4 },
  DEFAULT_LOG_TITLE: 'mock',
  DEFAULT_NOTIFICATION_DURATION: 5000,
  useLog: () => ({
    debug: vi.fn(),
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  }),
}));

// Pull the mocked debugLog out for assertions.
const mockedDebugLog = debugLog as unknown as ReturnType<typeof vi.fn>;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

beforeEach(() => {
  vi.clearAllMocks();
  // Re-attach after clearAllMocks.
  consoleSpy.mockClear();
});

function makeReadyService(): {
  service: TerminalWebSocketService;
  ws: { send: ReturnType<typeof vi.fn>; readyState: number };
  wsRef: { current: { send: ReturnType<typeof vi.fn>; readyState: number } | null };
} {
  const service = TerminalWebSocketService.createInstance();
  const ws = {
    send: vi.fn(),
    readyState: WebSocket.OPEN,
  };
  // Reach into the service to wire a fake WebSocket in place of the real one.
  (service as unknown as { ws: typeof ws }).ws = ws;
  (service as unknown as { isConnected: boolean }).isConnected = true;
  (service as unknown as { sessionId: string }).sessionId = 'test-session';
  return {
    service,
    ws,
    wsRef: { current: ws },
  };
}

// ---------------------------------------------------------------------------
// reprInput
// ---------------------------------------------------------------------------

describe('reprInput', () => {
  it('passes through printable ASCII verbatim', () => {
    expect(reprInput('a')).toBe('a');
    expect(reprInput('hello world')).toBe('hello world');
  });

  it('escapes ESC as \\x1b', () => {
    expect(reprInput('\x1b[A')).toBe('\\x1b[A');
    expect(reprInput('\x1b[1;2H')).toBe('\\x1b[1;2H');
  });

  it('renders \\n, \\r, \\t using their backslash forms', () => {
    expect(reprInput('hello\n')).toBe('hello\\n');
    expect(reprInput('a\rb')).toBe('a\\rb');
    expect(reprInput('a\tb')).toBe('a\\tb');
  });

  it('renders other control chars as ^X', () => {
    expect(reprInput('\x03')).toBe('^C');
    expect(reprInput('\x12')).toBe('^R');
    expect(reprInput('\x1a')).toBe('^Z');
  });

  it('renders DEL (0x7f) as ^?', () => {
    expect(reprInput('\x7f')).toBe('^?');
  });

  it('collapses large payloads into a length tag', () => {
    const big = 'x'.repeat(1000);
    expect(reprInput(big)).toBe('<paste 1000 chars>');
  });

  it('keeps payloads at the truncation boundary intact', () => {
    const at = 'a'.repeat(60);
    expect(reprInput(at)).toBe(at); // 60 chars exactly → no truncation
    const over = 'a'.repeat(61);
    expect(reprInput(over)).toBe('<paste 61 chars>');
  });
});

// ---------------------------------------------------------------------------
// sendRawInput — diagnostic-logging contract
//
// These tests are the regression net for the keystroke-pipeline bug. If a
// future change silently swallows sendRawInput again, these tests fail.
// ---------------------------------------------------------------------------

describe('sendRawInput diagnostic logging', () => {
  it('logs a "dropped (not connected)" reason when not connected', () => {
    const { service } = makeReadyService();
    (service as unknown as { isConnected: boolean }).isConnected = false;
    const result = (service as unknown as { sendRawInput: (s: string) => boolean }).sendRawInput('a');
    expect(result).toBe(false);
    expect(mockedDebugLog).toHaveBeenCalled();
    const message = mockedDebugLog.mock.calls[0][0] as string;
    expect(message).toContain('sendRawInput dropped');
    expect(message).toContain('not connected');
    expect(message).toContain('a');
  });

  it('logs a "dropped (no session)" reason when no session', () => {
    const { service } = makeReadyService();
    (service as unknown as { sessionId: string | null }).sessionId = null;
    const result = (service as unknown as { sendRawInput: (s: string) => boolean }).sendRawInput('a');
    expect(result).toBe(false);
    expect(mockedDebugLog).toHaveBeenCalled();
    const message = mockedDebugLog.mock.calls[0][0] as string;
    expect(message).toContain('sendRawInput dropped');
    expect(message).toContain('no session');
  });

  it('logs a "ws not open" reason when the WebSocket is not OPEN', () => {
    const { service, ws } = makeReadyService();
    ws.readyState = WebSocket.CONNECTING;
    const result = (service as unknown as { sendRawInput: (s: string) => boolean }).sendRawInput('a');
    expect(result).toBe(false);
    expect(mockedDebugLog).toHaveBeenCalled();
    const message = mockedDebugLog.mock.calls[0][0] as string;
    expect(message).toContain('sendRawInput dropped');
    expect(message).toContain('ws not open');
  });

  it('logs a "sent" reason with session id when send succeeds', () => {
    const { service, ws } = makeReadyService();
    const result = (service as unknown as { sendRawInput: (s: string) => boolean }).sendRawInput('hi');
    expect(result).toBe(true);
    expect(ws.send).toHaveBeenCalledWith(expect.stringContaining('"type":"input_raw"'));
    expect(mockedDebugLog).toHaveBeenCalled();
    const message = mockedDebugLog.mock.calls[0][0] as string;
    expect(message).toContain('sendRawInput sent');
    expect(message).toContain('test-session');
    expect(message).toContain('hi');
  });

  it('returns false (public API unchanged) for the drop path', () => {
    const { service } = makeReadyService();
    (service as unknown as { isConnected: boolean }).isConnected = false;
    const result = (service as unknown as { sendRawInput: (s: string) => boolean }).sendRawInput('a');
    expect(typeof result).toBe('boolean');
    expect(result).toBe(false);
  });
});
