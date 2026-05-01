/**
 * useEditorViewInit — manages CodeMirror EditorView lifecycle.
 *
 * Extracts the main editor initialization effect from EditorPane:
 * - EditorView creation
 * - Extension building and configuration
 * - LSP initialization
 * - Cleanup and registration
 *
 * Target: ~250 lines
 */

import { useEffect, useRef } from 'react';
import { EditorView as CMEditorView, keymap } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { searchKeymap } from '@codemirror/search';

import type { EditorBuffer } from '../types/editor';
import type { EditorSettingsCompartments } from './useEditorSettings';
import type { UseEditorExtensionsReturn } from './useEditorExtensions';
import type { KeymapActions } from './useEditorKeymaps';
import { resolveLanguageId } from '../extensions/languageRegistry';
import { buildLSPPluginExtensions, lspSyncOnDocChange, setGlobalDisplayFileCallback, type DisplayFileCallback, registerEditorView, unregisterEditorView } from '../extensions/lspExtensions';
import { getLSPClientService, LSP_SUPPORTED_LANGUAGES } from '../services/lspClientService';
import { debugLog } from '../utils/log';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseEditorViewInitOptions {
  paneId: string;
  editorRef: React.RefObject<HTMLDivElement | null>;
  viewRef: React.MutableRefObject<CMEditorView | null>;
  buffer: EditorBuffer | null | undefined;
  localContent: string;
  buildExtensions: UseEditorExtensionsReturn['buildExtensions'];
  compartments: UseEditorExtensionsReturn['compartments'];
  themePack: any;
  customHighlightStyle: any;
  lastInitLanguageKey: React.MutableRefObject<string | null>;
  keymaps: {
    customKeymap: any;
    replacePanelKeymap: any[];
    zoomKeymap: any[];
    semanticKeymap: any[];
  };
  localContentRef: React.MutableRefObject<string>;
  openWorkspaceBuffer: (buffer: any) => void;
  onCancelPendingFlush: () => void;
  onUpdate?: (update: any) => void;
  settings: {
    wordWrapEnabled: boolean;
    relativeLineNumbersEnabled: boolean;
    minimapEnabled: boolean;
    editorFontSize: number;
    editorTabSize: number;
    editorUsesTabs: boolean;
    whitespaceRenderingMode: WhitespaceRenderingMode;
  };
  actions: {
    getSaveFn: () => () => void;
  };
}

export interface UseEditorViewInitReturn {
  lastInitLanguageKey: React.MutableRefObject<string | null>;
  lastInitLanguageKeyRef: React.MutableRefObject<string | null>;
}

let globalDisplayFileRegistered = false;

/**
 * Hook that manages CodeMirror EditorView lifecycle.
 *
 * @param options - Configuration options
 * @returns Ref tracking last initialized language key
 */
export function useEditorViewInit(options: UseEditorViewInitOptions): void {
  const {
    paneId,
    editorRef,
    viewRef,
    buffer,
    localContent,
    compartments,
    buildExtensions,
    themePack,
    customHighlightStyle,
    lastInitLanguageKey,
    keymaps,
    localContentRef,
    openWorkspaceBuffer,
    onCancelPendingFlush,
    onUpdate,
    settings,
    actions,
  } = options;

  useEffect(() => {
    if (!editorRef.current) return;

    const resolvedLanguage = resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name);

    // Create update listener if onUpdate callback provided
    const updateListener = onUpdate ? CMEditorView.updateListener.of((update: any) => {
      onUpdate(update);
    }) : null;

    const extensions = buildExtensions({
      paneId,
      settings,
      theme: { themePack, customHighlightStyle },
      buffer: {
        languageId: resolvedLanguage.languageId,
        getFilePath: () => buffer?.file?.path,
        getFileExt: () => buffer?.file?.ext,
        getContent: () => localContentRef.current,
      },
      actions,
      hotkeysCompartmentExtension: compartments.hotkeys.of(keymaps.customKeymap),
      extraKeymaps: [
        keymap.of(searchKeymap),
        keymap.of(keymaps.replacePanelKeymap),
        keymap.of(keymaps.zoomKeymap),
        keymap.of(keymaps.semanticKeymap),
        ...(updateListener ? [updateListener] : []),
      ],
    });

    const state = EditorState.create({
      doc: localContent,
      extensions,
    });

    const view = new CMEditorView({
      state,
      parent: editorRef.current,
    });

    viewRef.current = view;

    const filePath = buffer?.file?.path;
    if (filePath && !filePath.startsWith('__workspace/')) {
      registerEditorView(filePath, view);
    }

    if (resolvedLanguage.languageId && LSP_SUPPORTED_LANGUAGES.has(resolvedLanguage.languageId)) {
      const currentLangId = resolvedLanguage.languageId;
      const currentFilePath = buffer?.file?.path ?? '';
      const capturedView = view;

      if (!globalDisplayFileRegistered) {
        globalDisplayFileRegistered = true;
        const displayFileCb: DisplayFileCallback = async (filePath: string) => {
          const fileName = filePath.split('/').pop() || filePath;
          const dotIndex = fileName.lastIndexOf('.');
          const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

          openWorkspaceBuffer({
            kind: 'file',
            path: filePath,
            title: fileName,
            ext,
          });

          return null;
        };
        setGlobalDisplayFileCallback(displayFileCb);
      }

      void (async () => {
        try {
          const lspService = getLSPClientService();
          await lspService.getStatus();
          const client = await lspService.getClientForLanguage(currentLangId);
          if (client && viewRef.current === capturedView && capturedView.dom?.isConnected) {
            const lspExtensions = [
              ...buildLSPPluginExtensions(client, currentFilePath, currentLangId),
              ...lspSyncOnDocChange(currentLangId),
            ];
            capturedView.dispatch({
              effects: compartments.lsp.reconfigure(lspExtensions),
            });
          }
        } catch (err) {
          debugLog('[useEditorViewInit] Failed to initialize LSP:', err);
        }
      })();
    }

    lastInitLanguageKey.current = `${buffer?.id}:${buffer?.languageOverride ?? ''}:${buffer?.file?.ext ?? ''}:${buffer?.file?.name ?? ''}`;

    const cleanupFilePath = buffer?.file?.path;

    return () => {
      onCancelPendingFlush();
      if (cleanupFilePath && !cleanupFilePath.startsWith('__workspace/')) {
        unregisterEditorView(cleanupFilePath);
      }
      view.destroy();
      viewRef.current = null;
    };
  }, [
    paneId,
    buffer?.id,
    buffer?.file?.ext,
    buffer?.file?.name,
    editorRef,
    viewRef,
    localContent,
    compartments,
    themePack,
    customHighlightStyle,
    keymaps,
    localContentRef,
    openWorkspaceBuffer,
    onCancelPendingFlush,
    onUpdate,
    settings,
    actions,
  ]);
}
