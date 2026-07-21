/**
 * useEditorFileIO — encapsulates file load/save, external change detection,
 * and conflict resolution for the editor.
 *
 * This hook extracts all file I/O concerns from EditorPane into a single
 * cohesive unit:
 *
 * - **loadFile**: Reads a file from disk (or handles in-memory workspace
 *   buffers), updates the CodeMirror EditorView, restores cursor/scroll
 *   positions, fetches git diff, auto-detects indentation and line endings,
 *   and triggers diagnostic fetching.
 * - **handleSave**: Saves the current buffer to disk, dispatches editor-save
 *   cooldown events, re-fetches git diff and diagnostics.
 * - **External change listener**: Listens for `file_externally_modified` DOM
 *   events and shows a conflict dialog when the buffer has unsaved changes.
 * - **Auto-reload sync listener**: Listens for `file:auto-reloaded` DOM events
 *   (dispatched by `useAutoReloadCleanBuffers` for clean buffers) and syncs
 *   the CodeMirror view.
 * - **Buffer load effect**: React effect that loads file content when a buffer
 *   is assigned to the pane, with deduplication guards.
 *
 * Target: ~650 lines (SP-010 Phase 1).
 */

import { history } from '@codemirror/commands';
import { indentUnit } from '@codemirror/language';
import { EditorState, Transaction } from '@codemirror/state';
import type { Compartment } from '@codemirror/state';
import { useEffect, useRef, useCallback } from 'react';
import { showFileChangeDialog } from '../components/FileChangeDialog';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { updateDiffGutter, clearDiffGutter } from '../extensions/diffGutter';
import { detectIndentation } from '../extensions/indentDetect';
import { detectLineEnding, type LineEnding } from '../extensions/lineEndingDetect';
import { clearDiagnostics } from '../extensions/lintDiagnostics';
import { setOriginalContent } from '../extensions/unsavedLineHighlight';
import { ApiService } from '../services/api';
import { readFileWithConsent } from '../services/fileAccess';
import { notificationBus } from '../services/notificationBus';
import type { EditorBuffer } from '../types/editor';
import { useLog, debugLog, warn } from '../utils/log';
import { isImageFile, isAudioFile, isVideoFile, isBinaryFile } from '../utils/mediaPatterns';
import { generateUnifiedDiff } from '../utils/simpleDiff';
import { JUST_SAVED_THRESHOLD_MS, justSavedRef } from './useAutoReloadCleanBuffers';
import { TAB_SIZE_TABS_MODE, TAB_SIZE_DEFAULT } from './useEditorExtensions';
import type { CMViewAPI } from './useCMView';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Minimum number of indented lines required for auto-detection to be confident */
const MIN_INDENTED_LINES_FOR_DETECTION = 3;

// Transaction annotations for external content replacements (file reloads,
// initial loads, buffer switches). Prevents CodeMirror from recording
// these in the undo/redo stack.
const suppressHistoryAnnotations = [Transaction.addToHistory.of(false)];

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** State setters that the hook drives — passed from the host component. */
export interface FileIOStateSetters {
  setLoading: (v: boolean) => void;
  setSaving: (v: boolean) => void;
  setError: (v: string | null) => void;
  setLocalContent: (v: string) => void;
  setSelectionInfo: (v: { charCount: number; selectionCount: number } | null) => void;
  setEditorTabSize: (v: number) => void;
  setEditorUsesTabs: (v: boolean) => void;
  setLineEnding: (v: LineEnding) => void;
}

/** Compartment reconfiguration helpers — passed from useEditorExtensions. */
export interface FileIOCompartments {
  tabSize: Compartment;
  history: Compartment;
}

