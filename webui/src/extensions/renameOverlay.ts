/**
 * Rename overlay extension for CodeMirror.
 *
 * Provides an inline rename UI triggered by F2 that shows an input field at
 * the cursor position, highlights all rename locations, and lets the user
 * type a new name. On Enter, applies the rename. On Escape, cancels.
 *
 * For languages with semantic rename support (TypeScript, JavaScript, Go),
 * uses the backend API. For other languages, falls back to simple text replace.
 */

import { StateEffect, StateField, type EditorState } from '@codemirror/state';
import { Decoration } from '@codemirror/view';
import type { EditorView, DecorationSet } from '@codemirror/view';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';
import { resolveLanguageId } from './languageRegistry';
import './renameOverlay.css';

/** Semantic language IDs that support rename. */
const RENAME_LANGUAGES = new Set(['typescript', 'typescript-jsx', 'javascript', 'javascript-jsx', 'go']);

// ---------------------------------------------------------------------------
// StateEffects for decoration management
// ---------------------------------------------------------------------------

/** Effect to set rename highlight locations. */
export const setRenameLocations = StateEffect.define<Array<{ from: number; to: number }>>();

/** Effect to clear rename highlights. */
export const clearRenameLocations = StateEffect.define<void>();

// ---------------------------------------------------------------------------
// StateField for rename highlight decorations
// ---------------------------------------------------------------------------

/**
 * StateField that manages the highlight decorations for rename locations.
 * Use this in your editor configuration to enable rename highlighting.
 *
 * @example
 * ```typescript
 * const editor = new EditorView({
 *   extensions: [
 *     renameHighlightField,
 *     // ... other extensions
 *   ],
 * });
 * ```
 */
export const renameHighlightField = StateField.define<DecorationSet>({
  create() {
    return Decoration.none;
  },
  update(decorations, tr) {
    // Check for effects in the transaction
    for (const effect of tr.effects) {
      if (effect.is(setRenameLocations)) {
        const locs = effect.value;
        if (locs.length === 0) {
          return Decoration.none;
        }
        const marks = locs.map((loc) => Decoration.mark({ class: 'cm-rename-highlight' }).range(loc.from, loc.to));
        decorations = Decoration.set(marks, true);
      }
      if (effect.is(clearRenameLocations)) {
        return Decoration.none;
      }
    }
    // Map decorations through document changes
    if (tr.docChanged) {
      decorations = decorations.map(tr.changes);
    }
    return decorations;
  },
});

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface RenameOptions {
  getFilePath: () => string | undefined;
  getContent: () => string;
  /** Called after rename is applied (or cancelled) */
  onDone?: () => void;
}

interface OverlayState {
  overlay: HTMLDivElement;
  input: HTMLInputElement;
  from: number;
  to: number;
  locations: Array<{ from: number; to: number }>;
  isFallback: boolean;
}

// ---------------------------------------------------------------------------
// Helper: find all occurrences of a word in document
// ---------------------------------------------------------------------------

