/**
 * LSP Client Service
 *
 * Manages WebSocket connections to the backend LSP proxy server.
 * Provides a singleton service for creating and managing LSP clients per language.
 */

import { ApiService } from './api';

// Types from @codemirror/lsp-client
import type { Transport, LSPClientConfig, LSPClient } from '@codemirror/lsp-client';
import {
  LSPClient as LSPClientClass,
  languageServerExtensions,
} from '@codemirror/lsp-client';

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
]);

/** Default timeout for LSP client config (10 seconds). */
const DEFAULT_TIMEOUT_MS = 10_000;

/** WebSocket connection timeout (30 seconds). */
const WS_CONNECT_TIMEOUT_MS = 30_000;

// ---------------------------------------------------------------------------
// Transport Implementation
// ---------------------------------------------------------------------------

/**
 * Creates a WebSocket-based Transport for the LSP client.
 *
 * This Transport wraps a WebSocket connection and implements
 * the Transport interface required by @codemirror/lsp-client:
 * - send(msg: string) → ws.send(msg)
 * - subscribe(handler) → adds handler to message list
 * - unsubscribe(handler) → removes handler from list
 *
 * @param wsUrl - The WebSocket URL to connect to
 * @param onClose - Optional callback called when WebSocket closes after connection
 * @returns Promise that resolves to a Transport once WebSocket connects
 */
export async function createTransport(
  wsUrl: string,
  onClose?: () => void,
): Promise<Transport> {
  return new Promise<Transport>((resolve, reject) => {
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

// ---------------------------------------------------------------------------
// LSP Client Service
// ---------------------------------------------------------------------------

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

  /** Cached LSP server status */
  private statusCache: Map<string, LSPLanguageInfo> = new Map();

  /** Promise to prevent concurrent status fetches */
  private statusPromise: Promise<void> | null = null;

  /** Pending client creation promises to prevent race conditions */
  private pendingClients: Map<string, Promise<LSPClient | null>> = new Map();

  /** Workspace root path */
  private workspacePath: string = '';

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
      const response = await fetch('/api/lsp/status');
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

      console.log('[LSPClientService] Loaded LSP status for', this.statusCache.size, 'languages');
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
    // Check if already have a healthy client
    const existing = this.clients.get(languageId);
    if (existing && existing.connected) {
      return existing;
    }

    // Clean up dead client if exists
    if (existing) {
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
      console.log('[LSPClientService] LSP not available for:', languageId);
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
    // Ensure workspace is loaded
    await this.getWorkspacePath();

    try {
      // Create WebSocket transport with onClose callback
      const wsUrl = this.getWebSocketURL(languageId);
      console.log('[LSPClientService] Connecting to:', wsUrl);

      const transport = await createTransport(wsUrl, () => {
        console.log('[LSPClientService] Transport closed for:', languageId);
        const client = this.clients.get(languageId);
        if (client) {
          try {
            client.disconnect();
          } catch {
            // Ignore disconnect errors
          }
          this.clients.delete(languageId);
        }
      });

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

      // Store client
      this.clients.set(languageId, client);

      console.log('[LSPClientService] Connected LSP client for:', languageId);

      return client;
    } catch (err) {
      console.error('[LSPClientService] Failed to create LSP client for', languageId, ':', err);
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
    console.log('[LSPClientService] Cleaning up clients');

    this.clients.forEach((client, languageId) => {
      try {
        client.disconnect();
      } catch (err) {
        console.error('[LSPClientService] Error disconnecting client for', languageId, ':', err);
      }
    });

    this.clients.clear();
    this.statusCache.clear();
  }
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

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