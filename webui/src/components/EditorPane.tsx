import type { EditorView as CMEditorView } from '@codemirror/view';
import { useRef, useState, useMemo, useCallback, useEffect } from 'react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import { useEditorContextMenu } from '../hooks/useEditorContextMenu';
import { useEditorCursor } from '../hooks/useEditorCursor';
import { useEditorDiagnostics } from '../hooks/useEditorDiagnostics';
import { useEditorEvents } from '../hooks/useEditorEvents';
import { useEditorExtensions } from '../hooks/useEditorExtensions';
import { useEditorFileIO } from '../hooks/useEditorFileIO';
import { useEditorFileType } from '../hooks/useEditorFileType';
import { useEditorKeymaps } from '../hooks/useEditorKeymaps';
import { useEditorLSP } from '../hooks/useEditorLSP';
import { useEditorScrollSync } from '../hooks/useEditorScrollSync';
import { useEditorSemantic } from '../hooks/useEditorSemantic';
import { useEditorSettings } from '../hooks/useEditorSettings';
import { useEditorSymbols } from '../hooks/useEditorSymbols';
import { useEditorUpdate } from '../hooks/useEditorUpdate';
import { useLivePreview } from '../hooks/useLivePreview';
import { resolveLanguageId } from '../extensions/languageRegistry';
import {
  buildLSPPluginExtensions,
  lspSyncOnDocChange,
  registerEditorView,
  unregisterEditorView,
  setGlobalDisplayFileCallback,
  type DisplayFileCallback,
} from '../extensions/lspExtensions';
import { getLSPClientService } from '../services/lspClientService';
import type { EditorBuffer } from '../types/editor';
import BinaryFileViewer from './BinaryFileViewer';
import EditorContextMenu from './EditorContextMenu';
import EditorCore from './EditorCore';
import EditorPaneFooter from './EditorPaneFooter';
import EditorToolbar from './EditorToolbar';
import FindAllReferencesOverlay from './FindAllReferencesOverlay';
import GoToWorkspaceSymbolOverlay from './GoToWorkspaceSymbolOverlay';
import ImageViewer from './ImageViewer';
import LivePreview from './LivePreview';
import MarkdownPreview from './MarkdownPreview';
import MediaViewer from './MediaViewer';
import { useEditorToolbarActions } from './useEditorToolbarActions';
import WelcomeTab from './WelcomeTab';
import './EditorPane.css';
import {
  useCMView,
  type CMViewAPI,
  type CMViewKeymaps,
  type CMViewSettings,
  type OpenWorkspaceBufferFn,
} from '../hooks/useCMView';

interface EditorPaneProps {
  paneId: string;
  onOpenCommandPalette?: () => void;
}

// Module-level guard: `setGlobalDisplayFileCallback` must be invoked once
// per app (it stores a single global callback). The previous view-init
// layer used `globalDisplayFileRegistered` for the same purpose; we
// replicate that here, per-view, so the first pane to mount registers
// the callback and subsequent mounts are no-ops.
let displayFileCallbackRegistered = false;

