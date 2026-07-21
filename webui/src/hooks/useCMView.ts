/**
 * useCMView — owns the CodeMirror EditorView lifecycle for one editor pane.
 *
 * Architecture decision: React owns the lifetime boundary, while CodeMirror is
 * kept behind one stable API object. This replaces the EditorView-init layer's
 * mirrored action/update refs and its ref-to-ref save path. Callers can mutate
 * the view through the API without coordinating an additional `viewRef`.
 *
 * This hook does not own file I/O, cursor state, compartment reconfiguration,
 * or the implementation of LSP/bootstrap callbacks. Those concerns remain in
 * their respective hooks and are supplied here as callbacks or stable refs.
 */

import type { HighlightStyle } from '@codemirror/language';
import { searchKeymap } from '@codemirror/search';
import type { Compartment, Extension, TransactionSpec } from '@codemirror/state';
import { EditorState } from '@codemirror/state';
import { EditorView, keymap, type ViewUpdate, type KeyBinding } from '@codemirror/view';
import { useEffect, useRef } from 'react';
import type * as React from 'react';
import type { EditorBuffer } from '../types/editor';
import type { ThemePack } from '../themes/themePacks';
import type { UseEditorExtensionsReturn } from './useEditorExtensions';

// Reuse the legacy shape previously exported from the now-removed
// view-init hook so the existing call sites can adopt useCMView without
// a type-bridge layer.
export interface CMViewSettings {
  wordWrapEnabled: boolean;
  relativeLineNumbersEnabled: boolean;
  minimapEnabled: boolean;
  editorFontSize: number;
  editorTabSize: number;
  editorUsesTabs: boolean;
  whitespaceRenderingMode: 'none' | 'boundary' | 'all';
  inlayHintsEnabled: boolean;
  signatureHelpEnabled: boolean;
}

export interface CMViewKeymaps {
  customKeymap: Extension;
  replacePanelKeymap: readonly KeyBinding[];
  zoomKeymap: readonly KeyBinding[];
  semanticKeymap: readonly KeyBinding[];
}

export interface OpenWorkspaceBufferRequest {
  kind: 'file' | 'chat' | 'diff' | 'review' | 'compare';
  path: string;
  title: string;
  ext?: string;
}

export type OpenWorkspaceBufferFn = (req: OpenWorkspaceBufferRequest) => void;

export interface CMViewActions {
  /** Returns the current save function through one ref hop. */
  getSaveFn: () => () => void;
  /** Returns the current openWorkspaceBuffer through one ref hop. */
  getOpenWorkspaceBuffer: () => OpenWorkspaceBufferFn;
}

export interface UseCMViewOptions {
  paneId: string;
  editorRef: React.RefObject<HTMLDivElement | null>;
  /** Buffer is read for initial content and language; updates after mount
   *  do NOT recreate the view (use `api.setBuffer` to swap). */
  buffer: EditorBuffer | null | undefined;
  /** Stable ref mirror of buffer — the hook reads this for *current* buffer
   *  info in extension callbacks that fire after mount. */
  bufferRef: React.MutableRefObject<EditorBuffer | null | undefined>;
  /** Pre-resolved language id (already passed through resolveLanguageId). */
  languageId: string | null;
  /** Single-hop save handle. Callers set this in a useEffect after handleSave
   *  is defined. Hook never mirrors it through React state. */
  handleSaveRef: React.MutableRefObject<() => Promise<void>>;
  /** Open-workspace-buffer handle. Single-hop. */
  openWorkspaceBufferRef: React.MutableRefObject<OpenWorkspaceBufferFn>;
  /** Update listener. Note: this callback's identity may change on every
   *  render; the hook reads it via a ref-mirror internally, NOT through the
   *  effect dep array.
   *
   *  Accept a ref, not a value, so the hook can be created BEFORE the
   *  callback's identity stabilizes (e.g., before `useEditorUpdate` returns).
   *  The caller writes the latest callback into `onUpdateRef.current` during
   *  render. */
  onUpdateRef: React.MutableRefObject<(update: ViewUpdate) => void>;
  /** Settings ref. The current value is read when extensions are built.
   *  May be null until the caller writes the initial value during render. */
  settingsRef: React.MutableRefObject<CMViewSettings | null>;
  /** Keymaps ref. May be null until the caller writes the initial value. */
  keymapsRef: React.MutableRefObject<CMViewKeymaps | null>;
  /** Compartment handles from useEditorExtensions. Stable for component lifetime. */
  compartments: UseEditorExtensionsReturn['compartments'];
  /** Extension builder from useEditorExtensions. Stable for component lifetime. */
  buildExtensions: UseEditorExtensionsReturn['buildExtensions'];
  /** Theme. Changing this will recreate the view (necessary for syntax
   *  highlight style swap). */
  themePack: ThemePack;
  customHighlightStyle: HighlightStyle | null;
  /** LSP bootstrap. Called once on view mount when languageId is supported.
   *  Returns the LSP extensions to install in the LSP compartment.
   *  Implementations should be idempotent and handle their own cancellation. */
  bootstrapLSP?: (langId: string, filePath: string, view: EditorView) => Promise<Extension[]>;
  /** Cleanup hook called BEFORE view.destroy(). Use for unregistering
   *  listeners, canceling pending work, etc. */
  onWillDestroy?: (view: EditorView) => void;
  /** Called AFTER view is created. Use for global view registration (e.g.
   *  registerEditorView for LSP lookup). */
  onDidMount?: (view: EditorView, filePath: string | undefined) => void;
  /** Called when the view has been destroyed by this hook. */
  onDidDestroy?: () => void;
}

