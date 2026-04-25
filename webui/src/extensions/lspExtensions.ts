/**
 * LSP Extensions for CodeMirror
 *
 * Creates CodeMirror extensions from LSP client instances.
 * These extensions provide IDE-like features: completions, hover,
 * diagnostics, signature help, and keyboard shortcuts.
 */

import type { EditorView, ViewUpdate } from '@codemirror/view';
import { ViewPlugin } from '@codemirror/view';
import { StateEffect, StateField } from '@codemirror/state';
import type { Extension } from '@codemirror/state';
import type { LSPClient } from '@codemirror/lsp-client';

import { getLSPClientService, LSPClientService } from '../services/lspClientService';

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

/**
 * Convert a file path to a file:// URI.
 */
function filePathToURI(filePath: string): string {
  if (!filePath) return '';
  let normalized = filePath.replace(/\\/g, '/');
  if (!normalized.startsWith('/')) {
    normalized = '/' + normalized;
  }
  return `file://${normalized}`;
}

/**
 * Check if LSP is available for a language.
 */
function isLSPAvailable(languageId: string): boolean {
  return LSPClientService.lspClientService.isSupported(languageId);
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
export function buildLSPPluginExtensions(
  client: LSPClient | null,
  filePath: string,
  languageId: string,
): Extension[] {
  if (!client) return [];
  const fileURI = filePathToURI(filePath);
  return [client.plugin(fileURI, languageId)];
}

// ---------------------------------------------------------------------------
// Document Sync Extension (StateField + ViewPlugin)
// ---------------------------------------------------------------------------

/**
 * Effect to set the language ID in the document state.
 */
const setLanguageId = StateEffect.define<string>();

/**
 * StateField to store the languageId for LSP sync.
 */
const lspLanguageIdField = StateField.define<string>({
  create(): string {
    return '';
  },
  update(value, tr): string {
    for (const effect of tr.effects) {
      if (effect.is(setLanguageId)) {
        return effect.value;
      }
    }
    return value;
  },
});

/**
 * ViewPlugin that calls sync() on the LSP client on a debounced timer.
 *
 * This is critical because lsp-client does NOT auto-sync for diagnostics.
 */
function createLPSyncPlugin(languageId: string) {
  return ViewPlugin.fromClass(
    class {
      syncTimeout: ReturnType<typeof setTimeout> | null = null;

      constructor(view: EditorView) {
        view.dispatch({
          effects: setLanguageId.of(languageId),
        });
      }

      update(update: ViewUpdate): void {
        if (!update.docChanged) return;

        if (this.syncTimeout) {
          clearTimeout(this.syncTimeout);
        }

        this.syncTimeout = setTimeout(() => {
          const langId = update.view.state.field(lspLanguageIdField, false);
          if (langId) {
            LSPClientService.lspClientService.dispatchSyncToClient(langId);
          }
        }, 500);
      }

      destroy(): void {
        if (this.syncTimeout) {
          clearTimeout(this.syncTimeout);
        }
      }
    },
  );
}

/**
 * Create the LSP sync extension for document changes.
 */
export function lspSyncOnDocChange(languageId: string): Extension[] {
  if (!isLSPAvailable(languageId)) return [];
  return [lspLanguageIdField, createLPSyncPlugin(languageId)];
}

// ---------------------------------------------------------------------------
// Re-export from lspClientService
// ---------------------------------------------------------------------------

export {
  getLSPClientService,
  LSPClientService,
  createTransport,
  getInstance,
  LSP_SUPPORTED_LANGUAGES,
  type LSPLanguageInfo,
  type LSPStatusResponse,
} from '../services/lspClientService';