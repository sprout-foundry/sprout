import React, { createContext, useContext, useState, useCallback, useEffect, ReactNode } from 'react';
import { EditorBuffer, EditorPane, PaneLayout } from '../types/editor';

interface EditorManagerContextValue {
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
  activeBufferId: string | null;
  
  // Actions
  openFile: (file: any) => string; // Returns buffer ID
  closeBuffer: (bufferId: string) => void;
  closePane: (paneId: string) => void;
  switchPane: (paneId: string) => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => void;
  closeSplit: () => void;
  setPaneLayout: (layout: PaneLayout) => void;
  updateBufferContent: (bufferId: string, content: string) => void;
  updateBufferCursor: (bufferId: string, position: { line: number; column: number }) => void;
  updateBufferScroll: (bufferId: string, position: { top: number; left: number }) => void;
  saveBuffer: (bufferId: string) => Promise<void>;
  setBufferModified: (bufferId: string, isModified: boolean) => void;
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
  const [buffers, setBuffers] = useState<Map<string, EditorBuffer>>(new Map());
  const [panes, setPanes] = useState<EditorPane[]>([
    { id: 'pane-1', bufferId: null, isActive: true, position: 'primary' }
  ]);
  const [paneLayout, setPaneLayoutState] = useState<PaneLayout>('single');
  const [activePaneId, setActivePaneId] = useState<string | null>('pane-1');
  const [activeBufferId, setActiveBufferId] = useState<string | null>(null);

  // Open a file in an editor pane
  const openFile = useCallback((file: any) => {
    const filePath = file.path;

    // Check if file is already open in a buffer
    const existingBuffer = Array.from(buffers.entries()).find(([_, buffer]) => buffer.file.path === filePath);
    if (existingBuffer) {
      const [bufferId] = existingBuffer;
      activateBuffer(bufferId);
      return bufferId;
    }

    // Create new buffer
    const bufferId = `buffer-${Date.now()}`;
    const newBuffer: EditorBuffer = {
      id: bufferId,
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
  }, [buffers, activePaneId]);

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

  // Close a buffer
  const closeBuffer = useCallback((bufferId: string) => {
    const buffer = buffers.get(bufferId);
    if (!buffer) return;

    // If buffer is modified, we'd typically show a dialog here
    // For now, we'll just warn in console
    if (buffer.isModified) {
      console.warn(`Closing modified buffer: ${buffer.file.name}`);
    }

    setBuffers(prev => {
      const newBuffers = new Map(prev);
      newBuffers.delete(bufferId);
      return newBuffers;
    });

    // If this was the active buffer, clear the pane
    if (bufferId === activeBufferId) {
      activePaneId && setPanes(prev => prev.map(pane => 
        pane.id === activePaneId 
          ? { ...pane, bufferId: null }
          : pane
      ));
      setActiveBufferId(null);
    }
  }, [buffers, activeBufferId, activePaneId]);

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

    // Reset to single if we only have 1 pane left
    if (panes.length === 2) {
      setPaneLayoutState('single');
    }
  }, [panes, activePaneId, closeBuffer]);

  // Switch to a different pane
  const switchPane = useCallback((paneId: string) => {
    setActivePaneId(paneId);
    const pane = panes.find(p => p.id === paneId);
    if (pane?.bufferId) {
      setActiveBufferId(pane.bufferId);
    }
  }, [panes]);

  // Split a pane
  const splitPane = useCallback((paneId: string, direction: 'vertical' | 'horizontal') => {
    if (panes.length >= 3) return; // Max 3 panes

    const newPaneId = `pane-${Date.now()}`;
    const pane = panes.find(p => p.id === paneId);
    
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
    } else {
      setPaneLayoutState('split-grid');
    }

    // Activate new pane
    setActivePaneId(newPaneId);
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

  // Save a buffer
  const saveBuffer = useCallback(async (bufferId: string) => {
    const buffer = buffers.get(bufferId);
    if (!buffer) return;

    try {
      const response = await fetch(`/api/file?path=${encodeURIComponent(buffer.file.path)}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ content: buffer.content }),
      });

      if (response.ok) {
        const data = await response.json();
        if (data.message === 'File saved successfully') {
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
  }, [buffers]);

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

  const value: EditorManagerContextValue = {
    buffers,
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    openFile,
    closeBuffer,
    closePane,
    switchPane,
    splitPane,
    closeSplit,
    setPaneLayout,
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    saveBuffer,
    setBufferModified
  };

  return (
    <EditorManagerContext.Provider value={value}>
      {children}
    </EditorManagerContext.Provider>
  );
};
