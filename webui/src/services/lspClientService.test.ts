/**
 * lspClientService.test.ts — Unit tests for the LSP client service module.
 *
 * Tests getFileURI, uriToFilePath, LSP_SUPPORTED_LANGUAGES, and createTransport.
 * Uses Jest mocking patterns consistent with other test files in the project.
 */

// ── Mock modules before imports ───────────────────────────────────────

jest.mock('./api', () => ({
  ApiService: {
    getInstance: jest.fn(),
  },
}));

jest.mock('@codemirror/lsp-client', () => ({
  LSPClient: jest.fn(),
  languageServerExtensions: jest.fn(() => []),
}));

// ── Module under test ─────────────────────────────────────────────────

import {
  getFileURI,
  uriToFilePath,
  LSP_SUPPORTED_LANGUAGES,
  createTransport,
} from './lspClientService';

// ── getFileURI tests ──────────────────────────────────────────────────

describe('getFileURI', () => {
  it('returns empty string for empty input', () => {
    expect(getFileURI('')).toBe('');
  });

  it('converts absolute path to file:// URI', () => {
    expect(getFileURI('/home/user/project')).toBe('file:///home/user/project');
  });

  it('converts simple path', () => {
    expect(getFileURI('/tmp/test.go')).toBe('file:///tmp/test.go');
  });

  it('normalizes backslashes to forward slashes', () => {
    expect(getFileURI('C:\\Users\\project')).toBe('file:///C:/Users/project');
  });

  it('prepends slash for Windows paths without leading slash', () => {
    expect(getFileURI('C:/Users/project')).toBe('file:///C:/Users/project');
  });

  it('handles paths with special characters', () => {
    expect(getFileURI('/home/user/my project')).toBe('file:///home/user/my project');
  });
});

// ── uriToFilePath tests ───────────────────────────────────────────────

describe('uriToFilePath', () => {
  it('converts file:// URI to file path', () => {
    expect(uriToFilePath('file:///home/user/project')).toBe('/home/user/project');
  });

  it('returns URI unchanged when not a file:// scheme', () => {
    expect(uriToFilePath('https://example.com')).toBe('https://example.com');
  });

  it('decodes percent-encoded characters', () => {
    expect(uriToFilePath('file:///home/user/my%20project')).toBe('/home/user/my project');
  });

  it('handles simple file URI', () => {
    expect(uriToFilePath('file:///tmp/test.go')).toBe('/tmp/test.go');
  });

  it('handles URI with triple slash after file:', () => {
    expect(uriToFilePath('file:///etc/hosts')).toBe('/etc/hosts');
  });
});

// ── LSP_SUPPORTED_LANGUAGES tests ─────────────────────────────────────

describe('LSP_SUPPORTED_LANGUAGES', () => {
  it('contains go', () => {
    expect(LSP_SUPPORTED_LANGUAGES.has('go')).toBe(true);
  });

  it('contains typescript', () => {
    expect(LSP_SUPPORTED_LANGUAGES.has('typescript')).toBe(true);
  });

  it('contains typescript-jsx', () => {
    expect(LSP_SUPPORTED_LANGUAGES.has('typescript-jsx')).toBe(true);
  });

  it('contains javascript', () => {
    expect(LSP_SUPPORTED_LANGUAGES.has('javascript')).toBe(true);
  });

  it('contains javascript-jsx', () => {
    expect(LSP_SUPPORTED_LANGUAGES.has('javascript-jsx')).toBe(true);
  });

  it('is a Set', () => {
    expect(LSP_SUPPORTED_LANGUAGES).toBeInstanceOf(Set);
  });

  it('does not contain unsupported languages', () => {
    expect(LSP_SUPPORTED_LANGUAGES.has('python')).toBe(false);
    expect(LSP_SUPPORTED_LANGUAGES.has('ruby')).toBe(false);
  });
});

// ── createTransport tests ─────────────────────────────────────────────

