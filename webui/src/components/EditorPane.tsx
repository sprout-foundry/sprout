import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import {
  EditorView,
  keymap,
  type KeyBinding,
  lineNumbers,
  highlightSpecialChars,
  highlightActiveLine,
  highlightActiveLineGutter,
  rectangularSelection,
  crosshairCursor,
  dropCursor,
  drawSelection,
  scrollPastEnd,
} from '@codemirror/view';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { hyperLink } from '@uiw/codemirror-extensions-hyper-link';
import { color } from '@uiw/codemirror-extensions-color';
import { EditorState, Compartment, Transaction } from '@codemirror/state';
import { defaultKeymap, indentWithTab, history, undo, redo } from '@codemirror/commands';
import { search, searchKeymap, openSearchPanel, replaceAll, highlightSelectionMatches } from '@codemirror/search';
import { autocompletion, closeBrackets } from '@codemirror/autocomplete';
import {
  syntaxHighlighting,
  defaultHighlightStyle,
  codeFolding,
  foldGutter,
  indentOnInput,
  bracketMatching,
  indentUnit,
} from '@codemirror/language';
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import LivePreview from './LivePreview';
import EditorToolbar from './EditorToolbar';
import EditorBreadcrumb, { type BreadcrumbSymbol } from './EditorBreadcrumb';
import { isImageFile, isAudioFile, isVideoFile, isBinaryFile } from '../utils/mediaPatterns';
import ImageViewer from './ImageViewer';
import SvgPreview from './SvgPreview';
import GoToSymbolOverlay from './GoToSymbolOverlay';
import { getEnclosingSymbols } from './GoToSymbolOverlay';
import LanguageSwitcher from './LanguageSwitcher';
import BinaryFileViewer from './BinaryFileViewer';
import MediaViewer from './MediaViewer';
import { readFileWithConsent } from '../services/fileAccess';
import { showFileChangeDialog } from './FileChangeDialog';
import { getEditorKeymap } from '../utils/editorHotkeys';
import { diffGutter, updateDiffGutter, clearDiffGutter } from '../extensions/diffGutter';
import './EditorPane.css';
import { lintDiagnostics, clearDiagnostics, createDebouncedDiagnosticsUpdater } from '../extensions/lintDiagnostics';
import { cursorHistoryPlugin } from '../extensions/cursorHistory';
import { indentGuidesPlugin } from '../extensions/indentGuides';
import { bracketColorizationPlugin } from '../extensions/bracketColorization';
import { linkedScrollExtension, setLinkedScrollEnabled, suppressScrollSync } from '../extensions/linkedScroll';
import { getLanguageExtensions, resolveLanguageId } from '../extensions/languageRegistry';
import { detectIndentation, DEFAULT_INDENT_WIDTH } from '../extensions/indentDetect';
import {
  createEmmetCompartment,
  getInitialEmmetExtensions,
  buildEmmetExtensions,
} from '../extensions/emmet';
import { minimapExtension } from '../extensions/minimap';
import { tabExpandSnippets, setSnippetLanguage } from '../extensions/snippets';
import { trailingWhitespacePlugin } from '../extensions/trailingWhitespace';
import { whitespaceRenderingPlugin, type WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { unsavedLineHighlight, setOriginalContent } from '../extensions/unsavedLineHighlight';
import { ApiService } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { Loader2, AlertTriangle, Eye, Columns2, Copy, Navigation, FolderOpen, ClipboardCopy, ListOrdered } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';
import { generateUnifiedDiff } from '../utils/simpleDiff';
import { useLog, debugLog, warn } from '../utils/log';
import ContextMenu from './ContextMenu';
import WelcomeTab from './WelcomeTab';
import { JUST_SAVED_THRESHOLD_MS, justSavedRef } from '../hooks/useAutoReloadCleanBuffers';

interface EditorPaneProps {
  paneId: string;
  onOpenCommandPalette?: () => void;
}

// Font size constants
const FONT_SIZE_MIN = 8;       // Minimum legible font size
const FONT_SIZE_DEFAULT = 13;  // Default matches Monaco/Menlo editor defaults
const FONT_SIZE_MAX = 72;      // Maximum for accessibility (WCAG supports 200% zoom)

/** Tab size value meaning "use tabs for indentation" (stored in state and localStorage) */
const TAB_SIZE_TABS_MODE = 0;

// Tab size constants
const TAB_SIZE_DEFAULT = 4;
const TAB_SIZE_OPTIONS = [2, 4, 8] as const;

/** Minimum number of indented lines required for auto-detection to be confident */
const MIN_INDENTED_LINES_FOR_DETECTION = 3;

function isSemanticLanguage(languageId: string): boolean {
  return (
    languageId === 'typescript' ||
    languageId === 'typescript-jsx' ||
    languageId === 'javascript' ||
    languageId === 'javascript-jsx' ||
    languageId === 'go'
  );
}

// Transaction annotations for external content replacements (file reloads,
// initial loads, buffer switches). `Transaction.addToHistory.of(false)`
// prevents CodeMirror from recording these in the undo/redo stack.
const suppressHistoryAnnotations = [
  Transaction.addToHistory.of(false),
];

function EditorPane({ paneId, onOpenCommandPalette }: EditorPaneProps): JSX.Element {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const hotkeysCompartment = useRef(new Compartment());
  const lineWrappingCompartment = useRef(new Compartment());
  const relativeLineNumbersCompartment = useRef(new Compartment());
  const languageCompartment = useRef(new Compartment());
  const minimapCompartment = useRef(new Compartment());
  const whitespaceRenderingCompartment = useRef(new Compartment());
  const emmetCompartment = useRef(createEmmetCompartment());
  const fontSizeCompartment = useRef(new Compartment());
  const tabSizeCompartment = useRef(new Compartment());
  const lastInitLanguageKey = useRef<string | null>(null);
  const [wordWrapEnabled, setWordWrapEnabled] = useState(true);
  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');
  const [showGoToSymbol, setShowGoToSymbol] = useState<boolean>(false);
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

  const log = useLog();

  const {
    panes,
    buffers,
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    saveBuffer,
    setBufferModified,
    setBufferOriginalContent,
    setBufferExternallyModified,
    clearBufferExternallyModified,
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
    }
  }, [buffer?.id]);

  // API service instance (singleton)
  const apiService = useRef(ApiService.getInstance()).current;

  // Debounced diagnostics updater — coalesces rapid diagnostic pushes
  const debouncedDiag = useRef(createDebouncedDiagnosticsUpdater(500));

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
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const isExternalUpdateRef = useRef<boolean>(false);
  const loadFileRef = useRef<((filePath: string) => Promise<void>) | null>(null);
  const fetchDiagnosticsRef = useRef<(filePath: string, content: string, trigger?: 'edit' | 'save') => void>(() => {
    /* noop */
  });

  // Stable action references for hotkey keymap (used in both init and reconfigure effects)
  const hotkeyActionsRef = useRef<{
    onSave: () => void;
    onGoToLine: () => void;
    onGoToSymbol: () => void;
    onToggleWordWrap: () => void;
    onToggleRelativeLineNumbers: () => void;
  } | null>(null);

  // Load file content - updates buffer in context to keep it in sync with editor
  const loadFile = useCallback(
    async (filePath: string) => {
      setLoading(true);
      setError(null);
      isExternalUpdateRef.current = true;

      try {
        const response = await readFileWithConsent(filePath);
        if (!response.ok) {
          throw new Error(`Failed to load file: ${response.statusText}`);
        }

        // Server returns raw file content as text, not JSON
        const content = await response.text();

        setLocalContent(content);

        // Update buffer in context to keep it in sync with editor.
        // Set originalContent so the buffer is NOT marked as modified just
        // because it was loaded from disk (the content matches what's on disk).
        if (buffer) {
          updateBufferContent(buffer.id, content);
          setBufferOriginalContent(buffer.id, content);
        }

        // Update editor if it exists
        if (viewRef.current) {
          viewRef.current.dispatch({
            changes: {
              from: 0,
              to: viewRef.current.state.doc.length,
              insert: content,
            },
            annotations: suppressHistoryAnnotations,
            effects: setOriginalContent.of(content),
          });
        }

        // Restore cursor position from buffer state (layout persistence).
        // Line numbers are 1-based (matching CodeMirror's doc.line().number).
        // Only restore if non-zero to avoid jarring jumps for files without
        // saved positions.
        if (buffer && viewRef.current && (buffer.cursorPosition.line > 0 || buffer.cursorPosition.column > 0)) {
          const { line, column } = buffer.cursorPosition;
          const doc = viewRef.current.state.doc;
          // Skip restoration if document is empty
          if (doc.lines > 0) {
            const targetLine = Math.max(0, Math.min(line, doc.lines - 1));
            const lineInfo = doc.line(targetLine + 1);
            const pos = lineInfo.from + Math.max(0, Math.min(column, lineInfo.length));
            viewRef.current.dispatch({
              selection: { anchor: pos },
              annotations: suppressHistoryAnnotations,
            });
          }
        }

        // Restore scroll position from buffer state (layout persistence).
        // Uses rAF so the DOM has rendered the new content before scrolling.
        if (buffer && viewRef.current && (buffer.scrollPosition.top > 0 || buffer.scrollPosition.left > 0)) {
          const { top, left } = buffer.scrollPosition;
          requestAnimationFrame(() => {
            if (viewRef.current) {
              viewRef.current.scrollDOM.scrollTop = top;
              viewRef.current.scrollDOM.scrollLeft = left;
            }
          });
        }

        // Fetch git diff after loading file
        if (filePath && viewRef.current) {
          try {
            const diffResponse = await apiService.getGitDiff(filePath);
            if (diffResponse.diff && diffResponse.diff.trim()) {
              updateDiffGutter(viewRef.current, diffResponse.diff);
            } else {
              clearDiffGutter(viewRef.current);
            }
          } catch (err) {
            // Graceful degradation - just clear diff if API fails
            debugLog('[EditorPane] Failed to fetch git diff for diagnostics:', err);
            notificationBus.notify('warning', 'Git Diff', 'Failed to fetch git diff for diagnostics');
            clearDiffGutter(viewRef.current);
          }
        }

        // Auto-detect indentation from file content (skip if user has manually set their preference)
        if (!indentManuallySetRef.current) {
          const detected = detectIndentation(content);
          if (detected.indentedLineCount >= MIN_INDENTED_LINES_FOR_DETECTION) {
            const detectedSize = detected.useTabs ? TAB_SIZE_DEFAULT : detected.indentWidth;
            setEditorTabSize(detected.useTabs ? TAB_SIZE_TABS_MODE : detectedSize);
            setEditorUsesTabs(detected.useTabs);
            // Note: auto-detected settings are NOT persisted to localStorage.
            // Only user's manual cycle choice is persisted.
            if (viewRef.current) {
              viewRef.current.dispatch({
                effects: tabSizeCompartment.current.reconfigure([
                  EditorState.tabSize.of(detectedSize),
                  indentUnit.of(detected.useTabs ? '\t' : ' '.repeat(detectedSize)),
                ]),
              });
            }
          } else {
            // Reset to defaults when detection is inconclusive
            setEditorUsesTabs(false);
            setEditorTabSize(TAB_SIZE_DEFAULT);
            if (viewRef.current) {
              viewRef.current.dispatch({
                effects: tabSizeCompartment.current.reconfigure([
                  EditorState.tabSize.of(TAB_SIZE_DEFAULT),
                  indentUnit.of(' '.repeat(TAB_SIZE_DEFAULT)),
                ]),
              });
            }
          }
        }

        // Fetch diagnostics for the loaded file
        if (viewRef.current) {
          fetchDiagnosticsRef.current(filePath, content);
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Unknown error';
        log.error(`[EditorPane loadFile] Error: ${errorMessage}`, { title: 'File Load Error' });
        setError(errorMessage);
      } finally {
        isExternalUpdateRef.current = false;
        setLoading(false);
      }
    },
    [apiService, buffer, updateBufferContent, setBufferOriginalContent, log],
  ); // eslint-disable-line react-hooks/exhaustive-deps -- fetchDiagnostics is accessed via ref to avoid forward-reference issue

  // Keep ref in sync
  loadFileRef.current = loadFile;

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
      new CustomEvent('ledit:reveal-in-explorer', {
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

  // Save buffer
  const handleSave = useCallback(async () => {
    if (!buffer || !viewRef.current) return;

    setSaving(true);
    setError(null);

    // Notify the external file watcher and auto-reload cooldown *before*
    // the HTTP roundtrip.  The server-side fsnotify fires as soon as it
    // writes the file, and the WebSocket "file_content_changed" event can
    // reach the browser *before* the HTTP save response.  Setting the
    // cooldown early prevents the echo from popping the "changed on disk"
    // dialog.
    document.dispatchEvent(
      new CustomEvent('file:editor-saved', {
        detail: {
          path: buffer.file.path,
          mtime: Math.floor(Date.now() / 1000),
        },
      }),
    );

    try {
      const saveResult = await saveBuffer(buffer.id);
      const serverMtime =
        saveResult && typeof saveResult.mod_time === 'number' ? saveResult.mod_time : null;

      // Re-dispatch with the authoritative server mtime so the watcher
      // tracks the correct timestamp going forward.
      document.dispatchEvent(
        new CustomEvent('file:editor-saved', {
          detail: {
            path: buffer.file.path,
            mtime: serverMtime ?? Math.floor(Date.now() / 1000),
          },
        }),
      );

      // Re-run diagnostics on save so save-only checks (for example go vet)
      // can run without paying that cost on each keystroke.
      if (buffer.file.path && viewRef.current) {
        await fetchDiagnosticsRef.current(buffer.file.path, viewRef.current.state.doc.toString(), 'save');
      }

      // Re-fetch diff after save
      if (buffer.file.path && viewRef.current) {
        try {
          const diffResponse = await apiService.getGitDiff(buffer.file.path);
          if (diffResponse.diff && diffResponse.diff.trim()) {
            updateDiffGutter(viewRef.current, diffResponse.diff);
          } else {
            clearDiffGutter(viewRef.current);
          }
        } catch (err) {
          debugLog('[EditorPane] Failed to re-fetch git diff after save:', err);
          notificationBus.notify('warning', 'Git Diff', 'Failed to re-fetch git diff after save');
        }
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to save file';
      setError(errorMessage);
      log.error(`Save error: ${errorMessage}`, { title: 'Save Error' });
    } finally {
      setSaving(false);
    }
  }, [buffer, saveBuffer, apiService]); // eslint-disable-line react-hooks/exhaustive-deps -- updateDiffGutter/clearDiffGutter are module-level functions

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
        effects: fontSizeCompartment.current.reconfigure([
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
        effects: fontSizeCompartment.current.reconfigure([
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
      effects: fontSizeCompartment.current.reconfigure([
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
          effects: tabSizeCompartment.current.reconfigure([
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
          effects: tabSizeCompartment.current.reconfigure([
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
        effects: tabSizeCompartment.current.reconfigure([
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

  // Fetch diagnostics for the current file and push them into the editor
  const fetchDiagnostics = useCallback(
    async (filePath: string, content: string, trigger: 'edit' | 'save' = 'edit') => {
      if (!viewRef.current) return;

      const languageId = resolveLanguageId(
        buffer?.languageOverride,
        buffer?.file?.ext?.replace(/^\./, ''),
        buffer?.file?.name,
      ).languageId ?? '';

      try {
        if (isSemanticLanguage(languageId)) {
          const semantic = await apiService.getSemanticDiagnostics(filePath, content, languageId, trigger);
          if (semantic.capabilities?.diagnostics) {
            debugLog(`[fetchDiagnostics] semantic latency ${semantic.duration_ms ?? -1}ms (${languageId}, trigger=${trigger})`);
            if (semantic.diagnostics && semantic.diagnostics.length > 0) {
              debouncedDiag.current.update(viewRef.current, semantic.diagnostics);
            } else {
              clearDiagnostics(viewRef.current);
            }
            return;
          }
        }
      } catch (err) {
        debugLog('[fetchDiagnostics] semantic diagnostics unavailable, falling back:', err);
      }

      try {
        const result = await apiService.getDiagnostics(filePath, content);
        if (result.diagnostics && result.diagnostics.length > 0) {
          debouncedDiag.current.update(viewRef.current, result.diagnostics);
        } else {
          clearDiagnostics(viewRef.current);
        }
      } catch (err) {
        debugLog('[fetchDiagnostics] best-effort diagnostic fetch failed:', err);
        clearDiagnostics(viewRef.current);
      }
    },
    [apiService, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name],
  );

  // Keep ref in sync so loadFile can call fetchDiagnostics without a forward reference
  fetchDiagnosticsRef.current = fetchDiagnostics;

  const lastLoadedRef = useRef<{ bufferId: string; filePath: string } | null>(null);
  const currentBufferIdRef = useRef<string | null>(null);

  // Sync original content to the unsaved line highlight extension
  // whenever it changes (e.g., after save completes).
  useEffect(() => {
    if (viewRef.current && buffer?.originalContent !== undefined) {
      viewRef.current.dispatch({
        effects: setOriginalContent.of(buffer.originalContent),
      });
    }
  }, [buffer?.originalContent]); // eslint-disable-line react-hooks/exhaustive-deps

  // Load file when pane has a buffer assigned
  useEffect(() => {
    // Skip if no buffer or no file
    if (!buffer || !buffer.file || buffer.file.isDir) {
      setLocalContent('');
      if (viewRef.current) {
        viewRef.current.dispatch({
          changes: {
            from: 0,
            to: viewRef.current.state.doc.length,
            insert: '',
          },
          annotations: suppressHistoryAnnotations,
          effects: setOriginalContent.of(''),
        });
      }
      setError(null);
      lastLoadedRef.current = null;
      currentBufferIdRef.current = null;
      if (viewRef.current) {
        clearDiffGutter(viewRef.current);
        clearDiagnostics(viewRef.current);
      }
      return;
    }

    if (buffer.kind !== 'file') {
      const nextContent = buffer.content || '';
      setLocalContent(nextContent);
      setError(null);
      lastLoadedRef.current = { bufferId: buffer.id, filePath: buffer.file.path };
      if (viewRef.current) {
        viewRef.current.dispatch({
          changes: {
            from: 0,
            to: viewRef.current.state.doc.length,
            insert: nextContent,
          },
          annotations: suppressHistoryAnnotations,
          effects: setOriginalContent.of(nextContent),
        });
        clearDiffGutter(viewRef.current);
        clearDiagnostics(viewRef.current);
      }
      return;
    }

    // Skip loading virtual workspace buffers — they have no on-disk file.
    if (buffer.file.path.startsWith('__workspace/')) {
      const nextContent = buffer.content || '';
      setLocalContent(nextContent);
      setError(null);
      lastLoadedRef.current = { bufferId: buffer.id, filePath: buffer.file.path };
      currentBufferIdRef.current = buffer.id;
      if (viewRef.current) {
        viewRef.current.dispatch({
          changes: {
            from: 0,
            to: viewRef.current.state.doc.length,
            insert: nextContent,
          },
          annotations: suppressHistoryAnnotations,
          effects: setOriginalContent.of(nextContent),
        });
        clearDiffGutter(viewRef.current);
        clearDiagnostics(viewRef.current);
      }
      return;
    }

    // Skip if same buffer already tracked
    if (currentBufferIdRef.current === buffer.id) {
      return;
    }

    // Mark new buffer as tracked
    currentBufferIdRef.current = buffer.id;

    // Skip if same buffer and same file already loaded
    if (
      lastLoadedRef.current &&
      lastLoadedRef.current.bufferId === buffer.id &&
      lastLoadedRef.current.filePath === buffer.file.path
    ) {
      return;
    }

    // Load file from server
    lastLoadedRef.current = { bufferId: buffer.id, filePath: buffer.file.path };

    // Skip loading content for binary/media buffers — they are rendered by
    // dedicated viewers (ImageViewer, MediaViewer, BinaryFileViewer) that
    // fetch the file themselves as blobs.
    const fileExt = buffer.file.ext?.toLowerCase();
    if (
      fileExt &&
      (isImageFile(fileExt) || isAudioFile(fileExt) || isVideoFile(fileExt) || isBinaryFile(fileExt))
    ) {
      // Still track the buffer so we don't re-process it on re-render
      return;
    }

    // Use ref to avoid dependency issues - only pass filePath now
    if (loadFileRef.current) {
      loadFileRef.current(buffer.file.path);
    }
  }, [buffer, paneId]);

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

      // Track scroll position changes for layout persistence
      if (buffer && update.viewportChanged) {
        const scrollInfo = update.view.scrollDOM;
        if (scrollInfo) {
          updateBufferScroll(buffer.id, { top: scrollInfo.scrollTop, left: scrollInfo.scrollLeft });
        }
      }
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
        setShowGoToSymbol(true);
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
        run: () => {
          void handleGoToDefinition();
          return true;
        },
      },
    ];

    const resolvedLanguage = resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name);

    const extensions = [
      updateListener,
      EditorState.allowMultipleSelections.of(true),
      rectangularSelection(),
      drawSelection(),
      crosshairCursor(),
      dropCursor(),
      keymap.of(defaultKeymap),
      tabExpandSnippets(),
      keymap.of([indentWithTab]),
      keymap.of(searchKeymap),
      hotkeysCompartment.current.of(keymap.of(customKeymap)),
      keymap.of(replacePanelKeymap),
      keymap.of(zoomKeymap),
      keymap.of(semanticKeymap),
      search(),
      highlightSelectionMatches(),
      hyperLink,
      color,
      autocompletion(),
      closeBrackets(),
      history(),
      cursorHistoryPlugin,
      indentGuidesPlugin(),
      linkedScrollExtension(paneId, () => buffer?.file?.path ?? null),
      indentOnInput(),
      highlightSpecialChars(),
      highlightActiveLine(),
      highlightActiveLineGutter(),
      bracketMatching(),
      bracketColorizationPlugin(),
      syntaxHighlighting(
        customHighlightStyle ||
          (themePack.editorSyntaxStyle === 'one-dark' ? oneDarkHighlightStyle : defaultHighlightStyle),
      ),
      diffGutter(),
      lintDiagnostics(),
      trailingWhitespacePlugin(),
      unsavedLineHighlight(),
      whitespaceRenderingCompartment.current.of(whitespaceRenderingPlugin(whitespaceRenderingMode)),
      relativeLineNumbersCompartment.current.of(relativeLineNumbersEnabled ? lineNumbersRelative : lineNumbers()),
      scrollPastEnd(),
      foldGutter({
        openText: '▼',
        closedText: '▶',
      }),
      codeFolding(),
      minimapCompartment.current.of(minimapEnabled ? minimapExtension() : []),
      fontSizeCompartment.current.of([
        EditorView.theme({
          '&': {
            fontSize: `${editorFontSize}px`,
          },
        }),
      ]),
      tabSizeCompartment.current.of([
        EditorState.tabSize.of(editorTabSize === TAB_SIZE_TABS_MODE ? TAB_SIZE_DEFAULT : editorTabSize),
        indentUnit.of(editorUsesTabs ? '\t' : ' '.repeat(editorTabSize === TAB_SIZE_TABS_MODE ? TAB_SIZE_DEFAULT : editorTabSize)),
      ]),
      EditorView.theme({
        '&': {
          height: '100%',
          fontFamily: "'Monaco', 'Menlo', 'Fira Code', monospace",
          backgroundColor: 'var(--cm-bg)',
          color: 'var(--cm-fg)',
        },
        '.cm-content': {
          padding: '16px',
          caretColor: `var(--cm-cursor, ${themePack.mode === 'dark' ? '#f8f8f2' : '#526fff'})`,
        },
        '.cm-focused': {
          outline: 'none',
        },
        '.cm-gutters': {
          backgroundColor: 'var(--cm-gutter-bg)',
          border: 'none',
          color: 'var(--cm-gutter-fg)',
        },
        '.cm-scroller': {
          fontFamily: 'inherit',
          overflow: 'auto',
          minHeight: '0',
          height: '100%',
        },
        '.cm-cursor': {
          borderLeftColor: themePack.mode === 'dark' ? 'var(--cm-cursor, #f8f8f2)' : 'var(--cm-cursor, #526fff)',
          borderLeftWidth: '2px',
        },
        '&.cm-focused .cm-cursor': {
          borderLeftColor: themePack.mode === 'dark' ? 'var(--cm-cursor, #f8f8f2)' : 'var(--cm-cursor, #526fff)',
          borderLeftWidth: '2px',
        },
        '.cm-dropCursor': {
          borderLeftColor: themePack.mode === 'dark' ? 'var(--cm-cursor, #f8f8f2)' : 'var(--cm-cursor, #526fff)',
        },
        '.cm-selectionBackground, .cm-content ::selection': {
          backgroundColor: 'var(--cm-selection) !important',
        },
        '&.cm-focused .cm-activeLine': {
          backgroundColor: 'var(--cm-active-line)',
        },
        '.cm-activeLineGutter': {
          backgroundColor: 'var(--cm-active-line-gutter)',
          color: 'var(--cm-gutter-fg-active)',
        },
        '.cm-foldGutter': {
          width: '20px',
        },
        '.cm-foldGutter .cm-gutterElement': {
          padding: '0 4px',
          fontSize: '12px',
        },
        '.cm-foldGutter .cm-gutterElement:hover': {
          color: 'var(--accent-primary, #6366f1)',
        },
      }),
      lineWrappingCompartment.current.of(wordWrapEnabled ? EditorView.lineWrapping : []),
      emmetCompartment.current.of(
        getInitialEmmetExtensions(resolvedLanguage.languageId),
      ),
      languageCompartment.current.of(
        getLanguageExtensions(resolvedLanguage.languageId),
      ),
    ];

    const state = EditorState.create({
      doc: localContent,
      extensions,
    });

    const view = new EditorView({
      state,
      parent: editorRef.current,
    });

    viewRef.current = view;

    // Track which language was set during init so the reconfiguration effect
    // can skip a redundant reconfigure on the same buffer/language combo.
    lastInitLanguageKey.current = `${buffer?.id}:${buffer?.languageOverride ?? ''}:${buffer?.file?.ext ?? ''}:${buffer?.file?.name ?? ''}`;

    // Snapshot ref value for cleanup (ref.current in cleanup triggers exhaustive-deps)
    const debounced = debouncedDiag.current;

    return () => {
      debounced.cancel();
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

    view.dispatch({
      effects: [
        languageCompartment.current.reconfigure(getLanguageExtensions(languageId)),
        emmetCompartment.current.reconfigure(buildEmmetExtensions(languageId)),
      ],
    });
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
        setShowGoToSymbol(true);
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
      effects: hotkeysCompartment.current.reconfigure(
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
      effects: lineWrappingCompartment.current.reconfigure(next ? EditorView.lineWrapping : []),
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
      effects: minimapCompartment.current.reconfigure(next ? minimapExtension() : []),
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
      effects: relativeLineNumbersCompartment.current.reconfigure(next ? lineNumbersRelative : lineNumbers()),
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
      effects: whitespaceRenderingCompartment.current.reconfigure(
        whitespaceRenderingPlugin(next),
      ),
    });
  }, [setWhitespaceRenderingMode]);

  // Keep the ref mirror in sync whenever the state value changes from context
  // and reconfigure the CodeMirror compartment so the editor reflects the change.
  useEffect(() => {
    whitespaceRenderingModeRef.current = whitespaceRenderingMode;
    viewRef.current?.dispatch({
      effects: whitespaceRenderingCompartment.current.reconfigure(
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

  // Keep module-level linked scroll state in sync with context.
  useEffect(() => {
    setLinkedScrollEnabled(isLinkedScrollEnabled);
  }, [isLinkedScrollEnabled]);

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
    };
  }, [handleGoToLine, onToggleMinimap, onToggleRelativeLineNumbers, onToggleWordWrap, toggleLinkedScroll, onCycleWhitespaceRendering]);

  // Listen for scroll sync events from other panes (linked scrolling).
  useEffect(() => {
    const handleLinkedScroll = (e: Event) => {
      const customEvent = e as CustomEvent;
      const { sourcePaneId, filePath, topLine } = customEvent.detail;

      // Skip if same pane or different file
      if (sourcePaneId === paneId) return;
      const currentPath = buffer?.file?.path;
      if (!currentPath || currentPath !== filePath) return;
      if (!viewRef.current) return;

      const view = viewRef.current;

      // topLine is 1-based; validate bounds.
      if (topLine < 1 || topLine > view.state.doc.lines) return;

      // Suppress this pane's next viewportChanged dispatch so the
      // programmatic scroll doesn't cause an echo loop (A → B → A → …).
      suppressScrollSync(paneId);

      // Get the layout block for the target line and scroll it to the top.
      const targetPos = view.state.doc.line(topLine).from;
      const block = view.lineBlockAt(targetPos);
      view.scrollDOM.scrollTo(0, block.top);
    };

    document.addEventListener('editor:linked-scroll', handleLinkedScroll);
    return () => document.removeEventListener('editor:linked-scroll', handleLinkedScroll);
  }, [paneId, buffer?.file?.path]);

  // Ref to always read current buffer state (avoids stale closures in event listeners)
  const bufferRef = useRef(buffer);
  bufferRef.current = buffer;

  // Listen for external file modifications and show dialog to the user.
  useEffect(() => {
    if (!buffer || buffer.kind !== 'file' || buffer.file.path.startsWith('__workspace/')) return;

    const filePath = buffer.file.path;

    const handleExternalChange = (e: Event) => {
      const detail = (e as CustomEvent).detail as {
        path: string;
        mtime: number;
        size: number;
        deleted: boolean;
      };
      if (detail.path !== filePath) return;

      // Suppress the dialog when the change was caused by the editor's own save.
      const justSavedAt = justSavedRef.get(detail.path) ?? 0;
      if (Date.now() - justSavedAt < JUST_SAVED_THRESHOLD_MS) return;

      // Read current buffer state via ref to avoid stale closure.
      const currentBuffer = bufferRef.current;
      if (!currentBuffer) return;

      // Skip showing dialog if the user has unsaved changes — don't overwrite silently.
      if (currentBuffer.isModified) {
        // Fetch the disk content for the conflict dialog, or show deletion alert.
        if (detail.deleted) {
          showFileChangeDialog(currentBuffer.file.name, { deleted: true, hasUnsavedChanges: true })
            .then((action) => {
              if (action === 'keep-mine') {
                // User wants to keep their unsaved edits in the editor.
                // Mark as externally modified so the tab shows the indicator.
                setBufferExternallyModified(currentBuffer.id, '');
              }
              // 'ignore' → dismissed without action (no indicator needed)
            })
            .catch((err) => {
              debugLog('[EditorPane] File change dialog error:', err);
              notificationBus.notify('error', 'File Change', 'File change dialog error: ' + String(err));
            });
          return;
        }

        readFileWithConsent(filePath)
          .then((response) => {
            if (!response.ok) return;
            return response.text();
          })
          .then(async (diskContent) => {
            if (diskContent === undefined) return;
            const action = await showFileChangeDialog(currentBuffer.file.name, {
              deleted: false,
              hasUnsavedChanges: true,
            });
            if (action === 'reload') {
              if (loadFileRef.current) {
                loadFileRef.current(filePath);
              }
              clearBufferExternallyModified(currentBuffer.id);
            } else if (action === 'keep-mine') {
              setBufferExternallyModified(currentBuffer.id, diskContent);
            } else if (action === 'show-diff') {
              try {
                const editorContent = bufferRef.current?.content || '';
                const diffText = generateUnifiedDiff(editorContent, diskContent, 'Editor', 'Disk');
                if (!diffText) return;

                openWorkspaceBuffer({
                  kind: 'diff',
                  path: `diff:${filePath}`,
                  title: `Diff: ${currentBuffer.file.name} (editor ↔ disk)`,
                  content: diffText,
                  ext: '.diff',
                  isPinned: false,
                  isClosable: true,
                  metadata: { sourcePath: filePath, diffType: 'external-change' },
                });

                const bufferRefId = bufferRef.current?.id;
                if (bufferRefId) {
                  setBufferExternallyModified(bufferRefId, diskContent);
                }
              } catch (err) {
                debugLog('[EditorPane] Failed to generate diff for external changes:', err);
                notificationBus.notify('warning', 'Diff Generation', 'Failed to generate diff for external changes');
              }
            }
          })
          .catch((err) => {
            warn(`Failed to read externally modified file: ${err instanceof Error ? err.message : String(err)}`);
          });
        return;
      }

      // Clean (unmodified) buffers are auto-reloaded by EditorManagerContext.
      // No dialog needed here.
    };

    document.addEventListener('file_externally_modified', handleExternalChange);
    return () => document.removeEventListener('file_externally_modified', handleExternalChange);
  }, [buffer, clearBufferExternallyModified, setBufferExternallyModified, openWorkspaceBuffer]); // eslint-disable-line react-hooks/exhaustive-deps -- clearBufferExternallyModified/setBufferExternallyModified are stable

  // Listen for auto-reloaded events to sync the CodeMirror editor view.
  useEffect(() => {
    if (!buffer) return;

    const handleAutoReloaded = (e: Event) => {
      const detail = (e as CustomEvent).detail as { bufferId: string; content: string };
      if (detail.bufferId !== buffer.id) return;

      isExternalUpdateRef.current = true;
      try {
        if (viewRef.current) {
          viewRef.current.dispatch({
            changes: {
              from: 0,
              to: viewRef.current.state.doc.length,
              insert: detail.content,
            },
            annotations: suppressHistoryAnnotations,
          });
        }
        setLocalContent(detail.content);

        // Re-detect indentation on auto-reload (skip if user has manually set their preference)
        if (!indentManuallySetRef.current) {
          const detected = detectIndentation(detail.content);
          if (detected.indentedLineCount >= MIN_INDENTED_LINES_FOR_DETECTION) {
            const detectedSize = detected.useTabs ? TAB_SIZE_DEFAULT : detected.indentWidth;
            setEditorTabSize(detected.useTabs ? TAB_SIZE_TABS_MODE : detectedSize);
            setEditorUsesTabs(detected.useTabs);
            if (viewRef.current) {
              viewRef.current.dispatch({
                effects: tabSizeCompartment.current.reconfigure([
                  EditorState.tabSize.of(detectedSize),
                  indentUnit.of(detected.useTabs ? '\t' : ' '.repeat(detectedSize)),
                ]),
              });
            }
          } else {
            // Reset to defaults when detection is inconclusive
            setEditorUsesTabs(false);
            setEditorTabSize(TAB_SIZE_DEFAULT);
            if (viewRef.current) {
              viewRef.current.dispatch({
                effects: tabSizeCompartment.current.reconfigure([
                  EditorState.tabSize.of(TAB_SIZE_DEFAULT),
                  indentUnit.of(' '.repeat(TAB_SIZE_DEFAULT)),
                ]),
              });
            }
          }
        }
      } finally {
        isExternalUpdateRef.current = false;
      }
    };

    document.addEventListener('file:auto-reloaded', handleAutoReloaded);
    return () => document.removeEventListener('file:auto-reloaded', handleAutoReloaded);
  }, [buffer]);

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

  // Compute enclosing symbols for breadcrumb display (before early returns).
  // buffer.cursorPosition.line is 0-based; getEnclosingSymbols expects 1-based.
  // Debounced to avoid running extractSymbols on every cursor move.
  const [enclosingSymbols, setEnclosingSymbols] = useState<BreadcrumbSymbol[]>([]);

  useEffect(() => {
    if (!localContent || !buffer?.file?.ext) {
      setEnclosingSymbols([]);
      return;
    }

    const timer = setTimeout(() => {
      setEnclosingSymbols(getEnclosingSymbols(localContent, buffer.file.ext, buffer.cursorPosition.line + 1));
    }, 100);

    return () => clearTimeout(timer);
  }, [localContent, buffer?.file?.ext, buffer?.cursorPosition.line]);

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
            {
              id: 'relative-line-numbers',
              title: 'Toggle relative line numbers',
              icon: <ListOrdered size={16} />,
              onClick: onToggleRelativeLineNumbers,
              active: relativeLineNumbersEnabled,
            },
          ]}
        />
        <GoToSymbolOverlay
          visible={showGoToSymbol}
          content={localContent}
          fileExtension={buffer?.file?.ext}
          onSelectSymbol={(line) => {
            handleGoToLine(line);
            setShowGoToSymbol(false);
            viewRef.current?.focus();
          }}
          onClose={() => {
            setShowGoToSymbol(false);
            viewRef.current?.focus();
          }}
        />

      <EditorBreadcrumb
        filePath={buffer.file.path}
        onNavigate={(path) => {
          // Reveal the clicked path segment in the file explorer sidebar
          window.dispatchEvent(
            new CustomEvent('ledit:reveal-in-explorer', {
              detail: { path },
            }),
          );
        }}
        symbols={enclosingSymbols}
        onNavigateToSymbol={(line) => {
          handleGoToLine(line);
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

      <div className="pane-content" onContextMenu={handleEditorContextMenu}>
        <div ref={editorRef} className="editor" />
      </div>

      <div className="pane-footer">
        <div className="editor-stats">
          <span className="line-count">Lines: {(buffer?.content || '').split('\n').length}</span>
          <span className="char-count">Chars: {(buffer?.content || '').length}</span>
          <span className="cursor-position">
            Ln {buffer.cursorPosition.line + 1}, Col {buffer.cursorPosition.column + 1}
          </span>
          {editorFontSize !== FONT_SIZE_DEFAULT && (
            <span className="zoom-level">
              Zoom: {Math.round((editorFontSize / FONT_SIZE_DEFAULT) * 100)}%
            </span>
          )}
          <span className="tab-size" role="button" tabIndex={0} onClick={onCycleTabSize} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCycleTabSize(); } }} title="Click to change tab size (Spaces: 2, 4, 8 / Tabs)">
            {editorUsesTabs ? 'Tabs' : `Spaces: ${editorTabSize}`}
          </span>
          {whitespaceRenderingMode !== 'none' && (
            <span className="whitespace-mode" role="button" tabIndex={0} onClick={onCycleWhitespaceRendering} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onCycleWhitespaceRendering(); } }} title="Click to change whitespace rendering (none → boundary → all)">
              {whitespaceRenderingMode === 'boundary' ? 'WS: boundary' : 'WS: all'}
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
