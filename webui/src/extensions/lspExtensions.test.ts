/**
 * lspExtensions.test.ts — Unit tests for the LSP extensions module.
 *
 * Tests isLSPClientConnected, getClientForLanguageSync, buildLSPPluginExtensions,
 * and lspSyncOnDocChange. Uses Jest mocking patterns consistent with other test files.
 */

// ── Mock modules before imports ───────────────────────────────────────

vi.mock('@codemirror/lsp-client', () => ({
  Transport: {},
}));

vi.mock('@codemirror/view', () => ({
  ViewPlugin: {
    fromClass: vi.fn((Class) => ({ type: 'Plugin', Class })),
  },
  EditorView: vi.fn(),
}));

vi.mock('@codemirror/state', () => ({
  StateEffect: {
    define: vi.fn(() => ({ type: 'StateEffect' })),
  },
  StateField: {
    define: vi.fn(() => ({ type: 'StateField' })),
  },
}));

// ── LSPClientService mock ─────────────────────────────────────────────

const mockGetClientSync = vi.fn();
const mockIsSupported = vi.fn();
const mockDispatchSyncToClient = vi.fn();

vi.mock('../services/lspClientService', () => ({
  LSPClientService: {
    lspClientService: {
      getClientSync: (...args: unknown[]) => mockGetClientSync(...args),
      isSupported: (...args: unknown[]) => mockIsSupported(...args),
      dispatchSyncToClient: (...args: unknown[]) => mockDispatchSyncToClient(...args),
    },
  },
  getFileURI: (filePath: string) => {
    if (!filePath) return '';
    return `file://${filePath}`;
  },
  uriToFilePath: (uri: string) => {
    if (uri.startsWith('file://')) return uri.replace('file://', '');
    return uri;
  },
  getLSPClientService: vi.fn(),
  createTransport: vi.fn(),
  getInstance: vi.fn(),
  LSP_SUPPORTED_LANGUAGES: new Set(['go', 'typescript']),
  setGlobalDisplayFileCallback: vi.fn(),
  getGlobalDisplayFileCallback: vi.fn(),
  registerEditorView: vi.fn(),
  unregisterEditorView: vi.fn(),
  findEditorView: vi.fn(),
}));

// ── Module under test ─────────────────────────────────────────────────

import {
  isLSPClientConnected,
  getClientForLanguageSync,
  buildLSPPluginExtensions,
  lspSyncOnDocChange,
} from './lspExtensions';

// ── isLSPClientConnected tests ────────────────────────────────────────

describe('isLSPClientConnected', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns true when client is connected', () => {
    mockGetClientSync.mockReturnValue({ connected: true, plugin: vi.fn() });
    expect(isLSPClientConnected('go')).toBe(true);
    expect(mockGetClientSync).toHaveBeenCalledWith('go');
  });

  it('returns false when client is not connected', () => {
    mockGetClientSync.mockReturnValue({ connected: false, plugin: vi.fn() });
    expect(isLSPClientConnected('go')).toBe(false);
  });

  it('returns false when client is null', () => {
    mockGetClientSync.mockReturnValue(null);
    expect(isLSPClientConnected('go')).toBe(false);
  });

  it('returns false when client is undefined', () => {
    mockGetClientSync.mockReturnValue(undefined);
    expect(isLSPClientConnected('typescript')).toBe(false);
  });
});

// ── getClientForLanguageSync tests ────────────────────────────────────

describe('getClientForLanguageSync', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns client when available', () => {
    const mockClient = { connected: true, plugin: vi.fn() };
    mockGetClientSync.mockReturnValue(mockClient);
    expect(getClientForLanguageSync('go')).toBe(mockClient);
    expect(mockGetClientSync).toHaveBeenCalledWith('go');
  });

  it('returns null when client is not available', () => {
    mockGetClientSync.mockReturnValue(null);
    expect(getClientForLanguageSync('go')).toBeNull();
  });
});

// ── buildLSPPluginExtensions tests ────────────────────────────────────

describe('buildLSPPluginExtensions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns empty array when client is null', () => {
    const result = buildLSPPluginExtensions(null, '/test/file.go', 'go');
    expect(result).toEqual([]);
  });

  it('returns empty array when client is undefined', () => {
    const result = buildLSPPluginExtensions(undefined as unknown as null, '/test/file.go', 'go');
    expect(result).toEqual([]);
  });

  it('returns [client.plugin()] when client exists', () => {
    const mockPlugin = vi.fn().mockReturnValue([]);
    const mockClient = { connected: true, plugin: mockPlugin };
    const result = buildLSPPluginExtensions(
      mockClient as unknown as unknown as import('@codemirror/lsp-client').LSPClient,
      '/test/file.go',
      'go',
    );
    expect(mockPlugin).toHaveBeenCalledWith('file:///test/file.go', 'go');
    expect(result).toEqual([[]]);
  });

  it('returns [client.plugin()] with empty file path', () => {
    const mockPlugin = vi.fn().mockReturnValue([]);
    const mockClient = { connected: true, plugin: mockPlugin };
    const result = buildLSPPluginExtensions(
      mockClient as unknown as unknown as import('@codemirror/lsp-client').LSPClient,
      '',
      'go',
    );
    expect(mockPlugin).toHaveBeenCalledWith('', 'go');
    expect(result).toEqual([[]]);
  });
});

// ── lspSyncOnDocChange tests ──────────────────────────────────────────

describe('lspSyncOnDocChange', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns empty array when LSP is not supported', () => {
    mockIsSupported.mockReturnValue(false);
    const result = lspSyncOnDocChange('python');
    expect(result).toEqual([]);
    expect(mockIsSupported).toHaveBeenCalledWith('python');
  });

  it('returns extensions when LSP is supported', () => {
    mockIsSupported.mockReturnValue(true);
    const result = lspSyncOnDocChange('go');
    expect(result).toBeDefined();
    expect(Array.isArray(result)).toBe(true);
    expect(result.length).toBeGreaterThan(0);
    expect(mockIsSupported).toHaveBeenCalledWith('go');
  });
});