describe('createTransport', () => {
  let mockWsInstance: {
    send: jest.Mock;
    close: jest.Mock;
    onopen: (() => void) | null;
    onerror: (() => void) | null;
    onclose: (() => void) | null;
    onmessage: ((event: { data: string }) => void) | null;
    readyState: number;
  };

  beforeEach(() => {
    mockWsInstance = {
      send: jest.fn(),
      close: jest.fn(),
      onopen: null,
      onerror: null,
      onclose: null,
      onmessage: null,
      readyState: 0,
    };

    // Save original WebSocket constants before mocking
    const OriginalWebSocket = globalThis.WebSocket;

    // Mock the global WebSocket constructor while preserving constants
    jest.spyOn(globalThis, 'WebSocket').mockImplementation(
      () => mockWsInstance as unknown as WebSocket
    );

    // Restore WebSocket.OPEN, CLOSING, CLOSED constants that jest.mock removes
    (globalThis.WebSocket as unknown as { OPEN: number }).OPEN = OriginalWebSocket.OPEN;
    (globalThis.WebSocket as unknown as { CLOSING: number }).CLOSING = OriginalWebSocket.CLOSING;
    (globalThis.WebSocket as unknown as { CLOSED: number }).CLOSED = OriginalWebSocket.CLOSED;

    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    (globalThis.WebSocket as jest.Mock).mockRestore();
  });

  it('resolves with a transport object that has send, subscribe, unsubscribe, close', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    // Simulate WebSocket open
    mockWsInstance.readyState = 1; // WebSocket.OPEN
    mockWsInstance.onopen!();

    const transport = await transportPromise;

    // Verify transport interface
    expect(typeof transport.send).toBe('function');
    expect(typeof transport.subscribe).toBe('function');
    expect(typeof transport.unsubscribe).toBe('function');
    expect(typeof transport.close).toBe('function');
  });

  it('transport.send calls ws.send', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    transport.send('{"jsonrpc":"2.0"}');

    expect(mockWsInstance.send).toHaveBeenCalledWith('{"jsonrpc":"2.0"}');
  });

  it('transport.send throws when WebSocket is not open', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    mockWsInstance.readyState = 2; // WebSocket.CLOSING
    expect(() => transport.send('test')).toThrow('WebSocket not open');
  });

  it('transport.subscribe adds a handler that receives messages', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    const handler = jest.fn();
    transport.subscribe(handler);

    // Simulate a message
    mockWsInstance.onmessage!({ data: 'hello' });

    expect(handler).toHaveBeenCalledWith('hello');
  });

  it('transport.unsubscribe removes a handler', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    const handler = jest.fn();
    transport.subscribe(handler);
    transport.unsubscribe(handler);

    // Simulate a message
    mockWsInstance.onmessage!({ data: 'hello' });

    expect(handler).not.toHaveBeenCalled();
  });

  it('transport.close calls ws.close when WebSocket is open', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    transport.close();

    expect(mockWsInstance.close).toHaveBeenCalled();
  });

  it('transport.close does not call ws.close when WebSocket is not open', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    mockWsInstance.readyState = 2; // WebSocket.CLOSING
    transport.close();

    expect(mockWsInstance.close).not.toHaveBeenCalled();
  });

  it('rejects when WebSocket connection times out', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    // Advance past the connection timeout (30s)
    jest.advanceTimersByTime(30_000);

    await expect(transportPromise).rejects.toThrow('WebSocket connection timeout');
  });

  it('rejects when WebSocket has an error', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.onerror!();

    await expect(transportPromise).rejects.toThrow('WebSocket error');
  });

  it('calls onClose when WebSocket closes after connection', async () => {
    const mockOnClose = jest.fn();
    const transportPromise = createTransport('ws://localhost:8080/lsp', mockOnClose);

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;

    // Subscribe a handler to ensure handlers.size > 0 (triggers onClose path)
    const handler = jest.fn();
    transport.subscribe(handler);

    // Simulate WebSocket close
    mockWsInstance.onclose!();

    expect(mockOnClose).toHaveBeenCalled();
  });

  it('ignores non-string message data', async () => {
    const transportPromise = createTransport('ws://localhost:8080/lsp');

    mockWsInstance.readyState = 1;
    mockWsInstance.onopen!();

    const transport = await transportPromise;
    const handler = jest.fn();
    transport.subscribe(handler);

    // Simulate non-string data (e.g., binary)
    mockWsInstance.onmessage!({ data: new ArrayBuffer(8) } as unknown as MessageEvent);

    expect(handler).not.toHaveBeenCalled();
  });
});
