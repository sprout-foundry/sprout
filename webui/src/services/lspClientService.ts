/**
 * LSP Client Service
 *
 * Manages WebSocket connections to the backend LSP proxy server.
 * Provides a singleton service for creating and managing LSP clients per language.
 */

import type { Transport, LSPClientConfig, LSPClient } from '@codemirror/lsp-client';
import { LSPClient as LSPClientClass, languageServerExtensions } from '@codemirror/lsp-client';
import type { EditorView } from '@codemirror/view';
import { warn } from '../utils/log';
import { ApiService } from './api';
import { clientFetch } from './clientSession';

// Types from @codemirror/lsp-client

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Information about LSP server availability for a language. */
export interface LSPLanguageInfo {
  available: boolean;
  binaryPath?: string;
  serverId: string;
}

/**
 * LSP status response from the backend.
 * GET /api/lsp/status returns this structure.
 */
export interface LSPStatusResponse {
  servers: LSPServerInfo[];
  active: number;
  workspace: string;
}

export interface LSPServerInfo {
  id: string;
  languages: string[];
  binary: string;
  available: boolean;
  binaryPath?: string;
  installHint?: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Set of languages that have LSP support on the backend. */
export const LSP_SUPPORTED_LANGUAGES: Set<string> = new Set([
  'go',
  'typescript',
  'typescript-jsx',
  'javascript',
  'javascript-jsx',
  // Additional languages
  'python',
  'rust',
  'c',
  'cpp',
  'csharp',
  'java',
  'ruby',
  'php',
  'swift',
  'kotlin',
  'dart',
  'lua',
  'shellscript',
  'bash',
  'sh',
]);

/** Default timeout for LSP client config (10 seconds). */
const DEFAULT_TIMEOUT_MS = 10_000;

/** WebSocket connection timeout (30 seconds). */
const WS_CONNECT_TIMEOUT_MS = 30_000;

/** Maximum reconnect backoff time (30 seconds). */
const MAX_RECONNECT_BACKOFF_MS = 30_000;

/** Maximum reconnect attempts before giving up. */
const MAX_RECONNECT_ATTEMPTS = 15;

/** LSP client connection state. */
export type LSPConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

// ---------------------------------------------------------------------------
// EditorView Registry for cross-file LSP navigation
// ---------------------------------------------------------------------------

/** Registry of file paths → their active EditorViews (for cross-file LSP navigation) */
const editorViewRegistry: Map<string, EditorView> = new Map();

/**
 * Register an editor view for a file path (called by EditorPane when editor is created).
 * This enables the LSP displayFile callback to find open editors for cross-file navigation.
 */
export function registerEditorView(filePath: string, view: EditorView): void {
  editorViewRegistry.set(filePath, view);
}

/**
 * Unregister an editor view (called when EditorPane destroys the editor).
 */
export function unregisterEditorView(filePath: string): void {
  editorViewRegistry.delete(filePath);
}

/**
 * Find an editor view by file path.
 * Used by the LSP displayFile callback to get the EditorView for position mapping.
 */
export function findEditorView(filePath: string): EditorView | null {
  return editorViewRegistry.get(filePath) ?? null;
}

// ---------------------------------------------------------------------------
// Transport Implementation
// ---------------------------------------------------------------------------

/**
 * Transport interface with close method for WebSocket cleanup.
 */
export interface TransportWithClose extends Transport {
  /** Close the underlying WebSocket connection */
  close: () => void;
}

/**
 * Creates a WebSocket-based Transport for the LSP client.
 *
 * This Transport wraps a WebSocket connection and implements
 * the Transport interface required by @codemirror/lsp-client:
 * - send(msg: string) → ws.send(msg)
 * - subscribe(handler) → adds handler to message list
 * - unsubscribe(handler) → removes handler from list
 * - close() → ws.close()
 *
 * @param wsUrl - The WebSocket URL to connect to
 * @param onClose - Optional callback called when WebSocket closes after connection
 * @returns Promise that resolves to a TransportWithClose once WebSocket connects
 */
export async function createTransport(wsUrl: string, onClose?: () => void): Promise<TransportWithClose> {
  return new Promise<TransportWithClose>((resolve, reject) => {
    const handlers: Set<(value: string) => void> = new Set();

    // Create WebSocket
    const ws = new WebSocket(wsUrl);

    // Set up connection timeout
    const connectionTimeout = setTimeout(() => {
      ws.close();
      reject(new Error(`WebSocket connection timeout after ${WS_CONNECT_TIMEOUT_MS}ms`));
    }, WS_CONNECT_TIMEOUT_MS);

    ws.onopen = () => {
      clearTimeout(connectionTimeout);
      // Resolve with a Transport that uses this WebSocket
      resolve({
        send(message: string): void {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(message);
          } else {
            throw new Error(`WebSocket not open: state ${ws.readyState}`);
          }
        },
        subscribe(handler: (value: string) => void): void {
          handlers.add(handler);
        },
        unsubscribe(handler: (value: string) => void): void {
          handlers.delete(handler);
        },
        close(): void {
          if (ws.readyState === WebSocket.OPEN) {
            ws.close();
          }
        },
      });
    };

    ws.onerror = () => {
      clearTimeout(connectionTimeout);
      reject(new Error(`WebSocket error for ${wsUrl}`));
    };

    ws.onclose = (event) => {
      clearTimeout(connectionTimeout);
      // If handlers are set, the transport was already resolved — notify of close
      if (handlers.size > 0) {
        onClose?.();
      } else {
        reject(new Error(`WebSocket closed before connection: code=${event.code}, reason=${event.reason}`));
      }
    };

    ws.onmessage = (event) => {
      const data = event.data;
      if (typeof data === 'string') {
        // Dispatch to all handlers
        handlers.forEach((handler) => {
          try {
            handler(data);
          } catch (err) {
            console.error('[LSPTransport] Handler error:', err);
          }
        });
      }
    };
  });
}

