import { useCallback, useEffect, useRef } from 'react';
import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import type { EditorBuffer, EditorPane } from '../types/editor';
import { persistTabWorkspacePath } from '../services/clientSession';
import { ApiService } from '../services/api';
import { getAppStateStorageKey } from '../services/appStatePersistence';
import {
  saveLayoutSnapshot,
  loadLayoutSnapshot,
  clearLayoutSnapshot,
  initBeforeUnloadFlush,
  dispose as disposeLayoutPersistence,
  writeStorageItem,
  getPaneLayoutStorageKey,
  getPaneSizesStorageKey,
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
    writeStorageItem(getPaneLayoutStorageKey(), paneLayout);
  }, [paneLayout]);

  // Persist pane sizes to localStorage (debounced)
  const paneSizesTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (paneSizesTimeoutRef.current) clearTimeout(paneSizesTimeoutRef.current);
    paneSizesTimeoutRef.current = setTimeout(() => {
      writeStorageItem(getPaneSizesStorageKey(), JSON.stringify(paneSizes));
    }, 300);
    return () => {
      if (paneSizesTimeoutRef.current) {
        clearTimeout(paneSizesTimeoutRef.current);
        writeStorageItem(getPaneSizesStorageKey(), JSON.stringify(paneSizes));
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

  // Auto-restore layout on first mount — wait for workspace path to be set first
  // so that restoreLayout loads the correct workspace-scoped snapshot.
  useEffect(() => {
    let cancelled = false;
    ApiService.getInstance()
      .getWorkspace()
      .then((ws) => {
        if (!cancelled && ws.workspace_root) {
          persistTabWorkspacePath(ws.workspace_root);
        }
      })
      .catch(() => {
        // Non-critical: workspace scoping will use _default
      })
      .finally(() => {
        if (!cancelled) {
          restoreLayout();
        }
      });
    return () => { cancelled = true; };
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

  // Clear stale file buffers when the workspace root changes (e.g. worktree switch).
  // Chat/welcome/diff buffers are preserved (non-default chat sessions are cleaned up
  // to avoid stale workspace-specific chats). Pinned buffers are also preserved.
  useEffect(() => {
    const handleWorkspaceChanged = (event: Event) => {
      const detail = (event as CustomEvent).detail;
      if (detail?.workspaceRoot) {
        persistTabWorkspacePath(detail.workspaceRoot);
      }
      // Clear chat persistence so stale messages from old workspace don't load
      window.localStorage.removeItem(getAppStateStorageKey());
      setBuffers((prev) => {
        const next = new Map(prev);
        let changed = false;
        next.forEach((buf, id) => {
          if ((buf.kind === 'file' && buf.isClosable !== false && !buf.isPinned) ||
              (buf.kind === 'chat' && buf.isClosable === true)) {
            next.delete(id);
            changed = true;
          }
        });
        return changed ? next : prev;
      });
      // Reset panes to show the chat buffer (or whatever is remaining) instead
      // of a stale file buffer that was just removed.
      setPanes((prev) =>
        prev.map((pane) => {
          if (pane.bufferId && !buffersRef.current.has(pane.bufferId)) {
            // Find the first chat buffer to show, or null for empty
            const chatBuf = Array.from(buffersRef.current.values()).find(
              (b) => b.kind === 'chat',
            );
            return { ...pane, bufferId: chatBuf?.id || null };
          }
          return pane;
        }),
      );
      // Also clear the layout snapshot since the workspace changed
      clearLayoutSnapshot();
    };

    window.addEventListener('ledit:workspace-changed', handleWorkspaceChanged);
    return () => window.removeEventListener('ledit:workspace-changed', handleWorkspaceChanged);
  }, [buffersRef, setBuffers, setPanes]);

  return { restoreLayout };
}
