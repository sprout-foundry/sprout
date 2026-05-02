/**
 * useEditorReconfigure — manages CodeMirror compartment reconfiguration.
 *
 * Extracts all compartment reconfiguration effects from EditorPane:
 * - Language reconfiguration (when buffer.languageOverride or file.ext changes)
 * - Hotkey compartment reconfiguration (when hotkeys changes)
 * - Snippet language sync
 * - Compartment reconfiguration (font size, tab size, word wrap, minimap, relative line numbers)
 * - Whitespace rendering compartment sync
 *
 * Target: ~250 lines
 */

import { useEffect } from 'react';
import { EditorView as CMEditorView } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { lineNumbers } from '@codemirror/view';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { indentUnit } from '@codemirror/language';

import { resolveLanguageId } from '../extensions/languageRegistry';
import { setSnippetLanguage } from '../extensions/snippets';
import { whitespaceRenderingPlugin, type WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { buildLSPPluginExtensions, lspSyncOnDocChange } from '../extensions/lspExtensions';
import { getLSPClientService, LSP_SUPPORTED_LANGUAGES } from '../services/lspClientService';
import { getLanguageExtensions } from '../extensions/languageRegistry';
import { minimapExtension } from '../extensions/minimap';
import { inlayHintsExtension } from '../extensions/inlayHints';
import { signatureHelpExtension } from '../extensions/signatureHelp';
import { debugLog } from '../utils/log';
import type { Compartment } from '@codemirror/state';
import type { Extension } from '@codemirror/state';
import type { EditorBuffer } from '../types/editor';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseEditorReconfigureOptions {
  viewRef: React.MutableRefObject<CMEditorView | null>;
  buffer: EditorBuffer | null | undefined;
  lastInitLanguageKey: React.MutableRefObject<string | null>;
  compartments: {
    language: Compartment;
    lsp: Compartment;
    hotkeys: Compartment;
    whitespaceRendering: Compartment;
    fontSize: Compartment;
    tabSize: Compartment;
    lineWrapping: Compartment;
    minimap: Compartment;
    relativeLineNumbers: Compartment;
    inlayHints: Compartment;
    signatureHelp: Compartment;
  };
  hotkeys: unknown;
  keymapsRef: React.MutableRefObject<{ customKeymap: Extension }>;
  editorFontSize: number;
  editorTabSize: number;
  editorUsesTabs: boolean;
  wordWrapEnabled: boolean;
  minimapEnabled: boolean;
  relativeLineNumbersEnabled: boolean;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  inlayHintsEnabled: boolean;
  signatureHelpEnabled: boolean;
}

/**
 * Hook that manages all compartment reconfiguration effects.
 *
 * @param options - Configuration options with refs and compartments
 */
export function useEditorReconfigure(options: UseEditorReconfigureOptions): void {
  const {
    viewRef,
    buffer,
    lastInitLanguageKey,
    compartments,
    hotkeys,
    keymapsRef,
    editorFontSize,
    editorTabSize,
    editorUsesTabs,
    wordWrapEnabled,
    minimapEnabled,
    relativeLineNumbersEnabled,
    whitespaceRenderingMode,
    inlayHintsEnabled,
    signatureHelpEnabled,
  } = options;

  // ---------------------------------------------------------------------------
  // Language reconfiguration
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view || !buffer) return;

    const key = `${buffer.id}:${buffer.languageOverride ?? ''}:${buffer.file?.ext ?? ''}:${buffer.file?.name ?? ''}`;
    if (key === lastInitLanguageKey.current) return;
    lastInitLanguageKey.current = key;

    const { languageId } = resolveLanguageId(
      buffer.languageOverride,
      buffer.file?.ext?.replace(/^\./, ''),
      buffer.file?.name,
    );

    view.dispatch({
      effects: [
        compartments.language.reconfigure(getLanguageExtensions(languageId)),
        compartments.lsp.reconfigure([]),
      ],
    });

    const lspService = getLSPClientService();
    const filePath = buffer.file?.path ?? '';

    if (languageId && LSP_SUPPORTED_LANGUAGES.has(languageId)) {
      void (async () => {
        try {
          const client = await lspService.getClientForLanguage(languageId);
          if (client && viewRef.current === view && view.dom?.isConnected) {
            view.dispatch({
              effects: compartments.lsp.reconfigure([
                ...buildLSPPluginExtensions(client, filePath, languageId),
                ...lspSyncOnDocChange(languageId),
              ]),
            });
          }
        } catch (err) {
          debugLog('[useEditorReconfigure] Failed to reconfigure LSP:', err);
        }
      })();
    }
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  // ---------------------------------------------------------------------------
  // Hotkey compartment reconfiguration
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    view.dispatch({
      effects: compartments.hotkeys.reconfigure(
        keymapsRef.current.customKeymap,
      ),
    });
  }, [hotkeys, keymapsRef]);

  // ---------------------------------------------------------------------------
  // Snippet language sync
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    if (buffer?.file) {
      const { languageId } = resolveLanguageId(
        buffer.languageOverride,
        buffer.file.ext?.replace(/^\./, ''),
        buffer.file.name,
      );
      setSnippetLanguage(view, languageId);
    } else {
      setSnippetLanguage(view, null);
    }
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  // ---------------------------------------------------------------------------
  // Whitespace rendering compartment sync
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    view.dispatch({
      effects: compartments.whitespaceRendering.reconfigure(
        whitespaceRenderingPlugin(whitespaceRenderingMode),
      ),
    });
  }, [whitespaceRenderingMode]);

  // ---------------------------------------------------------------------------
  // Compartment reconfiguration for settings changes
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    // Font size
    view.dispatch({
      effects: compartments.fontSize.reconfigure([
        CMEditorView.theme({ '&': { fontSize: `${editorFontSize}px` } }),
      ]),
    });

    // Tab size
    view.dispatch({
      effects: compartments.tabSize.reconfigure([
        EditorState.tabSize.of(editorTabSize === 0 ? 4 : editorTabSize),
        indentUnit.of(editorUsesTabs ? '\t' : ' '.repeat(editorTabSize === 0 ? 4 : editorTabSize)),
      ]),
    });

    // Word wrap
    view.dispatch({
      effects: compartments.lineWrapping.reconfigure(wordWrapEnabled ? CMEditorView.lineWrapping : []),
    });

    // Minimap
    view.dispatch({
      effects: compartments.minimap.reconfigure(minimapEnabled ? minimapExtension() : []),
    });

    // Relative line numbers
    view.dispatch({
      effects: compartments.relativeLineNumbers.reconfigure(
        relativeLineNumbersEnabled ? lineNumbersRelative : lineNumbers(),
      ),
    });
  }, [
    editorFontSize,
    editorTabSize,
    editorUsesTabs,
    wordWrapEnabled,
    minimapEnabled,
    relativeLineNumbersEnabled,
  ]);

  // ---------------------------------------------------------------------------
  // Inlay hints compartment sync
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    const ext = inlayHintsEnabled
      ? inlayHintsExtension(
          () => buffer?.file?.path,
          () => view.state.doc.toString(),
          resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name).languageId,
        )
      : [];

    view.dispatch({
      effects: compartments.inlayHints.reconfigure(ext),
    });
  }, [inlayHintsEnabled, buffer?.id, buffer?.file?.path, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  // ---------------------------------------------------------------------------
  // Signature help compartment sync
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    const ext = signatureHelpEnabled
      ? signatureHelpExtension(
          () => buffer?.file?.path,
          () => view.state.doc.toString(),
          resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name).languageId,
        )
      : [];

    view.dispatch({
      effects: compartments.signatureHelp.reconfigure(ext),
    });
  }, [signatureHelpEnabled, buffer?.id, buffer?.file?.path, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);
}
