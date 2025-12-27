import React, { useEffect, useRef, useState, useCallback } from 'react';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { defaultKeymap, indentWithTab } from '@codemirror/commands';
import { search, searchKeymap } from '@codemirror/search';
import { autocompletion } from '@codemirror/autocomplete';
import { oneDark } from '@codemirror/theme-one-dark';

// Language support
import { javascript } from '@codemirror/lang-javascript';
import { python } from '@codemirror/lang-python';
import { go } from '@codemirror/lang-go';
import { json } from '@codemirror/lang-json';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';

import { useEditorManager } from '../contexts/EditorManagerContext';
import { useTheme } from '../contexts/ThemeContext';
import { EditorBuffer } from '../types/editor';
import EditorToolbar from './EditorToolbar';
import './EditorPane.css';

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
}

interface FileResponse {
  message: string;
  path: string;
  content: string;
  size: number;
  modified: number;
  ext: string;
}

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
    switchPane,
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
        return [javascript({ typescript: false })];
      case '.ts':
      case '.tsx':
        return [javascript({ typescript: true })];
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

  // Load file content
  const loadFile = useCallback(async (filePath: string) => {
    setLoading(true);
    setError(null);

    try {
      const response = await fetch(`/api/file?path=${encodeURIComponent(filePath)}`);
      if (!response.ok) {
        throw new Error(`Failed to load file: ${response.statusText}`);
      }

      const data: FileResponse = await response.json();
      if (data.message === 'success') {
        setLocalContent(data.content);
        updateBufferContent(paneId, data.content);

        // Update editor if it exists
        if (viewRef.current) {
          viewRef.current.dispatch({
            changes: {
              from: 0,
              to: viewRef.current.state.doc.length,
              insert: data.content
            }
          });
        }
      } else {
        throw new Error(data.message);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  }, [paneId, updateBufferContent]);

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

  // Load file when buffer changes
  useEffect(() => {
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
      return;
    }

    // If buffer has content and hasn't been modified, use cached content
    if (buffer.content && !buffer.isModified) {
      setLocalContent(buffer.content);
      if (viewRef.current) {
        viewRef.current.dispatch({
          changes: {
            from: 0,
            to: viewRef.current.state.doc.length,
            insert: buffer.content
          }
        });
      }
      setError(null);
      return;
    }

    // Load file from server
    loadFile(buffer.file.path);
  }, [buffer?.id, buffer?.isModified, buffer?.content, buffer?.file, loadFile]);

  // Initialize CodeMirror editor
  useEffect(() => {
    if (!editorRef.current) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        const newContent = update.state.doc.toString();
        setLocalContent(newContent);
        updateBufferContent(paneId, newContent);
        setBufferModified(paneId, newContent !== buffer?.originalContent);

        // Update cursor position
        const selection = update.state.selection.main;
        if (selection) {
          const line = update.state.doc.lineAt(selection.head).number;
          const column = selection.head - update.state.doc.line(selection.head).from;
          updateBufferCursor(paneId, { line, column });
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
      getThemeExtension(),
      showLineNumbers ? lineNumbers() : [],
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
  }, [paneId, buffer?.id, buffer?.file?.ext, showLineNumbers, theme, updateBufferContent, setBufferModified, handleToggleLineNumbers, handleSave]); // NOTE: localContent is NOT in deps to avoid re-init on every keystroke

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

  const isModified = buffer.content !== buffer.originalContent;

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
          <span className="line-count">Lines: {localContent.split('\n').length}</span>
          <span className="char-count">Chars: {localContent.length}</span>
          <span className="cursor-position">
            Ln {buffer.cursorPosition.line + 1}, Col {buffer.cursorPosition.column + 1}
          </span>
        </div>
      </div>
    </div>
  );
};

export default EditorPane;
