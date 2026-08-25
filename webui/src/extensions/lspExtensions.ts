/**
 * LSP Extensions for CodeMirror
 *
 * Creates CodeMirror extensions from LSP client instances.
 * These extensions provide IDE-like features: completions, hover,
 * diagnostics, signature help, and keyboard shortcuts.
 */

import type { LSPClient } from '@codemirror/lsp-client';
import type { Extension } from '@codemirror/state';
import { LSPClientService, getFileURI } from '../services/lspClientService';

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

// getFileURI and uriToFilePath are now imported from lspClientService

// ---------------------------------------------------------------------------
// Check if LSP client is connected and ready
// ---------------------------------------------------------------------------

/**
 * Check if an LSP client is connected and healthy for a given language.
 *
 * @param languageId - The language ID to check
 * @returns true if LSP client is active for this language
 */
export function isLSPClientConnected(languageId: string): boolean {
  const client = LSPClientService.lspClientService.getClientSync(languageId);
  return client?.connected ?? false;
}

/**
 * Synchronously get an existing LSP client without creating one.
 *
 * @param languageId - The language ID
 * @returns The LSPClient instance, or null if not connected
 */
export function getClientForLanguageSync(languageId: string): LSPClient | null {
  return LSPClientService.lspClientService.getClientSync(languageId);
}

// ---------------------------------------------------------------------------
// Plugin Extensions (Core LSP Functionality)
// ---------------------------------------------------------------------------

/**
 * Build the full LSP plugin extensions from an existing client.
 *
 * This is the main entry point used by EditorPane after the client
 * is connected. It returns all the bundled LSP extensions.
 */
export function buildLSPPluginExtensions(client: LSPClient | null, filePath: string, languageId: string): Extension[] {
  if (!client) return [];
  const fileURI = getFileURI(filePath);
  return [client.plugin(fileURI, languageId)];
}

// ---------------------------------------------------------------------------
// Re-export from lspClientService
// ---------------------------------------------------------------------------

export {
  getLSPClientService,
  LSPClientService,
  createTransport,
  LSP_SUPPORTED_LANGUAGES,
  type LSPLanguageInfo,
  type LSPStatusResponse,
  setGlobalDisplayFileCallback,
  getGlobalDisplayFileCallback,
  type DisplayFileCallback,
  getFileURI,
  uriToFilePath,
  registerEditorView,
  unregisterEditorView,
  findEditorView,
} from '../services/lspClientService';
