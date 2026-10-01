import { useCallback, useEffect, useRef, useState } from 'react';
import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import { ApiService } from '../services/api';
import { getAppStateStorageKey } from '../services/appStatePersistence';
import { persistTabWorkspacePath } from '../services/clientSession';
import {
  saveLayoutSnapshot,
  loadLayoutSnapshot,
  clearLayoutSnapshot,
  initBeforeUnloadFlush,
  dispose as disposeLayoutPersistence,
  writeStorageItem,
  getPaneLayoutStorageKey,
  getPaneSizesStorageKey,
  getPanesStorageKey,
  getChatPanesStorageKey,
  loadSavedPanes,
  loadSavedPaneLayout,
  loadSavedPaneSizes,
  loadChatPanePlacement,
  loadTabOrder,
  getTabOrderStorageKey,
  type BufferLayoutEntry,
  type LayoutSnapshot,
} from '../services/layoutPersistence';
import type { EditorBuffer, EditorPane, PaneLayout, PaneSize } from '../types/editor';
import { resolveEditorFilePath, getLSPClientService } from '../services/lspClientService';
import { debugLog } from '../utils/log';

interface UseLayoutPersistenceParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  panesRef: MutableRefObject<EditorPane[]>;
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>;
  setPanes: Dispatch<SetStateAction<EditorPane[]>>;
  activePaneId: string | null;
  activeBufferId: string | null;
  setActivePaneId: (id: string | null) => void;
  setActiveBufferId: (id: string | null) => void;
  paneLayout: string;
  paneSizes: Record<string, number>;
  setPaneLayout?: (layout: PaneLayout) => void;
  setPaneSizes?: (sizes: PaneSize) => void;
}

const MAX_TAB_ORDER = 100;

/**
 * The tab order to save: open tabs in their current order, with tabs that
 * aren't open (yet) keeping their slots. Tabs reopen at different times after
 * a reload (files from the snapshot, chats from the chat list); replacing the
 * saved order with only the tabs open so far lost the rest's positions.
 */
