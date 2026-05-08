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
import { EditorView as CMEditorView, keymap, type ViewUpdate, type KeyBinding } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { searchKeymap } from '@codemirror/search';
import type { HighlightStyle } from '@codemirror/language';

import type { EditorBuffer } from '../types/editor';
import type { EditorSettingsCompartments } from './useEditorSettings';
import type { UseEditorExtensionsReturn } from './useEditorExtensions';
import type { KeymapActions } from './useEditorKeymaps';
import { resolveLanguageId } from '../extensions/languageRegistry';
import { buildLSPPluginExtensions, lspSyncOnDocChange, setGlobalDisplayFileCallback, type DisplayFileCallback, registerEditorView, unregisterEditorView } from '../extensions/lspExtensions';
import { getLSPClientService, LSP_SUPPORTED_LANGUAGES } from '../services/lspClientService';
import { debugLog } from '../utils/log';
import type { Extension } from '@codemirror/state';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import type { ThemePack } from '../themes/themePacks';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface EditorViewInitSettings {
  wordWrapEnabled: boolean;
  relativeLineNumbersEnabled: boolean;
  minimapEnabled: boolean;
  editorFontSize: number;
  editorTabSize: number;
  editorUsesTabs: boolean;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  inlayHintsEnabled: boolean;
  signatureHelpEnabled: boolean;
}

export interface EditorViewInitKeymaps {
  customKeymap: Extension;
  replacePanelKeymap: readonly KeyBinding[];
  zoomKeymap: readonly KeyBinding[];
  semanticKeymap: readonly KeyBinding[];
}

export interface EditorViewInitActions {
  getSaveFn: () => () => void;
}

export interface UseEditorViewInitOptions {
  paneId: string;
  editorRef: React.RefObject<HTMLDivElement | null>;
  viewRef: React.MutableRefObject<CMEditorView | null>;
  buffer: EditorBuffer | null | undefined;
  /** @deprecated Use localContentRef instead — reading localContent in the init effect triggers EditorView recreation on every keystroke */
  localContent: string;
  buildExtensions: UseEditorExtensionsReturn['buildExtensions'];
  compartments: UseEditorExtensionsReturn['compartments'];
  themePack: ThemePack;
  customHighlightStyle: HighlightStyle | null;
  lastInitLanguageKey: React.MutableRefObject<string | null>;
  /** Ref to keymaps — stable identity avoids EditorView recreation */
  keymapsRef: React.MutableRefObject<EditorViewInitKeymaps>;
  localContentRef: React.MutableRefObject<string>;
  openWorkspaceBuffer: (buffer: { kind: 'file' | 'chat' | 'diff' | 'review' | 'compare'; path: string; title: string; ext?: string }) => void;
  onCancelPendingFlush: () => void;
  /** Ref to the update handler — stable identity avoids EditorView recreation on every keystroke */
  onUpdateRef: React.MutableRefObject<(update: ViewUpdate) => void>;
  /** Ref to settings — stable identity avoids EditorView recreation */
  settingsRef: React.MutableRefObject<EditorViewInitSettings>;
  /** Ref to actions — stable identity avoids EditorView recreation */
  actionsRef: React.MutableRefObject<EditorViewInitActions>;
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
    compartments,
    buildExtensions,
    themePack,
    customHighlightStyle,
    lastInitLanguageKey,
    keymapsRef,
    localContentRef,
    openWorkspaceBuffer,
    onCancelPendingFlush,
    onUpdateRef,
    settingsRef,
    actionsRef,
  } = options;

  // Ref for openWorkspaceBuffer to avoid it being a dependency that causes
  // EditorView recreation when other panes switch buffers.
  const openWorkspaceBufferRef = useRef(openWorkspaceBuffer);
  openWorkspaceBufferRef.current = openWorkspaceBuffer;

  useEffect(() => {
    if (!editorRef.current) return;

    const resolvedLanguage = resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name);

    // Read current values from refs to avoid stale closure issues.
    // These are read inside the effect (not captured by dependency array)
    // so the EditorView is only recreated when identity-based deps change.
    const settings = settingsRef.current;
    const keymaps = keymapsRef.current;
    const actions = actionsRef.current;

    // Create update listener reading from stable ref — avoids EditorView
    // recreation when the callback identity changes on every keystroke.
    const updateListener = onUpdateRef.current
      ? CMEditorView.updateListener.of((update: ViewUpdate) => {
          onUpdateRef.current(update);
        })
      : null;

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

    // Use the ref to read the current content at the time the effect runs.
    // Do NOT add localContent to the dependency array below — doing so causes
    // the entire EditorView to be destroyed and recreated on every keystroke,
    // which breaks editing and resets scroll position.
    // Use buffer content directly when creating the view. Do NOT fall back to
    // localContentRef.current — it is stale from the previous buffer when
    // switching files, corrupting the history baseline (undo reveals old file content).
    const initContent = buffer?.content || '';

    const state = EditorState.create({
      doc: initContent,
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

          openWorkspaceBufferRef.current({
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
    compartments,
    themePack,
    customHighlightStyle,
    localContentRef,
    onCancelPendingFlush,
    onUpdateRef,
    settingsRef,
    keymapsRef,
    actionsRef,
  ]);
}
