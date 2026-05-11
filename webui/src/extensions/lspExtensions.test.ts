/**
 * lspExtensions.test.ts — Unit tests for the LSP extensions module.
 *
 * Tests isLSPClientConnected, getClientForLanguageSync, buildLSPPluginExtensions,
 * and lspSyncOnDocChange. Uses Jest mocking patterns consistent with other test files.
 */

// ── Mock modules before imports ───────────────────────────────────────

jest.mock('@codemirror/lsp-client', () => ({
  Transport: {},
}));

jest.mock('@codemirror/view', () => ({
  ViewPlugin: {
    fromClass: jest.fn((Class) => ({ type: 'Plugin', Class })),
  },
  EditorView: jest.fn(),
}));

jest.mock('@codemirror/state', () => ({
  StateEffect: {
    define: jest.fn(() => ({ type: 'StateEffect' })),
  },
  StateField: {
    define: jest.fn(() => ({ type: 'StateField' })),
  },
}));

// ── LSPClientService mock ─────────────────────────────────────────────

const mockGetClientSync = jest.fn();
const mockIsSupported = jest.fn();
const mockDispatchSyncToClient = jest.fn();

jest.mock('../services/lspClientService', () => ({
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
  getLSPClientService: jest.fn(),
  createTransport: jest.fn(),
  getInstance: jest.fn(),
  LSP_SUPPORTED_LANGUAGES: new Set(['go', 'typescript']),
  setGlobalDisplayFileCallback: jest.fn(),
  getGlobalDisplayFileCallback: jest.fn(),
  registerEditorView: jest.fn(),
  unregisterEditorView: jest.fn(),
  findEditorView: jest.fn(),
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
    jest.clearAllMocks();
  });

  it('returns true when client is connected', () => {
    mockGetClientSync.mockReturnValue({ connected: true, plugin: jest.fn() });
    expect(isLSPClientConnected('go')).toBe(true);
    expect(mockGetClientSync).toHaveBeenCalledWith('go');
  });

  it('returns false when client is not connected', () => {
    mockGetClientSync.mockReturnValue({ connected: false, plugin: jest.fn() });
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
    jest.clearAllMocks();
  });

  it('returns client when available', () => {
    const mockClient = { connected: true, plugin: jest.fn() };
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
    jest.clearAllMocks();
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
    const mockPlugin = jest.fn().mockReturnValue([]);
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
    const mockPlugin = jest.fn().mockReturnValue([]);
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
    jest.clearAllMocks();
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