function EditorPane({ paneId, onOpenCommandPalette }: EditorPaneProps): JSX.Element {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<CMEditorView | null>(null);
  const markdownPreviewBodyRef = useRef<HTMLDivElement>(null);

  const { compartments, buildExtensions } = useEditorExtensions();

  const {
    panes,
    buffers,
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    setBufferModified,
    splitPane,
    openWorkspaceBuffer,
    setBufferLanguageOverride,
    isLinkedScrollEnabled,
    toggleLinkedScroll,
    whitespaceRenderingMode,
    setWhitespaceRenderingMode,
    isFormatOnSaveEnabled,
    setFormatOnSaveEnabled,
    activePaneId,
  } = useEditorManager();
  const { themePack, customHighlightStyle } = useTheme();
  const { hotkeys } = useHotkeys();

  const pane = panes.find((p) => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;
  const bufferRef = useRef<EditorBuffer | null | undefined>(buffer);
  bufferRef.current = buffer;

  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');
  const [markdownPreviewMode, setMarkdownPreviewMode] = useState<'off' | 'split' | 'preview'>('off');

  const settings = useEditorSettings(compartments, buffer?.id);

  // Sync the context's whitespaceRenderingMode into the settings ref so that
  // onCycleWhitespaceRendering reads the correct starting value when changed
  // externally (e.g., from the footer or sidebar). useEffect (instead of
  // direct render-time mutation) keeps concurrent renders consistent.
  useEffect(() => {
    settings.whitespaceRenderingModeRef.current = whitespaceRenderingMode;
  }, [settings.whitespaceRenderingModeRef, whitespaceRenderingMode]);

  const { fetchDiagnosticsRef, isSemanticLanguage } = useEditorDiagnostics(viewRef, buffer);

  // Buffer ref + keymap/settings refs are needed before hooks that use them.
  // Assign during render (no useEffect mirror) so the first CM updateListener
  // invocation already reads the latest values.
  //
  // These are typed to the central hook's own shapes (`CMViewKeymaps` /
  // `CMViewSettings`) since `useCMView` is now the sole consumer — the
  // legacy `EditorViewInitKeymaps` / `EditorViewInitSettings` types are gone.
  const keymapsRef = useRef<CMViewKeymaps | null>(null);
  const settingsRef = useRef<CMViewSettings | null>(null);
  const handleSaveRef = useRef<() => Promise<void>>(async () => {});

  // cmViewApi is produced by useCMView (called later in this component) but
  // is read by hooks above via this ref indirection. The ref is populated
  // after useCMView returns on each render; the listener path inside the
  // CM view always reads the latest value. Until the first useCMView runs,
  // isExternalUpdate() returns false (no gating), which matches the
  // pre-refactor behavior of an uninitialized flag.
  const cmViewApiRef = useRef<CMViewAPI | null>(null);

  const { selectionInfo, setSelectionInfo, handleCursorUpdate } = useEditorCursor({
    bufferRef,
    updateBufferCursor,
    cmViewApiRef,
  });

  const { handleSave, saveRef } = useEditorFileIO(
    cmViewApiRef,
    buffer,
    bufferRef,
    compartments,
    settings.indentManuallySetRef,
    fetchDiagnosticsRef,
    paneId,
    {
      setLoading,
      setSaving,
      setError,
      setLocalContent,
      setSelectionInfo,
      setEditorTabSize: settings.setEditorTabSize,
      setEditorUsesTabs: settings.setEditorUsesTabs,
      setLineEnding: settings.setLineEnding,
    },
  );

  const { handleScrollUpdate, cancelPendingFlush } = useEditorScrollSync({
    paneId,
    viewRef,
    bufferRef,
    filePath: buffer?.file?.path,
    updateBufferScroll,
    isLinkedScrollEnabled,
  });

  // useEditorUpdate.onUpdate identity changes when localContent changes (it's
  // in the dep array). Reading it via a ref-mirror avoids recreating the
  // EditorView on every keystroke. The mirror is written during render — no
  // useEffect — so the listener always sees the latest callback.
  const onUpdateRef = useRef<(update: import('@codemirror/view').ViewUpdate) => void>(() => {});

  const { localContentRef, onUpdate } = useEditorUpdate({
    bufferRef,
    localContent,
    setLocalContent,
    cmViewApiRef,
    fetchDiagnosticsRef,
    handleCursorUpdate,
    handleScrollUpdate,
    updateBufferContent,
    setBufferModified,
  });
  onUpdateRef.current = onUpdate;

  // Single-hop save ref — written during render, read by the API's save()
  // method inside the CM updateListener (e.g., search panel). No useEffect
  // mirror, no actionsRef indirection.
  handleSaveRef.current = handleSave;

  // openWorkspaceBuffer from EditorManagerContext is itself a stable callback
  // across renders, but we still wire it through a ref because the CM
  // extensions builder reads `openWorkspaceBufferRef.current.getOpenWorkspaceBuffer()`
  // inside handlers that fire after mount (e.g., when LSP client opens a
  // file from `textDocument/definition`). Without a render-time `.current`
  // assignment, a stale value from the first render would persist. The
  // legacy code used `useRef(openWorkspaceBuffer)` which silently *never*
  // updated; that's the bug we're fixing here.
  const openWorkspaceBufferRef = useRef<OpenWorkspaceBufferFn>(openWorkspaceBuffer);
  openWorkspaceBufferRef.current = openWorkspaceBuffer;

  const { semanticHandlerRefs, buildKeymaps } = useEditorKeymaps(hotkeys, viewRef, bufferRef);

  const semantic = useEditorSemantic(viewRef, bufferRef, localContent, isSemanticLanguage, openWorkspaceBuffer);

  useEffect(() => {
    semanticHandlerRefs.handleGoToDefinition.current = semantic.handleGoToDefinition;
    semanticHandlerRefs.handleFindAllReferences.current = semantic.handleFindAllReferences;
  }, [
    semanticHandlerRefs.handleGoToDefinition,
    semanticHandlerRefs.handleFindAllReferences,
    semantic.handleGoToDefinition,
    semantic.handleFindAllReferences,
  ]);

  const keymapActions = {
    onSave: handleSave,
    onGoToLine: () => {
      const event = new CustomEvent('editor-goto-line');
      document.dispatchEvent(event);
    },
    onGoToSymbol: () => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_goto_symbol' } }));
    },
    onGoToWorkspaceSymbol: () => semantic.setShowGoToWorkspaceSymbol(true),
    onToggleWordWrap: settings.onToggleWordWrap,
    onToggleRelativeLineNumbers: settings.onToggleRelativeLineNumbers,
  };

  const keymaps = buildKeymaps(keymapActions);

  // Refs are written during render (no useEffect mirror) so the CM view
  // mounts with the latest values and configures compartments correctly
  // on the very first updateListener invocation. This eliminates the
  // "stale settings" bug where the previous render's settings were applied
  // to the current keystroke.
  keymapsRef.current = keymaps;
  settingsRef.current = {
    wordWrapEnabled: settings.wordWrapEnabled,
    relativeLineNumbersEnabled: settings.relativeLineNumbersEnabled,
    minimapEnabled: settings.minimapEnabled,
    editorFontSize: settings.editorFontSize,
    editorTabSize: settings.editorTabSize,
    editorUsesTabs: settings.editorUsesTabs,
    whitespaceRenderingMode: settings.whitespaceRenderingMode,
    inlayHintsEnabled: settings.inlayHintsEnabled,
    signatureHelpEnabled: settings.signatureHelpEnabled,
  };

  // Resolve language for the current buffer. The CM extensions builder
  // needs this to load the right syntax highlighter.
  const resolvedLanguage = resolveLanguageId(
    buffer?.languageOverride,
    buffer?.file?.ext?.replace(/^\./, ''),
    buffer?.file?.name,
  );

  // LSP bootstrap — owned here in EditorPane. Called once by `useCMView`
  // when the view mounts for a buffer with a supported language id.
  // Errors are best-effort: a failed LSP service must not abort editor
  // mounting.
  const bootstrapLSP = useCallback(async (langId: string, filePath: string) => {
    const lspService = getLSPClientService();
    try {
      // status check is best-effort; proceed without aborting on failure
      await lspService.getStatus();
    } catch {
      // swallow — LSP server may not be reachable yet
    }
    const client = await lspService.getClientForLanguage(langId);
    if (!client) return [];
    return [
      ...buildLSPPluginExtensions(client, filePath, langId),
      ...lspSyncOnDocChange(langId),
    ];
  }, []);

  // Per-view mount hook. Registers the view with the LSP service so
  // `gotoDefinition` et al. can find it by filePath. Installs the
  // global `setGlobalDisplayFileCallback` exactly once across the app
  // via the module-level `displayFileCallbackRegistered` flag.
  const onDidMount = useCallback((view: CMEditorView, filePath: string | undefined) => {
    if (filePath && !filePath.startsWith('__workspace/')) {
      registerEditorView(filePath, view);
    }
    if (!displayFileCallbackRegistered) {
      displayFileCallbackRegistered = true;
      const cb: DisplayFileCallback = async (fp: string) => {
        const fileName = fp.split('/').pop() || fp;
        const dotIndex = fileName.lastIndexOf('.');
        const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;
        openWorkspaceBufferRef.current({ kind: 'file', path: fp, title: fileName, ext });
        return null;
      };
      setGlobalDisplayFileCallback(cb);
    }
  }, []);

  // Per-view destroy hook. Cancels any pending scroll/diagnostic flush
  // and unregisters the view so future LSP requests for the same filePath
  // don't dispatch into a torn-down EditorView.
  const onWillDestroy = useCallback(
    (_view: CMEditorView) => {
      cancelPendingFlush();
      const buf = bufferRef.current;
      const filePath = buf?.file?.path;
      if (filePath && !filePath.startsWith('__workspace/')) {
        unregisterEditorView(filePath);
      }
    },
    [cancelPendingFlush],
  );

  // Single CM view API instance for this pane. The hook returns a stable
  // object reference (apiRef.current never reassigns); mutations to api.view
  // and api.isMounted are in place. The handleSaveRef, settingsRef, and
  // keymapsRef are read at call time inside the CM updateListener, so
  // there's no useEffect mirroring and no stale-closure race.
  const cmViewApi = useCMView({
    paneId,
    editorRef,
    buffer,
    bufferRef,
    languageId: resolvedLanguage.languageId,
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
    onDidMount,
    onWillDestroy,
  });
  // Make the API available to hooks called earlier in this component via
  // the ref indirection. Writes are safe during render — the ref object
  // is stable, only its `.current` changes.
  cmViewApiRef.current = cmViewApi;

  // Tracks whether this pane is the active one. Updated on every render so
  // useEditorEvents' stable handler can read the latest value via the ref
  // without needing to re-subscribe document listeners.
  const isActiveRef = useRef(false);
  isActiveRef.current = paneId === activePaneId;

  useEditorEvents({
    viewRef,
    bufferRef,
    isActiveRef,
    handleGoToLine: semantic.handleGoToLine,
    onToggleWordWrap: settings.onToggleWordWrap,
    onToggleMinimap: settings.onToggleMinimap,
    onToggleRelativeLineNumbers: settings.onToggleRelativeLineNumbers,
    onCycleWhitespaceRendering: () => {
      const next = settings.onCycleWhitespaceRendering();
      if (next) setWhitespaceRenderingMode(next);
    },
    toggleLinkedScroll,
    handleFindAllReferences: semantic.handleFindAllReferences,
    onGoToWorkspaceSymbol: () => semantic.setShowGoToWorkspaceSymbol(true),
    onToggleInlayHints: settings.onToggleInlayHints,
    onToggleSignatureHelp: settings.onToggleSignatureHelp,
    onCycleTabSize: settings.onCycleTabSize,
    onZoomIn: settings.onZoomIn,
    onZoomOut: settings.onZoomOut,
    onResetZoom: settings.onResetZoom,
    onToggleFormatOnSave: () => setFormatOnSaveEnabled(!isFormatOnSaveEnabled),
    // Live preview / markdown preview wiring is set up further down once
    // livePreview / markdownPreviewMode are in scope.
  });

  const contextMenuCallbacks = useMemo(
    () => ({
      onGoToDefinition: semantic.handleGoToDefinition,
      onFindAllReferences: semantic.handleFindAllReferences,
    }),
    [semantic.handleGoToDefinition, semantic.handleFindAllReferences],
  );

  const contextMenu = useEditorContextMenu(buffer, bufferRef, viewRef, contextMenuCallbacks);

  const lsp = useEditorLSP(buffer, setBufferLanguageOverride);

  const { enclosingSymbols } = useEditorSymbols(localContent, buffer);

  const fileType = useEditorFileType(buffer);

  const livePreview = useLivePreview(
    buffer,
    localContent,
    fileType.isSvgFile,
    fileType.isHtmlFile,
    splitPane,
    openWorkspaceBuffer,
    paneId,
  );

  const handleFormatDocument = useCallback(() => {
    document.dispatchEvent(new CustomEvent('editor-format-document'));
  }, []);

  // Live-preview and markdown-preview omnibox commands are listened for here
  // (rather than via useEditorEvents) because the consumers — `livePreview`
  // and `markdownPreviewMode` — are declared after useEditorEvents is called.
  useEffect(() => {
    const onLivePreview = () => {
      if (livePreview.openLivePreview && (fileType.isSvgFile || fileType.isHtmlFile)) {
        livePreview.openLivePreview();
      }
    };
    const onMdToggle = () => {
      if (!fileType.isMarkdownFile) return;
      setMarkdownPreviewMode((prev) => (prev === 'off' ? 'split' : prev === 'split' ? 'preview' : 'off'));
    };
    document.addEventListener('editor-open-live-preview', onLivePreview);
    document.addEventListener('editor-toggle-markdown-preview', onMdToggle);
    return () => {
      document.removeEventListener('editor-open-live-preview', onLivePreview);
      document.removeEventListener('editor-toggle-markdown-preview', onMdToggle);
    };
  }, [livePreview, fileType.isSvgFile, fileType.isHtmlFile, fileType.isMarkdownFile]);

  const { rightActions } = useEditorToolbarActions({
    isSvgFile: fileType.isSvgFile,
    isHtmlFile: fileType.isHtmlFile,
    isMarkdownFile: fileType.isMarkdownFile,
    markdownPreviewMode,
    relativeLineNumbersEnabled: settings.relativeLineNumbersEnabled,
    setMarkdownPreviewMode,
    onToggleRelativeLineNumbers: settings.onToggleRelativeLineNumbers,
    onOpenLivePreview: livePreview.openLivePreview,
    onOpenLivePreviewInSplit: livePreview.openLivePreviewInSplit,
    onFormatDocument: handleFormatDocument,
  });

  // EditorCore no longer takes `initOptions`. The EditorView lifecycle is
  // owned here in EditorPane via `useCMView`; EditorCore only handles the
  // memoized DOM container plus compartment reconfiguration.

  const editorCoreReconfigureOptions = useMemo(
    () => ({
      buffer,
      compartments,
      hotkeys,
      keymapsRef,
      editorFontSize: settings.editorFontSize,
      editorTabSize: settings.editorTabSize,
      editorUsesTabs: settings.editorUsesTabs,
      wordWrapEnabled: settings.wordWrapEnabled,
      minimapEnabled: settings.minimapEnabled,
      relativeLineNumbersEnabled: settings.relativeLineNumbersEnabled,
      whitespaceRenderingMode,
      inlayHintsEnabled: settings.inlayHintsEnabled,
      signatureHelpEnabled: settings.signatureHelpEnabled,
    }),
    [
      buffer,
      compartments,
      hotkeys,
      settings.editorFontSize,
      settings.editorTabSize,
      settings.editorUsesTabs,
      settings.wordWrapEnabled,
      settings.minimapEnabled,
      settings.relativeLineNumbersEnabled,
      whitespaceRenderingMode,
      settings.inlayHintsEnabled,
      settings.signatureHelpEnabled,
    ],
  );

  if (!buffer || !buffer.file || buffer.file.isDir) {
    return <WelcomeTab onOpenCommandPalette={onOpenCommandPalette} />;
  }

  // Hoisted hooks (e.g. editorCoreReconfigureOptions) must stay above these
  // early returns; otherwise react-hooks/rules-of-hooks fires when a hook
  // would be skipped by an early return on a subsequent render.

  if (fileType.isImage && buffer) {
    return (
      <div className="editor-pane">
        <ImageViewer filePath={buffer.file.path} fileName={buffer.file.name} fileSize={buffer.file.size} />
      </div>
    );
  }

  if ((fileType.isAudio || fileType.isVideo) && buffer) {
    return (
      <MediaViewer
        filePath={buffer.file.path}
        fileName={buffer.file.name}
        fileSize={buffer.file.size}
        mediaType={fileType.isAudio ? 'audio' : 'video'}
      />
    );
  }

  if (fileType.isBinary && buffer) {
    return <BinaryFileViewer fileName={buffer.file.name} filePath={buffer.file.path} fileSize={buffer.file.size} />;
  }

  if (fileType.isSvgPreviewBuffer || fileType.isHtmlPreviewBuffer) {
    return (
      <div className="editor-pane">
        <EditorToolbar saving={false} />
        <LivePreview
          content={buffer?.content || ''}
          language={(buffer?.metadata?.previewKind as 'svg' | 'html') || 'html'}
          fileName={(buffer?.metadata?.sourceName || buffer?.file?.name) as string}
          onContentChange={(newContent) => {
            if (buffer) updateBufferContent(buffer.id, newContent);
          }}
        />
      </div>
    );
  }

  return (
    <div className="editor-pane" data-testid="editor-pane">
      <EditorToolbar
        saving={saving}
        breadcrumbProps={{
          filePath: buffer.file.path,
          onNavigate: (path) => {
            window.dispatchEvent(
              new CustomEvent('sprout:reveal-in-explorer', {
                detail: { path },
              }),
            );
          },
          symbols: enclosingSymbols,
          onNavigateToSymbol: (line) => {
            semantic.handleGoToLine(line);
          },
        }}
        rightActions={rightActions}
      />
      <GoToWorkspaceSymbolOverlay
        visible={semantic.showGoToWorkspaceSymbol}
        onSelectSymbol={semantic.handleSelectWorkspaceSymbol}
        onClose={() => {
          semantic.setShowGoToWorkspaceSymbol(false);
          viewRef.current?.focus();
        }}
      />
      <FindAllReferencesOverlay
        visible={semantic.showFindRefs}
        symbolName={semantic.refsSymbolName}
        references={semantic.refsResults}
        onSelectReference={semantic.handleSelectReference}
        onClose={() => {
          semantic.setShowFindRefs(false);
          viewRef.current?.focus();
        }}
      />

      <EditorCore
        editorRef={editorRef}
        viewRef={viewRef}
        reconfigureOptions={editorCoreReconfigureOptions}
        loading={loading}
        error={error}
        onContextMenu={contextMenu.handleEditorContextMenu}
        markdownPreviewMode={markdownPreviewMode}
        isMarkdownFile={fileType.isMarkdownFile}
        localContent={localContent}
        markdownPreviewBodyRef={markdownPreviewBodyRef}
      />

      <EditorPaneFooter
        buffer={buffer}
        selectionInfo={selectionInfo}
        whitespaceRenderingMode={whitespaceRenderingMode}
        settings={settings}
        lsp={lsp}
        setWhitespaceRenderingMode={setWhitespaceRenderingMode}
      />

      <EditorContextMenu contextMenu={contextMenu} isSemanticLanguage={isSemanticLanguage} />
    </div>
  );
}

export default EditorPane;
