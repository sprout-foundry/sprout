/**
 * EditorCore — CodeMirror EditorView mount point component.
 *
 * NOTE: This component is currently not used by EditorPane. It exists for
 * potential future use and has its own test file (EditorCore.test.tsx).
 *
 * This is a controlled component that manages the CodeMirror EditorView lifecycle.
 * It does NOT own state for loading, saving, word wrap, etc. — those are owned
 * by the parent component (EditorPane). EditorCore only owns the EditorView
 * creation, destruction, and update listener management.
 *
 * ## Architecture
 *
 * EditorCore is a thin wrapper around CodeMirror's EditorView that:
 *
 * 1. Creates the EditorView when mounted (or when props change requiring recreation)
 * 2. Registers an updateListener that calls the parent's onUpdate callback
 * 3. Handles asynchronous LSP extension initialization if languageId is provided
 * 4. Destroys the view when unmounted
 * 5. Exposes the view and compartments to the parent via callbacks
 *
 * The parent component (EditorPane) is responsible for:
 * - Building extensions via useEditorExtensions.buildExtensions()
 * - Managing all state (loading, saving, word wrap, cursor position, etc.)
 * - Handling update callbacks (content changes, cursor moves, scroll)
 * - Compartment reconfiguration for settings changes
 *
 * ## Recreation Strategy
 *
 * The EditorView is recreated (destroyed and recreated) when any of these change:
 * - containerRef changes (different mount point)
 * - initialContent changes (different document)
 * - extensions array changes (new extension configuration)
 * - filePath or languageId changes (requires re-initialization)
 *
 * Compartment reconfiguration is used for runtime setting changes (word wrap,
 * font size, etc.) that don't require full recreation.
 *
 * ## Important Notes for Parents
 *
 * 1. **Callback stability**: `onViewCreated`, `onViewDestroying`, and `onUpdate` MUST be
 *    stable references (wrapped in `useCallback` with controlled deps) to prevent unnecessary
 *    editor recreation. Unstable callbacks cause full EditorView destroy/create cycles on
 *    every parent render.
 *
 * 2. **Extensions memoization**: The `extensions` array must be memoized with `useMemo`.
 *    `buildExtensions()` returns a new array on every call — passing it inline destroys
 *    and recreates the editor on every render.
 *
 * 3. **initialContent semantics**: `initialContent` should be the file's content at load time,
 *    NOT the live-edited content. The EditorView manages its own document after creation.
 *    Passing `localContent` here would recreate the editor on every keystroke.
 *
 * @see EditorPane.tsx for the consumer of this component
 */