/** Return type of the hook. */
export interface UseEditorFileIOReturn {
  /** Load a file from disk (or handle in-memory workspace buffer). */
  loadFile: (filePath: string) => Promise<void>;
  /** Ref mirror for loadFile (avoids stale closures). */
  loadFileRef: React.MutableRefObject<((filePath: string) => Promise<void>) | null>;
  /** Save the current buffer to disk. */
  handleSave: () => Promise<void>;
  /** Ref mirror for handleSave. */
  saveRef: React.MutableRefObject<() => Promise<void>>;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Hook that manages all file I/O for an editor pane.
 *
 * @param cmViewApiRef - Ref to the CodeMirror view API. Populated by EditorPane
 *                       after `useCMView` returns. The hook reads
 *                       `cmViewApiRef.current?.view` and `?.withExternalUpdate`
 *                       at call time — safe to call before the API is mounted
 *                       (operations are no-ops when the view is null).
 * @param buffer      - Current buffer (may be undefined/null for empty panes)
 * @param bufferRef   - Ref mirror of buffer (avoids stale closures)
 * @param compartments - Compartment handles from useEditorExtensions
 * @param indentManuallySetRef - Ref mirror of indent-manual flag
 * @param fetchDiagnosticsRef  - Ref to the diagnostics fetcher from useEditorDiagnostics
 * @param paneId      - The pane identifier
 * @param setters     - State setters from the host component
 */
export function useEditorFileIO(
  cmViewApiRef: React.MutableRefObject<CMViewAPI | null>,
  buffer: EditorBuffer | null | undefined,
  bufferRef: React.MutableRefObject<EditorBuffer | null | undefined>,
  compartments: FileIOCompartments,
  indentManuallySetRef: React.MutableRefObject<boolean>,
  fetchDiagnosticsRef: React.MutableRefObject<(filePath: string, content: string, trigger?: 'edit' | 'save') => void>,
  paneId: string,
  setters: FileIOStateSetters,
): UseEditorFileIOReturn {
  const {
    setLoading,
    setSaving,
    setError,
    setLocalContent,
    setSelectionInfo,
    setEditorTabSize,
    setEditorUsesTabs,
    setLineEnding,
  } = setters;

  const log = useLog();

  // Editor manager context — stable callbacks
  const {
    updateBufferContent,
    saveBuffer,
    setBufferOriginalContent,
    setBufferExternallyModified,
    clearBufferExternallyModified,
    openWorkspaceBuffer,
  } = useEditorManager();

  // API service singleton
  const apiService = useRef(ApiService.getInstance()).current;

  // Tracks whether a non-user (external) content replacement is in flight
  // (ref is passed in from EditorPane and shared with useEditorCursor)

  // Ref mirror for loadFile — avoids stale closure issues
  const loadFileRef = useRef<((filePath: string) => Promise<void>) | null>(null);

  // Tracks save operations in-flight for identity-based race prevention
  const saveInFlightRef = useRef<Set<string>>(new Set());

  // Ref mirror for handleSave
  const saveRef = useRef<() => Promise<void>>(async () => {});

  // Deduplication refs for the buffer-load effect
  const lastLoadedRef = useRef<{ bufferId: string; filePath: string } | null>(null);
  const currentBufferIdRef = useRef<string | null>(null);

  // Load sequence counter — bumps on each loadFile invocation so that
  // stale in-flight loads from rapid file-switching are discarded.
  const loadSeqRef = useRef(0);

  // ── Indentation detection helper ───────────────────────────────
  // Shared between loadFile and the auto-reload handler to avoid
  // duplicating ~35 lines of indent-detection + compartment dispatch.

  const applyIndentDetection = useCallback(
    (content: string) => {
      if (indentManuallySetRef.current) return;
      const detected = detectIndentation(content);
      if (detected.indentedLineCount >= MIN_INDENTED_LINES_FOR_DETECTION) {
        const detectedSize = detected.useTabs ? TAB_SIZE_DEFAULT : detected.indentWidth;
        setEditorTabSize(detected.useTabs ? TAB_SIZE_TABS_MODE : detectedSize);
        setEditorUsesTabs(detected.useTabs);
        if (cmViewApiRef.current?.view) {
          cmViewApiRef.current?.withExternalUpdate(() => {
            cmViewApiRef.current?.dispatch({
              effects: compartments.tabSize.reconfigure([
                EditorState.tabSize.of(detectedSize),
                indentUnit.of(detected.useTabs ? '\t' : ' '.repeat(detectedSize)),
              ]),
            });
          });
        }
      } else {
        setEditorUsesTabs(false);
        setEditorTabSize(TAB_SIZE_DEFAULT);
        if (cmViewApiRef.current?.view) {
          cmViewApiRef.current?.withExternalUpdate(() => {
            cmViewApiRef.current?.dispatch({
              effects: compartments.tabSize.reconfigure([
                EditorState.tabSize.of(TAB_SIZE_DEFAULT),
                indentUnit.of(' '.repeat(TAB_SIZE_DEFAULT)),
              ]),
            });
          });
        }
      }
    },
    // All values are accessed via refs or are stable setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  // ── Load file ────────────────────────────────────────────────────

  const loadFile = useCallback(
    async (filePath: string) => {
      // Bump sequence counter to cancel any in-flight loads.
      const seq = ++loadSeqRef.current;

      setError(null);

      try {
        // Virtual workspace buffers have no on-disk file — handle in-memory only.
        if (filePath.startsWith('__workspace/')) {
          const currentBuffer = bufferRef.current;
          const content = currentBuffer?.content || '';
          setLocalContent(content);
          setSelectionInfo(null);
          setError(null);
          if (currentBuffer) {
            updateBufferContent(currentBuffer.id, content);
            if (currentBuffer.originalContent === undefined) {
              setBufferOriginalContent(currentBuffer.id, content);
            }
          }
          if (cmViewApiRef.current?.view) {
            cmViewApiRef.current?.withExternalUpdate(() => {
              cmViewApiRef.current?.dispatch({
                changes: { from: 0, to: cmViewApiRef.current?.view?.state.doc.length ?? 0, insert: content },
                annotations: suppressHistoryAnnotations,
                effects: setOriginalContent.of(content),
              });
            });
            clearDiffGutter(cmViewApiRef.current?.view);
            clearDiagnostics(cmViewApiRef.current?.view);
          }
          return;
        }

        setLoading(true);
        const response = await readFileWithConsent(filePath);
        // Discard if a newer load has been initiated while we awaited.
        if (loadSeqRef.current !== seq) return;
        if (!response.ok) {
          throw new Error(`Failed to load file: ${response.statusText}`);
        }

        const content = await response.text();
        // Discard if a newer load has been initiated while we awaited.
        if (loadSeqRef.current !== seq) return;

        setLocalContent(content);
        setSelectionInfo(null);

        // Sync buffer context
        const buf = bufferRef.current ?? buffer;
        if (buf) {
          updateBufferContent(buf.id, content);
          setBufferOriginalContent(buf.id, content);
        }

        // Update editor view
        if (cmViewApiRef.current?.view) {
          cmViewApiRef.current?.withExternalUpdate(() => {
            cmViewApiRef.current?.dispatch({
              changes: { from: 0, to: cmViewApiRef.current?.view?.state.doc.length ?? 0, insert: content },
              annotations: suppressHistoryAnnotations,
              effects: setOriginalContent.of(content),
            });
          });
        }

        // Restore cursor position from buffer state (layout persistence).
        if (buf && cmViewApiRef.current?.view && (buf.cursorPosition.line > 0 || buf.cursorPosition.column > 0)) {
          const { line, column } = buf.cursorPosition;
          const doc = cmViewApiRef.current?.view.state.doc;
          if (doc.lines > 0) {
            const targetLine = Math.max(1, Math.min(line, doc.lines));
            const lineInfo = doc.line(targetLine);
            const pos = lineInfo.from + Math.max(0, Math.min(column, lineInfo.length));
            cmViewApiRef.current?.withExternalUpdate(() => {
              cmViewApiRef.current?.dispatch({
                selection: { anchor: pos },
                annotations: suppressHistoryAnnotations,
              });
            });
          }
        }

        // Restore scroll position from buffer state.
        if (buf && cmViewApiRef.current?.view && (buf.scrollPosition.top > 0 || buf.scrollPosition.left > 0)) {
          const { top, left } = buf.scrollPosition;
          const viewAtLoadTime = cmViewApiRef.current.view;
          // Use setTimeout with 0 to ensure this runs after the current render cycle
          // and after CodeMirror has finished layout
          setTimeout(() => {
            if (viewAtLoadTime && viewAtLoadTime.scrollDOM) {
              viewAtLoadTime.scrollDOM.scrollTop = top;
              viewAtLoadTime.scrollDOM.scrollLeft = left;
            }
          }, 0);
        }

        // Fetch git diff after loading file
        if (filePath && cmViewApiRef.current?.view) {
          try {
            const diffResponse = await apiService.getGitDiff(filePath);
            // Discard if a newer load has been initiated while we awaited.
            if (loadSeqRef.current !== seq) return;
            if (diffResponse.diff && diffResponse.diff.trim()) {
              updateDiffGutter(cmViewApiRef.current?.view, diffResponse.diff);
            } else {
              clearDiffGutter(cmViewApiRef.current?.view);
            }
          } catch (err) {
            debugLog('[useEditorFileIO] Failed to fetch git diff:', err);
            notificationBus.notify('warning', 'Git Diff', 'Failed to fetch git diff');
            if (cmViewApiRef.current?.view) clearDiffGutter(cmViewApiRef.current?.view);
          }
        }

        // Auto-detect indentation
        applyIndentDetection(content);

        // Detect line ending style
        const lineEndingResult = detectLineEnding(content);
        setLineEnding(lineEndingResult.lineEnding);

        // Fetch diagnostics for the loaded file
        if (cmViewApiRef.current?.view) {
          fetchDiagnosticsRef.current(filePath, content);
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Unknown error';
        log.error(`[useEditorFileIO loadFile] Error: ${errorMessage}`, { title: 'File Load Error' });
        setError(errorMessage);
      } finally {
        // Only clear loading state if this is still the active load.
        if (loadSeqRef.current === seq) {
          setLoading(false);
        }
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [updateBufferContent, setBufferOriginalContent, applyIndentDetection],
  );

  // Keep ref in sync
  loadFileRef.current = loadFile;

  // ── Save buffer ──────────────────────────────────────────────────

  const handleSave = useCallback(
    async () => {
      const buf = bufferRef.current;
      if (!buf || !cmViewApiRef.current?.view) return;

      // Only save real file buffers with on-disk paths.
      if (buf.kind !== 'file' || buf.file.path.startsWith('__workspace/')) return;

      setSaving(true);
      setError(null);

      // Track this save as in-flight to suppress redundant external change events
      saveInFlightRef.current.add(buf.file.path);

      // Notify the external file watcher and auto-reload cooldown *before*
      // the HTTP roundtrip. The server-side fsnotify fires as soon as it
      // writes the file, and the WebSocket "file_content_changed" event can
      // reach the browser *before* the HTTP save response.
      document.dispatchEvent(
        new CustomEvent('file:editor-saved', {
          detail: { path: buf.file.path, mtime: Math.floor(Date.now() / 1000) },
        }),
      );

      try {
        const saveResult = await saveBuffer(buf.id);
        const serverMtime = saveResult && typeof saveResult.mod_time === 'number' ? saveResult.mod_time : null;

        // If format-on-save was applied, update the CodeMirror view with the formatted content.
        // Guard against overwriting user edits made while the save was in flight.
        if (saveResult?.formattedContent && cmViewApiRef.current?.view) {
          const formattedContent = saveResult.formattedContent;
          const docNow = cmViewApiRef.current?.view.state.doc.toString();
          // Only apply formatted content if the editor still matches what we saved
          if (docNow === buf.content) {
            cmViewApiRef.current?.withExternalUpdate(() => {
              cmViewApiRef.current?.dispatch({
                changes: {
                  from: 0,
                  to: cmViewApiRef.current?.view?.state.doc.length ?? 0,
                  insert: formattedContent,
                },
                annotations: suppressHistoryAnnotations,
                effects: setOriginalContent.of(formattedContent),
              });
              setLocalContent(formattedContent);
              updateBufferContent(buf.id, formattedContent);
            });
          }
        }

        // Note: originalContent is updated by saveBuffer in EditorManagerContext
        // (no need to call setBufferOriginalContent here).

        // Re-dispatch with the authoritative server mtime
        document.dispatchEvent(
          new CustomEvent('file:editor-saved', {
            detail: {
              path: buf.file.path,
              mtime: serverMtime ?? Math.floor(Date.now() / 1000),
            },
          }),
        );

        // Re-run diagnostics on save (e.g., go vet save-only checks)
        if (buf.file.path && cmViewApiRef.current?.view) {
          await fetchDiagnosticsRef.current(buf.file.path, cmViewApiRef.current?.view.state.doc.toString(), 'save');
        }

        // Re-fetch diff after save
        if (buf.file.path && cmViewApiRef.current?.view) {
          try {
            const diffResponse = await apiService.getGitDiff(buf.file.path);
            if (diffResponse.diff && diffResponse.diff.trim()) {
              updateDiffGutter(cmViewApiRef.current?.view, diffResponse.diff);
            } else {
              clearDiffGutter(cmViewApiRef.current?.view);
            }
          } catch (err) {
            debugLog('[useEditorFileIO] Failed to re-fetch git diff after save:', err);
            notificationBus.notify('warning', 'Git Diff', 'Failed to re-fetch git diff after save');
          }
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Failed to save file';
        setError(errorMessage);
        log.error(`Save error: ${errorMessage}`, { title: 'Save Error' });
      } finally {
        saveInFlightRef.current.delete(buf.file.path);
        setSaving(false);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    // Safe: the only intentional dep is `saveBuffer` (stable context callback).
    // All other non-stable values are accessed via refs (`bufferRef`,
    // `saveInFlightRef`, `fetchDiagnosticsRef`, `apiService`) or through
    // the stable `cmViewApi`, while React state setters are stable by contract.
    [saveBuffer],
  );

  // Keep saveRef in sync
  saveRef.current = handleSave;

  // ── Buffer load effect ──────────────────────────────────────────
  // Loads file content when a buffer is assigned to this pane.

  useEffect(() => {
    if (!buffer || !buffer.file || buffer.file.isDir) {
      setLocalContent('');
      if (cmViewApiRef.current?.view) {
        cmViewApiRef.current?.dispatch({
          changes: { from: 0, to: cmViewApiRef.current?.view.state.doc.length, insert: '' },
          annotations: suppressHistoryAnnotations,
          effects: setOriginalContent.of(''),
        });
      }
      setSelectionInfo(null);
      setError(null);
      lastLoadedRef.current = null;
      currentBufferIdRef.current = null;
      if (cmViewApiRef.current?.view) {
        clearDiffGutter(cmViewApiRef.current?.view);
        clearDiagnostics(cmViewApiRef.current?.view);
      }
      return;
    }

    // ── Skip if same buffer already loaded ───────────────────────────
    // This guard applies to ALL buffer types (file, chat, diff, review,
    // workspace). Without it, non-file and workspace buffers would have
    // their editor content replaced on every keystroke because
    // updateBufferContent creates a new buffer object each time.
    if (currentBufferIdRef.current === buffer.id) {
      return;
    }

    // Set current buffer tracking BEFORE any early returns below
    currentBufferIdRef.current = buffer.id;

    // Reset CodeMirror's undo history when switching buffers.  Without this
    // the history field is shared across files: pressing Cmd-Z after a
    // buffer switch would revert the *previous* file's edits into the new
    // buffer's view.  Reconfiguring the history compartment installs a
    // fresh history() extension with an empty stack.
    if (cmViewApiRef.current?.view) {
      cmViewApiRef.current?.dispatch({
        effects: compartments.history.reconfigure(history()),
      });
    }

    // Skip if same buffer and same file already loaded (covers switching
    // away and back to the same buffer).
    if (
      lastLoadedRef.current &&
      lastLoadedRef.current.bufferId === buffer.id &&
      lastLoadedRef.current.filePath === buffer.file.path
    ) {
      return;
    }

    lastLoadedRef.current = { bufferId: buffer.id, filePath: buffer.file.path };

    if (buffer.kind !== 'file') {
      const nextContent = buffer.content || '';
      setLocalContent(nextContent);
      setSelectionInfo(null);
      setError(null);
      if (cmViewApiRef.current?.view) {
        cmViewApiRef.current?.dispatch({
          changes: { from: 0, to: cmViewApiRef.current?.view.state.doc.length, insert: nextContent },
          annotations: suppressHistoryAnnotations,
          effects: setOriginalContent.of(nextContent),
        });
        clearDiffGutter(cmViewApiRef.current?.view);
        clearDiagnostics(cmViewApiRef.current?.view);
      }
      return;
    }

    // Skip loading virtual workspace buffers — they have no on-disk file.
    if (buffer.file.path.startsWith('__workspace/')) {
      const nextContent = buffer.content || '';
      setLocalContent(nextContent);
      setSelectionInfo(null);
      setError(null);
      if (cmViewApiRef.current?.view) {
        cmViewApiRef.current?.dispatch({
          changes: { from: 0, to: cmViewApiRef.current?.view.state.doc.length, insert: nextContent },
          annotations: suppressHistoryAnnotations,
          effects: setOriginalContent.of(nextContent),
        });
        clearDiffGutter(cmViewApiRef.current?.view);
        clearDiagnostics(cmViewApiRef.current?.view);
      }
      return;
    }

    // Skip loading content for binary/media buffers — they are rendered by
    // dedicated viewers that fetch the file themselves as blobs.
    const fileExt = buffer.file.ext?.toLowerCase();
    if (fileExt && (isImageFile(fileExt) || isAudioFile(fileExt) || isVideoFile(fileExt) || isBinaryFile(fileExt))) {
      return;
    }

    if (loadFileRef.current) {
      loadFileRef.current(buffer.file.path);
    }
  }, [buffer?.id, buffer?.file?.path, buffer?.kind]); // eslint-disable-line react-hooks/exhaustive-deps
  // paneId is stable per pane instance; the guards above handle edge cases

  // ── External file change listener ──────────────────────────────
  // Shows conflict dialog when the buffer has unsaved changes and the
  // file is modified externally. Clean (unmodified) buffers are
  // auto-reloaded by useAutoReloadCleanBuffers.

  useEffect(() => {
    if (!buffer || buffer.kind !== 'file' || buffer.file.path.startsWith('__workspace/')) return;

    const filePath = buffer.file.path;

    const handleExternalChange = (e: Event) => {
      const detail = (e as CustomEvent).detail as {
        path: string;
        mtime: number;
        size: number;
        deleted: boolean;
      };
      if (detail.path !== filePath) return;

      // Suppress when the change was caused by the editor's own save.
      // Primary guard: identity-based check for save operations in-flight.
      if (saveInFlightRef.current.has(detail.path)) return;
      // Secondary guard: time-based cooldown for redundancy.
      const justSavedAt = justSavedRef.get(detail.path) ?? 0;
      if (Date.now() - justSavedAt < JUST_SAVED_THRESHOLD_MS) return;

      const currentBuffer = bufferRef.current;
      if (!currentBuffer) return;

      // Only handle modified buffers — clean ones are auto-reloaded.
      if (!currentBuffer.isModified) return;

      if (detail.deleted) {
        showFileChangeDialog(currentBuffer.file.name, { deleted: true, hasUnsavedChanges: true })
          .then((action) => {
            // Re-validate: user may have switched files during the dialog.
            if (bufferRef.current?.id !== currentBuffer.id) return;
            // Re-check modification state after async dialog.
            const currentBuf = bufferRef.current;
            if (!currentBuf || !currentBuf.isModified) return;
            if (action === 'keep-mine') {
              setBufferExternallyModified(currentBuffer.id, '');
            }
            // 'ignore': user dismissed the dialog; buffer stays as-is with no action needed.
          })
          .catch((err) => {
            debugLog('[useEditorFileIO] File change dialog error:', err);
            notificationBus.notify('error', 'File Change', 'File change dialog error: ' + String(err));
          });
        return;
      }

      readFileWithConsent(filePath)
        .then((response) => {
          if (!response.ok) return;
          return response.text();
        })
        .then(async (diskContent) => {
          if (diskContent === undefined) return;
          // Re-validate: user may have switched files during the read.
          if (bufferRef.current?.id !== currentBuffer.id) return;

          const editorContent = bufferRef.current?.content || '';
          const action = await showFileChangeDialog(currentBuffer.file.name, {
            deleted: false,
            hasUnsavedChanges: true,
            originalContent: editorContent,
            modifiedContent: diskContent,
          });
          // Re-validate after the async dialog.
          if (bufferRef.current?.id !== currentBuffer.id) return;
          // Re-check modification state after async dialog.
          const currentBuf = bufferRef.current;
          if (!currentBuf || !currentBuf.isModified) return;

          if (action === 'reload') {
            if (loadFileRef.current) {
              await loadFileRef.current(filePath);
            }
            clearBufferExternallyModified(currentBuffer.id);
          } else if (action === 'keep-mine') {
            setBufferExternallyModified(currentBuffer.id, diskContent);
          } else if (action === 'show-diff') {
            try {
              const diffText = generateUnifiedDiff(editorContent, diskContent, 'Editor', 'Disk');
              if (!diffText) return;

              openWorkspaceBuffer({
                kind: 'diff',
                path: `diff:${filePath}`,
                title: `Diff: ${currentBuffer.file.name} (editor ↔ disk)`,
                content: diffText,
                ext: '.diff',
                isPinned: false,
                isClosable: true,
                metadata: { sourcePath: filePath, diffType: 'external-change' },
              });

              const bufferRefId = bufferRef.current?.id;
              if (bufferRefId) {
                setBufferExternallyModified(bufferRefId, diskContent);
              }
            } catch (err) {
              debugLog('[useEditorFileIO] Failed to generate diff:', err);
              notificationBus.notify('warning', 'Diff Generation', 'Failed to generate diff for external changes');
            }
          }
        })
        .catch((err) => {
          warn(`Failed to read externally modified file: ${err instanceof Error ? err.message : String(err)}`);
        });
    };

    document.addEventListener('file_externally_modified', handleExternalChange);
    return () => document.removeEventListener('file_externally_modified', handleExternalChange);
  }, [
    buffer?.id,
    buffer?.kind,
    buffer?.file?.path,
    clearBufferExternallyModified,
    setBufferExternallyModified,
    openWorkspaceBuffer,
  ]); // eslint-disable-line react-hooks/exhaustive-deps

  // Ref to prevent rapid-fire duplicate auto-reload events for the same buffer
  const lastReloadKeyRef = useRef<string>('');

  // ── Auto-reload sync listener ──────────────────────────────────
  // Syncs the CodeMirror view when a clean buffer is auto-reloaded
  // by useAutoReloadCleanBuffers (dispatched via `file:auto-reloaded`).

  useEffect(() => {
    if (!buffer) return;

    const handleAutoReloaded = async (e: Event) => {
      const detail = (e as CustomEvent).detail as { bufferId: string; content: string };
      if (detail.bufferId !== buffer.id) return;

      // Skip duplicate rapid-fire events for the same content.
      const reloadKey = `${detail.bufferId}:${detail.content.length}`;
      if (lastReloadKeyRef.current === reloadKey) return;
      lastReloadKeyRef.current = reloadKey;

      // Skip if content hasn't actually changed to avoid resetting cursor/selection
      // when the file content is the same as what's already in the editor.
      const currentContent = cmViewApiRef.current?.view?.state.doc.toString();
      if (currentContent === detail.content) return;

      if (cmViewApiRef.current?.view) {
        cmViewApiRef.current?.withExternalUpdate(() => {
          cmViewApiRef.current?.dispatch({
            changes: { from: 0, to: cmViewApiRef.current?.view?.state.doc.length ?? 0, insert: detail.content },
            annotations: suppressHistoryAnnotations,
          });
          setLocalContent(detail.content);
          setSelectionInfo(null);
        });
      } else {
        setLocalContent(detail.content);
        setSelectionInfo(null);
      }

      // Refresh diff gutter after auto-reload
      if (bufferRef.current && bufferRef.current.file?.path && cmViewApiRef.current?.view) {
        try {
          const diffResponse = await apiService.getGitDiff(bufferRef.current.file.path);
          if (diffResponse.diff && diffResponse.diff.trim()) {
            updateDiffGutter(cmViewApiRef.current?.view, diffResponse.diff);
          } else {
            clearDiffGutter(cmViewApiRef.current?.view);
          }
        } catch (err) {
          debugLog('[useEditorFileIO] Failed to re-fetch git diff after auto-reload:', err);
          if (cmViewApiRef.current?.view) clearDiffGutter(cmViewApiRef.current?.view);
        }
      }

      // Re-detect indentation on auto-reload
      applyIndentDetection(detail.content);

      // Re-detect line ending on auto-reload
      const lineEndingResult = detectLineEnding(detail.content);
      setLineEnding(lineEndingResult.lineEnding);

      // Refresh diagnostics after auto-reload
      const buf = bufferRef.current;
      if (buf && buf.file?.path && cmViewApiRef.current?.view) {
        fetchDiagnosticsRef.current(buf.file.path, detail.content);
      }
    };

    document.addEventListener('file:auto-reloaded', handleAutoReloaded);
    return () => document.removeEventListener('file:auto-reloaded', handleAutoReloaded);
  }, [buffer?.id, applyIndentDetection]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Sync original content to unsaved-line highlight extension ──
  useEffect(() => {
    if (cmViewApiRef.current?.view && buffer?.originalContent !== undefined) {
      cmViewApiRef.current?.dispatch({
        effects: setOriginalContent.of(buffer.originalContent),
      });
    }
  }, [buffer?.originalContent]); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    loadFile,
    loadFileRef,
    handleSave,
    saveRef,
  };
}
