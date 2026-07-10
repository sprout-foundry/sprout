/**
 * useEditorExtensions — builds the CodeMirror extension set from buffer config.
 *
 * Manages Compartment refs for all reconfigurable extensions (language, keymaps,
 * word wrap, minimap, font size, tab size, whitespace rendering, emmet,
 * auto-close tags, LSP) and provides a `buildExtensions()` factory that produces
 * the full extension array used when creating or re-initializing an EditorView.
 *
 * The hook does NOT own the EditorView lifecycle — the consumer creates and
 * destroys the view. It only provides the extension configuration.
 *
 * Target: ~300 lines (SP-010 Phase 1; raised from original 150-line estimate to
 * accommodate compartment management, detailed type definitions, and inline docs).
 */

import { autocompletion, closeBrackets } from '@codemirror/autocomplete';
import { defaultKeymap, indentWithTab, history } from '@codemirror/commands';
import {
  syntaxHighlighting,
  defaultHighlightStyle,
  codeFolding,
  foldGutter,
  indentOnInput,
  bracketMatching,
  indentUnit,
} from '@codemirror/language';
import type { HighlightStyle } from '@codemirror/language';
import { EditorState, Compartment, type Extension } from '@codemirror/state';
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark';
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
import { color } from '@uiw/codemirror-extensions-color';
import { hyperLink } from '@uiw/codemirror-extensions-hyper-link';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { useRef, useCallback } from 'react';
import { createAutoCloseTagCompartment, getInitialAutoCloseTagExtensions } from '../extensions/autoCloseTag';
import { bracketColorizationPlugin } from '../extensions/bracketColorization';
import { createCodeActionsExtension } from '../extensions/codeActions';
import { codeLensPlugin } from '../extensions/codeLens';
import { cursorHistoryPlugin } from '../extensions/cursorHistory';
import { diffGutter } from '../extensions/diffGutter';
import { dragDropMovePlugin } from '../extensions/dragDropMove';
import { createEmmetCompartment, getInitialEmmetExtensions } from '../extensions/emmet';
import { errorLensPlugin } from '../extensions/errorLens';
import { createHoverTooltipExtension } from '../extensions/hoverTooltip';
import { indentGuidesPlugin } from '../extensions/indentGuides';
import { inlayHintsExtension } from '../extensions/inlayHints';
import { getLanguageExtensions } from '../extensions/languageRegistry';
import { linkedScrollExtension } from '../extensions/linkedScroll';
import { lintDiagnostics } from '../extensions/lintDiagnostics';
import { minimapExtension } from '../extensions/minimap';
import { renameHighlightField } from '../extensions/renameOverlay';
import { customSearchExtension } from '../extensions/searchPanel';
import { signatureHelpExtension } from '../extensions/signatureHelp';
import { tabExpandSnippets } from '../extensions/snippets';
import { stickyScrollPlugin } from '../extensions/stickyScroll';
import { trailingWhitespacePlugin } from '../extensions/trailingWhitespace';
import { unsavedLineHighlight } from '../extensions/unsavedLineHighlight';
import { whitespaceRenderingPlugin } from '../extensions/whitespaceRendering';
import type { WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { wordHighlightsExtension } from '../extensions/wordHighlights';
import type { ThemePack } from '../themes/themePacks';

// ── Settings constants (shared with EditorPane) ─────────────────────────

/** Tab size value meaning "use tabs for indentation" (stored in state and localStorage) */
export const TAB_SIZE_TABS_MODE = 0;

export const TAB_SIZE_DEFAULT = 4;

// ── Types ────────────────────────────────────────────────────────────────

export interface ExtensionSettings {
  wordWrapEnabled: boolean;
  relativeLineNumbersEnabled: boolean;
  minimapEnabled: boolean;
  editorFontSize: number;
  editorTabSize: number;
  editorUsesTabs: boolean;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  inlayHintsEnabled: boolean;
  signatureHelpEnabled: boolean;
}

export interface ThemeConfig {
  themePack: ThemePack;
  customHighlightStyle: HighlightStyle | null;
}

export interface BufferInfo {
  /** Language ID (already resolved by `resolveLanguageId`). `null` for unknown/binary. */
  languageId: string | null;
  /**
   * Dynamic callbacks that read current state — avoids stale closures.
   *
   * **IMPORTANT:** Callers MUST provide ref-backed functions (e.g. `() => ref.current`),
   * NOT plain closures over state, because these callbacks are captured once at editor
   * init time and used for the entire lifetime of the EditorView. Plain values would
   * become stale after the first render.
   */
  getFilePath: () => string | undefined;
  getFileExt: () => string | undefined;
  getContent: () => string;
}

export interface ActionCallbacks {
  /**
   * Returns the current save function.
   *
   * **IMPORTANT:** Callers MUST return a ref-backed function (e.g. `() => saveRef.current`)
   * so that the save callback remains current across buffer switches and re-renders.
   * The double-indirection (`getSaveFn()()`) ensures the latest save implementation
   * is always called, even when invoked asynchronously from search panels.
   */
  getSaveFn: () => () => void;
}

export interface BuildExtensionsOptions {
  paneId: string;
  settings: ExtensionSettings;
  theme: ThemeConfig;
  buffer: BufferInfo;
  actions: ActionCallbacks;
  /** Pre-built keymap compartments — the caller constructs keymaps externally. */
  hotkeysCompartmentExtension: ReturnType<Compartment['of']>;
  /** Additional keymaps (e.g. search, replace, zoom, semantic) injected into the extension array. */
  extraKeymaps: Extension[];
}

// ── Hook ─────────────────────────────────────────────────────────────────

export interface UseEditorExtensionsReturn {
  /** Compartment refs — consumers dispatch reconfigure effects through these. */
  compartments: {
    hotkeys: Compartment;
    lineWrapping: Compartment;
    relativeLineNumbers: Compartment;
    language: Compartment;
    minimap: Compartment;
    whitespaceRendering: Compartment;
    emmet: Compartment;
    autoCloseTag: Compartment;
    fontSize: Compartment;
    tabSize: Compartment;
    lsp: Compartment;
    inlayHints: Compartment;
    signatureHelp: Compartment;
    history: Compartment;
  };
  /**
   * Build the full CodeMirror extension array for `EditorState.create()`.
   * Called once on editor init; subsequent changes go through compartment
   * reconfiguration.
   */
  buildExtensions: (options: BuildExtensionsOptions) => Extension[];
}

export function useEditorExtensions(): UseEditorExtensionsReturn {
  // ── Compartment refs ──────────────────────────────────────────────────
  // Each compartment wraps one extension (or group) that can be swapped at
  // runtime via `view.dispatch({ effects: compartment.reconfigure(…) })`
  // without rebuilding the entire editor.
  const compartments = useRef({
    hotkeys: new Compartment(),
    lineWrapping: new Compartment(),
    relativeLineNumbers: new Compartment(),
    language: new Compartment(),
    minimap: new Compartment(),
    whitespaceRendering: new Compartment(),
    emmet: createEmmetCompartment(),
    autoCloseTag: createAutoCloseTagCompartment(),
    fontSize: new Compartment(),
    tabSize: new Compartment(),
    lsp: new Compartment(),
    inlayHints: new Compartment(),
    signatureHelp: new Compartment(),
    history: new Compartment(),
  }).current; // stable reference — never recreated

  // ── Extension builder ─────────────────────────────────────────────────
  const buildExtensions = useCallback((options: BuildExtensionsOptions): Extension[] => {
    const { paneId, settings, theme, buffer, actions, hotkeysCompartmentExtension, extraKeymaps } = options;
    const { themePack, customHighlightStyle } = theme;
    const cursorColor = themePack.mode === 'dark' ? '#f8f8f2' : '#526fff';
    const effectiveTabSize = settings.editorTabSize === TAB_SIZE_TABS_MODE ? TAB_SIZE_DEFAULT : settings.editorTabSize;

    return [
      // ── Multi-cursor & selection ──
      EditorState.allowMultipleSelections.of(true),
      rectangularSelection(),
      drawSelection(),
      crosshairCursor(),
      dropCursor(),
      dragDropMovePlugin,

      // ── Keymaps ──
      keymap.of(defaultKeymap),
      tabExpandSnippets(),
      keymap.of([indentWithTab]),
      hotkeysCompartmentExtension,
      ...extraKeymaps,

      // ── Search ──
      customSearchExtension(() => actions.getSaveFn()()),
      wordHighlightsExtension(),

      // ── Edit helpers ──
      renameHighlightField,
      hyperLink,
      color,
      autocompletion(),
      createHoverTooltipExtension(buffer.getFilePath, buffer.getContent),
      closeBrackets(),
      // Wrapped in a compartment so useEditorFileIO can reconfigure it on
      // buffer switch — this is the only documented way to reset CodeMirror's
      // undo stack and prevents Cmd-Z from restoring content from a previously
      // open file.
      compartments.history.of(history()),
      cursorHistoryPlugin,

      // ── Visual aids ──
      indentGuidesPlugin(),
      stickyScrollPlugin(buffer.getFileExt),
      codeLensPlugin(buffer.getFileExt),
      linkedScrollExtension(paneId, () => buffer.getFilePath() ?? null),
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

      // ── Diagnostics & diff ──
      diffGutter(),
      lintDiagnostics(),
      errorLensPlugin(),
      createCodeActionsExtension(buffer.getFilePath, buffer.getContent),
      trailingWhitespacePlugin(),
      unsavedLineHighlight(),

      // ── Compartment-wrapped settings ──
      compartments.whitespaceRendering.of(whitespaceRenderingPlugin(settings.whitespaceRenderingMode)),
      // Note: `lineNumbersRelative` is a pre-built Extension (not a factory function),
      // while `lineNumbers()` is a factory that returns an Extension. Both produce
      // valid Extension values; the difference is the @uiw package pre-bakes the extension.
      compartments.relativeLineNumbers.of(settings.relativeLineNumbersEnabled ? lineNumbersRelative : lineNumbers()),
      scrollPastEnd(),
      foldGutter({ openText: 'v', closedText: '>' }),
      codeFolding(),
      compartments.minimap.of(settings.minimapEnabled ? minimapExtension() : []),
      compartments.inlayHints.of(
        settings.inlayHintsEnabled ? inlayHintsExtension(buffer.getFilePath, buffer.getContent, buffer.languageId) : [],
      ),
      compartments.signatureHelp.of(
        settings.signatureHelpEnabled
          ? signatureHelpExtension(buffer.getFilePath, buffer.getContent, buffer.languageId)
          : [],
      ),
      compartments.fontSize.of([EditorView.theme({ '&': { fontSize: `${settings.editorFontSize}px` } })]),
      compartments.tabSize.of([
        EditorState.tabSize.of(effectiveTabSize),
        indentUnit.of(settings.editorUsesTabs ? '\t' : ' '.repeat(effectiveTabSize)),
      ]),

      // ── Base editor theme (CSS variables, layout, caret color) ──
      EditorView.theme({
        '&': {
          height: '100%',
          fontFamily: "'Monaco', 'Menlo', 'Fira Code', monospace",
          backgroundColor: 'var(--cm-bg)',
          color: 'var(--cm-fg)',
        },
        '.cm-content': {
          padding: '16px',
          caretColor: `var(--cm-cursor, ${cursorColor})`,
        },
        '.cm-focused': { outline: 'none' },
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
        '.cm-cursor': { borderLeftColor: `var(--cm-cursor, ${cursorColor})`, borderLeftWidth: '2px' },
        '&.cm-focused .cm-cursor': { borderLeftColor: `var(--cm-cursor, ${cursorColor})`, borderLeftWidth: '2px' },
        '.cm-dropCursor': { borderLeftColor: `var(--cm-cursor, ${cursorColor})` },
        '.cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'var(--cm-selection) !important' },
        '&.cm-focused .cm-activeLine': { backgroundColor: 'var(--cm-active-line)' },
        '.cm-activeLineGutter': {
          backgroundColor: 'var(--cm-active-line-gutter)',
          color: 'var(--cm-gutter-fg-active)',
        },
        '.cm-foldGutter': { width: '20px' },
        '.cm-foldGutter .cm-gutterElement': { padding: '0 4px', fontSize: '12px' },
        '.cm-foldGutter .cm-gutterElement:hover': { color: 'var(--accent-primary)' },
      }),

      // ── Compartment-wrapped toggles ──
      compartments.lineWrapping.of(settings.wordWrapEnabled ? EditorView.lineWrapping : []),
      compartments.emmet.of(getInitialEmmetExtensions(buffer.languageId)),
      compartments.autoCloseTag.of(getInitialAutoCloseTagExtensions(buffer.languageId)),
      compartments.language.of(getLanguageExtensions(buffer.languageId)),
      // LSP extensions are injected later via compartment reconfigure once the
      // language server connects. Empty array = no-op placeholder until then.
      compartments.lsp.of([]),
    ];
  }, []); // compartments is stable via useRef

  return { compartments, buildExtensions };
}
