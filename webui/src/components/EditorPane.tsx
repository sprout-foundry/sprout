import React, { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import { EditorView, keymap, KeyBinding, lineNumbers, highlightSpecialChars, highlightActiveLine, rectangularSelection, crosshairCursor } from '@codemirror/view';
import { EditorState, Compartment } from '@codemirror/state';
import { defaultKeymap, indentWithTab, history } from '@codemirror/commands';
import { search, searchKeymap, openSearchPanel, replaceAll } from '@codemirror/search';
import { autocompletion, closeBrackets } from '@codemirror/autocomplete';
import { syntaxHighlighting, defaultHighlightStyle, codeFolding, foldGutter, indentOnInput, bracketMatching } from '@codemirror/language';
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import EditorToolbar from './EditorToolbar';
import ImageViewer from './ImageViewer';
import SvgPreview from './SvgPreview';
import GoToSymbolOverlay from './GoToSymbolOverlay';
import LanguageSwitcher from './LanguageSwitcher';
import { readFileWithConsent } from '../services/fileAccess';
import { getEditorKeymap } from '../utils/editorHotkeys';
import { diffGutter, updateDiffGutter, clearDiffGutter } from '../extensions/diffGutter';
import { lintDiagnostics, clearDiagnostics, createDebouncedDiagnosticsUpdater } from '../extensions/lintDiagnostics';
import { cursorHistoryPlugin } from '../extensions/cursorHistory';
import { getLanguageExtensions, resolveLanguageId } from '../extensions/languageRegistry';
import { ApiService } from '../services/api';
import {
  File,
  Loader2,
  AlertTriangle,
  Eye,
  Columns2,
  WrapText,
} from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';
import './EditorPane.css';
import ContextMenu from './ContextMenu';

interface EditorPaneProps {
  paneId: string;
}

const EditorPane: React.FC<EditorPaneProps> = ({ paneId }) => {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const lineWrappingCompartment = useRef(new Compartment());
  const languageCompartment = useRef(new Compartment());
  const lastInitLanguageKey = useRef<string | null>(null);
  const [wordWrapEnabled, setWordWrapEnabled] = useState(true);
  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');
  const [showGoToSymbol, setShowGoToSymbol] = useState<boolean>(false);

  // Context menu state
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null);
  const [workspaceRoot, setWorkspaceRoot] = useState<string>('');

  const {
    panes,
    buffers,
    updateBufferContent,
    updateBufferCursor,
    saveBuffer,
    setBufferModified,
    setBufferOriginalContent,
    splitPane,
    openWorkspaceBuffer,
    setBufferLanguageOverride,
  } = useEditorManager();

  const { theme, themePack, customHighlightStyle } = useTheme();
  const { hotkeys } = useHotkeys();

  // Get buffer for this pane
  const pane = panes.find(p => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  // Image extensions that should be viewed as images
  const IMAGE_EXTENSIONS = new Set([
    '.png', '.jpg', '.jpeg', '.gif', '.bmp', '.webp', '.ico', '.tiff', '.tif', '.avif'
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
    apiService.getWorkspace().then(ws => {
      setWorkspaceRoot(ws.workspace_root || '');
    }).catch(() => {
      // Graceful degradation - absolute path option just won't appear
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const loadFileRef = useRef<((filePath: string) => Promise<void>) | null>(null);
  const fetchDiagnosticsRef = useRef<(filePath: string, content: string) => void>(() => {});

  // Load file content - updates buffer in context to keep it in sync with editor
  const loadFile = useCallback(async (filePath: string) => {
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
            insert: content
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
          console.warn('Failed to fetch git diff:', err);
          clearDiffGutter(viewRef.current);
        }
      }

      // Fetch diagnostics for the loaded file
      if (viewRef.current) {
        fetchDiagnosticsRef.current(filePath, content);
      }
    } catch (err) {
      console.error('[EditorPane loadFile] Error:', err);
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      isExternalUpdateRef.current = false;
      setLoading(false);
    }
  }, [apiService, buffer, updateBufferContent, setBufferOriginalContent]); // eslint-disable-line react-hooks/exhaustive-deps -- fetchDiagnostics is accessed via ref to avoid forward-reference issue

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
      scrollIntoView: true
    });

    // Focus the editor after navigation
    dispatch.focus();
  }, []);

  // ── Context menu handlers ─────────────────────────────────────
  const hideContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  const handleEditorContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!buffer || !buffer.file || buffer.file.isDir) return;
    if (buffer.kind !== 'file') return;
    setContextMenu({ x: e.clientX, y: e.clientY });
  }, [buffer]);

  const handleRevealInExplorer = useCallback(() => {
    if (!buffer || !buffer.file) return;
    window.dispatchEvent(new CustomEvent('ledit:reveal-in-explorer', {
      detail: { path: buffer.file.path }
    }));
    hideContextMenu();
  }, [buffer, hideContextMenu]);

  const handleCopyRelativePath = useCallback(() => {
    if (!buffer || !buffer.file) return;
    copyToClipboard(buffer.file.path);
    hideContextMenu();
  }, [buffer, hideContextMenu]);

  const handleCopyAbsolutePath = useCallback(() => {
    if (!buffer || !buffer.file) return;
    const root = workspaceRoot.replace(/\/+$/, '');
    copyToClipboard(root + '/' + buffer.file.path);
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
          console.warn('Failed to re-fetch git diff after save:', err);
        }
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Failed to save file';
      setError(errorMessage);
      console.error('Save error:', errorMessage);
    } finally {
      setSaving(false);
    }
  }, [buffer, saveBuffer, apiService]); // eslint-disable-line react-hooks/exhaustive-deps -- updateDiffGutter/clearDiffGutter are module-level functions

  // Fetch diagnostics for the current file and push them into the editor
  const fetchDiagnostics = useCallback(async (filePath: string, content: string) => {
    if (!viewRef.current) return;
    try {
      const result = await apiService.getDiagnostics(filePath, content);
      if (result.diagnostics && result.diagnostics.length > 0) {
        debouncedDiag.current.update(viewRef.current, result.diagnostics);
      } else {
        clearDiagnostics(viewRef.current);
      }
    } catch {
      // Diagnostics are best-effort — don't show errors for diagnostic failures
      clearDiagnostics(viewRef.current);
    }
  }, [apiService]); // eslint-disable-line react-hooks/exhaustive-deps

  // Keep ref in sync so loadFile can call fetchDiagnostics without a forward reference
  fetchDiagnosticsRef.current = fetchDiagnostics;

  const isExternalUpdateRef = useRef<boolean>(false);
  const lastLoadedRef = useRef<{bufferId: string, filePath: string} | null>(null);
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
            insert: ''
          }
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
            insert: nextContent
          }
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
    if (lastLoadedRef.current && 
        lastLoadedRef.current.bufferId === buffer.id && 
        lastLoadedRef.current.filePath === buffer.file.path) {
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

        // Update cursor position - wrap in try-catch to handle invalid positions during content loads
        try {
          const selection = update.state.selection.main;
          if (selection && buffer) {
            const line = update.state.doc.lineAt(selection.head).number;
            const column = selection.head - update.state.doc.line(selection.head).from;
            updateBufferCursor(buffer.id, { line, column });
          }
        } catch (e) {
          // Ignore position errors during large content changes
          console.debug('Cursor position update skipped during content change');
        }

        // Debounced: fetch diagnostics for the edited content
        if (buffer && buffer.kind === 'file' && buffer.file && !buffer.file.path.startsWith('__workspace/')) {
          fetchDiagnostics(buffer.file.path, newContent);
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
            const replaceInput = view.dom.querySelector<HTMLInputElement>(
              '.cm-search input[name="replace"]'
            );
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
      keymap.of([indentWithTab]),
      keymap.of(searchKeymap),
      keymap.of(customKeymap),
      keymap.of(replacePanelKeymap),
      search(),
      autocompletion(),
      closeBrackets(),
      history(),
      cursorHistoryPlugin,
      indentOnInput(),
      highlightSpecialChars(),
      highlightActiveLine(),
      bracketMatching(),
      syntaxHighlighting(customHighlightStyle || (themePack.editorSyntaxStyle === 'one-dark' ? oneDarkHighlightStyle : defaultHighlightStyle)),
      diffGutter(),
      lintDiagnostics(),
      lineNumbers(),
      foldGutter({
        openText: '▼',
        closedText: '▶',
      }),
      codeFolding(),
      EditorView.theme({
        '&': {
          height: '100%',
          fontSize: '13px',
          fontFamily: "'Monaco', 'Menlo', 'Fira Code', monospace",
          backgroundColor: 'var(--cm-bg)',
          color: 'var(--cm-fg)'
        },
        '.cm-content': {
          padding: '16px',
          caretColor: 'var(--cm-cursor, ' + (themePack.mode === 'dark' ? '#f8f8f2' : '#526fff') + ')'
        },
        '.cm-focused': {
          outline: 'none'
        },
        '.cm-gutters': {
          backgroundColor: 'var(--cm-gutter-bg)',
          border: 'none',
          color: 'var(--cm-gutter-fg)'
        },
        '.cm-scroller': {
          fontFamily: 'inherit',
          overflow: 'auto',
          minHeight: '0',
          height: '100%'
        },
        '.cm-cursor': {
          borderLeftColor: themePack.mode === 'dark' ? 'var(--cm-cursor, #f8f8f2)' : 'var(--cm-cursor, #526fff)',
          borderLeftWidth: '2px'
        },
        '&.cm-focused .cm-cursor': {
          borderLeftColor: themePack.mode === 'dark' ? 'var(--cm-cursor, #f8f8f2)' : 'var(--cm-cursor, #526fff)',
          borderLeftWidth: '2px'
        },
        '.cm-dropCursor': {
          borderLeftColor: themePack.mode === 'dark' ? 'var(--cm-cursor, #f8f8f2)' : 'var(--cm-cursor, #526fff)'
        },
        '.cm-selectionBackground, .cm-content ::selection': {
          backgroundColor: 'var(--cm-selection) !important'
        },
        '&.cm-focused .cm-activeLine': {
          backgroundColor: 'var(--cm-active-line)'
        },
        '.cm-activeLineGutter': {
          backgroundColor: 'var(--cm-active-line-gutter)',
          color: 'var(--cm-gutter-fg-active)'
        },
        '.cm-foldGutter': {
          width: '20px'
        },
        '.cm-foldGutter .cm-gutterElement': {
          padding: '0 4px',
          fontSize: '12px'
        },
        '.cm-foldGutter .cm-gutterElement:hover': {
          color: 'var(--accent-primary, #6366f1)'
        }
      }),
      lineWrappingCompartment.current.of(
        wordWrapEnabled ? EditorView.lineWrapping : []
      ),
      languageCompartment.current.of(
        getLanguageExtensions(
          resolveLanguageId(
            buffer?.languageOverride,
            buffer?.file?.ext?.replace(/^\./, ''),
            buffer?.file?.name,
          ).languageId,
        )
      ),
    ];

    const state = EditorState.create({
      doc: localContent,
      extensions
    });

    const view = new EditorView({
      state,
      parent: editorRef.current
    });

    viewRef.current = view;

    // Track which language was set during init so the reconfiguration effect
    // can skip a redundant reconfigure on the same buffer/language combo.
    lastInitLanguageKey.current = `${buffer?.id}:${buffer?.languageOverride ?? ''}:${buffer?.file?.ext ?? ''}:${buffer?.file?.name ?? ''}`;

    return () => {
      debouncedDiag.current.cancel();
      view.destroy();
      viewRef.current = null;
    };
  }, [paneId, buffer?.id, buffer?.file?.ext, theme, themePack.id, hotkeys, customHighlightStyle, updateBufferContent, setBufferModified, updateBufferCursor]); // eslint-disable-line react-hooks/exhaustive-deps -- handleSave intentionally excluded to prevent infinite re-init loop when buffer changes

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
      effects: languageCompartment.current.reconfigure(
        getLanguageExtensions(languageId),
      ),
    });
  }, [buffer?.id, buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]); // eslint-disable-line react-hooks/exhaustive-deps


  // Toggle word wrap: updates React state (for toolbar button) and
  // dispatches a CodeMirror compartment reconfigure to apply/remove
  // EditorView.lineWrapping.  Uses a ref mirror to avoid stale closures
  // inside the CodeMirror keymap and event-listener callbacks.
  const wordWrapRef = useRef(wordWrapEnabled);
  const lastWrapToggleRef = useRef(0);
  const onToggleWordWrap = useCallback(() => {
    const now = Date.now();
    if (now - lastWrapToggleRef.current < 100) return; // dedup: prevent double-toggle from global handler
    lastWrapToggleRef.current = now;
    const next = !wordWrapRef.current;
    wordWrapRef.current = next;
    setWordWrapEnabled(next);
    viewRef.current?.dispatch({
      effects: lineWrappingCompartment.current.reconfigure(
        next ? EditorView.lineWrapping : []
      ),
    });
  }, []);

  // Keep the ref mirror in sync whenever the state value changes from
  // an external source (e.g. the global event listener).
  useEffect(() => {
    wordWrapRef.current = wordWrapEnabled;
  }, [wordWrapEnabled]);

  // Listen for go to line event from toolbar and global word-wrap toggle.
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
      }
    };

    document.addEventListener('editor-goto-line', handler);
    document.addEventListener('editor-toggle-word-wrap', handler);
    return () => {
      document.removeEventListener('editor-goto-line', handler);
      document.removeEventListener('editor-toggle-word-wrap', handler);
    };
  }, [handleGoToLine, onToggleWordWrap]);

  // Compute effective language info for the LanguageSwitcher
  // (Must be declared before early returns to satisfy React hooks rules)
  const languageInfo = useMemo(() => {
    if (!buffer || !buffer.file) return { languageId: null as string | null, isAutoDetected: false };
    return resolveLanguageId(
      buffer.languageOverride ?? null,
      buffer.file?.ext?.replace(/^\./, ''),
      buffer.file?.name,
    );
  }, [buffer?.languageOverride, buffer?.file?.ext, buffer?.file?.name]);

  const handleLanguageChange = useCallback((languageId: string | null) => {
    if (!buffer) return;
    setBufferLanguageOverride(buffer.id, languageId);
  }, [buffer?.id, setBufferLanguageOverride]);


  if (!buffer || !buffer.file || buffer.file.isDir) {
    return (
      <div className="editor-pane empty">
        <div className="no-file-selected">
          <div className="no-file-icon"><File size={40} /></div>
          <div className="no-file-text">Select a file to edit</div>
        </div>
      </div>
    );
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
      }
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
      }
    });
  };

  if (imageFile) {
    return (
      <div className="editor-pane">
        <ImageViewer
          filePath={buffer.file.path}
          fileName={buffer.file.name}
          fileSize={buffer.file.size}
        />
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
          fileName={buffer.metadata?.sourceName || buffer.file.name}
          sourcePath={buffer.metadata?.sourcePath}
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
            {
              id: 'word-wrap-toggle',
              title: 'Toggle word wrap (Alt+Z)',
              icon: <WrapText size={16} />,
              active: wordWrapEnabled,
              onClick: () => document.dispatchEvent(new CustomEvent('editor-toggle-word-wrap')),
            },
            ...(isSvgFile ? [
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
              }
            ] : []),
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
};

export default EditorPane;
