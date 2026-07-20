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

import { indentUnit } from '@codemirror/language';
import { EditorState } from '@codemirror/state';
import type { Compartment, Extension } from '@codemirror/state';
import { EditorView as CMEditorView, lineNumbers } from '@codemirror/view';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { useEffect, useRef } from 'react';
import { inlayHintsExtension } from '../extensions/inlayHints';
import { resolveLanguageId, getLanguageExtensions } from '../extensions/languageRegistry';
import { buildLSPPluginExtensions, lspSyncOnDocChange } from '../extensions/lspExtensions';
import { minimapExtension } from '../extensions/minimap';
import { signatureHelpExtension } from '../extensions/signatureHelp';
import { setSnippetLanguage } from '../extensions/snippets';
import { whitespaceRenderingPlugin, type WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { getLSPClientService, LSP_SUPPORTED_LANGUAGES } from '../services/lspClientService';
import type { EditorBuffer } from '../types/editor';
import { debugLog } from '../utils/log';

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
  keymapsRef: React.MutableRefObject<{ customKeymap: Extension } | null>;
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

  // Monotonic token that invalidates stale async LSP client resolutions.
  // Each language reconfiguration increments it; the async closure captures
  // its token and bails out if it no longer matches. This prevents a slow
  // request for an old language from installing LSP extensions for the
  // wrong language after a language-override change (which reuses the same
  // EditorView, so viewRef.current === view passes but the language is stale).
  const lspConfigTokenRef = useRef(0);

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
      effects: [compartments.language.reconfigure(getLanguageExtensions(languageId)), compartments.lsp.reconfigure([])],
    });

    const lspService = getLSPClientService();
    const filePath = buffer.file?.path ?? '';
    const token = ++lspConfigTokenRef.current;

    if (languageId && LSP_SUPPORTED_LANGUAGES.has(languageId)) {
      void (async () => {
        try {
          const client = await lspService.getClientForLanguage(languageId);
          if (client && viewRef.current === view && view.dom?.isConnected && token === lspConfigTokenRef.current) {
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
      effects: compartments.hotkeys.reconfigure(keymapsRef.current?.customKeymap ?? []),
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
      effects: compartments.whitespaceRendering.reconfigure(whitespaceRenderingPlugin(whitespaceRenderingMode)),
    });
  }, [whitespaceRenderingMode]);

  // ---------------------------------------------------------------------------
  // Compartment reconfiguration for settings changes
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    // Batch all compartment reconfigurations into a single dispatch to avoid
    // multiple unnecessary editor re-renders.
    view.dispatch({
      effects: [
        // Font size
        compartments.fontSize.reconfigure([CMEditorView.theme({ '&': { fontSize: `${editorFontSize}px` } })]),
        // Tab size
        compartments.tabSize.reconfigure([
          EditorState.tabSize.of(editorTabSize === 0 ? 4 : editorTabSize),
          indentUnit.of(editorUsesTabs ? '\t' : ' '.repeat(editorTabSize === 0 ? 4 : editorTabSize)),
        ]),
        // Word wrap
        compartments.lineWrapping.reconfigure(wordWrapEnabled ? CMEditorView.lineWrapping : []),
        // Minimap
        compartments.minimap.reconfigure(minimapEnabled ? minimapExtension() : []),
        // Relative line numbers
        compartments.relativeLineNumbers.reconfigure(relativeLineNumbersEnabled ? lineNumbersRelative : lineNumbers()),
      ],
    });
  }, [editorFontSize, editorTabSize, editorUsesTabs, wordWrapEnabled, minimapEnabled, relativeLineNumbersEnabled]);

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
          resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name)
            .languageId,
        )
      : [];

    view.dispatch({
      effects: compartments.inlayHints.reconfigure(ext),
    });
  }, [
    inlayHintsEnabled,
    buffer?.id,
    buffer?.file?.path,
    buffer?.languageOverride,
    buffer?.file?.ext,
    buffer?.file?.name,
  ]);

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
          resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name)
            .languageId,
        )
      : [];

    view.dispatch({
      effects: compartments.signatureHelp.reconfigure(ext),
    });
  }, [
    signatureHelpEnabled,
    buffer?.id,
    buffer?.file?.path,
    buffer?.languageOverride,
    buffer?.file?.ext,
    buffer?.file?.name,
  ]);
}
