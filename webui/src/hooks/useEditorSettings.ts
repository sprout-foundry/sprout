/**
 * useEditorSettings — manages editor settings state and compartment reconfiguration.
 *
 * Extracts all editor settings logic from EditorPane:
 * - LocalStorage initialization and persistence
 * - Settings state (font size, tab size, word wrap, etc.)
 * - Toggle/cycle callbacks with compartment reconfiguration
 * - Ref mirrors for dedup and stale closure avoidance
 * - Settings reset on file change (indent detection, line ending)
 *
 * Target: ~400 lines
 */

import { indentUnit } from '@codemirror/language';
import { EditorState, type Compartment } from '@codemirror/state';
import { EditorView, lineNumbers } from '@codemirror/view';
import { lineNumbersRelative } from '@uiw/codemirror-extensions-line-numbers-relative';
import { useRef, useState, useEffect, useCallback } from 'react';
import type { LineEnding } from '../extensions/lineEndingDetect';
import { minimapExtension } from '../extensions/minimap';
import { whitespaceRenderingPlugin } from '../extensions/whitespaceRendering';
import { type WhitespaceRenderingMode } from '../extensions/whitespaceRendering';
import { debugLog } from '../utils/log';
import { TAB_SIZE_TABS_MODE, TAB_SIZE_DEFAULT } from './useEditorExtensions';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Minimum legible font size */
const FONT_SIZE_MIN = 8;

/** Default font size (matches Monaco/Menlo editor defaults) */
const FONT_SIZE_DEFAULT = 13;

/** Maximum font size for accessibility (WCAG supports 200% zoom) */
const FONT_SIZE_MAX = 72;

/** Tab size options for cycling */
const TAB_SIZE_OPTIONS = [2, 4, 8] as const;

/** Minimum number of indented lines required for auto-detection confidence */
const MIN_INDENTED_LINES_FOR_DETECTION = 3;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseEditorSettingsReturn {
  // State
  editorFontSize: number;
  editorTabSize: number;
  editorUsesTabs: boolean;
  wordWrapEnabled: boolean;
  relativeLineNumbersEnabled: boolean;
  minimapEnabled: boolean;
  whitespaceRenderingMode: WhitespaceRenderingMode;
  indentManuallySet: boolean;
  lineEnding: LineEnding;
  inlayHintsEnabled: boolean;
  signatureHelpEnabled: boolean;

  // Ref mirrors
  wordWrapRef: React.MutableRefObject<boolean>;
  minimapEnabledRef: React.MutableRefObject<boolean>;
  relativeLineNumbersEnabledRef: React.MutableRefObject<boolean>;
  whitespaceRenderingModeRef: React.MutableRefObject<WhitespaceRenderingMode>;
  indentManuallySetRef: React.MutableRefObject<boolean>;
  inlayHintsEnabledRef: React.MutableRefObject<boolean>;
  signatureHelpEnabledRef: React.MutableRefObject<boolean>;

  // Setters for external use (e.g., file loading, buffer changes)
  setEditorTabSize: (v: number) => void;
  setEditorUsesTabs: (v: boolean) => void;
  setIndentManuallySet: (v: boolean) => void;
  setLineEnding: (v: LineEnding) => void;

  // Callbacks
  onZoomIn: () => void;
  onZoomOut: () => void;
  onResetZoom: () => void;
  onCycleTabSize: () => void;
  onToggleWordWrap: () => void;
  onToggleMinimap: () => void;
  onToggleRelativeLineNumbers: () => void;
  onCycleWhitespaceRendering: () => WhitespaceRenderingMode;
  onToggleInlayHints: () => void;
  onToggleSignatureHelp: () => void;
}

export interface EditorSettingsCompartments {
  fontSize: Compartment;
  tabSize: Compartment;
  lineWrapping: Compartment;
  minimap: Compartment;
  relativeLineNumbers: Compartment;
  whitespaceRendering: Compartment;
}

/**
 * Hook that manages all editor settings state and compartment reconfiguration.
 *
 * @param compartments - CodeMirror compartments from useEditorExtensions
 * @param bufferId - Current buffer ID (triggers indent reset on change)
 * @returns Settings state, refs, setters, and toggle callbacks
 */
