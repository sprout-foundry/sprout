import { useCallback } from 'react';
import { useLog } from '../utils/log';
import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import type { EditorBuffer, EditorPane } from '../types/editor';

interface UseTabManagementParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  activePaneIdRef: MutableRefObject<string | null>;
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>;
  setPanes: Dispatch<SetStateAction<EditorPane[]>>;
  setActiveBufferId: Dispatch<SetStateAction<string | null>>;
  setActivePaneId: Dispatch<SetStateAction<string | null>>;
  activePaneId: string | null;
  activeBufferId: string | null;
  isAutoSaveEnabled: boolean;
  saveBuffer: (bufferId: string) => Promise<void>;
}

/** Core tab management: activate, switch, close, reorder, move. */
export function useTabManagement({
  buffersRef,
  activePaneIdRef,
  setBuffers,
  setPanes,
  setActiveBufferId,
  setActivePaneId,
  activePaneId,
  activeBufferId,
  isAutoSaveEnabled,
  saveBuffer,
}: UseTabManagementParams) {
  const log = useLog();

  const activateBuffer = useCallback(
    (bufferId: string) => {
      const currentActivePane = activePaneIdRef.current;
      setActiveBufferId(bufferId);
      setBuffers((prev) => {
        const next = new Map(prev);
        const buffer = next.get(bufferId);
        if (buffer) {
          if (currentActivePane) {
            Array.from(next.entries()).forEach(([id, buf]) => {
              if (buf.paneId === currentActivePane && id !== bufferId) next.set(id, { ...buf, isActive: false });
            });
          }
          next.set(bufferId, { ...buffer, isActive: true, paneId: currentActivePane });
        }
        return next;
      });
      setPanes((prev) => prev.map((pane) => (pane.id === currentActivePane ? { ...pane, bufferId } : pane)));
    },
    [activePaneIdRef, setActiveBufferId, setBuffers, setPanes],
  );

  const switchToBuffer = useCallback(
    (bufferId: string) => {
      const existingBuffer = buffersRef.current.get(bufferId);
      if (!existingBuffer) return;
      const currentPaneId = activePaneIdRef.current;
      if (existingBuffer.paneId && existingBuffer.paneId !== currentPaneId) {
        setActivePaneId(existingBuffer.paneId);
        setActiveBufferId(bufferId);
        setBuffers((prev) => {
          const next = new Map(prev);
          Array.from(next.entries()).forEach(([id, buf]) => {
            if (buf.paneId === existingBuffer.paneId) next.set(id, { ...buf, isActive: id === bufferId });
          });
          return next;
        });
        setPanes((prev) => prev.map((pane) => (pane.id === existingBuffer.paneId ? { ...pane, bufferId } : pane)));
        return;
      }
      setActiveBufferId(bufferId);
      setBuffers((prev) => {
        const next = new Map(prev);
        Array.from(next.entries()).forEach(([id, buf]) => {
          if (buf.paneId === currentPaneId) next.set(id, { ...buf, isActive: id === bufferId });
        });
        const buffer = next.get(bufferId);
        if (buffer) next.set(bufferId, { ...buffer, isActive: true, paneId: currentPaneId });
        return next;
      });
      setPanes((prev) => prev.map((pane) => (pane.id === currentPaneId ? { ...pane, bufferId } : pane)));
    },
    [activePaneIdRef, setActiveBufferId, setActivePaneId, setBuffers, setPanes, buffersRef],
  );

  const closeBuffer = useCallback(
    (bufferId: string) => {
      const buffer = buffersRef.current.get(bufferId);
      if (!buffer || buffer.isClosable === false) return;
      if (buffer.isModified && isAutoSaveEnabled) {
        saveBuffer(bufferId).catch(() => log.error('Failed to save buffer before closing', { title: 'Save Error' }));
      }
      const remain = Array.from(buffersRef.current.values()).filter((c) => c.id !== bufferId);
      const nextPaneBuffer = buffer.paneId
        ? remain.find((c) => c.paneId === buffer.paneId) || remain.find((c) => !c.paneId) || null
        : null;
      const currentActivePane = activePaneIdRef.current;
      setBuffers((prev) => {
        const next = new Map(prev);
        next.delete(bufferId);
        if (buffer.paneId && nextPaneBuffer) {
          const r = next.get(nextPaneBuffer.id);
          if (r) {
            next.set(nextPaneBuffer.id, {
              ...r,
              isActive: currentActivePane === buffer.paneId,
              paneId: buffer.paneId,
            });
          }
        }
        return next;
      });
      if (buffer.paneId) {
        setPanes((prev) =>
          prev.map((p) => (p.id === buffer.paneId ? { ...p, bufferId: nextPaneBuffer?.id || null } : p)),
        );
      }
      if (bufferId === activeBufferId) setActiveBufferId(nextPaneBuffer?.id || null);
    },
    [activeBufferId, activePaneIdRef, buffersRef, isAutoSaveEnabled, saveBuffer, setBuffers, setPanes, setActiveBufferId, log],
  );

  const closeAllBuffers = useCallback(() => {
    const cb = buffersRef.current;
    const closableIds = Array.from(cb.entries())
      .filter(([_, b]) => b.isClosable !== false)
      .map(([id]) => id);
    for (const bid of closableIds) {
      const b = cb.get(bid);
      if (!b) continue;
      if (b.isModified && isAutoSaveEnabled) {
        saveBuffer(bid).catch(() => log.error('Failed to save buffer', { title: 'Save Error' }));
      }
      setBuffers((prev) => {
        const n = new Map(prev);
        n.delete(bid);
        return n;
      });
    }
    const remaining = Array.from(cb.values()).filter((b) => !closableIds.includes(b.id));
    const nextBuffer = remaining[0] || null;
    setActiveBufferId(nextBuffer?.id || null);
    setPanes((prev) =>
      prev.map((p) => ({
        ...p,
        bufferId: closableIds.includes(p.bufferId || '') ? nextBuffer?.id || null : p.bufferId,
      })),
    );
  }, [buffersRef, isAutoSaveEnabled, saveBuffer, setBuffers, setPanes, setActiveBufferId, log]);

  const closeOtherBuffers = useCallback(
    (keepBufferId: string) => {
      const cb = buffersRef.current;
      const ids = Array.from(cb.entries())
        .filter(([id, b]) => id !== keepBufferId && b.isClosable !== false)
        .map(([id]) => id);
      for (const bid of ids) {
        const b = cb.get(bid);
        if (!b) continue;
        if (b.isModified && isAutoSaveEnabled) {
          saveBuffer(bid).catch(() => log.error('Failed to save buffer', { title: 'Save Error' }));
        }
        setBuffers((prev) => {
          const n = new Map(prev);
          n.delete(bid);
          return n;
        });
      }
      const kb = cb.get(keepBufferId);
      const pid = kb?.paneId || activePaneIdRef.current;
      setActiveBufferId(keepBufferId);
      setBuffers((prev) => {
        const n = new Map(prev);
        const b = n.get(keepBufferId);
        if (b) n.set(keepBufferId, { ...b, isActive: true, paneId: pid });
        return n;
      });
      setPanes((prev) => prev.map((p) => (p.id === pid ? { ...p, bufferId: keepBufferId } : p)));
    }, [activePaneIdRef, buffersRef, isAutoSaveEnabled, saveBuffer, setBuffers, setPanes, setActiveBufferId, log],);

  const reorderBuffers = useCallback(
    (sourceBufferId: string, targetBufferId: string) => {
      if (!sourceBufferId || !targetBufferId || sourceBufferId === targetBufferId) return;
      setBuffers((prev) => {
        const entries = Array.from(prev.entries());
        const si = entries.findIndex(([id]) => id === sourceBufferId);
        const ti = entries.findIndex(([id]) => id === targetBufferId);
        if (si === -1 || ti === -1) return prev;
        const [moved] = entries.splice(si, 1);
        const nti = entries.findIndex(([id]) => id === targetBufferId);
        entries.splice(nti, 0, moved);
        return new Map(entries);
      });
    },
    [setBuffers],
  );

  const moveBufferToPane = useCallback(
    (bufferId: string, paneId: string) => {
      const buffer = buffersRef.current.get(bufferId);
      if (!buffer || buffer.paneId === paneId) return;
      setBuffers((prev) => {
        const next = new Map(prev);
        next.forEach((existing, key) => {
          if (key !== bufferId && existing.paneId === paneId) next.set(key, { ...existing, isActive: false });
        });
        const moved = next.get(bufferId);
        if (!moved) return prev;
        next.set(bufferId, { ...moved, paneId, isActive: activePaneId === paneId });
        return next;
      });
      setPanes((prev) =>
        prev.map((pane) => {
          if (pane.id === paneId) return { ...pane, bufferId };
          if (pane.bufferId === bufferId) return { ...pane, bufferId: null };
          return pane;
        }),
      );
      if (activePaneId === paneId) setActiveBufferId(bufferId);
    },
    [activePaneId, buffersRef, setBuffers, setPanes, setActiveBufferId],
  );

  return {
    activateBuffer,
    switchToBuffer,
    closeBuffer,
    closeAllBuffers,
    closeOtherBuffers,
    reorderBuffers,
    moveBufferToPane,
  };
}
