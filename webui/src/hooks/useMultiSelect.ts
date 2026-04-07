import { useState, useCallback, useRef, useMemo } from 'react';
import type { FileInfo } from '../components/FileTree';

/** Visible file entry used for range selection ordering. */
interface VisibleEntry {
  path: string;
  depth: number;
}

export interface MultiSelectState {
  /** Set of currently multi-selected paths (may include directories). */
  selectedPaths: Set<string>;
  /** Whether checkboxes should be rendered. */
  showCheckboxes: boolean;
  /** Progress message for batch operations (e.g. "Deleting 2/4…"). */
  batchProgress: string | null;
  /** Whether a batch operation is currently running. */
  isBatchBusy: boolean;
}

export interface MultiSelectActions {
  /** Toggle a path on Ctrl/Cmd+Click. */
  togglePath: (path: string) => void;
  /** Range-select from lastClickedPath to `path`. */
  rangeSelect: (path: string, visibleOrder: VisibleEntry[]) => void;
  /** Clear the entire multi-selection. */
  clearSelection: () => void;
  /** Select all visible paths. */
  selectAll: (visibleOrder: VisibleEntry[]) => void;
  /** Handle a normal click (no modifier) — clears multi-selection. */
  handleNormalClick: (path: string) => void;
  /** Handle Ctrl/Cmd+Click — toggles path. */
  handleCtrlClick: (path: string) => void;
  /** Handle Shift+Click — range select. */
  handleShiftClick: (path: string, visibleOrder: VisibleEntry[]) => void;
  /** Check if a path is in the multi-selection. */
  isSelected: (path: string) => boolean;
  /** Set batch progress message. */
  setBatchProgress: (msg: string | null) => void;
  /** Set batch busy state. */
  setBatchBusy: (busy: boolean) => void;
  /** Force set selected paths (e.g. from external logic). */
  setSelectedPaths: (paths: Set<string>) => void;
}

/**
 * Recursively flatten a FileInfo tree into an ordered array of visible entries.
 * This matches the render order of `renderFileTree`.
 */
export function flattenVisibleFiles(items: FileInfo[]): VisibleEntry[] {
  const result: VisibleEntry[] = [];
  const walk = (list: FileInfo[], depth: number) => {
    for (const item of list) {
      result.push({ path: item.path, depth });
      if (item.isDir && item.children) {
        walk(item.children, depth + 1);
      }
    }
  };
  walk(items, 0);
  return result;
}

/**
 * Custom hook managing multi-select state for the FileTree.
 *
 * Encapsulates Ctrl/Cmd+Click toggle, Shift+Click range select, Ctrl+A
 * select-all, Escape clear, and batch operation progress tracking.
 */
export function useMultiSelect(): [MultiSelectState, MultiSelectActions] {
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [showCheckboxes, setShowCheckboxes] = useState(false);
  const [batchProgress, setBatchProgress] = useState<string | null>(null);
  const [isBatchBusy, setBatchBusy] = useState(false);

  /** The anchor for Shift+Click range selection. */
  const lastClickedPathRef = useRef<string | null>(null);

  const togglePath = useCallback((path: string) => {
    setSelectedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
        // If only one remaining item was removed, hide checkboxes
        if (next.size === 0) {
          // Defer to avoid setState-during-render
          setTimeout(() => setShowCheckboxes(false), 0);
        }
      } else {
        next.add(path);
        setShowCheckboxes(true);
      }
      return next;
    });
  }, []);

  const rangeSelect = useCallback((path: string, visibleOrder: VisibleEntry[]) => {
    const anchor = lastClickedPathRef.current;
    if (!anchor) {
      // No anchor: just select this single item
      setSelectedPaths(new Set([path]));
      setShowCheckboxes(true);
      return;
    }

    const anchorIdx = visibleOrder.findIndex((e) => e.path === anchor);
    const clickedIdx = visibleOrder.findIndex((e) => e.path === path);

    if (anchorIdx === -1 || clickedIdx === -1) {
      // Anchor or clicked not visible — just toggle the clicked one
      setSelectedPaths(new Set([path]));
      setShowCheckboxes(true);
      return;
    }

    const start = Math.min(anchorIdx, clickedIdx);
    const end = Math.max(anchorIdx, clickedIdx);

    const range = new Set<string>();
    for (let i = start; i <= end; i++) {
      range.add(visibleOrder[i].path);
    }
    setSelectedPaths(range);
    setShowCheckboxes(true);
  }, []);

  const clearSelection = useCallback(() => {
    setSelectedPaths(new Set());
    setShowCheckboxes(false);
    setBatchProgress(null);
    lastClickedPathRef.current = null;
  }, []);

  const selectAll = useCallback((visibleOrder: VisibleEntry[]) => {
    const allPaths = new Set(visibleOrder.map((e) => e.path));
    setSelectedPaths(allPaths);
    setShowCheckboxes(true);
  }, []);

  const handleNormalClick = useCallback((path: string) => {
    // Normal click clears multi-selection and sets anchor
    lastClickedPathRef.current = path;
    // Only clear if there was something selected
    setSelectedPaths((prev) => (prev.size > 0 ? new Set() : prev));
    setShowCheckboxes((prev) => (prev ? false : prev));
  }, []);

  const handleCtrlClick = useCallback(
    (path: string) => {
      lastClickedPathRef.current = path;
      togglePath(path);
    },
    [togglePath],
  );

  const handleShiftClick = useCallback(
    (path: string, visibleOrder: VisibleEntry[]) => {
      rangeSelect(path, visibleOrder);
    },
    [rangeSelect],
  );

  const isSelected = useCallback(
    (path: string) => selectedPaths.has(path),
    [selectedPaths],
  );

  const state: MultiSelectState = useMemo(
    () => ({
      selectedPaths,
      showCheckboxes,
      batchProgress,
      isBatchBusy,
    }),
    [selectedPaths, showCheckboxes, batchProgress, isBatchBusy],
  );

  const actions: MultiSelectActions = useMemo(
    () => ({
      togglePath,
      rangeSelect,
      clearSelection,
      selectAll,
      handleNormalClick,
      handleCtrlClick,
      handleShiftClick,
      isSelected,
      setBatchProgress,
      setBatchBusy,
      setSelectedPaths,
    }),
    [
      togglePath,
      rangeSelect,
      clearSelection,
      selectAll,
      handleNormalClick,
      handleCtrlClick,
      handleShiftClick,
      isSelected,
    ],
  );

  return [state, actions];
}
