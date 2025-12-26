import React, { useEffect, useRef, useState, useCallback } from 'react';
import { EditorView, keymap } from '@codemirror/view';
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
import { EditorBuffer } from '../types/editor';
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
  const [showUnsavedDialog, setShowUnsavedDialog] = useState<boolean>(false);
  const [pendingFile, setPendingFile] = useState<EditorBuffer | null>(null);

  const {
    panes,
    buffers,
    switchPane,
    updateBufferContent,
    updateBufferCursor,
    saveBuffer,
    setBufferModified
  } = useEditorManager();

  // Get the buffer for this pane
  const pane = panes.find(p => p.id === paneId);
  const buffer = pane?.bufferId ? buffers.get(pane.bufferId) : null;

  // Get language support based on file extension
  const getLanguageSupport = (ext?: string) => {
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
  };

  // Load file content
  const loadFile = async (filePath: string) => {
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
        updateBufferContent(paneId!, data.content);

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
  };

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
  }, [buffer?.id]);

  // Initialize CodeMirror editor
  useEffect(() => {
    if (!editorRef.current) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        const newContent = update.state.doc.toString();
        setLocalContent(newContent);
        updateBufferContent(paneId!, newContent);
        setBufferModified(paneId!, newContent !== buffer?.originalContent);
      }
    });

    const saveKeymap = {
      key: 'Mod-s',
      preventDefault: true,
      run: () => {
        handleSave();
        return true;
      }
    };

    const state = EditorState.create({
      doc: localContent,
      extensions: [
        updateListener,
        keymap.of(defaultKeymap),
        keymap.of([indentWithTab]),
        keymap.of(searchKeymap),
        keymap.of([saveKeymap]),
        search(),
        autocompletion(),
        oneDark,
        EditorView.theme({
          '&': {
            height: '100%',
            fontSize: '13px',
            fontFamily: "'Monaco', 'Menlo', monospace"
          },
          '.cm-content': {
            padding: '16px'
          },
          '.cm-focused': {
            outline: 'none'
          }
        }),
        EditorView.lineWrapping,
        ...getLanguageSupport(buffer?.file.ext)
      ]
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
  }, [buffer?.id]);

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        handleSave();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [buffer]);

  // Save buffer
  const handleSave = async () => {
    if (!buffer || !viewRef.current) return;

    try {
      await saveBuffer(paneId!);
    } catch (err) {
      setError('Failed to save file');
    }
  };

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
    <div className="editor-pane">
      <div className="pane-header">
        <div className="pane-info">
          <span className="file-name">{buffer.file.name}</span>
          <span className="file-path">{buffer.file.path}</span>
          {isModified && <span className="modified-indicator">● Modified</span>}
        </div>
        <div className="pane-actions">
          <button
            onClick={handleSave}
            disabled={!isModified}
            className="save-button"
            title="Save file (Ctrl+S)"
          >
            💾 Save
          </button>
        </div>
      </div>

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
        </div>
      </div>
    </div>
  );
};

export default EditorPane;
