/* MergeViewWrapper - diff viewer React component */
import { defaultKeymap, history, historyKeymap, undo, redo } from '@codemirror/commands';
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';
import {
  MergeView,
  goToNextChunk,
  goToPreviousChunk,
  acceptChunk,
  rejectChunk,
  unifiedMergeView,
} from '@codemirror/merge';
import { EditorState } from '@codemirror/state';
import type { Extension } from '@codemirror/state';
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import React, { useRef, useEffect, useCallback, useState } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import { getLanguageExtensions, detectLanguage } from '../extensions/languageRegistry';
import './MergeViewWrapper.css';

// Re-export utilities for consumers
export { goToNextChunk, goToPreviousChunk, acceptChunk, rejectChunk };

export interface MergeViewWrapperProps {
  /** Original content (left pane in side-by-side mode) */
  originalContent: string;
  /** Modified content (right pane in side-by-side mode) */
  modifiedContent: string;
  /** Display mode: 'side-by-side' (default) or 'unified' */
  mode?: 'side-by-side' | 'unified';
  /** File name for language detection */
  fileName?: string;
  /** Make editors read-only (default: true) */
  readOnly?: boolean;
  /** Highlight changes in diff */
  highlightChanges?: boolean;
  /** Show gutter markers for changed lines */
  gutter?: boolean;
  /** Collapse unchanged regions */
  collapseUnchanged?: { margin?: number; minSize?: number };
  /** Additional CSS class name */
  className?: string;
  /** Inline styles */
  style?: React.CSSProperties;
  /** Called when user accepts a chunk in unified mode */
  onAcceptChunk?: () => void;
  /** Called when user rejects a chunk in unified mode */
  onRejectChunk?: () => void;
  /** Show accept/reject controls in unified mode (default: true) */
  mergeControls?: boolean;
  /** Show Prev/Next navigation in side-by-side mode (default: true) */
  sideBySideNavigation?: boolean;
  /** Label for side A (original) - side-by-side mode */
  aLabel?: string;
  /** Label for side B (modified) - side-by-side mode */
  bLabel?: string;
  /** Called when the user saves (Cmd+S) in an editable side-by-side pane */
  onSave?: (content: string) => void;
}

/**
 * MergeViewWrapper - a shared React component that wraps @codemirror/merge
 * functionality. Supports both side-by-side and unified diff modes.
 *
 * In side-by-side mode, content changes update the editors in-place via
 * EditorView.dispatch() to avoid DOM teardown and scroll-position loss.
 *
 * The content sync effect tracks user edits (reverts) and does not overwrite
 * them when props re-render — it only syncs when the prop content actually
 * changes from the parent.
 */