/**
 * Type for displayFile callback.
 * Called by the custom workspace when navigating to another file
 * (go-to-definition, find references, etc.).
 *
 * Return the EditorView of the opened file, or null if it couldn't be opened.
 * If a view is returned, the LSP client will use it to compute position mappings.
 * If null, the LSP client will show an error but the callback may have still opened
 * the file in the editor.
 */
export type DisplayFileCallback = (filePath: string) => Promise<EditorView | null>;

/**
 * Set the global displayFile callback for LSP cross-file navigation.
 * This should be called once during app initialization, before any LSP
 * clients are created. The callback is invoked by the custom Workspace
 * whenever the LSP client needs to navigate to a different file.
 */
let globalDisplayFileCallback: DisplayFileCallback | null = null;

export function setGlobalDisplayFileCallback(callback: DisplayFileCallback): void {
  globalDisplayFileCallback = callback;
}

export function getGlobalDisplayFileCallback(): DisplayFileCallback | null {
  return globalDisplayFileCallback;
}

/**
 * Convert a file:// URI back to a file path.
 * Used internally by patchWorkspaceDisplayFile.
 */
function privateUriToFilePath(uri: string): string {
  if (uri.startsWith('file://')) {
    const pathPart = uri.replace(/^file:\/?\/+/, '/');
    return decodeURIComponent(pathPart);
  }
  return uri;
}

/**
 * Patch the workspace's displayFile method on an existing LSP client.
 *
 * The default workspace only returns views for already-open files.
 * This override:
 * 1. First tries the default behavior (file already open in some editor)
 * 2. Then checks the EditorView registry for open editors
 * 3. Finally invokes the global callback to open the file (if not already open)
 *
 * This is called after client creation because we can't extend the
 * internal DefaultWorkspace class (it's not exported).
 *
 * @param client - The LSP client to patch
 */
