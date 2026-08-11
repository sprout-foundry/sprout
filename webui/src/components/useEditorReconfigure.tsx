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
import { LSPPlugin } from '@codemirror/lsp-client';
import { EditorState } from '@codemirror/state';
import type { Compartment, Extension } from '@codemirror/state';
import { EditorView as CMEditorView, lineNumbers } from '@codemirror/view';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { useEffect, useRef } from 'react';
import { aiCompletionsExtension } from '../extensions/aiCompletions';
import { inlayHintsExtension } from '../extensions/inlayHints';
import { resolveLanguageId, getLanguageExtensions } from '../extensions/languageRegistry';
import { buildLSPPluginExtensions } from '../extensions/lspExtensions';
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
    aiCompletions: Compartment;
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
  aiCompletionsEnabled: boolean;
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
    aiCompletionsEnabled,
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
              effects: compartments.lsp.reconfigure(buildLSPPluginExtensions(client, filePath, languageId)),
            });
          }
        } catch (err) {
          debugLog('[useEditorReconfigure] Failed to reconfigure LSP:', err);
        }
      })();
    }
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  // ---------------------------------------------------------------------------
  // LSP reconnect re-wire
  // ---------------------------------------------------------------------------
  // The editor keeps the plugin of the client it was wired with. When the
  // LSP WebSocket drops (idle keepalive timeout, network blip) the service
  // creates a fresh client, but the old plugin's transport is dead — LSP
  // features (diagnostics, references, hover) silently stop until the buffer
  // changes. Subscribe to connection-state changes and re-install the
  // compartment with the new client when one appears.

  useEffect(() => {
    const view = viewRef.current;
    if (!view || !buffer?.file?.path) return;

    const languageId = resolveLanguageId(
      buffer.languageOverride,
      buffer.file?.ext?.replace(/^\./, ''),
      buffer.file?.name,
    ).languageId;
    if (!languageId || !LSP_SUPPORTED_LANGUAGES.has(languageId)) return;

    const filePath = buffer.file.path;
    const lspService = getLSPClientService();

    const unsubscribe = lspService.onStateChange((langId, state) => {
      if (langId !== languageId || state !== 'connected') return;
      void (async () => {
        try {
          const client = await lspService.getClientForLanguage(languageId);
          if (!client || viewRef.current !== view || !view.dom?.isConnected) return;
          // Skip if this view is already wired to this exact client (initial
          // connect races bootstrapLSP / the language-reconfigure effect).
          const current = LSPPlugin.get(view);
          if (current?.client === client) return;
          view.dispatch({
            effects: compartments.lsp.reconfigure(buildLSPPluginExtensions(client, filePath, languageId)),
          });
          debugLog('[useEditorReconfigure] Re-wired LSP plugin for', languageId);
        } catch (err) {
          debugLog('[useEditorReconfigure] Failed to re-wire LSP:', err);
        }
      })();
    });

    return unsubscribe;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [buffer?.id, buffer?.file?.path, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

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

  // ---------------------------------------------------------------------------
  // AI completions compartment sync
  // ---------------------------------------------------------------------------

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    const ext = aiCompletionsEnabled
      ? aiCompletionsExtension(
          () => buffer?.file?.path,
          resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name)
            .languageId,
        )
      : [];

    view.dispatch({
      effects: compartments.aiCompletions.reconfigure(ext),
    });
  }, [
    aiCompletionsEnabled,
    buffer?.id,
    buffer?.file?.path,
    buffer?.languageOverride,
    buffer?.file?.ext,
    buffer?.file?.name,
  ]);
}