export const MergeViewWrapper: React.FC<MergeViewWrapperProps> = ({
  originalContent,
  modifiedContent,
  mode = 'side-by-side',
  fileName,
  readOnly = true,
  highlightChanges = true,
  gutter = true,
  collapseUnchanged,
  className = '',
  style,
  onAcceptChunk,
  onRejectChunk,
  mergeControls = true,
  sideBySideNavigation = true,
  aLabel = 'Original',
  bLabel = 'Modified',
  onSave,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const mergeViewRef = useRef<MergeView | null>(null);
  const editorViewRef = useRef<EditorView | null>(null);
  // Track whether the side-by-side view has been created
  const sideBySideCreatedRef = useRef(false);
  // Track the last prop content we synced, so we don't overwrite user edits
  const lastSyncedContentRef = useRef<{ original: string; modified: string }>({ original: '', modified: '' });
  const [hunkInfo, setHunkInfo] = useState<{ current: number; total: number } | null>(null);

  const { theme } = useTheme();
  const isDark = theme === 'dark';

  // Build base extensions shared by both panes
  const buildBaseExtensions = useCallback(
    (editable?: boolean) => {
      const syntaxStyle = isDark ? oneDarkHighlightStyle : defaultHighlightStyle;
      const extensions: Extension[] = [lineNumbers(), keymap.of(defaultKeymap), syntaxHighlighting(syntaxStyle)];

      if (fileName) {
        const ext = fileName.split('.').pop();
        const languageId = detectLanguage(ext || '', fileName);
        const langExtensions = getLanguageExtensions(languageId);
        if (langExtensions.length > 0) {
          extensions.push(...langExtensions);
        }
      }

      if (readOnly && !editable) {
        extensions.push(EditorState.readOnly.of(true));
      }

      return extensions;
    },
    [fileName, isDark, readOnly],
  );

  // Build extensions for the editable pane B in side-by-side mode.
  // Includes history for Ctrl+Z / Ctrl+Shift+Z support, revert keybindings,
  // and Cmd+S / Ctrl+S save.
  const buildEditableExtensions = useCallback(() => {
    const extensions = buildBaseExtensions(true);
    // Add history so reverts can be undone/redone
    extensions.push(history());
    extensions.push(keymap.of(historyKeymap));
    // Add save keybinding
    if (onSave) {
      extensions.push(
        keymap.of([
          {
            key: 'Mod-s',
            run: (view) => {
              onSave(view.state.doc.toString());
              return true;
            },
          },
        ]),
      );
    }
    return extensions;
  }, [buildBaseExtensions, onSave]);

  // Build unified mode extensions (closure captures originalContent)
  const buildUnifiedExtensions = useCallback(() => {
    const extensions = buildBaseExtensions();

    extensions.push(
      unifiedMergeView({
        original: originalContent,
        highlightChanges,
        gutter,
        mergeControls,
        collapseUnchanged,
      }),
    );

    extensions.push(
      keymap.of([
        { key: 'Alt-ArrowUp', run: goToPreviousChunk, preventDefault: true },
        { key: 'Alt-ArrowDown', run: goToNextChunk, preventDefault: true },
      ]),
    );

    return extensions;
  }, [originalContent, highlightChanges, gutter, mergeControls, collapseUnchanged, buildBaseExtensions]);

  // Accept/reject handlers for unified mode
  const handleAccept = useCallback(() => {
    if (editorViewRef.current && acceptChunk(editorViewRef.current) && onAcceptChunk) {
      onAcceptChunk();
    }
  }, [onAcceptChunk]);

  const handleReject = useCallback(() => {
    if (editorViewRef.current && rejectChunk(editorViewRef.current) && onRejectChunk) {
      onRejectChunk();
    }
  }, [onRejectChunk]);

  const handlePrevChunk = useCallback(() => {
    if (editorViewRef.current) goToPreviousChunk(editorViewRef.current);
  }, []);

  const handleNextChunk = useCallback(() => {
    if (editorViewRef.current) goToNextChunk(editorViewRef.current);
  }, []);

  // ── Side-by-side hunk navigation helpers ──

  const updateSbsHunkInfo = useCallback(() => {
    if (!mergeViewRef.current || mode !== 'side-by-side') return;
    const chunks = mergeViewRef.current.chunks;
    if (!chunks.length) {
      setHunkInfo(null);
      return;
    }
    const view = mergeViewRef.current.b;
    const pos = view.state.selection.main.head;
    let current = 0;
    for (let i = 0; i < chunks.length; i++) {
      if (pos >= chunks[i].fromB) {
        current = i;
      } else {
        break;
      }
    }
    setHunkInfo({ current: current + 1, total: chunks.length });
  }, [mode]);

  const handleSbsPrevChunk = useCallback(() => {
    if (!mergeViewRef.current?.b) {
      console.warn('[MergeViewWrapper] handleSbsPrevChunk: MergeView or pane B not available');
      return;
    }
    goToPreviousChunk(mergeViewRef.current.b);
    setTimeout(updateSbsHunkInfo, 10);
  }, [updateSbsHunkInfo]);

  const handleSbsNextChunk = useCallback(() => {
    if (!mergeViewRef.current?.b) {
      console.warn('[MergeViewWrapper] handleSbsNextChunk: MergeView or pane B not available');
      return;
    }
    goToNextChunk(mergeViewRef.current.b);
    setTimeout(updateSbsHunkInfo, 10);
  }, [updateSbsHunkInfo]);

  const handleSbsUndo = useCallback(() => {
    if (mergeViewRef.current?.b) undo(mergeViewRef.current.b);
  }, []);

  const handleSbsRedo = useCallback(() => {
    if (mergeViewRef.current?.b) redo(mergeViewRef.current.b);
  }, []);

  const handleUnifiedUndo = useCallback(() => {
    if (editorViewRef.current) undo(editorViewRef.current);
  }, []);

  const handleUnifiedRedo = useCallback(() => {
    if (editorViewRef.current) redo(editorViewRef.current);
  }, []);

  // Reset hunk info when mode changes
  useEffect(() => {
    if (mode !== 'side-by-side') setHunkInfo(null);
  }, [mode]);

  // ── Side-by-side mode ──

  // Create the MergeView once when switching into side-by-side mode
  useEffect(() => {
    if (mode !== 'side-by-side' || !containerRef.current) return;
    // Clear container for fresh mount
    containerRef.current.replaceChildren();
    sideBySideCreatedRef.current = false;

    // Pane A: read-only (original document)
    const aExtensions = buildBaseExtensions(false);
    // Pane B: editable with history for revert support
    const bExtensions = buildEditableExtensions();

    // Add hunk navigation keymaps to both panes
    const aKeymaps = [
      ...aExtensions,
      keymap.of([
        { key: 'Alt-ArrowUp', run: goToPreviousChunk, preventDefault: true },
        { key: 'Alt-ArrowDown', run: goToNextChunk, preventDefault: true },
      ]),
    ];
    const bKeymaps = [
      ...bExtensions,
      keymap.of([
        { key: 'Alt-ArrowUp', run: goToPreviousChunk, preventDefault: true },
        { key: 'Alt-ArrowDown', run: goToNextChunk, preventDefault: true },
      ]),
    ];

    const mv = new MergeView({
      a: EditorState.create({ doc: originalContent, extensions: aKeymaps }),
      b: EditorState.create({ doc: modifiedContent, extensions: bKeymaps }),
      parent: containerRef.current,
      orientation: 'a-b',
      revertControls: 'a-to-b',
      highlightChanges,
      gutter,
      collapseUnchanged,
    });

    mergeViewRef.current = mv;
    sideBySideCreatedRef.current = true;
    lastSyncedContentRef.current = { original: originalContent, modified: modifiedContent };

    // Attach hunk tracking listeners to pane B after MergeView is created
    let cleanupListeners: (() => void) | undefined;
    if (sideBySideNavigation) {
      const view = mv.b;
      const immediateUpdate = () => updateSbsHunkInfo();
      immediateUpdate();

      const handleClick = () => setTimeout(immediateUpdate, 10);
      const handleKeyup = (e: KeyboardEvent) => {
        if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
          setTimeout(immediateUpdate, 10);
        }
      };

      view.dom.addEventListener('click', handleClick);
      view.dom.addEventListener('keyup', handleKeyup);
      cleanupListeners = () => {
        view.dom.removeEventListener('click', handleClick);
        view.dom.removeEventListener('keyup', handleKeyup);
      };
    }

    return () => {
      cleanupListeners?.();
      if (mergeViewRef.current) {
        mergeViewRef.current.destroy();
        mergeViewRef.current = null;
      }
      sideBySideCreatedRef.current = false;
    };
    // Only recreate when structural config changes; content updates handled separately
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    mode,
    buildBaseExtensions,
    buildEditableExtensions,
    highlightChanges,
    gutter,
    collapseUnchanged,
    sideBySideNavigation,
  ]);

  // Update side-by-side content in-place without tearing down the MergeView.
  // Uses a ref to track the last prop content that was synced, so user edits
  // (reverts) are not overwritten when the component re-renders with the same
  // prop values.
  useEffect(() => {
    if (mode !== 'side-by-side' || !mergeViewRef.current || !sideBySideCreatedRef.current) return;

    const mv = mergeViewRef.current;
    const a = mv.a;
    const b = mv.b;
    const synced = lastSyncedContentRef.current;

    // Only update if the prop content has actually changed from what we last synced
    const aNeedsUpdate = originalContent !== synced.original;
    const bNeedsUpdate = modifiedContent !== synced.modified;

    if (aNeedsUpdate || bNeedsUpdate) {
      if (aNeedsUpdate) {
        a.dispatch({ changes: { from: 0, to: a.state.doc.length, insert: originalContent } });
      }
      if (bNeedsUpdate) {
        b.dispatch({ changes: { from: 0, to: b.state.doc.length, insert: modifiedContent } });
      }
      lastSyncedContentRef.current = { original: originalContent, modified: modifiedContent };
      // Update hunk info after content change
      setTimeout(updateSbsHunkInfo, 50);
    }
  }, [mode, originalContent, modifiedContent, updateSbsHunkInfo]);

  // ── Unified mode ──

  // Create/recreate unified view (full recreation required since unifiedMergeView
  // is a StateField that captures originalContent at creation time)
  useEffect(() => {
    if (mode !== 'unified' || !containerRef.current) return;
    containerRef.current.replaceChildren();

    const extensions = buildUnifiedExtensions();
    // Add history for undo/redo support
    extensions.push(history());
    extensions.push(keymap.of(historyKeymap));
    // Add save keybinding
    if (onSave) {
      extensions.push(
        keymap.of([
          {
            key: 'Mod-s',
            run: (view) => {
              onSave(view.state.doc.toString());
              return true;
            },
          },
        ]),
      );
    }

    const state = EditorState.create({
      doc: modifiedContent,
      extensions,
    });

    const view = new EditorView({ state, parent: containerRef.current });
    editorViewRef.current = view;

    return () => {
      if (editorViewRef.current) {
        editorViewRef.current.destroy();
        editorViewRef.current = null;
      }
    };
  }, [mode, buildUnifiedExtensions, modifiedContent]);

  const wrapperClassName =
    `merge-view-wrapper ${mode === 'side-by-side' ? 'side-by-side' : 'unified'} ${className}`.trim();

  return (
    <div className={wrapperClassName} style={style}>
      {mode === 'side-by-side' && (
        <div className="merge-view-header">
          <span className="header-label header-labelOriginal">{aLabel}</span>
          <span className="header-label header-labelModified">{bLabel}</span>
        </div>
      )}
      <div className="merge-view-container" ref={containerRef} />
      {mode === 'side-by-side' && sideBySideNavigation && (
        <div className="merge-view-controls sbs-navigation">
          <span className="sbs-hunk-info">
            {hunkInfo ? `${hunkInfo.current}/${hunkInfo.total} changes` : 'No changes'}
          </span>
          <div className="sbs-nav-buttons">
            <button type="button" className="btn-prev" onClick={handleSbsPrevChunk} title="Previous Change (Alt+Up)">
              Prev
              <span className="shortcut-hint">Alt+Up</span>
            </button>
            <button type="button" className="btn-next" onClick={handleSbsNextChunk} title="Next Change (Alt+Down)">
              Next
              <span className="shortcut-hint">Alt+Down</span>
            </button>
            <span className="btn-separator">|</span>
            <button type="button" className="btn-undo" onClick={handleSbsUndo} title="Undo Revert (Ctrl+Z)">
              Undo
              <span className="shortcut-hint">Ctrl+Z</span>
            </button>
            <button type="button" className="btn-redo" onClick={handleSbsRedo} title="Redo Revert (Ctrl+Shift+Z)">
              Redo
              <span className="shortcut-hint">Ctrl+⇧+Z</span>
            </button>
          </div>
        </div>
      )}
      {mode === 'unified' && mergeControls && (
        <div className="merge-view-controls">
          <button type="button" className="btn-prev" onClick={handlePrevChunk} title="Previous Change (Alt+Up)">
            Prev
            <span className="shortcut-hint">Alt+Up</span>
          </button>
          <button type="button" className="btn-next" onClick={handleNextChunk} title="Next Change (Alt+Down)">
            Next
            <span className="shortcut-hint">Alt+Down</span>
          </button>
          <span className="btn-separator">|</span>
          <button type="button" className="btn-undo" onClick={handleUnifiedUndo} title="Undo (Ctrl+Z)">
            Undo
            <span className="shortcut-hint">Ctrl+Z</span>
          </button>
          <button type="button" className="btn-redo" onClick={handleUnifiedRedo} title="Redo (Ctrl+Shift+Z)">
            Redo
            <span className="shortcut-hint">Ctrl+⇧+Z</span>
          </button>
          <span className="btn-separator">|</span>
          <button type="button" className="btn-reject" onClick={handleReject} title="Reject Change">
            Reject
          </button>
          <button type="button" className="btn-accept" onClick={handleAccept} title="Accept Change">
            Accept
          </button>
        </div>
      )}
    </div>
  );
};

export default MergeViewWrapper;