export function mergeTabOrder(saved: string[], open: string[]): string[] {
  const openSet = new Set(open);
  const queue = [...open];
  const merged: string[] = [];
  for (const path of saved) {
    if (!openSet.has(path)) merged.push(path);
    else if (queue.length > 0) merged.push(queue.shift()!);
  }
  merged.push(...queue);
  const unique = Array.from(new Set(merged));
  return unique.slice(Math.max(0, unique.length - MAX_TAB_ORDER));
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
  setPaneLayout,
  setPaneSizes,
}: UseLayoutPersistenceParams) {
  const activeBufferIdRef = useRef(activeBufferId);
  activeBufferIdRef.current = activeBufferId;
  // Nothing is saved until the saved layout has been restored: the initial
  // single pane would otherwise overwrite the saved split first.
  const [layoutRestored, setLayoutRestored] = useState(false);
  // Persist pane layout type to localStorage
  useEffect(() => {
    if (!layoutRestored) return;
    writeStorageItem(getPaneLayoutStorageKey(), paneLayout);
  }, [paneLayout, layoutRestored]);

  // Persist pane sizes to localStorage (debounced)
  const paneSizesTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (!layoutRestored) return;
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
  }, [paneSizes, layoutRestored]);

  /**
   * Restore open-file tabs from the persisted layout snapshot.
   * Buffer content is NOT restored — EditorPane fetches it on mount.
   */
  const restoreLayout = useCallback(() => {
    // The split first: the panes must exist before tabs go back into them.
    let validPaneIds = new Set(panesRef.current.map((p) => p.id));
    const savedPanes = loadSavedPanes();
    if (savedPanes && panesRef.current.length === 1) {
      const restoredIds = new Set(savedPanes.map((p) => p.id));
      const placement = loadChatPanePlacement();
      const moved = new Map(buffersRef.current);
      for (const [id, b] of moved) {
        const chatId = b.kind === 'chat' ? (b.metadata?.chatId as string | null | undefined) : undefined;
        const target = chatId ? placement[chatId] : undefined;
        if (target && restoredIds.has(target) && b.paneId !== target) moved.set(id, { ...b, paneId: target });
      }
      buffersRef.current = moved;
      setBuffers(moved);
      // Keep the focused tab focused (its chat stays the active chat).
      const focused = activeBufferIdRef.current ? moved.get(activeBufferIdRef.current) : undefined;
      const activePane = focused?.paneId && restoredIds.has(focused.paneId) ? focused.paneId : 'pane-1';
      const shownIn = (paneId: string) => {
        const inPane = Array.from(moved.values()).filter((b) => b.paneId === paneId);
        if (paneId === activePane && focused) return focused.id;
        return (inPane.find((b) => b.isActive) ?? inPane[0])?.id ?? null;
      };
      const restoredPanes: EditorPane[] = savedPanes.map((p) => ({
        id: p.id,
        position: p.position,
        isActive: p.id === activePane,
        bufferId: shownIn(p.id),
      }));
      panesRef.current = restoredPanes;
      setPanes(restoredPanes);
      setActivePaneId(activePane);
      setPaneLayout?.(loadSavedPaneLayout() ?? 'split-vertical');
      const sizes = loadSavedPaneSizes();
      setPaneSizes?.(
        sizes && savedPanes.every((p) => typeof sizes[p.id] === 'number')
          ? sizes
          : Object.fromEntries(savedPanes.map((p) => [p.id, 100 / savedPanes.length])),
      );
      validPaneIds = restoredIds;
    }

    const snapshot = loadLayoutSnapshot();
    if (!snapshot || snapshot.buffers.length === 0) return;
    const existingBuffers = buffersRef.current;
    const newBuffers = new Map(existingBuffers);
    const pathToBufferId = new Map<string, string>();

    const createBuffer = (entry: BufferLayoutEntry, index: number): EditorBuffer | null => {
      const filePath = resolveEditorFilePath(entry.filePath);
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

    // A restored file that was active takes its pane: the other tabs there
    // (the startup chat tab) stop being the pane's active one, or the chat
    // tab sync would treat it as focused and move focus back to chat.
    const restoredActivePanes = new Set(
      Array.from(newBuffers.values())
        .filter((b) => b.kind === 'file' && b.isActive && pathToBufferId.has(b.file.path))
        .map((b) => b.paneId),
    );
    for (const [id, b] of newBuffers) {
      if (b.isActive && restoredActivePanes.has(b.paneId) && !pathToBufferId.has(b.file.path)) {
        newBuffers.set(id, { ...b, isActive: false });
      }
    }
    buffersRef.current = newBuffers;
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
  }, [buffersRef, panesRef, setBuffers, setPanes, setActivePaneId, setActiveBufferId, setPaneLayout, setPaneSizes]);

  // Auto-restore layout on first mount — wait for workspace path to be set first
  // so that restoreLayout loads the correct workspace-scoped snapshot.
  useEffect(() => {
    let cancelled = false;
    ApiService.getInstance()
      .getWorkspace()
      .then((ws) => {
        if (!cancelled && ws.workspace_root) {
          persistTabWorkspacePath(ws.workspace_root);
          // Seed the LSP client service's cached workspace root so path
          // resolution (relative → absolute) works before any LSP client connects.
          getLSPClientService().setWorkspaceRoot(ws.workspace_root);
        }
      })
      .catch((err) => {
        debugLog('[LayoutPersistence] Failed to fetch workspace root:', err);
      })
      .finally(() => {
        if (!cancelled) {
          restoreLayout();
          setLayoutRestored(true);
        }
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Register beforeunload listener
  useEffect(() => {
    initBeforeUnloadFlush();
  }, []);

  // Save the pane list (so a split survives reload) and which pane each chat
  // tab is in. Chat tabs reopen from the chat list, not the file snapshot, so
  // their placement is kept separately.
  const chatPlacementFingerprintRef = useRef('');
  const panesFingerprintRef = useRef('');
  useEffect(() => {
    if (!layoutRestored) return;
    const panesJSON = JSON.stringify(_panes.map((p) => ({ id: p.id, position: p.position })));
    if (panesJSON !== panesFingerprintRef.current) {
      panesFingerprintRef.current = panesJSON;
      writeStorageItem(getPanesStorageKey(), panesJSON);
    }
    const validPaneIds = new Set(_panes.map((p) => p.id));
    // Merge: a chat whose tab hasn't reopened yet (the chat list loads after
    // the layout) keeps its saved pane rather than losing it.
    const placement: Record<string, string> = { ...loadChatPanePlacement() };
    for (const b of buffersRef.current.values()) {
      const chatId = b.kind === 'chat' ? (b.metadata?.chatId as string | null | undefined) : undefined;
      if (chatId && b.paneId && validPaneIds.has(b.paneId)) placement[chatId] = b.paneId;
    }
    const fingerprint = JSON.stringify(placement);
    if (fingerprint !== chatPlacementFingerprintRef.current) {
      chatPlacementFingerprintRef.current = fingerprint;
      writeStorageItem(getChatPanesStorageKey(), fingerprint);
    }
  }, [_panes, buffers, buffersRef, layoutRestored]);

  // Keep the tab strip in the saved order. Chat tabs reopen from the chat
  // list after files are restored, so without this every reload put the
  // files first. Tabs are sorted only when the set of tabs changes (a tab
  // appeared); a pure reorder is the user dragging, and is just saved.
  const tabSetRef = useRef('');
  const tabOrderRef = useRef('');
  useEffect(() => {
    if (!layoutRestored) return;
    const all = Array.from(buffers.entries()).filter(([, b]) => !b.metadata?.creating);
    const tabSet = all
      .map(([, b]) => b.file.path)
      .sort()
      .join('\n');
    if (tabSet !== tabSetRef.current) {
      tabSetRef.current = tabSet;
      const saved = loadTabOrder();
      if (saved) {
        const rank = new Map(saved.map((path, i) => [path, i]));
        const entries = Array.from(buffers.entries());
        const sorted = entries
          .map((entry, i) => ({ entry, i, r: rank.get(entry[1].file.path) ?? Number.POSITIVE_INFINITY }))
          .sort((a, b) => (a.r === b.r ? a.i - b.i : a.r - b.r))
          .map((x) => x.entry);
        if (sorted.some((entry, i) => entry[0] !== entries[i][0])) {
          const next = new Map(sorted);
          buffersRef.current = next;
          setBuffers(next);
          return;
        }
      }
    }
    const order = JSON.stringify(
      mergeTabOrder(
        loadTabOrder() ?? [],
        all.map(([, b]) => b.file.path),
      ),
    );
    if (order !== tabOrderRef.current) {
      tabOrderRef.current = order;
      writeStorageItem(getTabOrderStorageKey(), order);
    }
  }, [buffers, layoutRestored, buffersRef, setBuffers]);

  // Save layout snapshot on relevant state changes (skip first render).
  // Use buffersRef to avoid re-running on every keystroke (Map identity changes
  // on content updates). Only save when layout-relevant properties actually differ.
  const hasFirstRenderCompletedRef = useRef(false);
  // Track previous layout fingerprint to avoid redundant localStorage writes.
  const prevLayoutFingerprintRef = useRef<string>('');

  useEffect(() => {
    if (!hasFirstRenderCompletedRef.current) {
      hasFirstRenderCompletedRef.current = true;
      return;
    }
    // Saving before the restore would store the still-empty tab set over the
    // saved one.
    if (!layoutRestored) return;

    const allBuffers = buffersRef.current;
    const validPaneIds = new Set(panesRef.current.map((p) => p.id));
    const fileBuffers = Array.from(allBuffers.entries()).filter(
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

    const activeBuffer = activeBufferId ? allBuffers.get(activeBufferId) : null;
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

    // Build a fingerprint of layout-relevant state to skip redundant saves.
    const fingerprint = JSON.stringify({
      activePaneId,
      activeBufferFilePath,
      bufferCount: snapshotBuffers.length,
      bufferOrder: snapshotBuffers.map((b) => b.filePath),
    });

    // Only write to localStorage when the buffer layout actually changed
    // (files opened/closed, active file switched, pane switched).
    // Cursor/scroll positions are captured on every render but only persisted
    // when the layout fingerprint changes, avoiding excessive localStorage I/O.
    if (fingerprint !== prevLayoutFingerprintRef.current) {
      prevLayoutFingerprintRef.current = fingerprint;
      const snapshot: LayoutSnapshot = {
        version: 1,
        activePaneId,
        activeBufferFilePath,
        buffers: snapshotBuffers,
        bufferOrder: snapshotBuffers.map((b) => b.filePath),
      };
      saveLayoutSnapshot(snapshot);
    }
  }, [activePaneId, activeBufferId, buffersRef, panesRef, layoutRestored]);

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
          // Modified buffers survive the switch: they may hold unsaved edits
          // that only exist in this browser session, and deleting them here
          // destroyed user work whenever a worktree/workspace switch fired
          // in the background. The user closes them manually (with the
          // confirm dialog) or auto-save flushes them to disk on cadence.
          if (buf.isModified) return;
          if (
            (buf.kind === 'file' && buf.isClosable !== false && !buf.isPinned) ||
            (buf.kind === 'chat' && buf.isClosable === true)
          ) {
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
            const chatBuf = Array.from(buffersRef.current.values()).find((b) => b.kind === 'chat');
            return { ...pane, bufferId: chatBuf?.id || null };
          }
          return pane;
        }),
      );
      // Also clear the layout snapshot since the workspace changed
      clearLayoutSnapshot();
    };

    window.addEventListener('sprout:workspace-changed', handleWorkspaceChanged);
    return () => window.removeEventListener('sprout:workspace-changed', handleWorkspaceChanged);
  }, [buffersRef, setBuffers, setPanes]);

  return { restoreLayout };
}
