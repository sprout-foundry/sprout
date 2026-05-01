import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import {
  EditorView,
  keymap,
  type KeyBinding,
  lineNumbers,
} from '@codemirror/view';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { EditorState, Transaction } from '@codemirror/state';
import { undo, redo } from '@codemirror/commands';
import { searchKeymap, openSearchPanel, replaceAll } from '@codemirror/search';
import { indentUnit } from '@codemirror/language';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import { useEditorExtensions, TAB_SIZE_TABS_MODE, TAB_SIZE_DEFAULT } from '../hooks/useEditorExtensions';
import { useEditorDiagnostics } from '../hooks/useEditorDiagnostics';
import { useEditorFileIO } from '../hooks/useEditorFileIO';
import { useEditorScrollSync } from '../hooks/useEditorScrollSync';
import { useEditorSymbols, type BreadcrumbSymbol } from '../hooks/useEditorSymbols';
import type { EditorBuffer } from '../types/editor';
import LivePreview from './LivePreview';
import MarkdownPreview from './MarkdownPreview';
import EditorToolbar from './EditorToolbar';
import { isImageFile, isAudioFile, isVideoFile, isBinaryFile } from '../utils/mediaPatterns';
import ImageViewer from './ImageViewer';
import SvgPreview from './SvgPreview';
import GoToWorkspaceSymbolOverlay from './GoToWorkspaceSymbolOverlay';
import FindAllReferencesOverlay from './FindAllReferencesOverlay';
import type { ReferenceInfo } from './FindAllReferencesOverlay';
import LanguageSwitcher from './LanguageSwitcher';
import BinaryFileViewer from './BinaryFileViewer';
import MediaViewer from './MediaViewer';
import { getEditorKeymap } from '../utils/editorHotkeys';
import './EditorPane.css';
import { codeActionsKeybinding } from '../extensions/codeActions';
import { getLanguageExtensions, resolveLanguageId } from '../extensions/languageRegistry';
import { triggerRename } from '../extensions/renameOverlay';
import type { LineEnding } from '../extensions/lineEndingDetect';
import {
  buildEmmetExtensions,
} from '../extensions/emmet';
import {
  buildAutoCloseTagExtensions,
} from '../extensions/autoCloseTag';
import { minimapExtension } from '../extensions/minimap';
import { setSnippetLanguage } from '../extensions/snippets';
import { whitespaceRenderingPlugin, type WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { ApiService } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { formatCode, formatCodeWithConfigDiscovery, setConfigFetcher } from '../services/formatter';
import { getLSPClientService, LSP_SUPPORTED_LANGUAGES, type LSPConnectionState } from '../services/lspClientService';
import { buildLSPPluginExtensions, lspSyncOnDocChange, setGlobalDisplayFileCallback, type DisplayFileCallback, registerEditorView, unregisterEditorView } from '../extensions/lspExtensions';

// LSP commands from @codemirror/lsp-client for keybinding integration
import { jumpToDefinition, findReferences, renameSymbol } from '@codemirror/lsp-client';

import { Loader2, AlertTriangle, Eye, Columns2, Copy, Navigation, FolderOpen, ClipboardCopy, ListOrdered } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';
import { debugLog, warn } from '../utils/log';
import ContextMenu from './ContextMenu';
import WelcomeTab from './WelcomeTab';

/** Track if the global displayFile callback has been registered */
let globalDisplayFileRegistered = false;

interface EditorPaneProps {
  paneId: string;
  onOpenCommandPalette?: () => void;
}

// Font size constants
const FONT_SIZE_MIN = 8;       // Minimum legible font size
const FONT_SIZE_DEFAULT = 13;  // Default matches Monaco/Menlo editor defaults
const FONT_SIZE_MAX = 72;      // Maximum for accessibility (WCAG supports 200% zoom)

// Tab size constants (TAB_SIZE_TABS_MODE and TAB_SIZE_DEFAULT imported from useEditorExtensions)
const TAB_SIZE_OPTIONS = [2, 4, 8] as const;

function EditorPane({ paneId, onOpenCommandPalette }: EditorPaneProps): JSX.Element {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const { compartments, buildExtensions } = useEditorExtensions();
  const lastInitLanguageKey = useRef<string | null>(null);

  const [wordWrapEnabled, setWordWrapEnabled] = useState(true);
  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');
  const [selectionInfo, setSelectionInfo] = useState<{ charCount: number; selectionCount: number } | null>(null);
  const [showGoToWorkspaceSymbol, setShowGoToWorkspaceSymbol] = useState<boolean>(false);

  // Find All References state
  const [showFindRefs, setShowFindRefs] = useState<boolean>(false);
  const [refsSymbolName, setRefsSymbolName] = useState<string>('');
  const [refsResults, setRefsResults] = useState<ReferenceInfo[]>([]);
  const [refsLoading, setRefsLoading] = useState<boolean>(false);

  // LSP connection status for footer indicator
  const [lspState, setLspState] = useState<LSPConnectionState>('disconnected');
  const [lspLanguage, setLspLanguage] = useState<string | null>(null);

  const [relativeLineNumbersEnabled, setRelativeLineNumbersEnabled] = useState(() => {
    try {
      const stored = localStorage.getItem('editor:relative-line-numbers-enabled');
      return stored !== null ? stored === 'true' : false; // default off
    } catch (err) {
      debugLog('Failed to read relative line numbers setting from localStorage:', err);
      return false; // default off if localStorage unavailable
    }
  });
  const [minimapEnabled, setMinimapEnabled] = useState(() => {
    try {
      const stored = localStorage.getItem('editor:minimap-enabled');
      return stored !== null ? stored === 'true' : true; // default on
    } catch (err) {
      debugLog('Failed to read minimap setting from localStorage:', err);
      return true; // default on if localStorage unavailable
    }
  });

  const [editorFontSize, setEditorFontSize] = useState<number>(() => {
    try {
      const stored = localStorage.getItem('editor:font-size');
      if (stored === null) return FONT_SIZE_DEFAULT;
      const parsed = parseInt(stored, 10);
      // Validate: must be a number and within acceptable range
      if (!isNaN(parsed) && parsed >= FONT_SIZE_MIN && parsed <= FONT_SIZE_MAX) {
        return parsed;
      }
      return FONT_SIZE_DEFAULT; // default if invalid
    } catch (err) {
      debugLog('Failed to read font size from localStorage:', err);
      return FONT_SIZE_DEFAULT; // default if localStorage unavailable
    }
  });

  const [editorTabSize, setEditorTabSize] = useState<number>(() => {
    try {
      const stored = localStorage.getItem('editor:tab-size');
      if (stored === null) return TAB_SIZE_DEFAULT;
      // '0' means tabs mode
      if (stored === '0') return TAB_SIZE_TABS_MODE;
      const parsed = parseInt(stored, 10);
      if (!isNaN(parsed) && TAB_SIZE_OPTIONS.includes(parsed as typeof TAB_SIZE_OPTIONS[number])) {
        return parsed;
      }
      return TAB_SIZE_DEFAULT;
    } catch (err) {
      debugLog('Failed to read tab size from localStorage:', err);
      return TAB_SIZE_DEFAULT;
    }
  });

  // Whether the current file uses tabs for indentation (auto-detected on load)
  const [editorUsesTabs, setEditorUsesTabs] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem('editor:tab-size');
      return stored === '0';
    } catch (err) {
      return false;
    }
  });

  // Whether the user has manually overridden the indent setting via the footer cycle.
  // Resets when switching files so auto-detection runs fresh for each file.
  const [indentManuallySet, setIndentManuallySet] = useState<boolean>(false);

  // Line ending style for the current file (auto-detected on load)
  const [lineEnding, setLineEnding] = useState<LineEnding>('LF');

  // Markdown preview mode for .md files
  const [markdownPreviewMode, setMarkdownPreviewMode] = useState<'off' | 'split' | 'preview'>('off');
  const markdownPreviewBodyRef = useRef<HTMLDivElement>(null);

  // Ref mirror for indentManuallySet — read inside loadFile and auto-reload handler
  // to avoid stale closures (those callbacks cannot list indentManuallySet as a dep).
  const indentManuallySetRef = useRef(false);
  // Keep the ref in sync whenever the state value changes.
  useEffect(() => { indentManuallySetRef.current = indentManuallySet; }, [indentManuallySet]);

  // Clean up orphaned localStorage key from previous versions that persisted
  // indentManuallySet globally. Run once on mount.
  useEffect(() => {
    try { localStorage.removeItem('editor:indent-manual'); } catch (_err) { /* ignore */ }
  }, []);

  // Context menu state
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; hasSelection: boolean; languageId: string } | null>(null);
  const [workspaceRoot, setWorkspaceRoot] = useState<string>('');

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
  } = useEditorManager();

  const { theme, themePack, customHighlightStyle } = useTheme();
  const { hotkeys } = useHotkeys();

  // Get buffer for this pane
  const pane = panes.find((p) => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  // Diagnostics hook — encapsulates diagnostic fetching logic
  const { fetchDiagnostics, fetchDiagnosticsRef, isSemanticLanguage } = useEditorDiagnostics(viewRef, buffer);

  // Ref mirror for buffer (avoids stale closures in hook callbacks)
  const bufferRef = useRef<EditorBuffer | null | undefined>(buffer);
  bufferRef.current = buffer;

  // File I/O hook — file load/save, external change detection, conflict resolution
  const {
    handleSave,
    saveRef,
    isExternalUpdateRef,
  } = useEditorFileIO(
    viewRef,
    buffer,
    bufferRef,
    { tabSize: compartments.tabSize },
    indentManuallySetRef,
    fetchDiagnosticsRef,
    paneId,
    {
      setLoading,
      setSaving,
      setError,
      setLocalContent,
      setSelectionInfo,
      setEditorTabSize,
      setEditorUsesTabs,
      setLineEnding,
    },
  );

  // Editor scroll sync hook — manages scroll position persistence and cross-pane linked scrolling
  const { handleScrollUpdate, cancelPendingFlush } = useEditorScrollSync({
    paneId,
    viewRef,
    bufferRef,
    filePath: buffer?.file?.path,
    updateBufferScroll,
    isLinkedScrollEnabled,
  });

  // Reset manual indent override when switching to a different file,
  // so auto-detection runs fresh for each file.
  const prevBufferIdForIndentRef = useRef<string | null>(null);
  useEffect(() => {
    const currentBufferId = buffer?.id ?? null;
    if (currentBufferId !== prevBufferIdForIndentRef.current) {
      prevBufferIdForIndentRef.current = currentBufferId;
      // Reset both the ref (synchronous — read by loadFile closure) and the
      // state (async — drives React re-renders / UI display).
      indentManuallySetRef.current = false;
      setIndentManuallySet(false);
      // Reset line ending to default; loadFile will re-detect for the new file.
      setLineEnding('LF');
    }
  }, [buffer?.id]);

  // API service instance (singleton)
  const apiService = useRef(ApiService.getInstance()).current;

  // Fetch workspace root on mount (for absolute path copy)
  useEffect(() => {
    apiService
      .getWorkspace()
      .then((ws) => {
        setWorkspaceRoot(ws.workspace_root || '');
      })
      .catch((err) => {
        warn(`Failed to fetch workspace root: ${err instanceof Error ? err.message : String(err)}`);
      });

    // Set up Prettier config fetcher for formatter service
    setConfigFetcher(async (filePath: string) => {
      return apiService.getPrettierConfig(filePath);
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Stable action references for hotkey keymap (used in both init and reconfigure effects)
  const hotkeyActionsRef = useRef<{
    onSave: () => void;
    onGoToLine: () => void;
    onGoToSymbol: () => void;
    onToggleWordWrap: () => void;
    onToggleRelativeLineNumbers: () => void;
  } | null>(null);

  // Go to specific line
  const handleGoToLine = useCallback((lineNum: number) => {
    if (!viewRef.current) return;

    // Use gotoLine command from CodeMirror commands
    const dispatch = viewRef.current;
    const state = dispatch.state;
    const doc = state.doc;

    // Cannot navigate in an empty document
    if (doc.lines === 0) return;

    // Convert line number (1-based) to position
    const line = Math.min(Math.max(lineNum - 1, 0), doc.lines - 1);
    const pos = doc.line(line + 1).from;

    dispatch.dispatch({
      selection: { anchor: pos, head: pos },
      scrollIntoView: true,
    });

    // Focus the editor after navigation
    dispatch.focus();
  }, []);

  // ── Context menu handlers ─────────────────────────────────────
  const hideContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  const handleEditorContextMenu = useCallback(
    (e: ReactMouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (!buffer || !buffer.file || buffer.file.isDir) return;
      if (buffer.kind !== 'file') return;
      const hasSelection =
        !!viewRef.current &&
        !viewRef.current.state.selection.main.empty;
      const langId =
        resolveLanguageId(
          buffer.languageOverride,
          buffer.file.ext?.replace(/^\./, ''),
          buffer.file.name,
        ).languageId ?? '';
      setContextMenu({ x: e.clientX, y: e.clientY, hasSelection, languageId: langId });
    },
    [buffer],
  );

  const handleCopySelection = useCallback(() => {
    if (!viewRef.current) return;
    const state = viewRef.current.state;
    const text = state.sliceDoc(state.selection.main.from, state.selection.main.to);
    copyToClipboard(text).catch((err) => {
      debugLog('Clipboard write failed for selection:', err);
    });
    hideContextMenu();
  }, [hideContextMenu]);

  const handleRevealInExplorer = useCallback(() => {
    if (!buffer || !buffer.file) return;
    window.dispatchEvent(
      new CustomEvent('sprout:reveal-in-explorer', {
        detail: { path: buffer.file.path },
      }),
    );
    hideContextMenu();
  }, [buffer, hideContextMenu]);

  const handleCopyRelativePath = useCallback(() => {
    if (!buffer || !buffer.file) return;
    copyToClipboard(buffer.file.path).catch((err) => {
      debugLog('Clipboard write failed for relative path:', err);
    });
    hideContextMenu();
  }, [buffer, hideContextMenu]);

  const handleCopyAbsolutePath = useCallback(() => {
    if (!buffer || !buffer.file) return;
    const root = workspaceRoot.replace(/\/+$/, '');
    copyToClipboard(`${root}/${buffer.file.path}`).catch((err) => {
      debugLog('Clipboard write failed for absolute path:', err);
    });
    hideContextMenu();
  }, [buffer, hideContextMenu, workspaceRoot]);
  // ──────────────────────────────────────────────────────────────

  // Zoom in/out: adjust font size and persist to localStorage
  const onZoomIn = useCallback(() => {
    setEditorFontSize((prev) => {
      const next = Math.min(prev + 1, FONT_SIZE_MAX);
      try {
        localStorage.setItem('editor:font-size', String(next));
      } catch (err) {
        debugLog('[onZoomIn] localStorage persist failed:', err);
      }
      viewRef.current?.dispatch({
        effects: compartments.fontSize.reconfigure([
          EditorView.theme({
            '&': {
              fontSize: `${next}px`,
            },
          }),
        ]),
      });
      return next;
    });
  }, []);

  const onZoomOut = useCallback(() => {
    setEditorFontSize((prev) => {
      const next = Math.max(prev - 1, FONT_SIZE_MIN);
      try {
        localStorage.setItem('editor:font-size', String(next));
      } catch (err) {
        debugLog('[onZoomOut] localStorage persist failed:', err);
      }
      viewRef.current?.dispatch({
        effects: compartments.fontSize.reconfigure([
          EditorView.theme({
            '&': {
              fontSize: `${next}px`,
            },
          }),
        ]),
      });
      return next;
    });
  }, []);

  const onResetZoom = useCallback(() => {
    setEditorFontSize(FONT_SIZE_DEFAULT);
    try {
      localStorage.setItem('editor:font-size', String(FONT_SIZE_DEFAULT));
    } catch (err) {
      debugLog('[onResetZoom] localStorage persist failed:', err);
    }
    viewRef.current?.dispatch({
      effects: compartments.fontSize.reconfigure([
        EditorView.theme({
          '&': {
            fontSize: `${FONT_SIZE_DEFAULT}px`,
          },
        }),
      ]),
    });
  }, []);

  // Cycle tab size: rotates through Spaces:2 → Spaces:4 → Spaces:8 → Tabs → Spaces:2 …
  // Each manual cycle marks the indent as user-chosen for this file, so auto-detection won't override it.
  // Resets when switching to a different file.
  const onCycleTabSize = useCallback(() => {
    // Mark that the user has manually chosen an indent setting for this file.
    // Write to both the ref (synchronous — read by loadFile closure) and the
    // state (drives React re-renders).
    indentManuallySetRef.current = true;
    setIndentManuallySet(true);

    setEditorTabSize((prev) => {
      // Cycle order: 2 → 4 → 8 → "tabs" (represented as TAB_SIZE_TABS_MODE) → 2 …
      if (prev === TAB_SIZE_TABS_MODE) {
        // Was tabs → cycle to spaces:2
        setEditorUsesTabs(false);
        try { localStorage.setItem('editor:tab-size', '2'); } catch (err) { debugLog('[onCycleTabSize] localStorage persist failed:', err); }
        viewRef.current?.dispatch({
          effects: compartments.tabSize.reconfigure([
            EditorState.tabSize.of(2),
            indentUnit.of('  '),
          ]),
        });
        return 2;
      }
      const currentIdx = TAB_SIZE_OPTIONS.indexOf(prev as typeof TAB_SIZE_OPTIONS[number]);
      if (currentIdx === TAB_SIZE_OPTIONS.length - 1) {
        // Last space option (8) → switch to tabs
        setEditorUsesTabs(true);
        try { localStorage.setItem('editor:tab-size', '0'); } catch (err) { debugLog('[onCycleTabSize] localStorage persist failed:', err); }
        viewRef.current?.dispatch({
          effects: compartments.tabSize.reconfigure([
            EditorState.tabSize.of(TAB_SIZE_DEFAULT),
            indentUnit.of('\t'),
          ]),
        });
        return TAB_SIZE_TABS_MODE;
      }
      const nextIdx = (currentIdx + 1) % TAB_SIZE_OPTIONS.length;
      const next = TAB_SIZE_OPTIONS[nextIdx];
      setEditorUsesTabs(false);
      try { localStorage.setItem('editor:tab-size', String(next)); } catch (err) { debugLog('[onCycleTabSize] localStorage persist failed:', err); }
      viewRef.current?.dispatch({
        effects: compartments.tabSize.reconfigure([
          EditorState.tabSize.of(next),
          indentUnit.of(' '.repeat(next)),
        ]),
      });
      return next;
    });
  }, []);

  // Ref to always read current buffer state without subscribing to identity changes.
  // This prevents handleGoToDefinition from changing identity on every buffer
  // update (e.g. scroll position changes), which would trigger a full
  // CodeMirror editor destroy/recreate cycle via the init effect's deps.
  const bufferStateRef = useRef<typeof buffer>(buffer);
  bufferStateRef.current = buffer;

  // Ref for localContent so handleGoToDefinition doesn't change identity
  // when the user types (which would destroy/recreate the editor).
  const localContentRef = useRef(localContent);
  localContentRef.current = localContent;

  const handleGoToDefinition = useCallback(async () => {
    const buf = bufferStateRef.current;
    if (!viewRef.current || !buf || buf.kind !== 'file' || !buf.file || buf.file.path.startsWith('__workspace/')) {
      return;
    }

    const languageId = resolveLanguageId(
      buf.languageOverride,
      buf.file.ext?.replace(/^\./, ''),
      buf.file.name,
    ).languageId ?? '';
    if (!isSemanticLanguage(languageId)) {
      notificationBus.notify('info', 'Go to Definition', 'Semantic definition is currently available for TypeScript/JavaScript and Go files.');
      return;
    }

    const selection = viewRef.current.state.selection.main;
    const lineInfo = viewRef.current.state.doc.lineAt(selection.head);
    const line = lineInfo.number;
    const column = selection.head - lineInfo.from + 1;

    try {
      const result = await apiService.getSemanticDefinition(buf.file.path, localContentRef.current, languageId, line, column);
      if (!result.capabilities?.definition) {
        notificationBus.notify('warning', 'Go to Definition', 'Semantic engine is not available for this language in this environment.');
        return;
      }

      const def = result.definition;
      if (!def || !def.path) {
        notificationBus.notify('info', 'Go to Definition', 'No definition found at cursor.');
        return;
      }

      if (def.path === buf.file.path) {
        handleGoToLine(def.line);
        return;
      }

      const fileName = def.path.split('/').pop() || def.path;
      const dotIndex = fileName.lastIndexOf('.');
      const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

      openWorkspaceBuffer({
        kind: 'file',
        path: def.path,
        title: fileName,
        ext,
      });

      requestAnimationFrame(() => {
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line: def.line } }));
      });
    } catch (err) {
      debugLog('[EditorPane] Go to definition failed:', err);
      notificationBus.notify('warning', 'Go to Definition', 'Failed to resolve definition.');
    }
  }, [apiService, openWorkspaceBuffer, handleGoToLine]);

  const handleGoToDefinitionFromMenu = useCallback(() => {
    hideContextMenu();
    void handleGoToDefinition();
  }, [hideContextMenu, handleGoToDefinition]);

  const handleFindAllReferences = useCallback(async () => {
    const buf = bufferStateRef.current;
    if (!viewRef.current || !buf || buf.kind !== 'file' || !buf.file || buf.file.path.startsWith('__workspace/')) {
      return;
    }

    const languageId = resolveLanguageId(
      buf.languageOverride,
      buf.file.ext?.replace(/^\./, ''),
      buf.file.name,
    ).languageId ?? '';
    if (!isSemanticLanguage(languageId)) {
      notificationBus.notify('info', 'Find All References', 'Semantic references are currently available for TypeScript/JavaScript and Go files.');
      return;
    }

    const selection = viewRef.current.state.selection.main;
    const lineInfo = viewRef.current.state.doc.lineAt(selection.head);
    const line = lineInfo.number;
    const column = selection.head - lineInfo.from + 1;

    setShowFindRefs(true);
    setRefsLoading(true);
    setRefsSymbolName('');
    setRefsResults([]);

    try {
      const result = await apiService.getSemanticReferences(buf.file.path, localContentRef.current, languageId, line, column);
      setRefsLoading(false);

      if (!result.capabilities?.references) {
        notificationBus.notify('warning', 'Find All References', 'Semantic references are not available for this language in this environment.');
        setShowFindRefs(false);
        return;
      }

      if (result.error || !result.references?.locations?.length) {
        setRefsResults([]);
        setRefsSymbolName('');
        return;
      }

      setRefsSymbolName(result.references.symbolName || '');
      setRefsResults(result.references.locations);
    } catch (err) {
      debugLog('[EditorPane] Find all references failed:', err);
      setRefsLoading(false);
      notificationBus.notify('warning', 'Find All References', 'Failed to find references.');
      setShowFindRefs(false);
    }
  }, [apiService]);

  const handleFindAllReferencesFromMenu = useCallback(() => {
    hideContextMenu();
    void handleFindAllReferences();
  }, [hideContextMenu, handleFindAllReferences]);

  const handleSelectReference = useCallback((filePath: string, line: number) => {
    const buf = bufferStateRef.current;
    if (!buf) return;

    if (filePath === buf.file.path) {
      handleGoToLine(line);
      viewRef.current?.focus();
      return;
    }

    const fileName = filePath.split('/').pop() || filePath;
    const dotIndex = fileName.lastIndexOf('.');
    const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

    openWorkspaceBuffer({
      kind: 'file',
      path: filePath,
      title: fileName,
      ext,
    });

    requestAnimationFrame(() => {
      document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
    });
  }, [handleGoToLine, openWorkspaceBuffer]);

  const handleSelectWorkspaceSymbol = useCallback((filePath: string, line?: number) => {
    const fileName = filePath.split('/').pop() || filePath;
    const dotIndex = fileName.lastIndexOf('.');
    const ext = dotIndex >= 0 ? fileName.slice(dotIndex) : undefined;

    openWorkspaceBuffer({
      kind: 'file',
      path: filePath,
      title: fileName,
      ext,
    });

    if (line !== undefined && line !== null) {
      requestAnimationFrame(() => {
        document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
      });
    }
  }, [openWorkspaceBuffer]);

  // Initialize CodeMirror editor
  useEffect(() => {
    if (!editorRef.current) return;

    const updateListener = EditorView.updateListener.of((update) => {
      // Update cursor position on ANY selection change (cursor moves, clicks, typing)
      if (buffer && update.selectionSet) {
        try {
          const selection = update.state.selection.main;
          if (selection) {
            const line = update.state.doc.lineAt(selection.head).number;
            const column = selection.head - update.state.doc.line(selection.head).from;
            updateBufferCursor(buffer.id, { line, column });
          }
        } catch (err) {
          debugLog('Cursor position update skipped:', err);
        }
      }

      // Update selection info on selection change
      if (update.selectionSet) {
        const sel = update.state.selection;
        const ranges = sel.ranges;
        if (ranges.length > 1) {
          // Multiple selections — show count and total chars
          const totalChars = ranges.reduce((sum, range) => sum + (range.to - range.from), 0);
          setSelectionInfo({ charCount: totalChars, selectionCount: ranges.length });
        } else if (ranges.length === 1 && !ranges[0].empty) {
          // Single non-empty selection — show character count
          const charCount = ranges[0].to - ranges[0].from;
          setSelectionInfo({ charCount, selectionCount: 1 });
        } else {
          // No selection (just a cursor)
          setSelectionInfo(null);
        }
      }

      if (update.docChanged && !isExternalUpdateRef.current) {
        const newContent = update.state.doc.toString();
        // Only update localContent if this is a user edit (content differs from localContent)
        // This prevents infinite loop with external content loading
        if (newContent !== localContent) {
          setLocalContent(newContent);
        }
        if (buffer) {
          updateBufferContent(buffer.id, newContent);
          setBufferModified(buffer.id, newContent !== buffer.originalContent);
        }

        // Debounced: fetch diagnostics for the edited content
        if (buffer && buffer.kind === 'file' && buffer.file && !buffer.file.path.startsWith('__workspace/')) {
          fetchDiagnostics(buffer.file.path, newContent);
        }
      }

      // Track scroll position changes for layout persistence (delegated to useEditorScrollSync hook)
      handleScrollUpdate(update);
    });

    const customKeymap = getEditorKeymap(hotkeys, {
      onSave: () => {
        handleSave();
      },
      onGoToLine: () => {
        const event = new CustomEvent('editor-goto-line');
        document.dispatchEvent(event);
      },
      onGoToSymbol: () => {
        window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_goto_symbol' } }));
      },
      onGoToWorkspaceSymbol: () => {
        setShowGoToWorkspaceSymbol(true);
      },
      onToggleWordWrap: () => {
        // Dispatch globally so all editor panes toggle together
        // (consistent with the toolbar button and command palette paths).
        // NOTE: onToggleWordWrap MUST remain stable (empty useCallback deps).
        // It accesses state only via refs to avoid stale closures in this
        // keymap, which is captured once during editor init.
        document.dispatchEvent(new CustomEvent('editor-toggle-word-wrap'));
      },
      onToggleRelativeLineNumbers: () => {
        // Dispatch globally so all editor panes toggle together
        // (consistent with the toolbar button and command palette paths).
        document.dispatchEvent(new CustomEvent('editor-toggle-relative-line-numbers'));
      },
    });

    // Ctrl+H / Cmd+H: open search panel and focus the replace input field.
    // The standard searchKeymap only binds Mod-f (find). This extra binding
    // provides VS Code-style Ctrl+H to jump straight into replace mode.
    //
    // NOTE: In read-only mode, the replace fields are not rendered by the
    // search extension (@codemirror/search SearchPanel constructor), so
    // the focus-shift to the replace input silently no-ops. Ctrl+H falls
    // back to opening the find panel — identical to Ctrl+F behavior.
    const replacePanelKeymap: KeyBinding[] = [
      {
        key: 'Mod-h',
        preventDefault: true,
        run: (view: EditorView) => {
          openSearchPanel(view);
          // After opening the panel, focus the replace input field.
          // The panel is rendered asynchronously, so we use requestAnimationFrame.
          requestAnimationFrame(() => {
            const replaceInput = view.dom.querySelector<HTMLInputElement>('.cm-search input[name="replace"]');
            if (replaceInput) {
              replaceInput.focus();
              replaceInput.select();
            }
          });
          return true;
        },
      },
      // replaceNext is NOT bound here — the built-in SearchPanel's keydown
      // handler already maps Enter (in the replace input) → replaceNext.
      // Replace All bound to Ctrl+Alt+Enter within the search panel scope.
      {
        key: 'Mod-Alt-Enter',
        preventDefault: true,
        run: replaceAll,
        scope: 'search-panel',
      },
    ];

    // Zoom in/out keybindings: Mod+= to zoom in, Mod+- to zoom out, Mod-0 to reset
    // NOTE: onZoomIn/onZoomOut/onResetZoom use empty dependency arrays to prevent
    // editor destruction/recreation when this keymap is captured during
    // initialization. Do NOT add state dependencies to these callbacks.
    const zoomKeymap: KeyBinding[] = [
      {
        key: 'Mod-=',
        preventDefault: true,
        run: () => {
          onZoomIn();
          return true;
        },
      },
      {
        key: 'Mod--',
        preventDefault: true,
        run: () => {
          onZoomOut();
          return true;
        },
      },
      {
        key: 'Mod-0',
        preventDefault: true,
        run: () => {
          onResetZoom();
          return true;
        },
      },
    ];

    const semanticKeymap: KeyBinding[] = [
      {
        key: 'F12',
        preventDefault: true,
        run: (view) => {
          // Try LSP jumpToDefinition first - it returns true if it handled the action
          if (jumpToDefinition(view)) return true;
          // Fall back to old semantic API
          void handleGoToDefinition();
          return true;
        },
      },
      {
        key: 'F2',
        preventDefault: true,
        run: (view) => {
          // Try LSP rename first
          if (renameSymbol(view)) return true;
          // Fall back to old rename trigger
          if (viewRef.current) {
            triggerRename(viewRef.current, {
              getFilePath: () => bufferStateRef.current?.file?.path,
              getContent: () => localContentRef.current,
            });
          }
          return true;
        },
      },
      {
        key: 'Shift-F12',
        preventDefault: true,
        run: (view) => {
          // Try LSP findReferences first
          if (findReferences(view)) return true;
          // Fall back to old semantic API
          void handleFindAllReferences();
          return true;
        },
      },
      codeActionsKeybinding(),
    ];

    const resolvedLanguage = resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name);

    const extensions = buildExtensions({
      paneId,
      settings: {
        wordWrapEnabled,
        relativeLineNumbersEnabled,
        minimapEnabled,
        editorFontSize,
        editorTabSize,
        editorUsesTabs,
        whitespaceRenderingMode,
      },
      theme: { themePack, customHighlightStyle },
      buffer: {
        languageId: resolvedLanguage.languageId,
        getFilePath: () => buffer?.file?.path,
        getFileExt: () => buffer?.file?.ext,
        getContent: () => localContentRef.current,
      },
      actions: { getSaveFn: () => saveRef.current },
      hotkeysCompartmentExtension: compartments.hotkeys.of(keymap.of(customKeymap)),
      extraKeymaps: [
        keymap.of(searchKeymap),
        keymap.of(replacePanelKeymap),
        keymap.of(zoomKeymap),
        keymap.of(semanticKeymap),
      ],
    });

    // Prepend the updateListener (not in buildExtensions because it accesses
    // component-local state setters directly — the hook is stateless).
    extensions.unshift(updateListener);

    const state = EditorState.create({
      doc: localContent,
      extensions,
    });

    const view = new EditorView({
      state,
      parent: editorRef.current,
    });

    viewRef.current = view;

    // Register the editor view for cross-file LSP navigation
    const filePath = buffer?.file?.path;
    if (filePath && !filePath.startsWith('__workspace/')) {
      registerEditorView(filePath, view);
    }

    // Initialize LSP extensions asynchronously (after editor is ready).
    // Capture the view reference at creation time to avoid applying
    // extensions to a different editor if the user switches files
    // before the LSP client connects.
    if (resolvedLanguage.languageId && LSP_SUPPORTED_LANGUAGES.has(resolvedLanguage.languageId)) {
      const currentLangId = resolvedLanguage.languageId;
      const currentFilePath = buffer?.file?.path ?? '';
      const capturedView = view; // capture the specific view for this init

      // Track callback registration at module level to avoid duplication
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

          // The caller (patchWorkspaceDisplayFile) polls the EditorView registry
          // after this callback returns, so we just need to open the file here.
          // Return null — we don't need to find the view ourselves.
          return null;
        };
        setGlobalDisplayFileCallback(displayFileCb);
        debugLog('[LSP] DisplayFile callback set');
      }

      void (async () => {
        try {
          const lspService = getLSPClientService();
          await lspService.getStatus();
          const client = await lspService.getClientForLanguage(currentLangId);
          // Only apply if this editor is still active (not replaced by file switch)
          if (client && viewRef.current === capturedView && capturedView.dom?.isConnected) {
            const lspExtensions = [
              ...buildLSPPluginExtensions(client, currentFilePath, currentLangId),
              ...lspSyncOnDocChange(currentLangId),
            ];
            capturedView.dispatch({
              effects: compartments.lsp.reconfigure(lspExtensions),
            });
            debugLog('[LSP] Extensions activated for', currentLangId);
          }
        } catch (err) {
          debugLog('[LSP] Failed to initialize:', err);
        }
      })();
    }

    // Track which language was set during init so the reconfiguration effect
    // can skip a redundant reconfigure on the same buffer/language combo.
    lastInitLanguageKey.current = `${buffer?.id}:${buffer?.languageOverride ?? ''}:${buffer?.file?.ext ?? ''}:${buffer?.file?.name ?? ''}`;

    // Capture file path for cleanup
    const cleanupFilePath = buffer?.file?.path;

    return () => {
      cancelPendingFlush();
      // Unregister the editor view for cross-file LSP navigation
      if (cleanupFilePath && !cleanupFilePath.startsWith('__workspace/')) {
        unregisterEditorView(cleanupFilePath);
      }
      view.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- handleSave intentionally excluded to prevent infinite re-init loop when buffer changes
  }, [
    paneId,
    buffer?.id,
    buffer?.file?.ext,
    theme,
    themePack.id,
    customHighlightStyle,
    updateBufferContent,
    setBufferModified,
    updateBufferCursor,
    updateBufferScroll,
    handleGoToDefinition,
  ]);

  // Reconfigure the language compartment when the language override changes,
  // without requiring a full editor re-initialization.
  // A guard key prevents a redundant reconfigure on the same render cycle
  // where the init effect already set the correct language.
  useEffect(() => {
    const view = viewRef.current;
    if (!view || !buffer) return;

    const key = `${buffer.id}:${buffer.languageOverride ?? ''}:${buffer.file?.ext ?? ''}:${buffer.file?.name ?? ''}`;
    if (key === lastInitLanguageKey.current) return; // init already applied this language
    lastInitLanguageKey.current = key;

    const { languageId } = resolveLanguageId(
      buffer.languageOverride,
      buffer.file?.ext?.replace(/^\./, ''),
      buffer.file?.name,
    );

    // Clear LSP extensions when language changes, then asynchronously re-initialize LSP for the new language
    const lspService = getLSPClientService();
    const filePath = buffer.file?.path ?? '';

    view.dispatch({
      effects: [
        compartments.language.reconfigure(getLanguageExtensions(languageId)),
        compartments.emmet.reconfigure(buildEmmetExtensions(languageId)),
        compartments.autoCloseTag.reconfigure(buildAutoCloseTagExtensions(languageId)),
        compartments.lsp.reconfigure([]),
      ],
    });

    // Re-initialize LSP for the new language asynchronously
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
            debugLog('[LSP] Extensions reconfigured for', languageId, '(language change)');
          }
        } catch (err) {
          debugLog('[LSP] Failed to reconfigure:', err);
        }
      })();
    }
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]); // eslint-disable-line react-hooks/exhaustive-deps

  // Reconfigure the hotkey compartment when hotkeys change, without requiring
  // a full editor re-initialization. This prevents the undo/redo history from
  // being wiped when hotkeys are fetched from the API.
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    // Build the same action functions used in the init effect
    const actions = {
      onSave: () => {
        handleSave();
      },
      onGoToLine: () => {
        const event = new CustomEvent('editor-goto-line');
        document.dispatchEvent(event);
      },
      onGoToSymbol: () => {
        window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_goto_symbol' } }));
      },
      onGoToWorkspaceSymbol: () => {
        setShowGoToWorkspaceSymbol(true);
      },
      onToggleWordWrap: () => {
        document.dispatchEvent(new CustomEvent('editor-toggle-word-wrap'));
      },
      onToggleRelativeLineNumbers: () => {
        document.dispatchEvent(new CustomEvent('editor-toggle-relative-line-numbers'));
      },
    };

    // Keep the ref in sync for use in other effects
    hotkeyActionsRef.current = actions;

    view.dispatch({
      effects: compartments.hotkeys.reconfigure(
        keymap.of(getEditorKeymap(hotkeys, actions)),
      ),
    });
  }, [hotkeys]); // eslint-disable-line react-hooks/exhaustive-deps -- only depends on hotkeys

  // Keep the snippet expansion language in sync with the current buffer.
  // Reconfigures a per-view compartment so two panes showing files in
  // different languages don't interfere with each other.
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  // Toggle word wrap: updates React state (for toolbar button) and
  // dispatches a CodeMirror compartment reconfigure to apply/remove
  // EditorView.lineWrapping.  Uses a ref mirror to avoid stale closures
  // inside the CodeMirror keymap and event-listener callbacks.
  const wordWrapRef = useRef(wordWrapEnabled);
  const lastWrapToggleRef = useRef(0);
  const minimapEnabledRef = useRef(minimapEnabled);
  const onToggleWordWrap = useCallback(() => {
    const now = Date.now();
    if (now - lastWrapToggleRef.current < 100) return; // dedup: prevent double-toggle from global handler
    lastWrapToggleRef.current = now;
    const next = !wordWrapRef.current;
    wordWrapRef.current = next;
    setWordWrapEnabled(next);
    viewRef.current?.dispatch({
      effects: compartments.lineWrapping.reconfigure(next ? EditorView.lineWrapping : []),
    });
  }, []);

  // Toggle minimap: updates React state (for toolbar button) and
  // dispatches a CodeMirror compartment reconfigure to enable/disable
  // the minimap gutter extension.
  const lastMinimapToggleRef = useRef(0);
  const onToggleMinimap = useCallback(() => {
    const now = Date.now();
    if (now - lastMinimapToggleRef.current < 100) return; // dedup
    lastMinimapToggleRef.current = now;
    const next = !minimapEnabledRef.current;
    minimapEnabledRef.current = next;
    setMinimapEnabled(next);
    try {
      localStorage.setItem('editor:minimap-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleMinimap] localStorage persist failed:', err);
    }
    viewRef.current?.dispatch({
      effects: compartments.minimap.reconfigure(next ? minimapExtension() : []),
    });
  }, []);

  // Toggle relative line numbers: updates React state (for toolbar button) and
  // dispatches a CodeMirror compartment reconfigure to switch between absolute
  // and relative line numbers in the gutter.
  const relativeLineNumbersEnabledRef = useRef(relativeLineNumbersEnabled);
  const lastRelativeLineNumbersToggleRef = useRef(0);
  const onToggleRelativeLineNumbers = useCallback(() => {
    const now = Date.now();
    if (now - lastRelativeLineNumbersToggleRef.current < 100) return; // dedup
    lastRelativeLineNumbersToggleRef.current = now;
    const next = !relativeLineNumbersEnabledRef.current;
    relativeLineNumbersEnabledRef.current = next;
    setRelativeLineNumbersEnabled(next);
    try {
      localStorage.setItem('editor:relative-line-numbers-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleRelativeLineNumbers] localStorage persist failed:', err);
    }
    viewRef.current?.dispatch({
      effects: compartments.relativeLineNumbers.reconfigure(next ? lineNumbersRelative : lineNumbers()),
    });
  }, []);

  // Cycle whitespace rendering mode: none → boundary → all → none
  const whitespaceRenderingModeRef = useRef(whitespaceRenderingMode);
  const lastWhitespaceToggleRef = useRef(0);
  const onCycleWhitespaceRendering = useCallback(() => {
    const now = Date.now();
    if (now - lastWhitespaceToggleRef.current < 100) return; // dedup
    lastWhitespaceToggleRef.current = now;
    const next: WhitespaceRenderingMode =
      whitespaceRenderingModeRef.current === 'none' ? 'boundary' :
      whitespaceRenderingModeRef.current === 'boundary' ? 'all' : 'none';
    setWhitespaceRenderingMode(next);
    viewRef.current?.dispatch({
      effects: compartments.whitespaceRendering.reconfigure(
        whitespaceRenderingPlugin(next),
      ),
    });
  }, [setWhitespaceRenderingMode]);

  // Keep the ref mirror in sync whenever the state value changes from context
  // and reconfigure the CodeMirror compartment so the editor reflects the change.
  useEffect(() => {
    whitespaceRenderingModeRef.current = whitespaceRenderingMode;
    viewRef.current?.dispatch({
      effects: compartments.whitespaceRendering.reconfigure(
        whitespaceRenderingPlugin(whitespaceRenderingMode),
      ),
    });
  }, [whitespaceRenderingMode]);

  // Keep the ref mirror in sync whenever the state value changes from
  // an external source (e.g. the global event listener).
  useEffect(() => {
    wordWrapRef.current = wordWrapEnabled;
  }, [wordWrapEnabled]);

  useEffect(() => {
    minimapEnabledRef.current = minimapEnabled;
  }, [minimapEnabled]);

  useEffect(() => {
    relativeLineNumbersEnabledRef.current = relativeLineNumbersEnabled;
  }, [relativeLineNumbersEnabled]);

  // Listen for go to line event, global word-wrap toggle, and linked scroll toggle.
  // A small dedup guard prevents double-toggle if the same keyboard event is
  // handled by both the CodeMirror keymap AND the global HotkeyProvider (e.g.
  // if a user manually sets global:true on editor_toggle_word_wrap).
  useEffect(() => {
    const handler = (e: Event) => {
      if (e.type === 'editor-goto-line') {
        const customEvent = e as CustomEvent;
        if (customEvent.detail?.line) {
          handleGoToLine(customEvent.detail.line);
        }
      } else if (e.type === 'editor-toggle-word-wrap') {
        onToggleWordWrap();
      } else if (e.type === 'editor-toggle-linked-scroll') {
        toggleLinkedScroll();
      } else if (e.type === 'editor-toggle-minimap') {
        onToggleMinimap();
      } else if (e.type === 'editor-toggle-relative-line-numbers') {
        onToggleRelativeLineNumbers();
      } else if (e.type === 'editor-cycle-whitespace-rendering') {
        onCycleWhitespaceRendering();
      } else if (e.type === 'editor-undo') {
        if (viewRef.current) {
          undo(viewRef.current);
        }
      } else if (e.type === 'editor-redo') {
        if (viewRef.current) {
          redo(viewRef.current);
        }
      } else if (e.type === 'editor-find') {
        if (viewRef.current) {
          openSearchPanel(viewRef.current);
        }
      } else if (e.type === 'editor-find-replace') {
        if (viewRef.current) {
          openSearchPanel(viewRef.current);
          requestAnimationFrame(() => {
            const replaceInput = viewRef.current?.dom.querySelector<HTMLInputElement>('.cm-search input[name="replace"]');
            if (replaceInput) {
              replaceInput.focus();
              replaceInput.select();
            }
          });
        }
      } else if (e.type === 'editor-select-all') {
        if (viewRef.current) {
          viewRef.current.dispatch({
            selection: { anchor: 0, head: viewRef.current.state.doc.length },
          });
        }
      } else if (e.type === 'editor-format-document') {
        const currentBuffer = bufferRef.current;
        if (viewRef.current && currentBuffer) {
          const detail = (e as CustomEvent).detail as { requestId?: string; content?: string } | undefined;
          const requestId = detail?.requestId;
          // When called by format-on-save, the content to format is passed
          // explicitly in the event detail so each save operates on the
          // content it captured (prevents rapid-save race conditions).
          // Falls back to the editor's current state for manual format commands.
          const content = detail?.content ?? viewRef.current.state.doc.toString();
          const formatPromise = formatCodeWithConfigDiscovery(content, currentBuffer.file.path, currentBuffer.file.size);
          const capturedBufferId = currentBuffer.id;

          if (requestId) {
            // Called by format-on-save — resolve the pending promise when done
            formatPromise.then(result => {
              // Guard: if the buffer changed while formatting was in-flight,
              // discard the result so we don't apply stale content.
              if (bufferRef.current?.id !== capturedBufferId) {
                return;
              }
              const windowAny = window as unknown as Record<string, Map<string, (r: { formatted: string; error?: string }) => void>>;
              const resolveMap = windowAny.__formatResolveMap;
              // Check if the request was already consumed (timed out).
              // If so, the file was already saved with the original content —
              // don't apply the format to the editor to avoid a mismatch.
              const stillActive = resolveMap?.has(requestId);
              if (result.error) {
                notificationBus.notify('warning', 'Format Document', `Format failed: ${result.error}`);
              }
              if (stillActive && !result.error && result.formatted !== content && viewRef.current) {
                viewRef.current.dispatch({
                  changes: {
                    from: 0,
                    to: viewRef.current.state.doc.length,
                    insert: result.formatted,
                  },
                  annotations: [Transaction.addToHistory.of(false)],
                });
              }
              // Always resolve so the save doesn't hang
              if (resolveMap) {
                const resolve = resolveMap.get(requestId);
                if (resolve) {
                  resolve(result);
                  resolveMap.delete(requestId);
                }
              }
            });
          } else {
            // Manual format command — no callback needed
            formatPromise.then(result => {
              if (bufferRef.current?.id !== capturedBufferId) return;
              if (result.error) {
                notificationBus.notify('warning', 'Format Document', `Format failed: ${result.error}`);
                return;
              }
              if (result.formatted !== content && viewRef.current) {
                viewRef.current.dispatch({
                  changes: {
                    from: 0,
                    to: viewRef.current.state.doc.length,
                    insert: result.formatted,
                  },
                  annotations: [Transaction.addToHistory.of(false)],
                });
              }
            });
          }
        }
      } else if (e.type === 'format-on-save-failed') {
        // Format-on-save failed in EditorManagerContext - notify the user
        const detail = (e as CustomEvent).detail as { bufferId?: string; filePath?: string } | undefined;
        notificationBus.notify('warning', 'Format Document', 'Format on save failed - file saved without formatting');
      } else if (e.type === 'editor-find-all-references') {
        void handleFindAllReferences();
      } else if (e.type === 'editor-go-to-workspace-symbol') {
        setShowGoToWorkspaceSymbol(true);
      } else if (e.type === 'editor-go-to-symbol') {
        window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_goto_symbol' } }));
      }
    };

    document.addEventListener('editor-goto-line', handler);
    document.addEventListener('editor-toggle-word-wrap', handler);
    document.addEventListener('editor-toggle-linked-scroll', handler);
    document.addEventListener('editor-toggle-minimap', handler);
    document.addEventListener('editor-toggle-relative-line-numbers', handler);
    document.addEventListener('editor-cycle-whitespace-rendering', handler);
    document.addEventListener('editor-undo', handler);
    document.addEventListener('editor-redo', handler);
    document.addEventListener('editor-find', handler);
    document.addEventListener('editor-find-replace', handler);
    document.addEventListener('editor-select-all', handler);
    document.addEventListener('editor-format-document', handler);
    document.addEventListener('format-on-save-failed', handler);
    document.addEventListener('editor-find-all-references', handler);
    document.addEventListener('editor-go-to-workspace-symbol', handler);
    document.addEventListener('editor-go-to-symbol', handler);
    return () => {
      document.removeEventListener('editor-goto-line', handler);
      document.removeEventListener('editor-toggle-word-wrap', handler);
      document.removeEventListener('editor-toggle-linked-scroll', handler);
      document.removeEventListener('editor-toggle-minimap', handler);
      document.removeEventListener('editor-toggle-relative-line-numbers', handler);
      document.removeEventListener('editor-cycle-whitespace-rendering', handler);
      document.removeEventListener('editor-undo', handler);
      document.removeEventListener('editor-redo', handler);
      document.removeEventListener('editor-find', handler);
      document.removeEventListener('editor-find-replace', handler);
      document.removeEventListener('editor-select-all', handler);
      document.removeEventListener('editor-format-document', handler);
      document.removeEventListener('format-on-save-failed', handler);
      document.removeEventListener('editor-find-all-references', handler);
      document.removeEventListener('editor-go-to-workspace-symbol', handler);
      document.removeEventListener('editor-go-to-symbol', handler);
    };
  }, [handleGoToLine, onToggleMinimap, onToggleRelativeLineNumbers, onToggleWordWrap, toggleLinkedScroll, onCycleWhitespaceRendering, handleFindAllReferences]);

  // Compute effective language info for the LanguageSwitcher
  // (Must be declared before early returns to satisfy React hooks rules)
  const languageInfo = useMemo(() => {
    if (!buffer || !buffer.file) return { languageId: null as string | null, isAutoDetected: false };
    return resolveLanguageId(buffer.languageOverride ?? null, buffer.file?.ext?.replace(/^\./, ''), buffer.file?.name);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  const handleLanguageChange = useCallback(
    (languageId: string | null) => {
      if (!buffer) return;
      setBufferLanguageOverride(buffer.id, languageId);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [buffer?.id, setBufferLanguageOverride],
  );

  // Subscribe to LSP connection state changes for the footer indicator.
  useEffect(() => {
    const langId = languageInfo.languageId;
    if (!langId || !LSP_SUPPORTED_LANGUAGES.has(langId)) {
      setLspLanguage(null);
      return;
    }

    setLspLanguage(langId);

    const lspService = getLSPClientService();
    // Sync initial state
    setLspState(lspService.getLSPState(langId));

    const unsubscribe = lspService.onStateChange((languageId, state) => {
      if (languageId === langId) {
        setLspState(state);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [languageInfo.languageId]);

  // Compute enclosing symbols for breadcrumb display.
  // Optimized: expensive extraction keyed to content changes only, not cursor position.
  const { enclosingSymbols } = useEditorSymbols(localContent, buffer);

  if (!buffer || !buffer.file || buffer.file.isDir) {
    return <WelcomeTab onOpenCommandPalette={onOpenCommandPalette} />;
  }

  // Detect file type for specialized viewers
  const ext = buffer?.file?.ext?.toLowerCase();
  const isImage = !!ext && isImageFile(ext);
  const isAudio = !!ext && isAudioFile(ext);
  const isVideo = !!ext && isVideoFile(ext);
  const isBinary = !!ext && isBinaryFile(ext);
  const isSvgFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.svg';
  const isHtmlFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.html';
  const isSvgPreviewBuffer = buffer?.metadata?.previewKind === 'svg';
  const isHtmlPreviewBuffer = buffer?.metadata?.previewKind === 'html';
  const isMarkdownFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.md';

  const openLivePreview = () => {
    if (!buffer) return;
    const lang = isSvgFile ? 'svg' : 'html';
    openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/live-preview:${buffer.file.path}`,
      title: `${buffer.file.name} Live Preview`,
      content: localContent || buffer.content || '',
      ext: `.${lang}.preview`,
      metadata: {
        previewKind: lang,
        sourcePath: buffer.file.path,
        sourceName: buffer.file.name,
      },
    });
  };

  const openLivePreviewInSplit = () => {
    if (!buffer) return;
    const lang = isSvgFile ? 'svg' : 'html';
    const newPaneId = splitPane(paneId, 'vertical');
    if (!newPaneId) {
      openLivePreview();
      return;
    }
    // small delay for the new pane to mount
    setTimeout(() => {
      openWorkspaceBuffer({
        kind: 'file',
        path: `__workspace/live-preview:${buffer.file.path}`,
        title: `${buffer.file.name} Live Preview`,
        content: localContent || buffer.content || '',
        ext: `.${lang}.preview`,
        metadata: {
          previewKind: lang,
          sourcePath: buffer.file.path,
          sourceName: buffer.file.name,
        },
      });
    }, 100);
  };

  if (isImage && buffer) {
    return (
      <div className="editor-pane">
        <ImageViewer filePath={buffer.file.path} fileName={buffer.file.name} fileSize={buffer.file.size} />
      </div>
    );
  }

  if ((isAudio || isVideo) && buffer) {
    return (
      <MediaViewer
        filePath={buffer.file.path}
        fileName={buffer.file.name}
        fileSize={buffer.file.size}
        mediaType={isAudio ? 'audio' : 'video'}
      />
    );
  }

  if (isBinary && buffer) {
    return (
      <BinaryFileViewer
        fileName={buffer.file.name}
        filePath={buffer.file.path}
        fileSize={buffer.file.size}
      />
    );
  }

  if (isSvgPreviewBuffer || isHtmlPreviewBuffer) {
    return (
      <div className="editor-pane">
        <EditorToolbar
          onSave={handleSave}
          saving={false}
          showSave={false}
        />
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
              handleGoToLine(line);
            },
          }}
          rightActions={[
            ...(isSvgFile || isHtmlFile
              ? [
                  {
                    id: 'live-preview',
                    title: isSvgFile ? 'Open SVG live preview' : 'Open HTML live preview',
                    icon: <Eye size={16} />,
                    onClick: openLivePreview,
                  },
                  {
                    id: 'live-preview-split',
                    title: isSvgFile ? 'Open SVG live preview in split' : 'Open HTML live preview in split',
                    icon: <Columns2 size={16} />,
                    onClick: openLivePreviewInSplit,
                  },
                ]
              : []),
            ...(isMarkdownFile
              ? [
                  {
                    id: 'md-toggle',
                    title: markdownPreviewMode === 'off' ? 'Toggle markdown preview' : 'Close markdown preview',
                    icon: <Eye size={16} />,
                    onClick: () => setMarkdownPreviewMode((prev) => prev === 'off' ? 'split' : prev === 'split' ? 'preview' : 'off'),
                    active: markdownPreviewMode !== 'off',
                  },
                  ...(markdownPreviewMode !== 'off'
                    ? [
                        {
                          id: 'md-split',
                          title: 'Side-by-side view',
                          icon: <Columns2 size={16} />,
                          onClick: () => setMarkdownPreviewMode('split'),
                          active: markdownPreviewMode === 'split',
                        },
                      ]
                    : []),
                ]
              : []),
            {
              id: 'relative-line-numbers',
              title: 'Toggle relative line numbers',
              icon: <ListOrdered size={16} />,
              onClick: onToggleRelativeLineNumbers,
              active: relativeLineNumbersEnabled,
            },
          ]}
        />
        <GoToWorkspaceSymbolOverlay
          visible={showGoToWorkspaceSymbol}
          onSelectSymbol={handleSelectWorkspaceSymbol}
          onClose={() => {
            setShowGoToWorkspaceSymbol(false);
            viewRef.current?.focus();
          }}
        />
        <FindAllReferencesOverlay
          visible={showFindRefs}
          symbolName={refsSymbolName}
          references={refsResults}
          onSelectReference={handleSelectReference}
          onClose={() => {
            setShowFindRefs(false);
            viewRef.current?.focus();
          }}
        />

      {loading && (
        <div className="loading-indicator">
          <Loader2 size={16} className="spinner" />
          <span>Loading file...</span>
        </div>
      )}

      {error && (
        <div className="error-message">
          <AlertTriangle size={16} className="error-icon" />
          <span className="error-text">{error}</span>
        </div>
      )}

      <div className={`pane-content-wrapper${markdownPreviewMode === 'split' ? ' pane-content-wrapper-md-split' : ''}`}>
        {isMarkdownFile && markdownPreviewMode === 'preview' ? (
          <div className="pane-content pane-content-md-preview-full">
            <MarkdownPreview content={localContent} scrollRef={markdownPreviewBodyRef} />
          </div>
        ) : (
          <>
            <div className={`pane-content${markdownPreviewMode === 'split' ? ' pane-content-md-editor-side' : ''}`} onContextMenu={handleEditorContextMenu}>
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

      <div className="pane-footer">
        <div className="editor-stats">
          <span className="line-count">Lines: {(buffer?.content || '').split('\n').length}</span>
          <span className="char-count">Chars: {(buffer?.content || '').length}</span>
          <span className="cursor-position">
            Ln {buffer.cursorPosition.line + 1}, Col {buffer.cursorPosition.column + 1}
            {selectionInfo && selectionInfo.selectionCount > 1 && ` (${selectionInfo.selectionCount} selections)`}
            {selectionInfo && selectionInfo.selectionCount === 1 && ` (${selectionInfo.charCount} selected)`}
          </span>
          {editorFontSize !== FONT_SIZE_DEFAULT && (
            <span className="zoom-level">
              Zoom: {Math.round((editorFontSize / FONT_SIZE_DEFAULT) * 100)}%
            </span>
          )}
          <span className="tab-size" role="button" tabIndex={0} onClick={onCycleTabSize} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCycleTabSize(); } }} title="Click to change tab size (Spaces: 2, 4, 8 / Tabs)">
            {editorUsesTabs ? 'Tabs' : `Spaces: ${editorTabSize}`}
          </span>
          <span className="encoding-indicator" title="File encoding and line endings">
            UTF-8 · {lineEnding}
          </span>
          {whitespaceRenderingMode !== 'none' && (
            <span className="whitespace-mode" role="button" tabIndex={0} onClick={onCycleWhitespaceRendering} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCycleWhitespaceRendering(); } }} title="Click to change whitespace rendering (none → boundary → all)">
              {whitespaceRenderingMode === 'boundary' ? 'WS: boundary' : 'WS: all'}
            </span>
          )}
          {lspLanguage && (
            <span
              className="cm-footer-lsp"
              title={`LSP: ${lspState}`}
              style={{
                color:
                  lspState === 'connected'
                    ? 'var(--cm-status-ok, #4caf50)'
                    : lspState === 'disconnected'
                      ? 'var(--cm-status-error, #666)'
                      : 'var(--cm-status-warning, #c90)',
              }}
            >
              LSP:{lspState === 'connected' ? '✓' : lspState === 'connecting' || lspState === 'reconnecting' ? '…' : '✗'}
            </span>
          )}
        </div>
        <LanguageSwitcher
          currentLanguageId={languageInfo.languageId}
          isAutoDetected={languageInfo.isAutoDetected}
          onLanguageChange={handleLanguageChange}
        />
      </div>

      <ContextMenu
        isOpen={contextMenu !== null}
        x={contextMenu?.x ?? 0}
        y={contextMenu?.y ?? 0}
        onClose={hideContextMenu}
      >
        {contextMenu?.hasSelection && (
          <button className="context-menu-item" onClick={handleCopySelection} type="button">
            <Copy size={13} />
            <span className="menu-item-label">Copy</span>
          </button>
        )}
        {contextMenu?.languageId && isSemanticLanguage(contextMenu.languageId) && (
          <button className="context-menu-item" onClick={handleGoToDefinitionFromMenu} type="button">
            <Navigation size={13} />
            <span className="menu-item-label">Go to Definition</span>
          </button>
        )}
        {contextMenu?.languageId && isSemanticLanguage(contextMenu.languageId) && (
          <button className="context-menu-item" onClick={handleFindAllReferencesFromMenu} type="button">
            <Eye size={13} />
            <span className="menu-item-label">Find All References</span>
          </button>
        )}
        {(contextMenu?.hasSelection || (contextMenu?.languageId && isSemanticLanguage(contextMenu.languageId))) && (
          <div className="context-menu-divider" />
        )}
        <button className="context-menu-item" onClick={handleRevealInExplorer} type="button">
          <FolderOpen size={13} />
          <span className="menu-item-label">Reveal in Explorer</span>
        </button>
        <button className="context-menu-item" onClick={handleCopyRelativePath} type="button">
          <ClipboardCopy size={13} />
          <span className="menu-item-label">Copy relative path</span>
        </button>
        {workspaceRoot && (
          <button className="context-menu-item" onClick={handleCopyAbsolutePath} type="button">
            <ClipboardCopy size={13} />
            <span className="menu-item-label">Copy absolute path</span>
          </button>
        )}
      </ContextMenu>
    </div>
  );
}

export default EditorPane;