export function useEditorSettings(
  compartments: EditorSettingsCompartments,
  bufferId: string | undefined,
): UseEditorSettingsReturn {
  // ---------------------------------------------------------------------------
  // LocalStorage helpers
  // ---------------------------------------------------------------------------

  const getStoredFontSize = (): number => {
    try {
      const stored = localStorage.getItem('editor:font-size');
      if (stored === null) return FONT_SIZE_DEFAULT;
      const parsed = parseInt(stored, 10);
      if (!isNaN(parsed) && parsed >= FONT_SIZE_MIN && parsed <= FONT_SIZE_MAX) {
        return parsed;
      }
      return FONT_SIZE_DEFAULT;
    } catch (err) {
      debugLog('Failed to read font size from localStorage:', err);
      return FONT_SIZE_DEFAULT;
    }
  };

  const getStoredTabSize = (): number => {
    try {
      const stored = localStorage.getItem('editor:tab-size');
      if (stored === null) return TAB_SIZE_DEFAULT;
      if (stored === '0') return TAB_SIZE_TABS_MODE;
      const parsed = parseInt(stored, 10);
      if (!isNaN(parsed) && TAB_SIZE_OPTIONS.includes(parsed as (typeof TAB_SIZE_OPTIONS)[number])) {
        return parsed;
      }
      return TAB_SIZE_DEFAULT;
    } catch (err) {
      debugLog('Failed to read tab size from localStorage:', err);
      return TAB_SIZE_DEFAULT;
    }
  };

  const getStoredRelativeLineNumbers = (): boolean => {
    try {
      const stored = localStorage.getItem('editor:relative-line-numbers-enabled');
      return stored !== null ? stored === 'true' : false;
    } catch (err) {
      debugLog('Failed to read relative line numbers setting from localStorage:', err);
      return false;
    }
  };

  const getStoredWordWrapEnabled = (): boolean => {
    try {
      const stored = localStorage.getItem('editor:word-wrap-enabled');
      return stored !== null ? stored === 'true' : true;
    } catch (err) {
      debugLog('Failed to read word wrap setting from localStorage:', err);
      return true;
    }
  };

  const getStoredMinimapEnabled = (): boolean => {
    try {
      const stored = localStorage.getItem('editor:minimap-enabled');
      return stored !== null ? stored === 'true' : true;
    } catch (err) {
      debugLog('Failed to read minimap setting from localStorage:', err);
      return true;
    }
  };

  const getStoredWhitespaceRenderingMode = (): WhitespaceRenderingMode => {
    try {
      const stored = localStorage.getItem('editor:whitespace-rendering-mode');
      if (stored === 'none' || stored === 'boundary' || stored === 'all') {
        return stored as WhitespaceRenderingMode;
      }
      return 'none';
    } catch (err) {
      debugLog('Failed to read whitespace rendering mode from localStorage:', err);
      return 'none';
    }
  };

  const getStoredInlayHintsEnabled = (): boolean => {
    try {
      const stored = localStorage.getItem('editor:inlay-hints-enabled');
      return stored !== null ? stored === 'true' : true;
    } catch (err) {
      debugLog('Failed to read inlay hints setting from localStorage:', err);
      return true;
    }
  };

  const getStoredSignatureHelpEnabled = (): boolean => {
    try {
      const stored = localStorage.getItem('editor:signature-help-enabled');
      return stored !== null ? stored === 'true' : true;
    } catch (err) {
      debugLog('Failed to read signature help setting from localStorage:', err);
      return true;
    }
  };

  // ---------------------------------------------------------------------------
  // Settings state
  // ---------------------------------------------------------------------------

  const [wordWrapEnabled, setWordWrapEnabled] = useState<boolean>(getStoredWordWrapEnabled);

  const [relativeLineNumbersEnabled, setRelativeLineNumbersEnabled] = useState<boolean>(getStoredRelativeLineNumbers);

  const [minimapEnabled, setMinimapEnabled] = useState<boolean>(getStoredMinimapEnabled);

  const [editorFontSize, setEditorFontSize] = useState<number>(getStoredFontSize);

  const [editorTabSize, setEditorTabSize] = useState<number>(getStoredTabSize);

  const [editorUsesTabs, setEditorUsesTabs] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem('editor:tab-size');
      return stored === '0';
    } catch (err) {
      return false;
    }
  });

  const [indentManuallySet, setIndentManuallySet] = useState<boolean>(false);

  const [lineEnding, setLineEnding] = useState<LineEnding>('LF');

  const [inlayHintsEnabled, setInlayHintsEnabled] = useState<boolean>(getStoredInlayHintsEnabled);
  const [signatureHelpEnabled, setSignatureHelpEnabled] = useState<boolean>(getStoredSignatureHelpEnabled);

  // ---------------------------------------------------------------------------
  // Ref mirrors for dedup and stale closure avoidance
  // ---------------------------------------------------------------------------

  const wordWrapRef = useRef(wordWrapEnabled);
  const lastWrapToggleRef = useRef(0);
  const minimapEnabledRef = useRef(minimapEnabled);
  const lastMinimapToggleRef = useRef(0);
  const relativeLineNumbersEnabledRef = useRef(relativeLineNumbersEnabled);
  const lastRelativeLineNumbersToggleRef = useRef(0);
  const whitespaceRenderingModeRef = useRef<WhitespaceRenderingMode>(getStoredWhitespaceRenderingMode());
  const lastWhitespaceToggleRef = useRef(0);
  const indentManuallySetRef = useRef(false);
  const inlayHintsEnabledRef = useRef(inlayHintsEnabled);
  const lastInlayHintsToggleRef = useRef(0);
  const signatureHelpEnabledRef = useRef(signatureHelpEnabled);
  const lastSignatureHelpToggleRef = useRef(0);

  // ---------------------------------------------------------------------------
  // Ref sync effects
  // ---------------------------------------------------------------------------

  useEffect(() => {
    wordWrapRef.current = wordWrapEnabled;
  }, [wordWrapEnabled]);

  useEffect(() => {
    minimapEnabledRef.current = minimapEnabled;
  }, [minimapEnabled]);

  useEffect(() => {
    relativeLineNumbersEnabledRef.current = relativeLineNumbersEnabled;
  }, [relativeLineNumbersEnabled]);

  // Keep the ref in sync whenever the state value changes from context
  useEffect(() => {
    indentManuallySetRef.current = indentManuallySet;
  }, [indentManuallySet]);

  useEffect(() => {
    inlayHintsEnabledRef.current = inlayHintsEnabled;
  }, [inlayHintsEnabled]);

  useEffect(() => {
    signatureHelpEnabledRef.current = signatureHelpEnabled;
  }, [signatureHelpEnabled]);

  // ---------------------------------------------------------------------------
  // Orphaned localStorage cleanup
  // ---------------------------------------------------------------------------

  useEffect(() => {
    try {
      localStorage.removeItem('editor:indent-manual');
    } catch (_err) {
      /* ignore */
    }
  }, []);

  // ---------------------------------------------------------------------------
  // Reset indent when switching files
  // ---------------------------------------------------------------------------

  const prevBufferIdForIndentRef = useRef<string | null>(null);

  useEffect(() => {
    const currentBufferId = bufferId ?? null;
    if (currentBufferId !== prevBufferIdForIndentRef.current) {
      prevBufferIdForIndentRef.current = currentBufferId;
      indentManuallySetRef.current = false;
      setIndentManuallySet(false);
      setLineEnding('LF');
    }
  }, [bufferId]);

  // ---------------------------------------------------------------------------
  // Font size callbacks
  // ---------------------------------------------------------------------------

  const onZoomIn = useCallback(() => {
    setEditorFontSize((prev) => {
      const next = Math.min(prev + 1, FONT_SIZE_MAX);
      try {
        localStorage.setItem('editor:font-size', String(next));
      } catch (err) {
        debugLog('[onZoomIn] localStorage persist failed:', err);
      }
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
  }, []);

  // ---------------------------------------------------------------------------
  // Tab size callback
  // ---------------------------------------------------------------------------

  const onCycleTabSize = useCallback(() => {
    indentManuallySetRef.current = true;
    setIndentManuallySet(true);

    setEditorTabSize((prev) => {
      if (prev === TAB_SIZE_TABS_MODE) {
        setEditorUsesTabs(false);
        try {
          localStorage.setItem('editor:tab-size', '2');
        } catch (err) {
          debugLog('[onCycleTabSize] localStorage persist failed:', err);
        }
        return 2;
      }
      const currentIdx = TAB_SIZE_OPTIONS.indexOf(prev as (typeof TAB_SIZE_OPTIONS)[number]);
      if (currentIdx === TAB_SIZE_OPTIONS.length - 1) {
        setEditorUsesTabs(true);
        try {
          localStorage.setItem('editor:tab-size', '0');
        } catch (err) {
          debugLog('[onCycleTabSize] localStorage persist failed:', err);
        }
        return TAB_SIZE_TABS_MODE;
      }
      const nextIdx = (currentIdx + 1) % TAB_SIZE_OPTIONS.length;
      const next = TAB_SIZE_OPTIONS[nextIdx];
      setEditorUsesTabs(false);
      try {
        localStorage.setItem('editor:tab-size', String(next));
      } catch (err) {
        debugLog('[onCycleTabSize] localStorage persist failed:', err);
      }
      return next;
    });
  }, []);

  // ---------------------------------------------------------------------------
  // Toggle callbacks (these need the view ref, so they accept it as parameter)
  // ---------------------------------------------------------------------------

  const onToggleWordWrap = useCallback(() => {
    const now = Date.now();
    if (now - lastWrapToggleRef.current < 100) return;
    lastWrapToggleRef.current = now;
    const next = !wordWrapRef.current;
    wordWrapRef.current = next;
    setWordWrapEnabled(next);
    try {
      localStorage.setItem('editor:word-wrap-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleWordWrap] localStorage persist failed:', err);
    }
  }, []);

  const onToggleMinimap = useCallback(() => {
    const now = Date.now();
    if (now - lastMinimapToggleRef.current < 100) return;
    lastMinimapToggleRef.current = now;
    const next = !minimapEnabledRef.current;
    minimapEnabledRef.current = next;
    setMinimapEnabled(next);
    try {
      localStorage.setItem('editor:minimap-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleMinimap] localStorage persist failed:', err);
    }
  }, []);

  const onToggleRelativeLineNumbers = useCallback(() => {
    const now = Date.now();
    if (now - lastRelativeLineNumbersToggleRef.current < 100) return;
    lastRelativeLineNumbersToggleRef.current = now;
    const next = !relativeLineNumbersEnabledRef.current;
    relativeLineNumbersEnabledRef.current = next;
    setRelativeLineNumbersEnabled(next);
    try {
      localStorage.setItem('editor:relative-line-numbers-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleRelativeLineNumbers] localStorage persist failed:', err);
    }
  }, []);

  const onCycleWhitespaceRendering = useCallback((): WhitespaceRenderingMode => {
    const now = Date.now();
    if (now - lastWhitespaceToggleRef.current < 100) return whitespaceRenderingModeRef.current;
    lastWhitespaceToggleRef.current = now;
    const next: WhitespaceRenderingMode =
      whitespaceRenderingModeRef.current === 'none'
        ? 'boundary'
        : whitespaceRenderingModeRef.current === 'boundary'
          ? 'all'
          : 'none';
    whitespaceRenderingModeRef.current = next;
    try {
      localStorage.setItem('editor:whitespace-rendering-mode', next);
    } catch (err) {
      debugLog('[onCycleWhitespaceRendering] localStorage persist failed:', err);
    }
    return next;
  }, []);

  const onToggleInlayHints = useCallback(() => {
    const now = Date.now();
    if (now - lastInlayHintsToggleRef.current < 100) return;
    lastInlayHintsToggleRef.current = now;
    const next = !inlayHintsEnabledRef.current;
    inlayHintsEnabledRef.current = next;
    setInlayHintsEnabled(next);
    try {
      localStorage.setItem('editor:inlay-hints-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleInlayHints] localStorage persist failed:', err);
    }
  }, []);

  const onToggleSignatureHelp = useCallback(() => {
    const now = Date.now();
    if (now - lastSignatureHelpToggleRef.current < 100) return;
    lastSignatureHelpToggleRef.current = now;
    const next = !signatureHelpEnabledRef.current;
    signatureHelpEnabledRef.current = next;
    setSignatureHelpEnabled(next);
    try {
      localStorage.setItem('editor:signature-help-enabled', String(next));
    } catch (err) {
      debugLog('[onToggleSignatureHelp] localStorage persist failed:', err);
    }
  }, []);

  return {
    // State
    editorFontSize,
    editorTabSize,
    editorUsesTabs,
    wordWrapEnabled,
    relativeLineNumbersEnabled,
    minimapEnabled,
    whitespaceRenderingMode: whitespaceRenderingModeRef.current,
    indentManuallySet,
    lineEnding,
    inlayHintsEnabled,
    signatureHelpEnabled,

    // Ref mirrors
    wordWrapRef,
    minimapEnabledRef,
    relativeLineNumbersEnabledRef,
    whitespaceRenderingModeRef,
    indentManuallySetRef,
    inlayHintsEnabledRef,
    signatureHelpEnabledRef,

    // Setters
    setEditorTabSize,
    setEditorUsesTabs,
    setIndentManuallySet,
    setLineEnding,

    // Callbacks
    onZoomIn,
    onZoomOut,
    onResetZoom,
    onCycleTabSize,
    onToggleWordWrap,
    onToggleMinimap,
    onToggleRelativeLineNumbers,
    onCycleWhitespaceRendering,
    onToggleInlayHints,
    onToggleSignatureHelp,
  };
}
