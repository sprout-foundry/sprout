import React, { useEffect, useRef, useState, useCallback } from 'react';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { defaultKeymap, indentWithTab } from '@codemirror/commands';
import { search, searchKeymap } from '@codemirror/search';
import { autocompletion } from '@codemirror/autocomplete';
import { syntaxHighlighting, defaultHighlightStyle, codeFolding, foldGutter } from '@codemirror/language';
import { bracketMatching } from '@codemirror/language';
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark';

// Language support
import { javascript } from '@codemirror/lang-javascript';
import { python } from '@codemirror/lang-python';
import { go } from '@codemirror/lang-go';
import { json } from '@codemirror/lang-json';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';
import { markdown } from '@codemirror/lang-markdown';
import { php } from '@codemirror/lang-php';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import EditorToolbar from './EditorToolbar';
import ImageViewer from './ImageViewer';
import { readFileWithConsent } from '../services/fileAccess';
import { getEditorKeymap } from '../utils/editorHotkeys';
import { diffGutter, updateDiffGutter, clearDiffGutter } from '../extensions/diffGutter';
import { ApiService } from '../services/api';
import {
  File,
  Loader2,
  AlertTriangle,
} from 'lucide-react';
import './EditorPane.css';

interface EditorPaneProps {
  paneId: string;
}

const EditorPane: React.FC<EditorPaneProps> = ({ paneId }) => {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');

  const {
    panes,
    buffers,
    updateBufferContent,
    updateBufferCursor,
    saveBuffer,
    setBufferModified
  } = useEditorManager();

  const { theme, themePack, customHighlightStyle } = useTheme();
  const { hotkeys } = useHotkeys();

  // Get buffer for this pane
  const pane = panes.find(p => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  // Image extensions that should be viewed as images
  const IMAGE_EXTENSIONS = new Set([
    '.png', '.jpg', '.jpeg', '.gif', '.bmp', '.webp', '.svg', '.ico', '.tiff', '.tif', '.avif'
  ]);

  const isImageFile = (ext?: string): boolean => {
    return !!ext && IMAGE_EXTENSIONS.has(ext.toLowerCase());
  };

  // API service instance (singleton)
  const apiService = useRef(ApiService.getInstance()).current;

  // Get language support based on file extension
  const getLanguageSupport = useCallback((ext?: string) => {
    if (!ext) return [];

    switch (ext.toLowerCase()) {
      case '.js':
      case '.jsx':
      case '.mjs':
        return [javascript({ typescript: false }), javascript()];
      case '.ts':
      case '.tsx':
        return [javascript({ typescript: true }), javascript()];
      case '.py':
        return [python()];
      case '.go':
        return [go()];
      case '.json':
        return [json()];
      case '.html':
      case '.htm':
        return [html()];
      case '.css':
        return [css()];
      case '.md':
      case '.markdown':
        return [markdown()];
      case '.php':
        return [php()];
      default:
        return [];
    }
  }, []);

  const loadFileRef = useRef<((filePath: string) => Promise<void>) | null>(null);

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

      // Update buffer in context to keep it in sync with editor
      if (buffer) {
        updateBufferContent(buffer.id, content);
        setBufferModified(buffer.id, content !== buffer.originalContent);
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
    } catch (err) {
      console.error('[EditorPane loadFile] Error:', err);
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      isExternalUpdateRef.current = false;
      setLoading(false);
    }
  }, [apiService, buffer, updateBufferContent, setBufferModified]);

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
  }, [buffer, saveBuffer, apiService, updateDiffGutter, clearDiffGutter]);

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
    });

    const extensions = [
      updateListener,
      keymap.of(defaultKeymap),
      keymap.of([indentWithTab]),
      keymap.of(searchKeymap),
      keymap.of(customKeymap),
      search(),
      autocompletion(),
      bracketMatching(),
      syntaxHighlighting(customHighlightStyle || (themePack.editorSyntaxStyle === 'one-dark' ? oneDarkHighlightStyle : defaultHighlightStyle)),
      diffGutter(),
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
      EditorView.lineWrapping,
      ...getLanguageSupport(buffer?.file.ext)
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

    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, [paneId, buffer?.id, buffer?.file?.ext, theme, themePack.id, hotkeys, customHighlightStyle, updateBufferContent, setBufferModified, updateBufferCursor, getLanguageSupport]); // eslint-disable-line react-hooks/exhaustive-deps -- handleSave intentionally excluded to prevent infinite re-init loop when buffer changes


  // Listen for go to line event from toolbar
  useEffect(() => {
    const handler = (e: Event) => {
      if (e.type === 'editor-goto-line') {
        const customEvent = e as CustomEvent;
        if (customEvent.detail?.line) {
          handleGoToLine(customEvent.detail.line);
        }
      }
    };

    document.addEventListener('editor-goto-line', handler);
    return () => document.removeEventListener('editor-goto-line', handler);
  }, [handleGoToLine]);

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

  return (
    <div className="editor-pane">
      <EditorToolbar
        paneId={paneId}
        onGoToLine={handleGoToLine}
        onSave={handleSave}
        saving={saving}
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

      <div className="pane-content">
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
    </div>
  );
};

export default EditorPane;
