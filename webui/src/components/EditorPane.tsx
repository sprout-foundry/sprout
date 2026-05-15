import { useRef, useState, useMemo, useCallback } from 'react';
import type { EditorView as CMEditorView } from '@codemirror/view';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import { useEditorExtensions } from '../hooks/useEditorExtensions';
import { useEditorDiagnostics } from '../hooks/useEditorDiagnostics';
import { useEditorFileIO } from '../hooks/useEditorFileIO';
import { useEditorCursor } from '../hooks/useEditorCursor';
import { useEditorScrollSync } from '../hooks/useEditorScrollSync';
import { useEditorSymbols } from '../hooks/useEditorSymbols';
import { useEditorSettings } from '../hooks/useEditorSettings';
import { useEditorKeymaps } from '../hooks/useEditorKeymaps';
import { useEditorEvents } from '../hooks/useEditorEvents';
import { useEditorSemantic } from '../hooks/useEditorSemantic';
import { useEditorContextMenu } from '../hooks/useEditorContextMenu';
import { useEditorLSP } from '../hooks/useEditorLSP';
import { useEditorFileType } from '../hooks/useEditorFileType';
import { useEditorUpdate } from '../hooks/useEditorUpdate';
import { useLivePreview } from '../hooks/useLivePreview';
import { useEditorViewInit } from '../hooks/useEditorViewInit';

import { useEditorReconfigure } from './useEditorReconfigure';
import { useEditorToolbarActions } from './useEditorToolbarActions';
import type { EditorBuffer } from '../types/editor';
import LivePreview from './LivePreview';
import MarkdownPreview from './MarkdownPreview';
import EditorToolbar from './EditorToolbar';
import ImageViewer from './ImageViewer';
import GoToWorkspaceSymbolOverlay from './GoToWorkspaceSymbolOverlay';
import FindAllReferencesOverlay from './FindAllReferencesOverlay';
import EditorPaneFooter from './EditorPaneFooter';
import EditorContextMenu from './EditorContextMenu';
import WelcomeTab from './WelcomeTab';
import BinaryFileViewer from './BinaryFileViewer';
import MediaViewer from './MediaViewer';
import { AlertTriangle } from 'lucide-react';
import { Skeleton } from '@sprout/ui';
import './EditorPane.css';

interface EditorPaneProps {
  paneId: string;
  onOpenCommandPalette?: () => void;
}

function EditorPane({ paneId, onOpenCommandPalette }: EditorPaneProps): JSX.Element {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<CMEditorView | null>(null);
  const lastInitLanguageKey = useRef<string | null>(null);
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

  useEditorViewInit({
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
    keymapsRef,
    localContentRef,
    openWorkspaceBuffer,
    onCancelPendingFlush: cancelPendingFlush,
    onUpdateRef,
    settingsRef,
    actionsRef,
  });

  useEditorReconfigure({
    viewRef,
    buffer,
    lastInitLanguageKey,
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
  });

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

      {loading && (
        <div className="editor-skeleton" role="status" aria-label="Loading file">
          <div className="editor-skeleton-line-numbers">
            {Array.from({ length: 25 }, (_, i) => (
              <Skeleton key={i} width="32px" height="14px" />
            ))}
          </div>
          <div className="editor-skeleton-content">
            {Array.from({ length: 25 }, (_, i) => (
              <Skeleton key={i} width={`${40 + Math.floor((i * 53) % 60)}%`} height="14px" />
            ))}
          </div>
          <span className="sr-only">Loading file...</span>
        </div>
      )}

      {error && (
        <div className="error-message">
          <AlertTriangle size={16} className="error-icon" />
          <span className="error-text">{error}</span>
        </div>
      )}

      <div className={`pane-content-wrapper${markdownPreviewMode === 'split' ? ' pane-content-wrapper-md-split' : ''}`}>
        {fileType.isMarkdownFile && markdownPreviewMode === 'preview' ? (
          <div className="pane-content pane-content-md-preview-full">
            <MarkdownPreview content={localContent} scrollRef={markdownPreviewBodyRef} />
          </div>
        ) : (
          <>
            <div
              className={`pane-content${markdownPreviewMode === 'split' ? ' pane-content-md-editor-side' : ''}`}
              onContextMenu={contextMenu.handleEditorContextMenu}
            >
              <div ref={editorRef} className="editor" />
            </div>
            {markdownPreviewMode === 'split' && (
              <div className="pane-md-preview-split">
                <MarkdownPreview content={localContent} scrollRef={markdownPreviewBodyRef} />
              </div>
            )}
          </>
        )}
      </div>

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
