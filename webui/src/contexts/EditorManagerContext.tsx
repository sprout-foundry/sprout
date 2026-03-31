import React, { createContext, useContext, useState, useCallback, useEffect, useRef, ReactNode } from 'react';
import { EditorBuffer, EditorPane, PaneLayout } from '../types/editor';
import { writeFileWithConsent } from '../services/fileAccess';

interface PaneSize {
  [paneId: string]: number; // Size in pixels or percentage
}

interface EditorManagerContextValue {
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
  activeBufferId: string | null;
  isAutoSaveEnabled: boolean;
  autoSaveInterval: number; // milliseconds
  paneSizes: PaneSize; // Track pane sizes for resizable split panes

  // Actions
  openFile: (file: any) => string; // Returns buffer ID
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, any>;
  }) => string;
  closeBuffer: (bufferId: string) => void;
  reorderBuffers: (sourceBufferId: string, targetBufferId: string) => void;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
  closePane: (paneId: string) => void;
  switchPane: (paneId: string) => void;
  switchToBuffer: (bufferId: string) => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  closeSplit: () => void;
  setPaneLayout: (layout: PaneLayout) => void;
  updateBufferContent: (bufferId: string, content: string) => void;
  updateBufferCursor: (bufferId: string, position: { line: number; column: number }) => void;
  updateBufferScroll: (bufferId: string, position: { top: number; left: number }) => void;
  saveBuffer: (bufferId: string) => Promise<void>;
  setBufferModified: (bufferId: string, isModified: boolean) => void;
  saveAllBuffers: () => Promise<void>;
  updatePaneSize: (paneId: string, size: number) => void; // Update pane size
}

const EditorManagerContext = createContext<EditorManagerContextValue | null>(null);

export const useEditorManager = () => {
  const context = useContext(EditorManagerContext);
  if (!context) {
    throw new Error('useEditorManager must be used within EditorManagerProvider');
  }
  return context;
};

interface EditorManagerProviderProps {
  children: ReactNode;
}