function patchWorkspaceDisplayFile(client: LSPClient): void {
  const workspace = client.workspace;
  const originalDisplayFile = workspace.displayFile.bind(workspace);

  workspace.displayFile = async (uri: string) => {
    // First, try default behavior (file already open in some editor)
    const existingView = await originalDisplayFile(uri);
    if (existingView) return existingView;

    const filePath = privateUriToFilePath(uri);
    if (!filePath) return null;

    // Check if there's already an open editor for this file in our registry
    const existingRegistryView = findEditorView(filePath);
    if (existingRegistryView) return existingRegistryView;

    // File not currently open — invoke the callback to open it
    if (globalDisplayFileCallback) {
      await globalDisplayFileCallback(filePath);
      // After opening, poll briefly for the view to appear in the registry
      for (let i = 0; i < 20; i++) {
        await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
        const view = findEditorView(filePath);
        if (view) return view;
      }
    }

    return null;
  };
}

/**
 * Singleton service for managing LSP client connections.
 *
 * This service:
 * - Maintains a cache of LSP server status (available languages)
 * - Creates WebSocket connections to the backend LSP proxy
 * - Creates and manages LSPClient instances per language
 * - Handles connection pooling (one connection per language)
 */
class LSPClientService {
  private static instance: LSPClientService;

  /** Active LSP clients, keyed by languageId */
  private clients: Map<string, LSPClient> = new Map();

  /** Languages whose clients have been disconnected (to work around connected getter bug) */
  private disconnectedLanguages: Set<string> = new Set();

  /** Close functions for active transports (for WebSocket cleanup) */
  private transportCloseFns: Map<string, () => void> = new Map();

  /** Cached LSP server status */
  private statusCache: Map<string, LSPLanguageInfo> = new Map();

  /** Promise to prevent concurrent status fetches */
  private statusPromise: Promise<void> | null = null;

  /** Pending client creation promises to prevent race conditions */
  private pendingClients: Map<string, Promise<LSPClient | null>> = new Map();

  /** Workspace root path */
  private workspacePath: string = '';

  /** Connection state for each language */
  private clientStates: Map<string, LSPConnectionState> = new Map();

  /** Reconnect timers for each language */
  private reconnectTimers: Map<string, ReturnType<typeof setTimeout>> = new Map();

  /** Reconnect attempt counts */
  private reconnectAttempts: Map<string, number> = new Map();

  /** State change callbacks */
  private stateChangeCallbacks: Set<(languageId: string, state: LSPConnectionState) => void> = new Set();

  /** Private constructor for singleton */
  private constructor() {}

  /**
   * Get the singleton instance.
   */
  static getInstance(): LSPClientService {
    if (!LSPClientService.instance) {
      LSPClientService.instance = new LSPClientService();
    }
    return LSPClientService.instance;
  }

  /**
   * Convenience: get lspClientService instance (matches ApiService pattern).
   * Use this in lspExtensions.ts to access the service.
   */
  static get lspClientService(): LSPClientService {
    return LSPClientService.getInstance();
  }

  /**
   * Get the workspace path from ApiService.
   * Returns empty string if workspace not set.
   */
  async getWorkspacePath(): Promise<string> {
    try {
      const workspace = await ApiService.getInstance().getWorkspace();
      this.workspacePath = workspace.workspace_root;
      return this.workspacePath;
    } catch (err) {
      console.error('[LSPClientService] Failed to get workspace:', err);
      return '';
    }
  }

  /**
   * Synchronous access to cached workspace path.
   * Returns empty if not yet fetched.
   */
  getWorkspacePathSync(): string {
    return this.workspacePath;
  }

  /**
   * Build the WebSocket URL for a given language.
   *
   * URL pattern: ws(s)://host/api/lsp/ws?language={langId}&workspace={encodedPath}
   *
   * @param languageId - The language ID (e.g., 'go', 'typescript')
   * @returns The WebSocket URL
   */
  getWebSocketURL(languageId: string): string {
    const workspacePath = this.getWorkspacePathSync();
    const encodedPath = encodeURIComponent(workspacePath);

    // Determine protocol based on current page
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;

    return `${protocol}//${host}/api/lsp/ws?language=${languageId}&workspace=${encodedPath}`;
  }

  /**
   * Get the LSP status from the backend and cache it.
   *
   * Calls GET /api/lsp/status and populates the status cache.
   *
   * @returns Map of languageId → LSPLanguageInfo
   */
  async getStatus(): Promise<Map<string, LSPLanguageInfo>> {
    // If there's already a pending status fetch, wait for it
    if (this.statusPromise) {
      await this.statusPromise;
      return this.statusCache;
    }

    // Fetch status from backend
    this.statusPromise = this.fetchAndCacheStatus();

    try {
      await this.statusPromise;
    } finally {
      this.statusPromise = null;
    }

    return this.statusCache;
  }

