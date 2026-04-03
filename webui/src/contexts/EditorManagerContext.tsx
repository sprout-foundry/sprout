import React, { createContext, useContext, useState, useCallback, useEffect, useRef, ReactNode } from 'react';
import { EditorBuffer, EditorPane, PaneLayout } from '../types/editor';
import { writeFileWithConsent } from '../services/fileAccess';
import { showThemedPrompt } from '../components/ThemedDialog';

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
  isLinkedScrollEnabled: boolean; // Whether linked scrolling is active for split panes (fix: was consumed by EditorPane but missing from context)
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
  closeAllBuffers: () => void;
  closeOtherBuffers: (bufferId: string) => void;
  reorderBuffers: (sourceBufferId: string, targetBufferId: string) => void;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
  closePane: (paneId: string) => void;
  switchPane: (paneId: string) => void;
  switchToBuffer: (bufferId: string) => void;
  splitPane: (paneId: string, direction: 'vertical' | 'horizontal') => string | null;
  splitIntoGrid: () => string[]; // Returns array of all pane IDs
  closeSplit: () => void;
  setPaneLayout: (layout: PaneLayout) => void;
  updateBufferContent: (bufferId: string, content: string) => void;
  updateBufferCursor: (bufferId: string, position: { line: number; column: number }) => void;
  updateBufferScroll: (bufferId: string, position: { top: number; left: number }) => void;
  updateBufferMetadata: (bufferId: string, updates: Record<string, any>) => void;
  updateBufferTitle: (bufferId: string, title: string) => void;
  saveBuffer: (bufferId: string) => Promise<void>;
  setBufferModified: (bufferId: string, isModified: boolean) => void;
  setBufferOriginalContent: (bufferId: string, originalContent: string) => void;
  revertBufferToOriginal: (bufferId: string) => void;
  saveAllBuffers: () => Promise<void>;
  updatePaneSize: (paneId: string, size: number) => void; // Update pane size
  setBufferLanguageOverride: (bufferId: string, languageId: string | null) => void;
  toggleLinkedScroll: () => void; // Toggle linked scrolling for split panes (fix: was consumed by EditorPane but missing from context)
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
      metadata: { chatId: null as string | null }
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
  const [isLinkedScrollEnabled, setIsLinkedScrollEnabled] = useState(false); // fix: consumed by EditorPane but was missing from context
  const [paneSizes, setPaneSizes] = useState<PaneSize>({ 'pane-1': 100 }); // Initial sizes in percentage

  // Keep a ref to the latest buffers Map so async closures don't read stale data
  const buffersRef = useRef(buffers);
  useEffect(() => {
    buffersRef.current = buffers;
  }, [buffers]);

  // Keep a ref to the latest activePaneId so callbacks don't read stale closure values
  const activePaneIdRef = useRef(activePaneId);
  useEffect(() => {
    activePaneIdRef.current = activePaneId;
  }, [activePaneId]);

  // Activate a buffer (display in active pane)
  const activateBuffer = useCallback((bufferId: string) => {
    const currentActivePane = activePaneIdRef.current;
    setActiveBufferId(bufferId);

    // Update buffers
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        // Deactivate previous active buffer for this pane, but keep paneId
        if (currentActivePane) {
          Array.from(newBuffers.entries()).forEach(([id, buf]) => {
            if (buf.paneId === currentActivePane && id !== bufferId) {
              newBuffers.set(id, { ...buf, isActive: false });
            }
          });
        }
        // Activate new buffer
        newBuffers.set(bufferId, { ...buffer, isActive: true, paneId: currentActivePane });
      }
      return newBuffers;
    });

    // Update pane
    setPanes(prev => prev.map(pane =>
      pane.id === currentActivePane
        ? { ...pane, bufferId }
        : pane
    ));
  }, []);

  // Switch to a different buffer in the active pane
  const switchToBuffer = useCallback((bufferId: string) => {
    const existingBuffer = buffersRef.current.get(bufferId);
    if (!existingBuffer) {
      return;
    }

    const currentPaneId = activePaneIdRef.current;

    if (existingBuffer.paneId && existingBuffer.paneId !== currentPaneId) {
      setActivePaneId(existingBuffer.paneId);
      setActiveBufferId(bufferId);
      setBuffers(prev => {
        const next = new Map(prev);
        Array.from(next.entries()).forEach(([id, buf]) => {
          if (buf.paneId === existingBuffer.paneId) {
            next.set(id, { ...buf, isActive: id === bufferId });
          }
        });
        return next;
      });
      setPanes(prev => prev.map(pane =>
        pane.id === existingBuffer.paneId ? { ...pane, bufferId } : pane
      ));
      return;
    }

    setActiveBufferId(bufferId);
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      // Deactivate all buffers in this pane, activate the target (keep paneId)
      Array.from(newBuffers.entries()).forEach(([id, buf]) => {
        if (buf.paneId === currentPaneId) {
          newBuffers.set(id, { ...buf, isActive: id === bufferId });
        }
      });
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, isActive: true, paneId: currentPaneId });
      }
      return newBuffers;
    });
    setPanes(prev => prev.map(pane =>
      pane.id === currentPaneId ? { ...pane, bufferId } : pane
    ));
  }, []);

  // Open a file in an editor pane
  const openFile = useCallback((file: any) => {
    const filePath = file.path;

    // Check if file is already open in a buffer
    const currentBuffers = buffersRef.current;
    const currentActivePane = activePaneIdRef.current;
    const existingBuffer = Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === filePath);
    if (existingBuffer) {
      const [bufferId, buffer] = existingBuffer;
      // If buffer is already in a pane, switch to that pane and activate properly
      if (buffer.paneId) {
        const pane = panes.find(p => p.id === buffer.paneId);
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
      id: bufferId,
      kind: 'file',
      file: file,
      content: '',
      originalContent: '',
      cursorPosition: { line: 0, column: 0 },
      scrollPosition: { top: 0, left: 0 },
      isModified: false,
      isActive: true,
      paneId: currentActivePane
    };

    setBuffers(prev => {
      const newBuffers = new Map(prev);
      // Deactivate previous buffer in the active pane, but keep paneId
      newBuffers.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === currentActivePane) {
          newBuffers.set(key, { ...existing, isActive: false });
        }
      });
      newBuffers.set(bufferId, newBuffer);
      return newBuffers;
    });

    // Assign to active pane
    setPanes(prev => prev.map(pane =>
      pane.id === currentActivePane
        ? { ...pane, bufferId }
        : pane
    ));

    setActiveBufferId(bufferId);

    return bufferId;
  }, [activateBuffer, panes, switchToBuffer]);

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
      // Deactivate previous buffer(s) in the target pane, but keep paneId
      next.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === targetPaneId) {
          next.set(key, { ...existing, isActive: false });
        }
      });
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
  const updateBufferMetadata = useCallback((bufferId: string, updates: Record<string, any>) => {
    setBuffers(prev => {
      const buf = prev.get(bufferId);
      if (!buf) return prev;
      const next = new Map(prev);
      next.set(bufferId, { ...buf, metadata: { ...buf.metadata, ...updates } });
      return next;
    });
  }, []);

  const updateBufferTitle = useCallback((bufferId: string, title: string) => {
    setBuffers(prev => {
      const buf = prev.get(bufferId);
      if (!buf) return prev;
      const next = new Map(prev);
      next.set(bufferId, { ...buf, file: { ...buf.file, name: title } });
      return next;
    });
  }, []);

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

  // Set the original content baseline for a buffer (e.g., after loading from disk).
  // This also resets isModified to false if the current content matches the new baseline.
  const setBufferOriginalContent = useCallback((bufferId: string, originalContent: string) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, {
          ...buffer,
          originalContent,
          isModified: buffer.content !== originalContent ? buffer.isModified : false,
        });
      }
      return newBuffers;
    });
  }, []);

  // Set or clear the language override for a buffer.
  // Pass null to revert to auto-detection by file extension.
  const setBufferLanguageOverride = useCallback((bufferId: string, languageId: string | null) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        newBuffers.set(bufferId, { ...buffer, languageOverride: languageId });
      }
      return newBuffers;
    });
  }, []);

  // Revert a buffer's content back to the last-saved state.
  // After calling this, the EditorPane is responsible for syncing
  // the CodeMirror editor view so the visual content matches.
  const revertBufferToOriginal = useCallback((bufferId: string) => {
    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buf = newBuffers.get(bufferId);
      if (!buf || buf.kind !== 'file') return prev;
      newBuffers.set(bufferId, {
        ...buf,
        content: buf.originalContent,
        isModified: false,
      });
      return newBuffers;
    });
  }, []);

  // Save a buffer to the server
  const saveBuffer = useCallback(async (bufferId: string) => {
    const buffer = buffersRef.current.get(bufferId);
    if (!buffer || buffer.kind !== 'file') return;

    // Handle virtual workspace buffers (untitled files created via Ctrl+N)
    if (buffer.file.path.startsWith('__workspace/')) {
      const filePath = await showThemedPrompt(
        'Enter a file path for the new file:',
        {
          title: 'Save As',
          defaultValue: 'untitled',
          placeholder: 'path/to/file.ts',
        }
      );

      if (!filePath || !filePath.trim()) {
        return; // User cancelled
      }

      const trimmedPath = filePath.trim();

      // Write the file to disk
      try {
        const response = await writeFileWithConsent(trimmedPath, buffer.content);
        if (!response.ok) {
          const errorText = await response.text().catch(() => response.statusText);
          throw new Error(errorText || `Failed to save file: ${response.statusText}`);
        }

        // Update the buffer path to the real file path
        const ext = trimmedPath.includes('.') ? trimmedPath.split('.').pop() : '';
        const name = trimmedPath.split('/').pop() || trimmedPath;

        setBuffers(prev => {
          const newBuffers = new Map(prev);
          const buf = newBuffers.get(bufferId);
          if (buf) {
            newBuffers.set(bufferId, {
              ...buf,
              file: {
                ...buf.file,
                name,
                path: trimmedPath,
                ext: ext || undefined,
              },
              originalContent: buf.content,
              isModified: false,
            });
          }
          return newBuffers;
        });
      } catch (error) {
        console.error('Failed to save new file:', error);
        throw error;
      }
      return;
    }

    // Normal save for existing files
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
      } else {
        // Server returned a non-2xx status (e.g., 400 validation error).
        // Log the error so it's visible during debugging.
        const errorBody = await response.text().catch(() => 'Unknown error');
        console.error(`Save failed (${response.status}) for ${buffer.file.path}: ${errorBody}`);
        throw new Error(`Save failed (${response.status}): ${errorBody}`);
      }
    } catch (error) {
      console.error('Failed to save buffer:', bufferId, error);
      throw error;
    }
  }, []);

  // Save all modified buffers
  const saveAllBuffers = useCallback(async () => {
    const currentBuffers = buffersRef.current;
    const savePromises = Array.from(currentBuffers.entries())
      .filter(([_, buffer]) => buffer.isModified && !buffer.file.path.startsWith('__workspace/'))
      .map(([bufferId, _]) =>
        saveBuffer(bufferId).catch(err => {
          console.error('Save failed for buffer:', bufferId, err);
        })
      );

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

    const remain = Array.from(buffersRef.current.values())
      .filter((candidate) => candidate.id !== bufferId);
    const nextPaneBuffer = buffer.paneId
      ? remain.find((candidate) => candidate.paneId === buffer.paneId)
        || remain.find((candidate) => !candidate.paneId)
        || null
      : null;

    const currentActivePane = activePaneIdRef.current;

    setBuffers(prev => {
      const newBuffers = new Map(prev);
      newBuffers.delete(bufferId);
      if (buffer.paneId && nextPaneBuffer) {
        const replacement = newBuffers.get(nextPaneBuffer.id);
        if (replacement) {
          newBuffers.set(nextPaneBuffer.id, {
            ...replacement,
            isActive: currentActivePane === buffer.paneId,
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
  }, [activeBufferId, isAutoSaveEnabled, saveBuffer]);

  // Close all closable buffers, keeping only pinned/non-closable ones
  const closeAllBuffers = useCallback(() => {
    const currentBuffers = buffersRef.current;
    const closableIds = Array.from(currentBuffers.entries())
      .filter(([_, buffer]) => buffer.isClosable !== false)
      .map(([id]) => id);

    for (const bufferId of closableIds) {
      const buffer = currentBuffers.get(bufferId);
      if (!buffer) continue;

      // Save before closing if modified and auto-save is enabled (fire-and-forget)
      if (buffer.isModified && isAutoSaveEnabled) {
        saveBuffer(bufferId).catch(err => {
          console.error('Failed to save buffer before closing:', bufferId, err);
        });
      }

      setBuffers(prev => {
        const newBuffers = new Map(prev);
        newBuffers.delete(bufferId);
        return newBuffers;
      });
    }

    // Find a remaining buffer to activate (pinned ones).
    // Use the snapshot captured at the top (currentBuffers), not
    // buffersRef.current, because setBuffers updates are asynchronous
    // and the ref won't reflect deletions until the next render.
    const remaining = Array.from(currentBuffers.values())
      .filter(b => !closableIds.includes(b.id));

    const nextBuffer = remaining[0] || null;
    setActiveBufferId(nextBuffer?.id || null);

    // Update panes — point each pane at a remaining buffer or null
    setPanes(prev => prev.map(pane => {
      const paneBuffer = closableIds.includes(pane.bufferId || '')
        ? (nextBuffer?.id || null)
        : pane.bufferId;
      return { ...pane, bufferId: paneBuffer };
    }));
  }, [isAutoSaveEnabled, saveBuffer]);

  // Close all buffers except the specified one (and pinned/non-closable ones)
  const closeOtherBuffers = useCallback((keepBufferId: string) => {
    const currentBuffers = buffersRef.current;
    const closableOtherIds = Array.from(currentBuffers.entries())
      .filter(([id, buffer]) => id !== keepBufferId && buffer.isClosable !== false)
      .map(([id]) => id);

    for (const bufferId of closableOtherIds) {
      const buffer = currentBuffers.get(bufferId);
      if (!buffer) continue;

      // Save before closing if modified and auto-save is enabled (fire-and-forget)
      if (buffer.isModified && isAutoSaveEnabled) {
        saveBuffer(bufferId).catch(err => {
          console.error('Failed to save buffer before closing:', bufferId, err);
        });
      }

      setBuffers(prev => {
        const newBuffers = new Map(prev);
        newBuffers.delete(bufferId);
        return newBuffers;
      });
    }

    // Activate the kept buffer
    const keptBuffer = currentBuffers.get(keepBufferId);
    const currentPaneId = keptBuffer?.paneId || activePaneIdRef.current;

    setActiveBufferId(keepBufferId);
    setBuffers(prev => {
      const next = new Map(prev);
      const buf = next.get(keepBufferId);
      if (buf) {
        next.set(keepBufferId, { ...buf, isActive: true, paneId: currentPaneId });
      }
      return next;
    });
    setPanes(prev => prev.map(pane =>
      pane.id === currentPaneId ? { ...pane, bufferId: keepBufferId } : pane
    ));
  }, [activeBufferId, isAutoSaveEnabled, saveBuffer]);

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
      // Deactivate previous active buffer in destination pane
      next.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === paneId) {
          next.set(key, { ...existing, isActive: false });
        }
      });
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
    if (panes.length >= 4) return null; // Max 4 panes

    const newPaneId = `pane-${Date.now()}`;

    const newPanes: EditorPane[] = [
      ...panes,
      {
        id: newPaneId,
        bufferId: null,
        isActive: false,
        position: panes.length === 1 ? 'secondary' : panes.length === 2 ? 'tertiary' : 'quaternary'
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

  // Split into a 2x2 grid (4 panes)
  const splitIntoGrid = useCallback(() => {
    const primaryPane = panes.find(p => p.position === 'primary') || panes[0];
    if (!primaryPane) return panes.map(p => p.id);

    // Detach buffers from panes that will be replaced (non-primary).
    // This prevents orphaned buffers that still reference dead pane IDs.
    const primaryPaneId = primaryPane.id;
    const displacedPaneIds = new Set(
      panes.filter(p => p.id !== primaryPaneId).map(p => p.id)
    );
    if (displacedPaneIds.size > 0) {
      setBuffers(prev => {
        const next = new Map(prev);
        let changed = false;
        next.forEach((buf, id) => {
          if (buf.paneId && displacedPaneIds.has(buf.paneId)) {
            next.set(id, { ...buf, paneId: undefined, isActive: false });
            changed = true;
          }
        });
        return changed ? next : prev;
      });
    }

    const now = Date.now();
    const newPaneIds = [
      `pane-${now}-1`, // top-right
      `pane-${now}-2`, // bottom-left
      `pane-${now}-3`, // bottom-right
    ];

    const newPanes: EditorPane[] = [
      { ...primaryPane, position: 'primary' },
      { id: newPaneIds[0], bufferId: null, isActive: false, position: 'secondary' },
      { id: newPaneIds[1], bufferId: null, isActive: false, position: 'tertiary' },
      { id: newPaneIds[2], bufferId: null, isActive: false, position: 'quaternary' },
    ];

    setPanes(newPanes);
    setPaneLayoutState('split-grid');
    setActivePaneId(primaryPane.id);

    // Initialize grid pane sizes: 50/50 for both row and column splits
    setPaneSizes({
      'grid:col': 50,
      'grid:row': 50,
    });

    return [primaryPane.id, ...newPaneIds];
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

  // Toggle linked scrolling for split panes showing the same file
  const toggleLinkedScroll = useCallback(() => {
    setIsLinkedScrollEnabled(prev => !prev);
  }, []);

  const value: EditorManagerContextValue = {
    buffers,
    panes,
    paneLayout,
    activePaneId,
    activeBufferId,
    isAutoSaveEnabled,
    autoSaveInterval,
    isLinkedScrollEnabled,
    paneSizes,
    openFile,
    openWorkspaceBuffer,
    closeBuffer,
    closeAllBuffers,
    closeOtherBuffers,
    reorderBuffers,
    moveBufferToPane,
    closePane,
    switchPane,
    switchToBuffer,
    splitPane,
    splitIntoGrid,
    closeSplit,
    setPaneLayout,
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    updateBufferMetadata,
    updateBufferTitle,
    saveBuffer,
    setBufferModified,
    setBufferOriginalContent,
    revertBufferToOriginal,
    saveAllBuffers,
    updatePaneSize,
    setBufferLanguageOverride,
    toggleLinkedScroll,
  };

  return (
    <EditorManagerContext.Provider value={value}>
      {children}
    </EditorManagerContext.Provider>
  );
};
