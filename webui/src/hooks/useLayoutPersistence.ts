import { useCallback, useEffect, useRef } from 'react';
import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import type { EditorBuffer, EditorPane } from '../types/editor';
import {
  saveLayoutSnapshot,
  loadLayoutSnapshot,
  initBeforeUnloadFlush,
  dispose as disposeLayoutPersistence,
  writeStorageItem,
  PANE_LAYOUT_STORAGE_KEY,
  PANE_SIZES_STORAGE_KEY,
  type BufferLayoutEntry,
  type LayoutSnapshot,
} from '../services/layoutPersistence';

interface UseLayoutPersistenceParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  panesRef: MutableRefObject<EditorPane[]>;
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>;
  setPanes: Dispatch<SetStateAction<EditorPane[]>>;
  activePaneId: string | null;
  activeBufferId: string | null;
  setActivePaneId: Dispatch<SetStateAction<string | null>>;
  setActiveBufferId: Dispatch<SetStateAction<string | null>>;
  paneLayout: string;
  paneSizes: Record<string, number>;
}

/** Layout persistence: restore open tabs on mount, save snapshot on changes, cleanup. */
export function useLayoutPersistence({
  buffersRef,
  panesRef,
  buffers,
  panes: _panes,
  setBuffers,
  setPanes,
  activePaneId,
  activeBufferId,
  setActivePaneId,
  setActiveBufferId,
  paneLayout,
  paneSizes,
}: UseLayoutPersistenceParams) {
  // Persist pane layout type to localStorage
  useEffect(() => {
    writeStorageItem(PANE_LAYOUT_STORAGE_KEY, paneLayout);
  }, [paneLayout]);

  // Persist pane sizes to localStorage (debounced)
  const paneSizesTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (paneSizesTimeoutRef.current) clearTimeout(paneSizesTimeoutRef.current);
    paneSizesTimeoutRef.current = setTimeout(() => {
      writeStorageItem(PANE_SIZES_STORAGE_KEY, JSON.stringify(paneSizes));
    }, 300);
    return () => {
      if (paneSizesTimeoutRef.current) {
        clearTimeout(paneSizesTimeoutRef.current);
        writeStorageItem(PANE_SIZES_STORAGE_KEY, JSON.stringify(paneSizes));
      }
    };
  }, [paneSizes]);

  /**
   * Restore open-file tabs from the persisted layout snapshot.
   * Buffer content is NOT restored — EditorPane fetches it on mount.
   */
  const restoreLayout = useCallback(() => {
    const snapshot = loadLayoutSnapshot();
    if (!snapshot || snapshot.buffers.length === 0) return;

    const currentPanes = panesRef.current;
    const validPaneIds = new Set(currentPanes.map((p) => p.id));
    const existingBuffers = buffersRef.current;
    const newBuffers = new Map(existingBuffers);
    const pathToBufferId = new Map<string, string>();

    const createBuffer = (entry: BufferLayoutEntry, index: number): EditorBuffer | null => {
      const filePath = entry.filePath;
      if (filePath.startsWith('__workspace/')) return null;
      const name = filePath.split('/').pop() || filePath;
      const dotIndex = name.lastIndexOf('.');
      const ext = dotIndex > 0 ? name.substring(dotIndex + 1) : undefined;
      const paneId = validPaneIds.has(entry.paneId) ? entry.paneId : 'pane-1';
      const bufferId = `buffer-file-${Date.now()}-${index}`;
      pathToBufferId.set(filePath, bufferId);
      return {
        id: bufferId,
        kind: 'file' as const,
        file: { name, path: filePath, isDir: false, size: 0, modified: 0, ext },
        content: '',
        originalContent: '',
        cursorPosition: entry.cursorPosition,
        scrollPosition: entry.scrollPosition,
        isModified: false,
        isActive: entry.isActive,
        paneId,
      };
    };

    const seen = new Set<string>();
    const existingPaths = new Set(Array.from(newBuffers.values()).map((b) => b.file.path));

    for (let idx = 0; idx < snapshot.bufferOrder.length; idx++) {
      const filePath = snapshot.bufferOrder[idx];
      if (seen.has(filePath) || existingPaths.has(filePath)) continue;
      seen.add(filePath);
      const entry = snapshot.buffers.find((b) => b.filePath === filePath);
      if (!entry) continue;
      const buf = createBuffer(entry, idx);
      if (buf) newBuffers.set(buf.id, buf);
    }

    let fallbackIdx = snapshot.bufferOrder.length;
    for (const entry of snapshot.buffers) {
      if (seen.has(entry.filePath) || existingPaths.has(entry.filePath)) continue;
      seen.add(entry.filePath);
      const buf = createBuffer(entry, fallbackIdx++);
      if (buf) newBuffers.set(buf.id, buf);
    }

    setBuffers(newBuffers);
    setPanes((prev) =>
      prev.map((pane) => {
        const activeBuf = Array.from(newBuffers.values()).find((b) => b.paneId === pane.id && b.isActive);
        return { ...pane, bufferId: activeBuf?.id ?? pane.bufferId };
      }),
    );

    if (snapshot.activePaneId && validPaneIds.has(snapshot.activePaneId)) setActivePaneId(snapshot.activePaneId);
    if (snapshot.activeBufferFilePath && pathToBufferId.has(snapshot.activeBufferFilePath)) {
      const bufferId = pathToBufferId.get(snapshot.activeBufferFilePath);
      if (bufferId) {
        setActiveBufferId(bufferId);
      }
    }
  }, [buffersRef, panesRef, setBuffers, setPanes, setActivePaneId, setActiveBufferId]);

  // Auto-restore layout on first mount
  useEffect(() => {
    restoreLayout();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Register beforeunload listener
  useEffect(() => {
    initBeforeUnloadFlush();
  }, []);

  // Save layout snapshot on every relevant state change (skip first render)
  const hasFirstRenderCompletedRef = useRef(false);
  useEffect(() => {
    if (!hasFirstRenderCompletedRef.current) {
      hasFirstRenderCompletedRef.current = true;
      return;
    }
    const validPaneIds = new Set(panesRef.current.map((p) => p.id));
    const fileBuffers = Array.from(buffers.entries()).filter(
      ([, b]) => b.kind === 'file' && !b.file.path.startsWith('__workspace/'),
    );

    if (fileBuffers.length === 0) {
      saveLayoutSnapshot({
        version: 1,
        activePaneId,
        activeBufferFilePath: null,
        buffers: [],
        bufferOrder: [],
      });
      return;
    }

    const activeBuffer = activeBufferId ? buffers.get(activeBufferId) : null;
    const activeBufferFilePath =
      activeBuffer?.kind === 'file' && activeBuffer.file.path && !activeBuffer.file.path.startsWith('__workspace/')
        ? activeBuffer.file.path
        : null;

    const snapshotBuffers: BufferLayoutEntry[] = fileBuffers.map(([, b]) => ({
      filePath: b.file.path,
      paneId: b.paneId && validPaneIds.has(b.paneId) ? b.paneId : 'pane-1',
      isActive: b.isActive,
      cursorPosition: b.cursorPosition,
      scrollPosition: b.scrollPosition,
    }));

    const snapshot: LayoutSnapshot = {
      version: 1,
      activePaneId,
      activeBufferFilePath,
      buffers: snapshotBuffers,
      bufferOrder: snapshotBuffers.map((b) => b.filePath),
    };
    saveLayoutSnapshot(snapshot);
  }, [buffers, activePaneId, activeBufferId, panesRef]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      disposeLayoutPersistence();
    };
  }, []);

  return { restoreLayout };
}