  /**
   * Internal method to fetch and cache status.
   */
  private async fetchAndCacheStatus(): Promise<void> {
    try {
      const response = await clientFetch('/api/lsp/status');
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const data: LSPStatusResponse = await response.json();

      // Clear and rebuild cache
      this.statusCache.clear();

      for (const server of data.servers) {
        for (const lang of server.languages) {
          this.statusCache.set(lang, {
            available: server.available,
            binaryPath: server.binaryPath,
            serverId: server.id,
          });
        }
      }

      console.warn('[LSPClientService] Loaded LSP status for', this.statusCache.size, 'languages');
    } catch (err) {
      console.error('[LSPClientService] Failed to fetch LSP status:', err);
      // Leave cache empty on error - assume no LSP available
    }
  }

  /**
   * Check if LSP is available for a language (from cache).
   *
   * @param languageId - The language ID to check
   * @returns true if LSP is available for this language
   */
  isAvailable(languageId: string): boolean {
    const info = this.statusCache.get(languageId);
    return info?.available ?? false;
  }

  /**
   * Check if LSP is supported for a language.
   * This checks both the backend capability and local support list.
   *
   * @param languageId - The language ID to check
   * @returns true if LSP is supported
   */
  isSupported(languageId: string): boolean {
    return LSP_SUPPORTED_LANGUAGES.has(languageId) && this.isAvailable(languageId);
  }

  /**
   * Get or create an LSP client for a language.
   *
   * If a client already exists for this language and is connected, returns it.
   * Otherwise, creates a new WebSocket connection and LSP client.
   * Handles race conditions by deduplicating concurrent creation attempts.
   *
   * @param languageId - The language ID (e.g., 'go', 'typescript')
   * @returns The LSPClient instance, or null if unavailable
   */
  async getClientForLanguage(languageId: string): Promise<LSPClient | null> {
    // Check if already have a healthy client (and not manually marked as disconnected)
    const existing = this.clients.get(languageId);
    if (existing && existing.connected && !this.disconnectedLanguages.has(languageId)) {
      return existing;
    }

    // Clean up dead client if exists
    if (existing) {
      // Close the transport WebSocket if we have a close function
      const closeFn = this.transportCloseFns.get(languageId);
      if (closeFn) {
        try {
          closeFn();
        } catch {
          /* ignore */
        }
        this.transportCloseFns.delete(languageId);
      }
      try {
        existing.disconnect();
      } catch {
        // Ignore disconnect errors
      }
      this.clients.delete(languageId);
    }

    // Check if there's already a pending client creation for this language
    const pending = this.pendingClients.get(languageId);
    if (pending) {
      return pending;
    }

    // Check if available
    if (!this.isSupported(languageId)) {
      console.warn('[LSPClientService] LSP not available for:', languageId);
      return null;
    }

    // Create the client and track it in pendingClients to handle race conditions
    const promise = this.doCreateClient(languageId);
    this.pendingClients.set(languageId, promise);
    try {
      return await promise;
    } finally {
      this.pendingClients.delete(languageId);
    }
  }

