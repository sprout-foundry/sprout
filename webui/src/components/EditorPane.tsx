import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import {
  EditorView,
  keymap,
  type KeyBinding,
  lineNumbers,
  highlightSpecialChars,
  highlightActiveLine,
  rectangularSelection,
  crosshairCursor,
} from '@codemirror/view';
import { EditorState, Compartment } from '@codemirror/state';
import { defaultKeymap, indentWithTab, history } from '@codemirror/commands';
import { search, searchKeymap, openSearchPanel, replaceAll } from '@codemirror/search';
import { autocompletion, closeBrackets } from '@codemirror/autocomplete';
import {
  syntaxHighlighting,
  defaultHighlightStyle,
  codeFolding,
  foldGutter,
  indentOnInput,
  bracketMatching,
} from '@codemirror/language';
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import EditorToolbar from './EditorToolbar';
import EditorBreadcrumb, { type BreadcrumbSymbol } from './EditorBreadcrumb';
import ImageViewer from './ImageViewer';
import SvgPreview from './SvgPreview';
import GoToSymbolOverlay from './GoToSymbolOverlay';
import { getEnclosingSymbols } from './GoToSymbolOverlay';
import LanguageSwitcher from './LanguageSwitcher';
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
import { minimapExtension } from '../extensions/minimap';
import { tabExpandSnippets, setSnippetLanguage } from '../extensions/snippets';
import { ApiService } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { Loader2, AlertTriangle, Eye, Columns2 } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';
import { generateUnifiedDiff } from '../utils/simpleDiff';
import { useLog, debugLog, warn } from '../utils/log';
import ContextMenu from './ContextMenu';
import WelcomeTab from './WelcomeTab';

interface EditorPaneProps {
  paneId: string;
  onOpenCommandPalette?: () => void;
}