export interface CMViewAPI {
  /** The EditorView. Stable reference for the lifetime of the API object. */
  view: EditorView | null;
  /** Whether the view is currently mounted. Becomes false briefly during
   *  destroy/recreate cycles. */
  isMounted: boolean;
  /** Dispatch a transaction, or no-op when the view is gone. */
  dispatch: (tr: TransactionSpec) => void;
  /** Synchronously gate cursor-skip behavior around a view mutation. */
  withExternalUpdate: <T>(fn: () => T) => T;
  /** Read the external-update flag directly. */
  isExternalUpdate: () => boolean;
  /** Single-hop save. */
  save: () => Promise<void>;
  /** Get current buffer info (always fresh from bufferRef). */
  getFilePath: () => string | undefined;
  getFileExt: () => string | undefined;
  getContent: () => string;
  /** Subscribe to view updates. Returns an unsubscribe function. */
  subscribe: (listener: (update: ViewUpdate) => void) => () => void;
  /** Compartments exposed for reconfiguration by callers. */
  compartments: UseEditorExtensionsReturn['compartments'];
}

/** Report callback failures without allowing one subscriber to stop CM's update pipeline. */
function reportError(scope: string, error: unknown): void {
  console.error(`[useCMView] ${scope}`, error);
}

// Safe defaults used when the caller has not yet populated the keymap or
// settings ref during render. These produce a fully-functional but bare
// CodeMirror view (no special keymaps or settings). The caller MUST write
// real values during render in production; these defaults only matter for
// the very first mount in tests or before React commits the first render.
const DEFAULT_CM_SETTINGS: CMViewSettings = {
  wordWrapEnabled: false,
  relativeLineNumbersEnabled: false,
  minimapEnabled: false,
  editorFontSize: 13,
  editorTabSize: 4,
  editorUsesTabs: false,
  whitespaceRenderingMode: 'none',
  inlayHintsEnabled: false,
  signatureHelpEnabled: false,
};

const DEFAULT_CM_KEYMAPS: CMViewKeymaps = {
  customKeymap: [],
  replacePanelKeymap: [],
  zoomKeymap: [],
  semanticKeymap: [],
};