  /**
   * Internal method to create an LSP client for a language.
   * Called by getClientForLanguage after deduplication.
   *
   * @param languageId - The language ID
   * @returns The LSPClient instance, or null if creation failed
   */
  private async doCreateClient(languageId: string): Promise<LSPClient | null> {
    // Set initial state to connecting
    this.setClientState(languageId, 'connecting');

    // Ensure workspace is loaded
    await this.getWorkspacePath();

    try {
      // Create WebSocket transport with onClose callback
      const wsUrl = this.getWebSocketURL(languageId);
      console.warn('[LSPClientService] Connecting to:', wsUrl);

      const transport = await createTransport(wsUrl, () => {
        console.warn('[LSPClientService] Transport closed for:', languageId);
        // Mark as disconnected (to work around connected getter bug)
        this.disconnectedLanguages.add(languageId);
        const client = this.clients.get(languageId);
        if (client) {
          try {
            client.disconnect();
          } catch {
            // Ignore disconnect errors
          }
          this.clients.delete(languageId);
        }
        this.transportCloseFns.delete(languageId);

        // Update state and schedule reconnection
        this.setClientState(languageId, 'disconnected');
        this.scheduleReconnect(languageId);
      });

      // Store the transport close function for cleanup
      this.transportCloseFns.set(languageId, transport.close);

      // Create LSP client
      const config: LSPClientConfig = {
        timeout: DEFAULT_TIMEOUT_MS,
        rootUri: this.getFileURI(this.getWorkspacePathSync()),
        extensions: languageServerExtensions(),
      };

      const client = new LSPClientClass(config);
      client.connect(transport);

      // Wait for initialization
      await client.initializing;

      // Patch the default workspace's displayFile to support cross-file navigation
      // via the app's editor management. The default workspace only handles files
      // already open in an editor; this patch adds support for opening new files.
      if (globalDisplayFileCallback) {
        patchWorkspaceDisplayFile(client);
      }

      // Clear any reconnect timer since we successfully connected
      const existingTimer = this.reconnectTimers.get(languageId);
      if (existingTimer) {
        clearTimeout(existingTimer);
        this.reconnectTimers.delete(languageId);
      }
      this.reconnectAttempts.delete(languageId);

      // Store client
      this.clients.set(languageId, client);

      // Clear the disconnected flag since we successfully created a new client
      this.disconnectedLanguages.delete(languageId);

      // Update state to connected
      this.setClientState(languageId, 'connected');

      console.warn('[LSPClientService] Connected LSP client for:', languageId);
      // Surface the negotiated server capabilities — invaluable when a feature
      // (hover, completion, rename…) silently no-ops because the server didn't
      // advertise support.  Falls back gracefully when older transports don't
      // expose the field.
      const caps = (client as unknown as { serverCapabilities?: unknown }).serverCapabilities;
      if (caps) {
        // eslint-disable-next-line no-console
        console.info('[LSPClientService]', languageId, 'capabilities:', caps);
      }

      return client;
    } catch (err) {
      console.error('[LSPClientService] Failed to create LSP client for', languageId, ':', err);
      this.setClientState(languageId, 'disconnected');
      return null;
    }
  }

  /**
   * Get an existing client without creating a new one.
   *
   * @param languageId - The language ID
   * @returns The LSPClient, or null if not connected
   */
  getClientSync(languageId: string): LSPClient | null {
    return this.clients.get(languageId) ?? null;
  }

  /**
   * Convert a workspace path to a file:// URI.
   *
   * @param filePath - The file path (absolute)
   * @returns file:// URI string
   */
  getFileURI(filePath: string): string {
    if (!filePath) return '';

    // Normalize path: ensure forward slashes, leading slash on Windows
    let normalized = filePath.replace(/\\/g, '/');
    if (!normalized.startsWith('/')) {
      // Windows path - add leading slash
      normalized = '/' + normalized;
    }

    return `file://${normalized}`;
  }

  /**
   * Dispatch sync to an LSP client to push pending document changes.
   *
   * This should be called after document changes to ensure
   * the server has the latest document state.
   *
   * @param languageId - The language ID
   */
  dispatchSyncToClient(languageId: string): void {
    const client = this.clients.get(languageId);
    if (client) {
      try {
        client.sync();
      } catch (err) {
        console.error('[LSPClientService] sync() error:', err);
      }
    }
  }

  /**
   * Disconnect and clean up all LSP clients.
   */
  cleanup(): void {
    console.warn('[LSPClientService] Cleaning up clients');

    // Close all WebSocket connections first
    this.transportCloseFns.forEach((close) => {
      try {
        close();
      } catch {
        /* ignore */
      }
    });
    this.transportCloseFns.clear();

    // Clear all reconnect timers
    this.reconnectTimers.forEach((timer) => clearTimeout(timer));
    this.reconnectTimers.clear();
    this.reconnectAttempts.clear();

    // Disconnect all LSP clients
    this.clients.forEach((client, languageId) => {
      try {
        client.disconnect();
      } catch (err) {
        console.error('[LSPClientService] Error disconnecting client for', languageId, ':', err);
      }
    });

    this.clients.clear();
    this.statusCache.clear();
    this.disconnectedLanguages.clear();
    this.clientStates.clear();
    this.stateChangeCallbacks.clear();
  }