export const EditorManagerProvider: React.FC<EditorManagerProviderProps> = ({ children }) => {
  const [buffers, setBuffers] = useState<Map<string, EditorBuffer>>(() => {
    const chatBuffer: EditorBuffer = {
      id: 'buffer-chat',
      kind: 'chat',
      file: {
        name: 'Chat',
        path: '__workspace/chat',
        isDir: false,
        size: 0,
        modified: 0,
        ext: '.chat'
      },
      content: '',
      originalContent: '',
      cursorPosition: { line: 0, column: 0 },
      scrollPosition: { top: 0, left: 0 },
      isModified: false,
      isActive: true,
      paneId: 'pane-1',
      isPinned: true,
      isClosable: false,
      metadata: {}
    };

    return new Map([[chatBuffer.id, chatBuffer]]);
  });
  const [panes, setPanes] = useState<EditorPane[]>([
    { id: 'pane-1', bufferId: 'buffer-chat', isActive: true, position: 'primary' }
  ]);
  const [paneLayout, setPaneLayoutState] = useState<PaneLayout>('single');
  const [activePaneId, setActivePaneId] = useState<string | null>('pane-1');
  const [activeBufferId, setActiveBufferId] = useState<string | null>('buffer-chat');
  const [isAutoSaveEnabled] = useState(true);
  const [autoSaveInterval] = useState(30000); // 30 seconds
  const [paneSizes, setPaneSizes] = useState<PaneSize>({ 'pane-1': 100 }); // Initial sizes in percentage

  // Keep a ref to the latest buffers Map so async closures don't read stale data
  const buffersRef = useRef(buffers);
  useEffect(() => {
    buffersRef.current = buffers;
  }, [buffers]);

  // Activate a buffer (display in active pane)
  const activateBuffer = useCallback((bufferId: string) => {
    setActiveBufferId(bufferId);

    // Update buffers
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        // Deactivate previous active buffer for this pane
        if (activePaneId) {
          Array.from(newBuffers.entries()).forEach(([id, buf]) => {
            if (buf.paneId === activePaneId && id !== bufferId) {
              newBuffers.set(id, { ...buf, isActive: false, paneId: undefined });
            }
          });
        }
        // Activate new buffer
        newBuffers.set(bufferId, { ...buffer, isActive: true, paneId: activePaneId });
      }
      return newBuffers;
    });

    // Update pane
    setPanes(prev => prev.map(pane =>
      pane.id === activePaneId
        ? { ...pane, bufferId }
        : pane
    ));
  }, [activePaneId]);

  // Open a file in an editor pane
  const openFile = useCallback((file: any) => {
    const filePath = file.path;

    // Check if file is already open in a buffer
    const currentBuffers = buffersRef.current;
    const existingBuffer = Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === filePath);
    if (existingBuffer) {
      const [bufferId, buffer] = existingBuffer;
      // If buffer is already in a pane, switch to that pane instead of moving it
      if (buffer.paneId) {
        const pane = panes.find(p => p.id === buffer.paneId);
        if (pane) {
          setActivePaneId(buffer.paneId);
          setActiveBufferId(bufferId);
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
      id: bufferId,
      kind: 'file',
      file: file,
      content: '',
      originalContent: '',
      cursorPosition: { line: 0, column: 0 },
      scrollPosition: { top: 0, left: 0 },
      isModified: false,
      isActive: true,
      paneId: activePaneId
    };

    setBuffers(prev => {
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, newBuffer);
      return newBuffers;
    });

    // Assign to active pane
    setPanes(prev => prev.map(pane =>
      pane.id === activePaneId
        ? { ...pane, bufferId }
        : pane
    ));

    setActiveBufferId(bufferId);

    return bufferId;
  }, [activePaneId, activateBuffer, panes]);

  // Helper to find the rightmost pane for chat placement
  const getRightmostPane = useCallback((paneList: EditorPane[]) => {
    if (paneList.length === 0) return null;
    // Position order: primary=0, secondary=1, tertiary=2
    const positionOrder: Record<string, number> = { 'primary': 0, 'secondary': 1, 'tertiary': 2 };
    return paneList.reduce((rightmost, pane) => {
      const rightmostOrder = positionOrder[rightmost.position as string] ?? 0;
      const paneOrder = positionOrder[pane.position as string] ?? 0;
      return paneOrder > rightmostOrder ? pane : rightmost;
    }, paneList[0]);
  }, []);

  const openWorkspaceBuffer = useCallback((options: {
    kind: 'chat' | 'diff' | 'review' | 'file';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, any>;
  }) => {
    const currentBuffers = buffersRef.current;
    const existingBufferEntry = Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === options.path);

    if (existingBufferEntry) {
      const [bufferId, buffer] = existingBufferEntry;
      setBuffers(prev => {
        const next = new Map(prev);
        next.set(bufferId, {
          ...buffer,
          kind: options.kind,
          file: {
            ...buffer.file,
            name: options.title,
            path: options.path,
            ext: options.ext || buffer.file.ext,
          },
          content: options.content ?? buffer.content,
          originalContent: options.content ?? buffer.originalContent,
          isPinned: options.isPinned ?? buffer.isPinned,
          isClosable: options.isClosable ?? buffer.isClosable,
          metadata: options.metadata ?? buffer.metadata,
        });
        return next;
      });
      activateBuffer(bufferId);
      return bufferId;
    }

    // For chat buffers, place them in the rightmost pane for better UX with context panel
    const targetPane = options.kind === 'chat' ? getRightmostPane(panes) : panes.find(p => p.id === activePaneId);
    const targetPaneId = targetPane?.id ?? activePaneId;

    const bufferId = `buffer-${options.kind}-${Date.now()}`;
    const newBuffer: EditorBuffer = {
      id: bufferId,
      kind: options.kind,
      file: {
        name: options.title,
        path: options.path,
        isDir: false,
        size: 0,
        modified: 0,
        ext: options.ext,
      },
      content: options.content ?? '',
      originalContent: options.content ?? '',
      cursorPosition: { line: 0, column: 0 },
      scrollPosition: { top: 0, left: 0 },
      isModified: false,
      isActive: true,
      paneId: targetPaneId,
      isPinned: options.isPinned ?? false,
      isClosable: options.isClosable ?? !options.isPinned,
      metadata: options.metadata ?? {},
    };

    setBuffers(prev => {
      const next = new Map(prev);
      next.set(bufferId, newBuffer);
      return next;
    });

    // Assign buffer to target pane and activate that pane
    setPanes(prev => prev.map(pane =>
      pane.id === targetPaneId
        ? { ...pane, bufferId }
        : pane
    ));

    // Switch to the target pane and activate the buffer
    setActivePaneId(targetPaneId);
    setActiveBufferId(bufferId);

    return bufferId;
  }, [activePaneId, activateBuffer, getRightmostPane, panes]);

  // Update buffer content
  const updateBufferContent = useCallback((bufferId: string, content: string) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, content, isModified: content !== buffer.originalContent });
      }
      return newBuffers;
    });
  }, []);

  // Update buffer cursor position
  const updateBufferCursor = useCallback((bufferId: string, position: { line: number; column: number }) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, cursorPosition: position });
      }
      return newBuffers;
    });
  }, []);

  // Update buffer scroll position
  const updateBufferScroll = useCallback((bufferId: string, position: { top: number; left: number }) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, scrollPosition: position });
      }
      return newBuffers;
    });
  }, []);

  // Set buffer modified state
  const setBufferModified = useCallback((bufferId: string, isModified: boolean) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, isModified });
      }
      return newBuffers;
    });
  }, []);

  // Save a buffer to the server
  const saveBuffer = useCallback(async (bufferId: string) => {
    const buffer = buffersRef.current.get(bufferId);
    if (!buffer || buffer.kind !== 'file') return;

    // Prevent saving virtual workspace buffers (untitled, previews, etc.)
    if (buffer.file.path.startsWith('__workspace/')) {
      setBufferModified(bufferId, false);
      return;
    }

    try {
      const response = await writeFileWithConsent(buffer.file.path, buffer.content);

      if (response.ok) {
        const data = await response.json();
        // Check for validation errors (hotkeys config)
        if (data.success === false) {
          console.error('Save validation failed:', data);
          throw new Error(data.error || 'Save validation failed');
        }
        // Check for success message
        if (data.message === 'File saved successfully' || data.success === true) {
          setBuffers(prev => {
            const newBuffers = new Map(prev);
            const buf = newBuffers.get(bufferId);
            if (buf) {
              newBuffers.set(bufferId, { ...buf, originalContent: buf.content, isModified: false });
            }
            return newBuffers;
          });
        }
      }
    } catch (error) {
      console.error('Failed to save buffer:', bufferId, error);
      throw error;
    }
  }, [setBufferModified]);

  // Save all modified buffers
  const saveAllBuffers = useCallback(async () => {
    const currentBuffers = buffersRef.current;
    const savePromises = Array.from(currentBuffers.entries())
      .filter(([_, buffer]) => buffer.isModified)
      .map(([bufferId, _]) => saveBuffer(bufferId));

    await Promise.all(savePromises);
  }, [saveBuffer]);

  // Close a buffer (triggers auto-save if modified)
  const closeBuffer = useCallback((bufferId: string) => {
    const buffer = buffersRef.current.get(bufferId);
    if (!buffer) return;
    if (buffer.isClosable === false) return;

    // Save before closing if modified and auto-save is enabled (fire-and-forget)
    if (buffer.isModified && isAutoSaveEnabled) {
      saveBuffer(bufferId).catch(err => {
        console.error('Failed to save buffer before closing:', bufferId, err);
      });
    }

    const remainingBufferEntries = Array.from(buffersRef.current.values())
      .filter((candidate) => candidate.id !== bufferId);
    const nextPaneBuffer = buffer.paneId
      ? remainingBufferEntries.find((candidate) => candidate.paneId === buffer.paneId)
        || remainingBufferEntries.find((candidate) => !candidate.paneId)
        || null
      : null;

    setBuffers(prev => {
      const newBuffers = new Map(prev);
      newBuffers.delete(bufferId);
      if (buffer.paneId && nextPaneBuffer) {
        const replacement = newBuffers.get(nextPaneBuffer.id);
        if (replacement) {
          newBuffers.set(nextPaneBuffer.id, {
            ...replacement,
            isActive: activePaneId === buffer.paneId,
            paneId: buffer.paneId,
          });
        }
      }
      return newBuffers;
    });

    if (buffer.paneId) {
      setPanes(prev => prev.map(pane =>
        pane.id === buffer.paneId
          ? { ...pane, bufferId: nextPaneBuffer?.id || null }
          : pane
      ));
    }

    if (bufferId === activeBufferId) {
      if (nextPaneBuffer) {
        setActiveBufferId(nextPaneBuffer.id);
      } else {
        setActiveBufferId(null);
      }
    }
  }, [activeBufferId, activePaneId, isAutoSaveEnabled, saveBuffer]);

  const reorderBuffers = useCallback((sourceBufferId: string, targetBufferId: string) => {
    if (!sourceBufferId || !targetBufferId || sourceBufferId === targetBufferId) {
      return;
    }

    setBuffers((prev) => {
      const entries = Array.from(prev.entries());
      const sourceIndex = entries.findIndex(([id]) => id === sourceBufferId);
      const targetIndex = entries.findIndex(([id]) => id === targetBufferId);

      if (sourceIndex === -1 || targetIndex === -1) {
        return prev;
      }

      const [moved] = entries.splice(sourceIndex, 1);
      const nextTargetIndex = entries.findIndex(([id]) => id === targetBufferId);
      entries.splice(nextTargetIndex, 0, moved);
      return new Map(entries);
    });
  }, []);

  const moveBufferToPane = useCallback((bufferId: string, paneId: string) => {
    const buffer = buffersRef.current.get(bufferId);
    if (!buffer || buffer.paneId === paneId) {
      return;
    }

    setBuffers((prev) => {
      const next = new Map(prev);
      const moved = next.get(bufferId);
      if (!moved) {
        return prev;
      }
      next.set(bufferId, {
        ...moved,
        paneId,
        isActive: activePaneId === paneId,
      });
      return next;
    });

    setPanes((prev) => prev.map((pane) => {
      if (pane.id === paneId) {
        return { ...pane, bufferId };
      }
      if (pane.bufferId === bufferId) {
        return { ...pane, bufferId: null };
      }
      return pane;
    }));

    if (activePaneId === paneId) {
      setActiveBufferId(bufferId);
    }
  }, [activePaneId]);

  // Close a pane
  const closePane = useCallback((paneId: string) => {
    if (panes.length === 1) return; // Can't close last pane

    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) {
      closeBuffer(pane.bufferId);
    }

    setPanes(prev => {
      const newPanes = prev.filter(p => p.id !== paneId);
      return newPanes;
    });

    // If we closed the active pane, activate another
    if (paneId === activePaneId) {
      const remainingPanes = panes.filter(p => p.id !== paneId);
      setActivePaneId(remainingPanes[0]?.id || null);
    }

    // (Going from 2 → 1, not 3 → 2 — a 2-pane split is still valid)
    if (panes.length === 2) {
      setPaneLayoutState('single');
    }
  }, [panes, activePaneId, closeBuffer]);

  // Switch to a different buffer in the active pane
  const switchToBuffer = useCallback((bufferId: string) => {
    const existingBuffer = buffersRef.current.get(bufferId);
    if (!existingBuffer) {
      return;
    }

    if (existingBuffer.paneId && existingBuffer.paneId !== activePaneId) {
      setActivePaneId(existingBuffer.paneId);
      setActiveBufferId(bufferId);
      return;
    }

    setActiveBufferId(bufferId);
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      // Deactivate all buffers in this pane, activate the target
      Array.from(newBuffers.entries()).forEach(([id, buf]) => {
        if (buf.paneId === activePaneId) {
          newBuffers.set(id, { ...buf, isActive: id === bufferId });
        }
      });
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, isActive: true, paneId: activePaneId });
      }
      return newBuffers;
    });
    setPanes(prev => prev.map(pane =>
      pane.id === activePaneId ? { ...pane, bufferId } : pane
    ));
  }, [activePaneId]);

  // Switch to a different pane
  const switchPane = useCallback((paneId: string) => {
    setActivePaneId(paneId);
    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) {
      setActiveBufferId(pane.bufferId);
    }
  }, [panes]);

  // Split a pane
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const splitPane = useCallback((paneId: string, direction: 'vertical' | 'horizontal') => {
    if (panes.length >= 3) return null; // Max 3 panes

    const newPaneId = `pane-${Date.now()}`;

    const newPanes: EditorPane[] = [
      ...panes,
      {
        id: newPaneId,
        bufferId: null,
        isActive: false,
        position: panes.length === 1 ? 'secondary' : 'tertiary'
      }
    ];

    setPanes(newPanes);

    // Update layout
    if (panes.length === 1) {
      setPaneLayoutState(direction === 'vertical' ? 'split-vertical' : 'split-horizontal');
      // Initialize pane sizes (50/50 split)
      setPaneSizes({
        [panes[0].id]: 50,
        [newPaneId]: 50
      });
    } else {
      // Preserve the original root split direction and let the caller
      // decide how the nested split should be rendered.
      setPaneSizes((prev) => ({
        ...prev,
        [newPaneId]: 50
      }));
    }

    // Activate new pane
    setActivePaneId(newPaneId);
    return newPaneId;
  }, [panes]);

  // Close split (reset to single pane)
  const closeSplit = useCallback(() => {
    const activePane = panes.find(p => p.id === activePaneId);

    // Close all panes except the primary one
    panes.forEach(pane => {
      if (pane.position !== 'primary' && pane.id !== activePaneId) {
        closePane(pane.id);
      }
    });

    setPanes(prev => {
      const primaryPane = prev.find(p => p.position === 'primary');
      return primaryPane ? [primaryPane] : prev;
    });

    setPaneLayoutState('single');
    setActivePaneId(panes[0]?.id || null);

    // Reset pane sizes
    setPaneSizes({ [panes[0]?.id || 'pane-1']: 100 });

    const remainingBuffer = activePane?.bufferId;
    if (remainingBuffer) {
      setActiveBufferId(remainingBuffer);
    }
  }, [panes, activePaneId, closePane]);

  // Set pane layout
  const setPaneLayout = useCallback((layout: PaneLayout) => {
    setPaneLayoutState(layout);

    // Adjust panes based on layout
    if (layout === 'single') {
      setPanes(prev => {
        const primary = prev.find(p => p.position === 'primary');
        return primary ? [primary] : prev;
      });
      activePaneId && setActivePaneId(activePaneId);
    }
  }, [activePaneId]);

  // Auto-save interval - saves all modified buffers every 30 seconds
  useEffect(() => {
    if (!isAutoSaveEnabled) return;

    const intervalId = setInterval(async () => {
      await saveAllBuffers();
    }, autoSaveInterval);

    return () => clearInterval(intervalId);
  }, [isAutoSaveEnabled, autoSaveInterval, saveAllBuffers]);

  // Update pane size (for resizable split panes)
  const updatePaneSize = useCallback((paneId: string, size: number) => {
    setPaneSizes(prev => ({
      ...prev,
      [paneId]: size
    }));
  }, []);

  const value: EditorManagerContextValue = {
    buffers,
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    isAutoSaveEnabled,
    autoSaveInterval,
    paneSizes,
    openFile,
    openWorkspaceBuffer,
    closeBuffer,
    reorderBuffers,
    moveBufferToPane,
    closePane,
    switchPane,
    switchToBuffer,
    splitPane,
    closeSplit,
    setPaneLayout,
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    saveBuffer,
    setBufferModified,
    saveAllBuffers,
    updatePaneSize
  };

  return (
    <EditorManagerContext.Provider value={value}>
      {children}
    </EditorManagerContext.Provider>
  );
};