export function useCMView(opts: UseCMViewOptions): CMViewAPI {
  const {
    paneId,
    editorRef,
    buffer,
    bufferRef,
    languageId,
    handleSaveRef,
    openWorkspaceBufferRef,
    onUpdateRef,
    settingsRef,
    keymapsRef,
    compartments,
    buildExtensions,
    themePack,
    customHighlightStyle,
    bootstrapLSP,
    onWillDestroy,
    onDidMount,
    onDidDestroy,
  } = opts;

  // Caller-provided ref to the update listener. The caller assigns to
  // onUpdateRef.current during render. This hook reads it inside the
  // updateListener at call time, so it always sees the latest callback.
  // We do not assign here — the caller owns the ref.

  const gate = useRef(false);
  const listeners = useRef(new Set<(update: ViewUpdate) => void>()).current;

  // The API object is initialized once. Its mutable view/isMounted fields are
  // updated in place, preserving identity for every caller across re-renders.
  const apiRef = useRef<CMViewAPI>({
    view: null,
    isMounted: false,
    dispatch: (tr: TransactionSpec) => {
      apiRef.current.view?.dispatch(tr);
    },
    withExternalUpdate: <T,>(fn: () => T): T => {
      const previous = gate.current;
      gate.current = true;
      try {
        return fn();
      } finally {
        gate.current = previous;
      }
    },
    isExternalUpdate: () => gate.current,
    save: () => handleSaveRef.current(),
    getFilePath: () => bufferRef.current?.file?.path,
    getFileExt: () => bufferRef.current?.file?.ext,
    getContent: () => bufferRef.current?.content ?? '',
    subscribe: (listener: (update: ViewUpdate) => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    compartments,
  });

  useEffect(() => {
    const parent = editorRef.current;
    if (!parent) return undefined;

    // Caller guarantees these refs are populated during render before mount;
    // if not, we fall back to safe no-op defaults rather than crash. This
    // matches the legacy behavior where the prior useEffect-based mirror
    // would have made the same trade-off.
    const settings = settingsRef.current ?? DEFAULT_CM_SETTINGS;
    const keymaps = keymapsRef.current ?? DEFAULT_CM_KEYMAPS;
    const initialFilePath = buffer?.file?.path;
    const initialLanguageId = languageId;

    // Initial content is intentionally read from buffer?.content exactly once
    // here. Do not use localContent/localContentRef: those values can belong to
    // the previous buffer during a pane switch.
    const initialContent = buffer?.content ?? '';

    // Option (a) for cursor gating: expose gate state through
    // api.isExternalUpdate. The cursor hook migration consumes this API rather
    // than sharing a useEffect-cleared isExternalUpdateRef.
    const updateListener = EditorView.updateListener.of((update: ViewUpdate) => {
      try {
        onUpdateRef.current(update);
      } catch (error) {
        reportError('update listener failed', error);
      }

      // Subscribers are independent: one bad listener must not prevent the
      // remaining listeners from observing this update.
      for (const listener of listeners) {
        try {
          listener(update);
        } catch (error) {
          reportError('update subscriber failed', error);
        }
      }
    });

    const actions: CMViewActions = {
      // The callback itself is stable in the extension set; each invocation
      // reads the current handle directly, without an actionsRef hop.
      getSaveFn: () => handleSaveRef.current,
      getOpenWorkspaceBuffer: () => openWorkspaceBufferRef.current,
    };

    const extensions = buildExtensions({
      paneId,
      settings,
      theme: { themePack, customHighlightStyle },
      buffer: {
        languageId: initialLanguageId,
        getFilePath: () => bufferRef.current?.file?.path,
        getFileExt: () => bufferRef.current?.file?.ext,
        getContent: () => bufferRef.current?.content ?? '',
      },
      actions,
      hotkeysCompartmentExtension: compartments.hotkeys.of(keymaps.customKeymap),
      extraKeymaps: [
        keymap.of(searchKeymap),
        keymap.of(keymaps.replacePanelKeymap),
        keymap.of(keymaps.zoomKeymap),
        keymap.of(keymaps.semanticKeymap),
        updateListener,
      ],
    });

    const state = EditorState.create({
      doc: initialContent,
      extensions,
    });
    let view: EditorView;
    try {
      view = new EditorView({ state, parent });
    } catch (e) {
      // Re-throw after invalidating any in-flight async work so the boundary
      // surfaces the failure and the cleanup path runs.
      throw e;
    }
    const capturedView = view;

    apiRef.current.view = capturedView;
    apiRef.current.isMounted = true;

    try {
      onDidMount?.(capturedView, initialFilePath);
    } catch (error) {
      reportError('onDidMount failed', error);
    }

    if (bootstrapLSP && initialLanguageId) {
      void (async () => {
        try {
          const lspExtensions = await bootstrapLSP(initialLanguageId, initialFilePath ?? '', capturedView);
          if (apiRef.current.view !== capturedView || !capturedView.dom.isConnected) return;
          capturedView.dispatch({
            effects: compartments.lsp.reconfigure(lspExtensions),
          });
        } catch (error) {
          // A bootstrap implementation owns cancellation; this guard keeps a
          // rejected/late bootstrap from affecting the replacement view.
          reportError('LSP bootstrap failed', error);
        }
      })();
    }

    return () => {
      // Invalidate async work before destroying the view. The captured-view
      // check in the bootstrap continuation will then reject late results.
      try {
        onWillDestroy?.(capturedView);
      } catch (error) {
        reportError('onWillDestroy failed', error);
      }

      try {
        capturedView.destroy();
      } finally {
        if (apiRef.current.view === capturedView) {
          apiRef.current.view = null;
          apiRef.current.isMounted = false;
        }
        try {
          onDidDestroy?.();
        } catch (error) {
          reportError('onDidDestroy failed', error);
        }
      }
    };
    // These are the lifecycle identity inputs by design. All callback and
    // mutable configuration refs are read through their current value above;
    // adding them here would recreate the EditorView during ordinary renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paneId, buffer?.id, buffer?.file?.ext, buffer?.file?.name, themePack, customHighlightStyle, editorRef]);

  return apiRef.current;
}