function EditorPane({ paneId, onOpenCommandPalette }: EditorPaneProps): JSX.Element {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const lineWrappingCompartment = useRef(new Compartment());
  const languageCompartment = useRef(new Compartment());
  const minimapCompartment = useRef(new Compartment());
  const lastInitLanguageKey = useRef<string | null>(null);
  const [wordWrapEnabled, setWordWrapEnabled] = useState(true);
  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');
  const [showGoToSymbol, setShowGoToSymbol] = useState<boolean>(false);
  const [minimapEnabled, setMinimapEnabled] = useState(() => {
    try {
      const stored = localStorage.getItem('editor:minimap-enabled');
      return stored !== null ? stored === 'true' : true; // default on
    } catch (err) {
      debugLog('Failed to read minimap setting from localStorage:', err);
      return true; // default on if localStorage unavailable
    }
  });

  // Context menu state
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null);
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
  } = useEditorManager();

  const { theme, themePack, customHighlightStyle } = useTheme();
  const { hotkeys } = useHotkeys();

  // Get buffer for this pane
  const pane = panes.find((p) => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  // Image extensions that should be viewed as images
  const IMAGE_EXTENSIONS = new Set([
    '.png',
    '.jpg',
    '.jpeg',
    '.gif',
    '.bmp',
    '.webp',
    '.ico',
    '.tiff',
    '.tif',
    '.avif',
  ]);

  const isImageFile = (ext?: string): boolean => {
    return !!ext && IMAGE_EXTENSIONS.has(ext.toLowerCase());
  };

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
  const fetchDiagnosticsRef = useRef<(filePath: string, content: string) => void>(() => {
    /* noop */
  });

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
          });
        }

        // Restore cursor position from buffer state (layout persistence).
        // Line numbers are 1-based (matching CodeMirror's doc.line().number).
        // Only restore if non-zero to avoid jarring jumps for files without
        // saved positions.
        if (buffer && viewRef.current && (buffer.cursorPosition.line > 0 || buffer.cursorPosition.column > 0)) {
          const { line, column } = buffer.cursorPosition;
          const doc = viewRef.current.state.doc;
          const targetLine = Math.max(0, Math.min(line, doc.lines - 1));
          const lineInfo = doc.line(targetLine + 1);
          const pos = lineInfo.from + Math.min(column, lineInfo.length);
          viewRef.current.dispatch({
            selection: { anchor: pos },
          });
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
      setContextMenu({ x: e.clientX, y: e.clientY });
    },
    [buffer],
  );

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

    try {
      await saveBuffer(buffer.id);

      // Notify the external file watcher that this file was saved from the
      // editor, so it updates its known mtime and doesn't re-fire a false
      // "changed externally" notification on the next poll.
      document.dispatchEvent(
        new CustomEvent('file:editor-saved', {
          detail: { path: buffer.file.path, mtime: Math.floor(Date.now() / 1000) },
        }),
      );

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

  // Fetch diagnostics for the current file and push them into the editor
  const fetchDiagnostics = useCallback(
    async (filePath: string, content: string) => {
      if (!viewRef.current) return;
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
    [apiService],
  ); // eslint-disable-line react-hooks/exhaustive-deps

  // Keep ref in sync so loadFile can call fetchDiagnostics without a forward reference
  fetchDiagnosticsRef.current = fetchDiagnostics;

  const lastLoadedRef = useRef<{ bufferId: string; filePath: string } | null>(null);
  const currentBufferIdRef = useRef<string | null>(null);

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

    const extensions = [
      updateListener,
      EditorState.allowMultipleSelections.of(true),
      rectangularSelection(),
      crosshairCursor(),
      keymap.of(defaultKeymap),
      tabExpandSnippets(),
      keymap.of([indentWithTab]),
      keymap.of(searchKeymap),
      keymap.of(customKeymap),
      keymap.of(replacePanelKeymap),
      search(),
      autocompletion(),
      closeBrackets(),
      history(),
      cursorHistoryPlugin,
      indentGuidesPlugin(),
      linkedScrollExtension(paneId, () => buffer?.file?.path ?? null),
      indentOnInput(),
      highlightSpecialChars(),
      highlightActiveLine(),
      bracketMatching(),
      bracketColorizationPlugin(),
      syntaxHighlighting(
        customHighlightStyle ||
          (themePack.editorSyntaxStyle === 'one-dark' ? oneDarkHighlightStyle : defaultHighlightStyle),
      ),
      diffGutter(),
      lintDiagnostics(),
      lineNumbers(),
      foldGutter({
        openText: '▼',
        closedText: '▶',
      }),
      codeFolding(),
      minimapCompartment.current.of(minimapEnabled ? minimapExtension() : []),
      EditorView.theme({
        '&': {
          height: '100%',
          fontSize: '13px',
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
      languageCompartment.current.of(
        getLanguageExtensions(
          resolveLanguageId(buffer?.languageOverride, buffer?.file?.ext?.replace(/^\./, ''), buffer?.file?.name)
            .languageId,
        ),
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
    hotkeys,
    customHighlightStyle,
    updateBufferContent,
    setBufferModified,
    updateBufferCursor,
    updateBufferScroll,
  ]);

  // Reconfigure the language compartment when the language override changes,
  // without requiring a full editor re-initialization.
  // A guard key prevents a redundant reconfigure on the same render cycle
  // where the init effect already set the correct language.
  useEffect(() => {
    if (!viewRef.current || !buffer) return;

    const key = `${buffer.id}:${buffer.languageOverride ?? ''}:${buffer.file?.ext ?? ''}:${buffer.file?.name ?? ''}`;
    if (key === lastInitLanguageKey.current) return; // init already applied this language
    lastInitLanguageKey.current = key;

    const { languageId } = resolveLanguageId(
      buffer.languageOverride,
      buffer.file?.ext?.replace(/^\./, ''),
      buffer.file?.name,
    );

    viewRef.current.dispatch({
      effects: languageCompartment.current.reconfigure(getLanguageExtensions(languageId)),
    });
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]); // eslint-disable-line react-hooks/exhaustive-deps

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

  // Keep the ref mirror in sync whenever the state value changes from
  // an external source (e.g. the global event listener).
  useEffect(() => {
    wordWrapRef.current = wordWrapEnabled;
  }, [wordWrapEnabled]);

  useEffect(() => {
    minimapEnabledRef.current = minimapEnabled;
  }, [minimapEnabled]);

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
      }
    };

    document.addEventListener('editor-goto-line', handler);
    document.addEventListener('editor-toggle-word-wrap', handler);
    document.addEventListener('editor-toggle-linked-scroll', handler);
    document.addEventListener('editor-toggle-minimap', handler);
    return () => {
      document.removeEventListener('editor-goto-line', handler);
      document.removeEventListener('editor-toggle-word-wrap', handler);
      document.removeEventListener('editor-toggle-linked-scroll', handler);
      document.removeEventListener('editor-toggle-minimap', handler);
    };
  }, [handleGoToLine, onToggleMinimap, onToggleWordWrap, toggleLinkedScroll]);

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
          });
        }
        setLocalContent(detail.content);
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

  // Detect if this is an image file
  const imageFile = buffer && buffer.file && !buffer.file.isDir && isImageFile(buffer.file.ext);
  const isSvgFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.svg';
  const isSvgPreviewBuffer = buffer?.metadata?.previewKind === 'svg';

  const openSvgPreview = () => {
    if (!buffer) return;

    openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/svg-preview:${buffer.file.path}`,
      title: `${buffer.file.name} Preview`,
      content: localContent || buffer.content || '',
      ext: '.svg.preview',
      metadata: {
        previewKind: 'svg',
        sourcePath: buffer.file.path,
        sourceName: buffer.file.name,
      },
    });
  };

  const openSvgPreviewInSplit = () => {
    if (!buffer) return;

    const newPaneId = splitPane(paneId, 'vertical');
    if (!newPaneId) {
      openSvgPreview();
      return;
    }

    openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/svg-preview:${buffer.file.path}`,
      title: `${buffer.file.name} Preview`,
      content: localContent || buffer.content || '',
      ext: '.svg.preview',
      metadata: {
        previewKind: 'svg',
        sourcePath: buffer.file.path,
        sourceName: buffer.file.name,
      },
    });
  };

  if (imageFile) {
    return (
      <div className="editor-pane">
        <ImageViewer filePath={buffer.file.path} fileName={buffer.file.name} fileSize={buffer.file.size} />
      </div>
    );
  }

  if (isSvgPreviewBuffer) {
    return (
      <div className="editor-pane">
        <EditorToolbar
          paneId={paneId}
          onGoToLine={handleGoToLine}
          onSave={handleSave}
          saving={false}
          showGoToLine={false}
          showSave={false}
        />
        <SvgPreview
          content={buffer.content || ''}
          fileName={(buffer.metadata?.sourceName || buffer.file.name) as string}
          sourcePath={buffer.metadata?.sourcePath as string | undefined}
        />
      </div>
    );
  }

  return (
    <div className="editor-pane">
      <div style={{ position: 'relative' }}>
        {/* Language switcher floats over the toolbar's left area */}
        <div className="editor-language-switcher-zone">
          <LanguageSwitcher
            currentLanguageId={languageInfo.languageId}
            isAutoDetected={languageInfo.isAutoDetected}
            onLanguageChange={handleLanguageChange}
          />
        </div>
        <EditorToolbar
          paneId={paneId}
          onGoToLine={handleGoToLine}
          onSave={handleSave}
          saving={saving}
          actions={[
            ...(isSvgFile
              ? [
                  {
                    id: 'svg-preview',
                    title: 'Open SVG preview',
                    icon: <Eye size={16} />,
                    onClick: openSvgPreview,
                  },
                  {
                    id: 'svg-preview-split',
                    title: 'Open SVG preview in split',
                    icon: <Columns2 size={16} />,
                    onClick: openSvgPreviewInSplit,
                  },
                ]
              : []),
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
      </div>

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
        </div>
      </div>

      <ContextMenu
        isOpen={contextMenu !== null}
        x={contextMenu?.x ?? 0}
        y={contextMenu?.y ?? 0}
        onClose={hideContextMenu}
      >
        <button className="context-menu-item" onClick={handleRevealInExplorer}>
          Reveal in File Explorer
        </button>
        <button className="context-menu-item" onClick={handleCopyRelativePath}>
          Copy relative path
        </button>
        {workspaceRoot && (
          <button className="context-menu-item" onClick={handleCopyAbsolutePath}>
            Copy absolute path
          </button>
        )}
      </ContextMenu>
    </div>
  );
}

export default EditorPane;
