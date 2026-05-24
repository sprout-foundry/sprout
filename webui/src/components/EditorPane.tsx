import type { EditorView as CMEditorView } from '@codemirror/view';
import { useRef, useState, useMemo, useCallback } from 'react';
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

interface EditorPaneProps {
  paneId: string;
  onOpenCommandPalette?: () => void;
}

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
  // externally (e.g., from the footer or sidebar).
  settings.whitespaceRenderingModeRef.current = whitespaceRenderingMode;

  const { fetchDiagnosticsRef, isSemanticLanguage } = useEditorDiagnostics(viewRef, buffer);

  // Shared ref that tracks whether an external (non-user) content update is in flight.
  // This is passed to useEditorCursor to guard against saving cursor positions
  // during external replacements (file reloads, auto-reload, initial loads).
  const isExternalUpdateRef = useRef<boolean>(false);

  const { selectionInfo, setSelectionInfo, handleCursorUpdate } = useEditorCursor({
    bufferRef,
    updateBufferCursor,
    isExternalUpdateRef,
  });

  const { handleSave, saveRef } = useEditorFileIO(
    viewRef,
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
    isExternalUpdateRef,
  );

  const { handleScrollUpdate, cancelPendingFlush } = useEditorScrollSync({
    paneId,
    viewRef,
    bufferRef,
    filePath: buffer?.file?.path,
    updateBufferScroll,
    isLinkedScrollEnabled,
  });

  const { localContentRef, onUpdate } = useEditorUpdate({
    bufferRef,
    localContent,
    setLocalContent,
    isExternalUpdateRef,
    fetchDiagnosticsRef,
    handleCursorUpdate,
    handleScrollUpdate,
    updateBufferContent,
    setBufferModified,
  });

  // Wrap onUpdate in a ref so the init effect doesn't re-run on every keystroke.
  // onUpdate's identity changes whenever localContent changes (it's in the dep array
  // of useEditorUpdate), which would destroy/recreate the EditorView and reset
  // scroll position, losing user focus. Using a ref stabilizes the dependency.
  const onUpdateRef = useRef(onUpdate);
  onUpdateRef.current = onUpdate;

  const { semanticHandlerRefs, buildKeymaps } = useEditorKeymaps(hotkeys, viewRef, bufferRef);

  const semantic = useEditorSemantic(viewRef, bufferRef, localContent, isSemanticLanguage, openWorkspaceBuffer);

  semanticHandlerRefs.handleGoToDefinition.current = semantic.handleGoToDefinition;
  semanticHandlerRefs.handleFindAllReferences.current = semantic.handleFindAllReferences;

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

  // Stable refs for keymaps, settings, and actions — passed to hooks so that
  // the dependency arrays don't change on every render, which would destroy
  // and recreate the EditorView (breaking editing and resetting scroll).
  const keymapsRef = useRef(keymaps);
  keymapsRef.current = keymaps;

  const settingsRef = useRef({
    wordWrapEnabled: settings.wordWrapEnabled,
    relativeLineNumbersEnabled: settings.relativeLineNumbersEnabled,
    minimapEnabled: settings.minimapEnabled,
    editorFontSize: settings.editorFontSize,
    editorTabSize: settings.editorTabSize,
    editorUsesTabs: settings.editorUsesTabs,
    whitespaceRenderingMode: settings.whitespaceRenderingMode,
    inlayHintsEnabled: settings.inlayHintsEnabled,
    signatureHelpEnabled: settings.signatureHelpEnabled,
  });
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

  const actionsRef = useRef({ getSaveFn: () => saveRef.current });
  actionsRef.current = { getSaveFn: () => saveRef.current };

  useEditorEvents({
    viewRef,
    bufferRef,
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

  const handleToggleFormatOnSave = useCallback(() => {
    setFormatOnSaveEnabled(!isFormatOnSaveEnabled);
  }, [isFormatOnSaveEnabled, setFormatOnSaveEnabled]);

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
    formatOnSaveEnabled: isFormatOnSaveEnabled,
    onToggleFormatOnSave: handleToggleFormatOnSave,
  });

  if (!buffer || !buffer.file || buffer.file.isDir) {
    return <WelcomeTab onOpenCommandPalette={onOpenCommandPalette} />;
  }

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
        <EditorToolbar onSave={handleSave} saving={false} showSave={false} />
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

  const editorCoreInitOptions = useMemo(() => ({
    paneId,
    buffer,
    localContent,
    compartments,
    buildExtensions,
    themePack,
    customHighlightStyle,
    keymapsRef,
    localContentRef,
    openWorkspaceBuffer,
    onCancelPendingFlush: cancelPendingFlush,
    onUpdateRef,
    settingsRef,
    actionsRef,
  }), [paneId, buffer, localContent, compartments, buildExtensions, themePack, customHighlightStyle, openWorkspaceBuffer, cancelPendingFlush, onUpdateRef, settingsRef, actionsRef]);

  const editorCoreReconfigureOptions = useMemo(() => ({
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
  }), [buffer, compartments, hotkeys, settings.editorFontSize, settings.editorTabSize, settings.editorUsesTabs, settings.wordWrapEnabled, settings.minimapEnabled, settings.relativeLineNumbersEnabled, whitespaceRenderingMode, settings.inlayHintsEnabled, settings.signatureHelpEnabled]);

  return (
    <div className="editor-pane">
      <EditorToolbar

        onSave={handleSave}
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
        initOptions={editorCoreInitOptions}
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
