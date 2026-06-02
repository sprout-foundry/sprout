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
 * Internal: persisted boolean setting with debounced toggle. Each editor
 * setting (word wrap, minimap, relative line numbers, inlay hints, signature
 * help) used to inline ~25 lines of state + ref + sync effect + toggle +
 * localStorage write. Five identical blocks collapsed to one.
 *
 * The ref mirror is necessary because the toggle is bound to global event
 * listeners (`useEditorEvents`) whose callbacks must read the *current*
 * value, not the value captured when the effect first registered.
 */
function useBooleanSetting(
  storageKey: string,
  defaultValue: boolean,
): {
  value: boolean;
  ref: React.MutableRefObject<boolean>;
  toggle: () => void;
  set: (v: boolean) => void;
} {
  const [value, setValue] = useState<boolean>(() => {
    try {
      const stored = localStorage.getItem(storageKey);
      return stored !== null ? stored === 'true' : defaultValue;
    } catch (err) {
      debugLog(`Failed to read ${storageKey} from localStorage:`, err);
      return defaultValue;
    }
  });
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  }, [value]);
  const lastToggleRef = useRef(0);
  const toggle = useCallback(() => {
    const now = Date.now();
    // Coalesce double-fires within 100ms. Some hotkey schemes (omnibox →
    // event → keybinding) can trigger the same toggle twice within a tick.
    if (now - lastToggleRef.current < 100) return;
    lastToggleRef.current = now;
    const next = !ref.current;
    ref.current = next;
    setValue(next);
    try {
      localStorage.setItem(storageKey, String(next));
    } catch (err) {
      debugLog(`[toggle ${storageKey}] localStorage persist failed:`, err);
    }
  }, [storageKey]);
  const set = useCallback(
    (v: boolean) => {
      ref.current = v;
      setValue(v);
      try {
        localStorage.setItem(storageKey, String(v));
      } catch (err) {
        debugLog(`[set ${storageKey}] localStorage persist failed:`, err);
      }
    },
    [storageKey],
  );
  return { value, ref, toggle, set };
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

  // ---------------------------------------------------------------------------
  // Settings state — boolean toggles delegated to useBooleanSetting (one
  // helper instead of 5 identical state + ref + sync effect + toggle blocks).
  // ---------------------------------------------------------------------------

  const wordWrap = useBooleanSetting('editor:word-wrap-enabled', true);
  const minimap = useBooleanSetting('editor:minimap-enabled', true);
  const relativeLineNumbers = useBooleanSetting('editor:relative-line-numbers-enabled', false);
  const inlayHints = useBooleanSetting('editor:inlay-hints-enabled', true);
  const signatureHelp = useBooleanSetting('editor:signature-help-enabled', true);

  // Re-export under the historical names so callers don't need to change.
  const wordWrapEnabled = wordWrap.value;
  const wordWrapRef = wordWrap.ref;
  const minimapEnabled = minimap.value;
  const minimapEnabledRef = minimap.ref;
  const relativeLineNumbersEnabled = relativeLineNumbers.value;
  const relativeLineNumbersEnabledRef = relativeLineNumbers.ref;
  const inlayHintsEnabled = inlayHints.value;
  const inlayHintsEnabledRef = inlayHints.ref;
  const signatureHelpEnabled = signatureHelp.value;
  const signatureHelpEnabledRef = signatureHelp.ref;

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

  // Whitespace rendering is tri-state (none/boundary/all), so it can't use
  // useBooleanSetting. The pattern is otherwise identical.
  const whitespaceRenderingModeRef = useRef<WhitespaceRenderingMode>(getStoredWhitespaceRenderingMode());
  const lastWhitespaceToggleRef = useRef(0);
  const indentManuallySetRef = useRef(false);

  useEffect(() => {
    indentManuallySetRef.current = indentManuallySet;
  }, [indentManuallySet]);

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
  // Toggle callbacks — boolean toggles delegate to useBooleanSetting; only
  // the tri-state whitespace cycle needs custom logic.
  // ---------------------------------------------------------------------------

  const onToggleWordWrap = wordWrap.toggle;
  const onToggleMinimap = minimap.toggle;
  const onToggleRelativeLineNumbers = relativeLineNumbers.toggle;

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

  const onToggleInlayHints = inlayHints.toggle;
  const onToggleSignatureHelp = signatureHelp.toggle;

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
