import React, { useEffect, useRef, useState, useCallback } from 'react';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { defaultKeymap, indentWithTab } from '@codemirror/commands';
import { search, searchKeymap } from '@codemirror/search';
import { autocompletion } from '@codemirror/autocomplete';
import { oneDark } from '@codemirror/theme-one-dark';
import { syntaxHighlighting, defaultHighlightStyle, codeFolding, foldGutter } from '@codemirror/language';
import { bracketMatching } from '@codemirror/language';

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
import { useTheme } from '../contexts/ThemeContext';
import EditorToolbar from './EditorToolbar';
import './EditorPane.css';

interface EditorPaneProps {
  paneId: string;
}

const EditorPane: React.FC<EditorPaneProps> = ({ paneId }) => {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [localContent, setLocalContent] = useState<string>('');
  const [showLineNumbers, setShowLineNumbers] = useState<boolean>(true);

  const {
    panes,
    buffers,
    updateBufferContent,
    updateBufferCursor,
    saveBuffer,
    setBufferModified
  } = useEditorManager();

  const { theme } = useTheme();

  // Get buffer for this pane
  const pane = panes.find(p => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

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

  // Get theme extension based on current theme
  const getThemeExtension = useCallback(() => {
    // For now, we use oneDark but support light theme via CSS overrides
    // A full light theme implementation would require additional packages
    return oneDark;
  }, []);

  const loadFileRef = useRef<((filePath: string) => Promise<void>) | null>(null);

  // Load file content - does NOT update buffer in context to avoid infinite loop
  const loadFile = useCallback(async (filePath: string) => {
    setLoading(true);
    setError(null);
    isExternalUpdateRef.current = true;

    try {
      const response = await fetch(`/api/file?path=${encodeURIComponent(filePath)}`);
      if (!response.ok) {
        throw new Error(`Failed to load file: ${response.statusText}`);
      }

      // Server returns raw file content as text, not JSON
      const content = await response.text();

      setLocalContent(content);

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
    } catch (err) {
      console.error('[EditorPane loadFile] Error:', err);
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      isExternalUpdateRef.current = false;
      setLoading(false);
    }
  }, []);

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

  // Toggle line numbers
  const handleToggleLineNumbers = useCallback(() => {
    setShowLineNumbers(prev => !prev);
  }, []);

  // Save buffer
  const handleSave = useCallback(async () => {
    if (!buffer || !viewRef.current) return;

    try {
      await saveBuffer(paneId);
    } catch (err) {
      setError('Failed to save file');
    }
  }, [buffer, paneId, saveBuffer]);

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
  }, [paneId]);

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

    // Keyboard shortcuts
    const customKeymap = [
      {
        key: 'Mod-s',
        preventDefault: true,
        run: () => {
          handleSave();
          return true;
        }
      },
      {
        key: 'Mod-l',
        preventDefault: true,
        run: () => {
          handleToggleLineNumbers();
          return true;
        }
      },
      {
        key: 'Mod-g',
        preventDefault: true,
        run: (view: EditorView) => {
          // Trigger go to line via an event that the toolbar handles
          const event = new CustomEvent('editor-goto-line');
          document.dispatchEvent(event);
          return true;
        }
      }
    ];

    const extensions = [
      updateListener,
      keymap.of(defaultKeymap),
      keymap.of([indentWithTab]),
      keymap.of(searchKeymap),
      keymap.of(customKeymap),
      search(),
      autocompletion(),
      bracketMatching(),
      syntaxHighlighting(defaultHighlightStyle),
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
          fontFamily: "'Monaco', 'Menlo', 'Fira Code', monospace"
        },
        '.cm-content': {
          padding: '16px'
        },
        '.cm-focused': {
          outline: 'none'
        },
        '.cm-gutters': {
          backgroundColor: 'var(--gutter-bg, #1e1e1e)',
          border: 'none',
          color: 'var(--gutter-fg, #666)'
        },
        '.cm-scroller': {
          fontFamily: 'inherit'
        },
        '&.cm-focused .cm-activeLine': {
          backgroundColor: 'rgba(99, 102, 241, 0.1)'
        },
        '.cm-activeLineGutter': {
          backgroundColor: 'rgba(99, 102, 241, 0.1)',
          color: 'var(--gutter-fg-active, #ccc)'
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
  }, [paneId, buffer?.id, buffer?.file?.ext, showLineNumbers, theme, updateBufferContent, setBufferModified, updateBufferCursor, getLanguageSupport, getThemeExtension]); // eslint-disable-line react-hooks/exhaustive-deps -- handleSave and handleToggleLineNumbers intentionally excluded to prevent infinite re-init loop when buffer changes


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
          <div className="no-file-icon">📄</div>
          <div className="no-file-text">Select a file to edit</div>
        </div>
      </div>
    );
  }

  return (
    <div className="editor-pane" data-theme={theme}>
      <EditorToolbar
        paneId={paneId}
        showLineNumbers={showLineNumbers}
        onToggleLineNumbers={handleToggleLineNumbers}
        onGoToLine={handleGoToLine}
      />

      {loading && (
        <div className="loading-indicator">
          <div className="spinner">⚡</div>
          <span>Loading file...</span>
        </div>
      )}

      {error && (
        <div className="error-message">
          <span className="error-icon">⚠️</span>
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