function findAllOccurrences(doc: EditorState['doc'], word: string): Array<{ from: number; to: number }> {
  const locations: Array<{ from: number; to: number }> = [];
  const docText = doc.toString();

  // Simple word-boundary match using regex
  const regex = new RegExp(`\\b${escapeRegex(word)}\\b`, 'g');
  let match;
  while ((match = regex.exec(docText)) !== null) {
    locations.push({ from: match.index, to: match.index + match[0].length });
  }

  return locations;
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// ---------------------------------------------------------------------------
// Helper: auto-size input to content
// ---------------------------------------------------------------------------

function updateInputWidth(input: HTMLInputElement): void {
  const measure = document.createElement('span');
  measure.style.cssText = 'position:absolute;visibility:hidden;white-space:pre;';
  measure.textContent = input.value || ' ';
  document.body.appendChild(measure);
  input.style.width = `${measure.offsetWidth + 10}px`;
  document.body.removeChild(measure);
}

// ---------------------------------------------------------------------------
// Helper: create rename overlay DOM
// ---------------------------------------------------------------------------

function createOverlayElement(
  view: EditorView,
  from: number,
  to: number,
  currentWord: string,
  locationCount: number,
  isFallback: boolean,
): OverlayState {
  const coords = view.coordsAtPos(from);
  if (!coords) {
    throw new Error('Cannot get coordinates for cursor position');
  }

  const editorRect = view.dom.getBoundingClientRect();

  // Create overlay container
  const overlay = document.createElement('div');
  overlay.className = 'cm-rename-overlay';

  // Position absolute to editor
  const left = coords.left - editorRect.left;
  const top = coords.bottom - editorRect.top + 2;
  overlay.style.left = `${left}px`;
  overlay.style.top = `${top}px`;

  // Caption showing match count
  const caption = document.createElement('div');
  caption.className = 'cm-rename-caption';
  caption.textContent = isFallback
    ? `Replace ${locationCount} occurrence${locationCount !== 1 ? 's' : ''}`
    : `${locationCount} location${locationCount !== 1 ? 's' : ''}`;
  overlay.appendChild(caption);

  // Input field
  const input = document.createElement('input');
  input.type = 'text';
  input.value = currentWord;
  input.spellcheck = false;
  input.autocomplete = 'off';
  input.className = 'cm-rename-input';
  input.setAttribute('aria-label', 'Rename ' + currentWord);

  // Auto-size: set width based on content
  input.addEventListener('input', () => updateInputWidth(input));
  overlay.appendChild(input);

  // Append to editor DOM
  view.dom.appendChild(overlay);

  // Select all text in input
  input.select();

  // Focus input
  input.focus();

  return { overlay, input, from, to, locations: [], isFallback };
}

// ---------------------------------------------------------------------------
// Main trigger function
// ---------------------------------------------------------------------------

/**
 * Trigger the rename UI at the current cursor position.
 * - Gets the current word at cursor
 * - For semantic languages: calls the backend API for rename locations
 * - For other languages: falls back to simple find/replace
 */
export function triggerRename(view: EditorView, options: RenameOptions): void {
  const filePath = options.getFilePath();
  if (!filePath || filePath.startsWith('__workspace/')) {
    debugLog('[rename] No file path or workspace file, skipping rename');
    return;
  }

  // Get current word at cursor
  const sel = view.state.selection.main;
  const word = view.state.wordAt(sel.head);
  if (!word) {
    debugLog('[rename] No word at cursor');
    return;
  }
  const currentWord = view.state.sliceDoc(word.from, word.to);

  if (!currentWord) {
    debugLog('[rename] No word at cursor');
    return;
  }

  // Resolve language
  const ext = filePath.split('.').pop() || '';
  const name = filePath.split('/').pop() || '';
  const { languageId } = resolveLanguageId(undefined, ext.replace(/^\./, ''), name);

  // Check if semantic rename is supported
  if (languageId && RENAME_LANGUAGES.has(languageId)) {
    triggerSemanticRename(view, options, filePath, languageId, word.from, word.to, currentWord);
  } else {
    // Fallback: simple word replace
    triggerFallbackRename(view, options, word.from, word.to, currentWord);
  }
}

/**
 * Trigger semantic rename using the backend API.
 */
function triggerSemanticRename(
  view: EditorView,
  options: RenameOptions,
  filePath: string,
  languageId: string,
  from: number,
  to: number,
  currentWord: string,
): void {
  const api = ApiService.getInstance();
  const line = view.state.doc.lineAt(from);
  const lineNum = line.number;
  const col = from - line.from + 1; // 1-based column

  // Show loading state
  const loadingOverlay = createLoadingOverlay(view, from, currentWord);

  api
    .getSemanticRename(filePath, options.getContent(), languageId, lineNum, col)
    .then((result) => {
      // Remove loading overlay
      loadingOverlay.remove();

      if (result.error || !result.rename?.locations?.length) {
        debugLog('[rename] No rename locations found, falling back');
        triggerFallbackRename(view, options, from, to, currentWord);
        return;
      }

      // Filter locations to current file (or locations without filePath)
      const locations = result.rename.locations
        .filter((l) => !l.filePath || l.filePath === filePath)
        .map((l) => ({ from: l.from, to: l.to }));

      if (locations.length === 0) {
        debugLog('[rename] No locations in current file');
        triggerFallbackRename(view, options, from, to, currentWord);
        return;
      }

      // Dispatch highlight decorations
      view.dispatch({ effects: setRenameLocations.of(locations) });

      // Show rename overlay
      const overlayState = createOverlayElement(view, from, to, currentWord, locations.length, false);
      overlayState.locations = locations;

      // Wire up events
      wireRenameInput(view, overlayState, options);
    })
    .catch((err) => {
      debugLog('[rename] Semantic rename failed:', err);
      loadingOverlay.remove();
      triggerFallbackRename(view, options, from, to, currentWord);
    });
}

/**
 * Trigger fallback rename (simple text replace of word at cursor).
 */
function triggerFallbackRename(
  view: EditorView,
  options: RenameOptions,
  from: number,
  to: number,
  currentWord: string,
): void {
  const locations = findAllOccurrences(view.state.doc, currentWord);

  if (locations.length === 0) {
    debugLog('[rename] No occurrences found');
    options.onDone?.();
    return;
  }

  // Show rename overlay
  const overlayState = createOverlayElement(view, from, to, currentWord, locations.length, true);
  overlayState.locations = locations;

  // Wire up events
  wireRenameInput(view, overlayState, options);
}

/**
 * Wire up the input events for the rename overlay.
 */
function wireRenameInput(view: EditorView, overlay: OverlayState, options: RenameOptions): void {
  const { input, from, locations } = overlay;
  const currentWord = view.state.sliceDoc(from, from + (overlay.to - overlay.from));

  // Guard: check if the editor view is still alive before dispatching.
  // Prevents errors if the editor is destroyed while the rename overlay is
  // active (e.g. React unmounts the component during a blur setTimeout).
  const isViewAlive = (): boolean => {
    return view.dom != null && view.dom.isConnected;
  };

  // Handle Enter: apply rename
  const handleEnter = () => {
    const newName = input.value.trim();
    if (!newName || newName === currentWord) {
      // No change, just cancel
      cleanup();
      options.onDone?.();
      return;
    }

    // Apply all replacements in ONE atomic transaction
    const changes = locations.map((loc) => {
      return { from: loc.from, to: loc.to, insert: newName };
    });

    if (isViewAlive()) {
      view.dispatch({
        changes: changes.sort((a, b) => b.from - a.from), // Apply from end to start
        effects: clearRenameLocations.of(undefined),
      });
    }

    // Remove overlay
    cleanup();
    options.onDone?.();
  };

  // Handle Escape: cancel
  const handleEscape = () => {
    if (isViewAlive()) {
      view.dispatch({ effects: clearRenameLocations.of(undefined) });
    }
    cleanup();
    options.onDone?.();
  };

  // Handle keydown events
  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleEnter();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      handleEscape();
    }
  };

  // Handle blur: cancel if focus lost
  const handleBlur = () => {
    // Small delay to allow Enter key to process first
    setTimeout(() => {
      if (document.activeElement !== input && !cleanedUp) {
        handleEscape();
      }
    }, 50);
  };

  input.addEventListener('keydown', handleKeyDown);
  input.addEventListener('blur', handleBlur);

  let cleanedUp = false;
  function cleanup() {
    if (cleanedUp) return;
    cleanedUp = true;
    input.removeEventListener('keydown', handleKeyDown);
    input.removeEventListener('blur', handleBlur);
    if (overlay.overlay.parentNode) {
      overlay.overlay.parentNode.removeChild(overlay.overlay);
    }
  }
}

// ---------------------------------------------------------------------------
// Helper: create loading overlay
// ---------------------------------------------------------------------------

function createLoadingOverlay(view: EditorView, pos: number, word: string): HTMLDivElement {
  const coords = view.coordsAtPos(pos);
  if (!coords) {
    const div = document.createElement('div');
    div.textContent = 'Loading...';
    view.dom.appendChild(div);
    return div;
  }

  const editorRect = view.dom.getBoundingClientRect();
  const overlay = document.createElement('div');
  overlay.className = 'cm-rename-overlay';
  overlay.style.left = `${coords.left - editorRect.left}px`;
  overlay.style.top = `${coords.bottom - editorRect.top + 2}px`;
  overlay.textContent = 'Loading...';
  overlay.style.padding = '4px 8px';

  view.dom.appendChild(overlay);
  return overlay;
}