  /**
   * Get the connection state for a language.
   *
   * @param languageId - The language ID
   * @returns The current connection state
   */
  getLSPState(languageId: string): LSPConnectionState {
    return this.clientStates.get(languageId) ?? 'disconnected';
  }

  /**
   * Subscribe to connection state changes.
   *
   * @param callback - Called when a language's connection state changes
   * @returns Unsubscribe function
   */
  onStateChange(callback: (languageId: string, state: LSPConnectionState) => void): () => void {
    this.stateChangeCallbacks.add(callback);
    return () => this.stateChangeCallbacks.delete(callback);
  }

  /**
   * Set the connection state for a language and notify listeners.
   */
  private setClientState(languageId: string, state: LSPConnectionState): void {
    const prev = this.clientStates.get(languageId);
    if (prev === state) return;
    this.clientStates.set(languageId, state);
    this.stateChangeCallbacks.forEach((cb) => {
      try {
        cb(languageId, state);
      } catch (err) {
        console.error('[LSPClientService] state change callback error:', err);
      }
    });
  }

  /**
   * Schedule a reconnection attempt with exponential backoff.
   */
  private scheduleReconnect(languageId: string): void {
    const attempts = (this.reconnectAttempts.get(languageId) ?? 0) + 1;
    this.reconnectAttempts.set(languageId, attempts);

    if (attempts > MAX_RECONNECT_ATTEMPTS) {
      console.warn(
        `[LSPClientService] Max reconnect attempts (${MAX_RECONNECT_ATTEMPTS}) reached for ${languageId}, giving up`,
      );
      this.setClientState(languageId, 'disconnected');
      return;
    }

    const backoff = Math.min(1000 * Math.pow(2, attempts - 1), MAX_RECONNECT_BACKOFF_MS);

    console.warn(`[LSPClientService] Scheduling reconnect for ${languageId} in ${backoff}ms (attempt ${attempts})`);
    this.setClientState(languageId, 'reconnecting');

    const timer = setTimeout(() => {
      this.reconnectTimers.delete(languageId);
      if (this.disconnectedLanguages.has(languageId)) {
        this.disconnectedLanguages.delete(languageId);
        this.getClientForLanguage(languageId).catch((err) => {
          warn('LSP reconnect failed: ' + (err instanceof Error ? err.message : String(err)));
        });
      }
    }, backoff);

    this.reconnectTimers.set(languageId, timer);
  }
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Convert a workspace path to a file:// URI.
 * Exported for use in other modules (e.g., lspExtensions.ts).
 */
export function getFileURI(filePath: string): string {
  if (!filePath) return '';

  // Normalize path: ensure forward slashes, leading slash on Windows
  let normalized = filePath.replace(/\\/g, '/');
  if (!normalized.startsWith('/')) {
    // Windows path - add leading slash
    normalized = '/' + normalized;
  }

  return `file://${normalized}`;
}

/**
 * Convert a file:// URI back to a file path.
 * Exported for use in other modules (e.g., lspExtensions.ts).
 */
export function uriToFilePath(uri: string): string {
  if (uri.startsWith('file://')) {
    const pathPart = uri.replace(/^file:\/?\/+/, '/');
    return decodeURIComponent(pathPart);
  }
  return uri;
}

/**
 * Get the LSP client service singleton.
 */
export function getLSPClientService(): LSPClientService {
  return LSPClientService.getInstance();
}

/**
 * Get LSPClientService class instance.
 */
export { LSPClientService };

/**
 * @deprecated Use getLSPClientService() instead.
 */
export { getLSPClientService as getInstance };

/**
 * @deprecated Use getLSPClientService() instead.
 */
export { getLSPClientService as getLSPCLientService };