import { useEffect, useRef } from 'react';
import { EditorView, ViewUpdate } from '@codemirror/view';
import { EditorState, Extension, Compartment } from '@codemirror/state';
import { getLSPClientService, LSP_SUPPORTED_LANGUAGES } from '../services/lspClientService';
import { buildLSPPluginExtensions, lspSyncOnDocChange, registerEditorView, unregisterEditorView } from '../extensions/lspExtensions';
import { debugLog } from '../utils/log';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface EditorCoreProps {
  /** DOM element ref to mount the editor into */
  containerRef: React.RefObject<HTMLDivElement | null>;
  /** Initial document content */
  initialContent: string;
  /** CodeMirror extensions (built by useEditorExtensions.buildExtensions()) */
  extensions: Extension[];
  /** LSP compartment for dynamic extension injection */
  lspCompartment: Compartment;
  /**
   * Called when the view is created — provides the view and viewRef for
   * parent-driven compartment reconfiguration and state access.
   */
  onViewCreated: (view: EditorView, viewRef: React.MutableRefObject<EditorView | null>) => void;
  /** Called when the view is about to be destroyed — for cleanup (LSP unregistration, etc.) */
  onViewDestroying: () => void;
  /**
   * Called on every editor update — content changes, cursor moves, scroll, etc.
   * The parent delegates to specialized hooks (useEditorCursor, useEditorScrollSync, etc.)
   */
  onUpdate: (update: ViewUpdate) => void;
  /** CSS class name for the container div (optional) */
  className?: string;
  /** File path for LSP registration (optional) */
  filePath?: string;
  /** Resolved language ID for LSP initialization (optional) */
  languageId?: string | null;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * CodeMirror EditorView mount point component.
 *
 * This component manages only the EditorView lifecycle. All state management,
 * file I/O, cursor tracking, and UI concerns are owned by the parent component.
 *
 * @param props - EditorCoreProps
 * @returns null (renders nothing; mounts editor into containerRef)
 */
export default function EditorCore(props: EditorCoreProps): JSX.Element | null {
  const {
    containerRef,
    initialContent,
    extensions,
    lspCompartment,
    onViewCreated,
    onViewDestroying,
    onUpdate,
    className,
    filePath = '',
    languageId = null,
  } = props;

  // Ref to the EditorView instance — provided to parent via onViewCreated
  const viewRef = useRef<EditorView | null>(null);

  // Track the last language we initialized LSP for — prevents redundant
  // initialization when languageId prop doesn't change but other props do.
  const lastLSPLanguageRef = useRef<string | null>(null);

  // ── EditorView lifecycle ───────────────────────────────────────────────

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // Build the extension array with the update listener as the first extension.
    // The updateListener calls the parent's onUpdate callback for delegation
    // to specialized hooks (useEditorCursor, useEditorScrollSync, etc.).
    const updateListener = EditorView.updateListener.of((update) => {
      onUpdate(update);
    });

    // Prepend the updateListener so it's first in the extension chain.
    const extensionsWithListener = [updateListener, ...extensions];

    // Clean up any residual DOM from a previous EditorView (destroy may not
    // fully clear the container in all browsers).
    container.innerHTML = '';

    // Create the EditorState with the provided extensions and initial content.
    const state = EditorState.create({
      doc: initialContent,
      extensions: extensionsWithListener,
    });

    // Create and mount the EditorView.
    const view = new EditorView({
      state,
      parent: container,
    });

    viewRef.current = view;

    // Notify the parent that the view is created — the parent uses this to
    // store the viewRef and access compartments for reconfiguration.
    onViewCreated(view, viewRef);

    // Register the editor view for cross-file LSP navigation
    if (filePath && !filePath.startsWith('__workspace/')) {
      registerEditorView(filePath, view);
    }

    // Apply CSS class name to the container if provided.
    if (className) {
      container.classList.add(className);
    }

    // ── LSP initialization (asynchronous) ──────────────────────────────
    // If a languageId is provided and it's supported by the LSP service,
    // connect to the language server and inject LSP extensions via the
    // lspCompartment. This runs asynchronously after the view is created.
    if (languageId && LSP_SUPPORTED_LANGUAGES.has(languageId)) {
      const currentLangId = languageId;
      const currentFilePath = filePath;
      lastLSPLanguageRef.current = currentLangId;

      // Capture the view at creation time to avoid applying extensions
      // to a different editor if the user switches files before the LSP
      // client connects.
      const capturedView = view;

      void (async () => {
        try {
          const lspService = getLSPClientService();
          await lspService.getStatus();
          const client = await lspService.getClientForLanguage(currentLangId);

          // Only apply extensions if:
          // 1. The LSP client is available
          // 2. The view is still the same (not replaced by file switch)
          // 3. The language hasn't changed (prevents stale LSP for rapid switching)
          if (client && viewRef.current === capturedView && capturedView.dom?.isConnected && lastLSPLanguageRef.current === currentLangId) {
            const lspExtensions = [
              ...buildLSPPluginExtensions(client, currentFilePath, currentLangId),
              ...lspSyncOnDocChange(currentLangId),
            ];

            capturedView.dispatch({
              effects: lspCompartment.reconfigure(lspExtensions),
            });

            debugLog('[EditorCore] LSP extensions activated for', currentLangId);
          }
        } catch (err) {
          debugLog('[EditorCore] LSP initialization failed:', err);
        }
      })();
    }

    // ── Cleanup ───────────────────────────────────────────────────────────
    return () => {
      // Notify the parent that the view is about to be destroyed.
      // The parent uses this to unregister the view from LSP navigation
      // and perform other cleanup.
      onViewDestroying();

      // Unregister the editor view for cross-file LSP navigation
      if (filePath && !filePath.startsWith('__workspace/')) {
        unregisterEditorView(filePath);
      }

      // Remove CSS class name if it was added.
      if (className && container) {
        container.classList.remove(className);
      }

      // Destroy the EditorView.
      view.destroy();
      viewRef.current = null;
      lastLSPLanguageRef.current = null;
    };
    // Recreate effect when any of these change:
    // - containerRef: different mount point
    // - initialContent: different document (full recreation required)
    // - extensions: new extension configuration
    // - lspCompartment: different compartment (unlikely but supported)
    // - className: CSS class change
    // - filePath: LSP registration depends on file path
    // - languageId: LSP initialization depends on language
    //
    // NOTE: Compartment reconfiguration is used for runtime setting changes
    // (word wrap, font size, etc.) that don't require full recreation.
  }, [containerRef, initialContent, extensions, lspCompartment, className, filePath, languageId, onViewCreated, onViewDestroying, onUpdate]);

  // This component renders nothing — it mounts the editor into containerRef.
  return null;
}
