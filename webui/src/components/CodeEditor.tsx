import React, { useEffect, useRef, useState } from 'react';
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

import './CodeEditor.css';

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

interface CodeEditorProps {
  file: FileInfo | null;
  onSave?: (content: string) => void;
}

const CodeEditor: React.FC<CodeEditorProps> = ({ file, onSave }) => {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const [content, setContent] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(false);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [isModified, setIsModified] = useState<boolean>(false);

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
        setContent(data.content);
        setIsModified(false);

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

  // Save file content
  const saveFile = async () => {
    if (!file || !viewRef.current) return;

    setSaving(true);
    setError(null);

    try {
      const currentContent = viewRef.current.state.doc.toString();

      const response = await fetch(`/api/file?path=${encodeURIComponent(file.path)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ content: currentContent }),
      });

      if (!response.ok) {
        throw new Error(`Failed to save file: ${response.statusText}`);
      }

      const data = await response.json();
      if (data.message === 'File saved successfully') {
        setIsModified(false);
        setContent(currentContent);
        if (onSave) {
          onSave(currentContent);
        }
      } else {
        throw new Error(data.message);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setSaving(false);
    }
  };

  // Initialize CodeMirror editor
  useEffect(() => {
    if (!editorRef.current) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        const newContent = update.state.doc.toString();
        setContent(newContent);
        setIsModified(newContent !== content || content === '');
      }
    });

    const saveKeymap = {
      key: 'Mod-s',
      preventDefault: true,
      run: () => {
        saveFile();
        return true;
      }
    };

    const state = EditorState.create({
      doc: content,
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
        ...getLanguageSupport(file?.ext)
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
  }, [file]);

  // Load file when file changes
  useEffect(() => {
    if (file && !file.isDir) {
      loadFile(file.path);
    } else {
      setContent('');
      setIsModified(false);
      if (viewRef.current) {
        viewRef.current.dispatch({
          changes: {
            from: 0,
            to: viewRef.current.state.doc.length,
            insert: ''
          }
        });
      }
    }
  }, [file]);

  // Keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        saveFile();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [file, content]);

  if (!file || file.isDir) {
    return (
      <div className="code-editor">
        <div className="editor-header">
          <h3>📝 Code Editor</h3>
        </div>
        <div className="no-file-selected">
          <div className="no-file-icon">📄</div>
          <div className="no-file-text">Select a file to edit</div>
        </div>
      </div>
    );
  }

  return (
    <div className="code-editor">
      <div className="editor-header">
        <div className="editor-info">
          <h3>📝 Code Editor</h3>
          <div className="file-info">
            <span className="file-name">{file.name}</span>
            <span className="file-path">{file.path}</span>
            {isModified && <span className="modified-indicator">● Modified</span>}
          </div>
        </div>
        <div className="editor-controls">
          <button
            onClick={saveFile}
            disabled={saving || !isModified}
            className="save-button"
            title="Save file (Ctrl+S)"
          >
            {saving ? '⚡ Saving...' : '💾 Save'}
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

      <div className="editor-container">
        <div ref={editorRef} className="editor" />
      </div>

      <div className="editor-footer">
        <div className="editor-stats">
          <span className="line-count">
            Lines: {content.split('\n').length}
          </span>
          <span className="char-count">
            Characters: {content.length}
          </span>
        </div>
        <div className="editor-help">
          <span className="help-text">Ctrl+S to save</span>
        </div>
      </div>
    </div>
  );
};

export default CodeEditor;