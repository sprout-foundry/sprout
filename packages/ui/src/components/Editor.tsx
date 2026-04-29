/**
 * Editor component for @sprout/ui
 *
 * A CodeMirror 6-based code editor. In the standalone package, this is a
 * simplified version that provides the core editing experience with language
 * support, search, and basic extensions. The full-featured editor with LSP,
 * semantic analysis, and API-backed features should be composed on top of this.
 */
import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import {
  EditorView,
  keymap,
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
import { EditorState, Compartment } from '@codemirror/state';
import { defaultKeymap, indentWithTab, history, undo, redo } from '@codemirror/commands';
import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
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
import { debugLog } from '../utils/log';
import './Editor.css';

// ── Types ──────────────────────────────────────────────────────────────

export interface EditorTheme {
  dark?: boolean;
  background?: string;
  foreground?: string;
  fontFamily?: string;
  fontSize?: number;
  highlightStyle?: typeof oneDarkHighlightStyle;
}

export interface CursorPosition {
  line: number;
  column: number;
}

export interface EditorProps {
  /** Initial content */
  value?: string;
  /** File path (used for language detection) */
  filePath?: string;
  /** Language override */
  language?: string;
  /** Read-only mode */
  readOnly?: boolean;
  /** Word wrap */
  wordWrap?: boolean;
  /** Font size in pixels */
  fontSize?: number;
  /** Font family */
  fontFamily?: string;
  /** Tab size */
  tabSize?: number;
  /** Theme */
  theme?: EditorTheme;
  /** Line number to highlight */
  highlightLine?: number;
  /** Whether the editor is focused */
  autoFocus?: boolean;

  // Callbacks
  onChange?: (value: string) => void;
  onSave?: (value: string) => void;
  onCursorChange?: (position: CursorPosition) => void;
  onFocus?: () => void;
  onBlur?: () => void;

  // Extra CodeMirror extensions
  extensions?: import('@codemirror/state').Extension[];
}

// ── Component ──────────────────────────────────────────────────────────

function Editor({
  value = '',
  filePath,
  language,
  readOnly = false,
  wordWrap = false,
  fontSize = 13,
  fontFamily = "'JetBrains Mono', 'Fira Code', Menlo, Monaco, monospace",
  tabSize = 4,
  theme,
  highlightLine,
  autoFocus = false,
  onChange,
  onSave,
  onCursorChange,
  onFocus,
  onBlur,
  extensions,
}: EditorProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const compartmentsRef = useRef({
    language: new Compartment(),
    tabSize: new Compartment(),
    readOnly: new Compartment(),
    wrap: new Compartment(),
    theme: new Compartment(),
    extra: new Compartment(),
  });
  const isExternalUpdateRef = useRef(false);

  // Create editor
  useEffect(() => {
    if (!containerRef.current) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (update.docChanged && !isExternalUpdateRef.current) {
        onChange?.(update.state.doc.toString());
      }
      if (update.selectionSet) {
        const pos = update.state.selection.main.head;
        const line = update.state.doc.lineAt(pos);
        onCursorChange?.({ line: line.number, column: pos - line.from + 1 });
      }
      if (update.focusChanged) {
        if (update.view.hasFocus) onFocus?.();
        else onBlur?.();
      }
    });

    const saveKeymap = keymap.of([
      {
        key: 'Mod-s',
        run: (view) => {
          onSave?.(view.state.doc.toString());
          return true;
        },
      },
    ]);

    const baseExtensions = [
      lineNumbers(),
      highlightSpecialChars(),
      highlightActiveLine(),
      highlightActiveLineGutter(),
      history(),
      foldGutter(),
      codeFolding(),
      drawSelection(),
      dropCursor(),
      indentOnInput(),
      bracketMatching(),
      closeBrackets(),
      autocompletion(),
      highlightSelectionMatches(),
      rectangularSelection(),
      crosshairCursor(),
      scrollPastEnd(),
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      syntaxHighlighting(oneDarkHighlightStyle),
      keymap.of([...defaultKeymap, indentWithTab, ...searchKeymap]),
      updateListener,
      saveKeymap,
      EditorView.lineWrapping,
      compartmentsRef.current.tabSize.of(indentUnit.of(' '.repeat(tabSize))),
      compartmentsRef.current.readOnly.of(EditorState.readOnly.of(readOnly)),
      compartmentsRef.current.wrap.of(wordWrap ? EditorView.lineWrapping : []),
      compartmentsRef.current.extra.of(extensions ?? []),
      EditorView.theme({
        '&': {
          fontSize: `${fontSize}px`,
          fontFamily,
        },
        '.cm-content': {
          fontFamily,
        },
        '.cm-gutters': {
          fontFamily,
        },
      }),
    ];

    const state = EditorState.create({
      doc: value,
      extensions: baseExtensions,
    });

    const view = new EditorView({
      state,
      parent: containerRef.current,
    });

    viewRef.current = view;

    if (autoFocus) {
      setTimeout(() => view.focus(), 50);
    }

    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Update value from outside
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    const currentValue = view.state.doc.toString();
    if (value !== currentValue) {
      isExternalUpdateRef.current = true;
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value },
      });
      isExternalUpdateRef.current = false;
    }
  }, [value]);

  // Update tab size
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: compartmentsRef.current.tabSize.reconfigure(indentUnit.of(' '.repeat(tabSize))),
    });
  }, [tabSize]);

  // Update readOnly
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: compartmentsRef.current.readOnly.reconfigure(EditorState.readOnly.of(readOnly)),
    });
  }, [readOnly]);

  // Update word wrap
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: compartmentsRef.current.wrap.reconfigure(wordWrap ? [EditorView.lineWrapping] : []),
    });
  }, [wordWrap]);

  // Update extra extensions
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: compartmentsRef.current.extra.reconfigure(extensions ?? []),
    });
  }, [extensions]);

  // Note: font size is set in the initial theme. For dynamic updates,
  // the host should provide extensions that use a Compartment for theme.

  return (
    <div
      ref={containerRef}
      className="sprout-editor"
      style={{ width: '100%', height: '100%' }}
    />
  );
}

export default Editor;
