import { useCallback } from 'react';
import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import type { EditorBuffer, EditorPane } from '../types/editor';

interface UseTabOpenParams {
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  activePaneIdRef: MutableRefObject<string | null>;
  panesRef: MutableRefObject<EditorPane[]>;
  setBuffers: Dispatch<SetStateAction<Map<string, EditorBuffer>>>;
  setPanes: Dispatch<SetStateAction<EditorPane[]>>;
  setActiveBufferId: Dispatch<SetStateAction<string | null>>;
  setActivePaneId: Dispatch<SetStateAction<string | null>>;
  activePaneId: string | null;
  activateBuffer: (bufferId: string) => void;
  switchToBuffer: (bufferId: string) => void;
}

/**
 * Tab opening logic: openFile, openWorkspaceBuffer.
 * Depends on activateBuffer and switchToBuffer from the caller.
 */
export function useTabOpen({
  buffersRef, activePaneIdRef, panesRef,
  setBuffers, setPanes, setActiveBufferId, setActivePaneId,
  activePaneId, activateBuffer, switchToBuffer,
}: UseTabOpenParams) {
  // Open a file in an editor pane
  const openFile = useCallback((file: any) => {
    const filePath = file.path;
    const currentBuffers = buffersRef.current;
    const currentActivePane = activePaneIdRef.current;
    const existingBuffer = Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === filePath);
    if (existingBuffer) {
      const [bufferId, buffer] = existingBuffer;
      // If buffer is already in a pane, switch to that pane and activate properly
      if (buffer.paneId) {
        const pane = panesRef.current.find(p => p.id === buffer.paneId);
        if (pane) {
          switchToBuffer(bufferId);
          return bufferId;
        }
      }
      // Otherwise activate in current pane
      activateBuffer(bufferId);
      return bufferId;
    }

    // Create new buffer
    const bufferId = `buffer-${Date.now()}`;
    const newBuffer: EditorBuffer = {
      id: bufferId, kind: 'file', file: file,
      content: '', originalContent: '',
      cursorPosition: { line: 0, column: 0 }, scrollPosition: { top: 0, left: 0 },
      isModified: false, isActive: true, paneId: currentActivePane,
    };

    setBuffers(prev => {
      const next = new Map(prev);
      next.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === currentActivePane) {
          next.set(key, { ...existing, isActive: false });
        }
      });
      next.set(bufferId, newBuffer);
      return next;
    });

    setPanes(prev => prev.map(pane =>
      pane.id === currentActivePane ? { ...pane, bufferId } : pane
    ));

    setActiveBufferId(bufferId);
    return bufferId;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activateBuffer, panesRef, setBuffers, setPanes, setActiveBufferId, buffersRef, activePaneIdRef]);

  // Helper to find the rightmost pane for chat placement
  const getRightmostPane = useCallback((paneList: EditorPane[]) => {
    if (paneList.length === 0) return null;
    const positionOrder: Record<string, number> = { 'primary': 0, 'secondary': 1, 'tertiary': 2 };
    return paneList.reduce((rightmost, pane) => {
      const rightmostOrder = positionOrder[rightmost.position as string] ?? 0;
      const paneOrder = positionOrder[pane.position as string] ?? 0;
      return paneOrder > rightmostOrder ? pane : rightmost;
    }, paneList[0]);
  }, []);

  const openWorkspaceBuffer = useCallback((options: {
    kind: 'chat' | 'diff' | 'review' | 'file';
    path: string; title: string; content?: string; ext?: string;
    isPinned?: boolean; isClosable?: boolean; metadata?: Record<string, any>;
  }) => {
    const currentBuffers = buffersRef.current;
    const existingBufferEntry = Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === options.path);

    if (existingBufferEntry) {
      const [bufferId, buffer] = existingBufferEntry;
      setBuffers(prev => {
        const next = new Map(prev);
        next.set(bufferId, {
          ...buffer, kind: options.kind,
          file: { ...buffer.file, name: options.title, path: options.path, ext: options.ext || buffer.file.ext },
          content: options.content ?? buffer.content, originalContent: options.content ?? buffer.originalContent,
          isPinned: options.isPinned ?? buffer.isPinned, isClosable: options.isClosable ?? buffer.isClosable,
          metadata: options.metadata ?? buffer.metadata,
        });
        return next;
      });
      activateBuffer(bufferId);
      return bufferId;
    }

    const currentPanes = panesRef.current;
    const targetPane = options.kind === 'chat' ? getRightmostPane(currentPanes) : currentPanes.find(p => p.id === activePaneId);
    const targetPaneId = targetPane?.id ?? activePaneId;

    const bufferId = `buffer-${options.kind}-${Date.now()}`;
    const newBuffer: EditorBuffer = {
      id: bufferId, kind: options.kind,
      file: { name: options.title, path: options.path, isDir: false, size: 0, modified: 0, ext: options.ext },
      content: options.content ?? '', originalContent: options.content ?? '',
      cursorPosition: { line: 0, column: 0 }, scrollPosition: { top: 0, left: 0 },
      isModified: false, isActive: true, paneId: targetPaneId,
      isPinned: options.isPinned ?? false, isClosable: options.isClosable ?? !options.isPinned,
      metadata: options.metadata ?? {},
    };

    setBuffers(prev => {
      const next = new Map(prev);
      next.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === targetPaneId) {
          next.set(key, { ...existing, isActive: false });
        }
      });
      next.set(bufferId, newBuffer);
      return next;
    });

    setPanes(prev => prev.map(pane =>
      pane.id === targetPaneId ? { ...pane, bufferId } : pane
    ));

    setActivePaneId(targetPaneId);
    setActiveBufferId(bufferId);
    return bufferId;
  }, [activePaneId, activateBuffer, buffersRef, getRightmostPane, panesRef, setBuffers, setPanes, setActiveBufferId, setActivePaneId]);

  return { openFile, openWorkspaceBuffer };
}
