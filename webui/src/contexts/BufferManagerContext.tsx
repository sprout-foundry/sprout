import React, { createContext, useContext, useState, useCallback, useEffect, useRef, type ReactNode } from 'react';
import type { EditorBuffer, EditorPane, EditorFileEntry } from '../types/editor';
import { showThemedPrompt } from '@sprout/ui';
import { formatCodeWithConfigDiscovery, isFormattable } from '../services/formatter';
import { debugLog } from '../utils/log';
import { useSproutFetch } from './SproutAdapterContext';
import { writeFileWithFetch } from './fileWriteHelpers';

// ---------------------------------------------------------------------------
// Pane Bridge Interface for cross-context communication
// ---------------------------------------------------------------------------

export interface PaneBridge {
  activePaneId: string | null;
  activeBufferId: string | null;
  panes: EditorPane[];
  setActiveBufferId: (id: string | null) => void;
  setActivePaneId: (id: string | null) => void;
  setPanes: React.Dispatch<React.SetStateAction<EditorPane[]>>;
  switchPane: (paneId: string) => void;
  closeBuffer: (bufferId: string) => void | Promise<void>;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
}

// ---------------------------------------------------------------------------
// Buffer Manager Context Interface
// ---------------------------------------------------------------------------

interface BufferManagerContextValue {
  buffers: Map<string, EditorBuffer>;
  openFile: (file: EditorFileEntry) => string;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
  openCompareBuffer: (options: {
    originalContent: string;
    modifiedContent: string;
    fileName: string;
    aLabel?: string;
    bLabel?: string;
    title?: string;
  }) => string;
  closeBuffer: (bufferId: string) => void | Promise<void>;
  reorderBuffers: (sourceBufferId: string, targetBufferId: string) => void;
  moveBufferToPane: (bufferId: string, paneId: string) => void;
  switchToBuffer: (bufferId: string) => void;
  updateBufferContent: (bufferId: string, content: string) => void;
  updateBufferCursor: (bufferId: string, position: { line: number; column: number }) => void;
  updateBufferScroll: (bufferId: string, position: { top: number; left: number }) => void;
  updateBufferMetadata: (bufferId: string, updates: Record<string, unknown>) => void;
  updateBufferTitle: (bufferId: string, title: string) => void;
  saveBuffer: (bufferId: string) => Promise<{ mod_time?: number; formattedContent?: string } | void>;
  setBufferModified: (bufferId: string, isModified: boolean) => void;
  setBufferOriginalContent: (bufferId: string, originalContent: string) => void;
  setBufferExternallyModified: (bufferId: string, diskContent: string, mtime?: number) => void;
  clearBufferExternallyModified: (bufferId: string) => void;
  setBufferLanguageOverride: (bufferId: string, languageId: string | null) => void;
  saveAllBuffers: () => Promise<void>;
  toggleBufferPin: (bufferId: string) => void;
  setBufferPinned: (bufferId: string, isPinned: boolean) => void;
  setBufferClosable: (bufferId: string, isClosable: boolean) => void;
  reloadBufferFromDisk: (bufferId: string, diskContent: string, mtime?: number) => void;
}

const BufferManagerContext = createContext<BufferManagerContextValue | null>(null);

export const useBufferManager = () => {
  const context = useContext(BufferManagerContext);
  if (!context) {
    throw new Error('useBufferManager must be used within BufferManagerProvider');
  }
  return context;
};

interface BufferManagerProviderProps {
  children: ReactNode;
  paneBridge: PaneBridge;
  isAutoSaveEnabled: boolean;
  isFormatOnSaveEnabled: boolean;
  autoSaveInterval: number;
}

export const BufferManagerProvider: React.FC<BufferManagerProviderProps> = ({
  children,
  paneBridge,
  isAutoSaveEnabled,
  isFormatOnSaveEnabled,
  autoSaveInterval,
}) => {
  const sproutFetch = useSproutFetch();

  // Initialize buffers with chat buffer
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

  // Keep a ref to the latest buffers Map so async closures don't read stale data
  const buffersRef = useRef(buffers);
  useEffect(() => {
    buffersRef.current = buffers;
  }, [buffers]);

  // Keep a ref to the latest activePaneId so callbacks don't read stale closure values
  const activePaneIdRef = useRef(paneBridge.activePaneId);
  useEffect(() => {
    activePaneIdRef.current = paneBridge.activePaneId;
  }, [paneBridge.activePaneId]);

  // Helper to find the rightmost pane for chat placement
  const getRightmostPane = useCallback((paneList: EditorPane[]) => {
    if (paneList.length === 0) return null;
    const positionOrder: Record<string, number> = {
      'primary': 0,
      'secondary': 1,
      'tertiary': 2,
      'quaternary': 3,
      'quinary': 4,
      'senary': 5
    };
    return paneList.reduce((rightmost, pane) => {
      const rightmostOrder = positionOrder[rightmost.position as string] ?? 0;
      const paneOrder = positionOrder[pane.position as string] ?? 0;
      return paneOrder > rightmostOrder ? pane : rightmost;
    }, paneList[0]);
  }, []);

  // Activate a buffer (display in active pane)
  const activateBuffer = useCallback((bufferId: string) => {
    const currentActivePane = activePaneIdRef.current;
    paneBridge.setActiveBufferId(bufferId);

    setBuffers(prev => {
      const newBuffers = new Map(prev);
      const buffer = newBuffers.get(bufferId);
      if (buffer) {
        if (currentActivePane) {
          Array.from(newBuffers.entries()).forEach(([id, buf]) => {
            if (buf.paneId === currentActivePane && id !== bufferId) {
              newBuffers.set(id, { ...buf, isActive: false });
            }
          });
        }
        newBuffers.set(bufferId, { ...buffer, isActive: true, paneId: currentActivePane });
      }
      return newBuffers;
    });

    paneBridge.setPanes(prev => prev.map(pane =>
      pane.id === currentActivePane
        ? { ...pane, bufferId }
        : pane
    ));
  }, [paneBridge]);

  // Switch to a different buffer in the active pane
  const switchToBuffer = useCallback((bufferId: string) => {
    const existingBuffer = buffersRef.current.get(bufferId);
    if (!existingBuffer) {
      return;
    }

    const currentPaneId = activePaneIdRef.current;

    if (existingBuffer.paneId && existingBuffer.paneId !== currentPaneId) {
      paneBridge.setActivePaneId(existingBuffer.paneId);
      paneBridge.setActiveBufferId(bufferId);
      setBuffers(prev => {
        const next = new Map(prev);
        Array.from(next.entries()).forEach(([id, buf]) => {
          if (buf.paneId === existingBuffer.paneId) {
            next.set(id, { ...buf, isActive: id === bufferId });
          }
        });
        return next;
      });
      paneBridge.setPanes(prev => prev.map(pane =>
        pane.id === existingBuffer.paneId ? { ...pane, bufferId } : pane
      ));
      return;
    }

    paneBridge.setActiveBufferId(bufferId);
    setBuffers(prev => {
      const newBuffers = new Map(prev);
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
    paneBridge.setPanes(prev => prev.map(pane =>
      pane.id === currentPaneId ? { ...pane, bufferId } : pane
    ));
  }, [paneBridge]);

  // Open a file in an editor pane
  const openFile = useCallback((file: EditorFileEntry) => {
    const filePath = file.path;

    const currentBuffers = buffersRef.current;
    const currentActivePane = activePaneIdRef.current;
    const existingBuffer = Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === filePath);
    if (existingBuffer) {
      const [bufferId, buffer] = existingBuffer;
      if (buffer.paneId) {
        const pane = paneBridge.panes.find(p => p.id === buffer.paneId);
        if (pane) {
          switchToBuffer(bufferId);
          return bufferId;
        }
      }
      activateBuffer(bufferId);
      return bufferId;
    }

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
      newBuffers.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === currentActivePane) {
          newBuffers.set(key, { ...existing, isActive: false });
        }
      });
      newBuffers.set(bufferId, newBuffer);
      return newBuffers;
    });

    paneBridge.setPanes(prev => prev.map(pane =>
      pane.id === currentActivePane
        ? { ...pane, bufferId }
        : pane
    ));

    paneBridge.setActiveBufferId(bufferId);

    return bufferId;
  }, [activateBuffer, switchToBuffer]);

  // Open workspace buffer
  const openWorkspaceBuffer = useCallback((options: {
    kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
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
      // Navigate to the buffer's existing pane if it's in a different pane
      if (buffer.paneId && buffer.paneId !== paneBridge.activePaneId) {
        paneBridge.setActivePaneId(buffer.paneId);
        switchToBuffer(bufferId);
      } else {
        activateBuffer(bufferId);
      }
      return bufferId;
    }

    const targetPane = options.kind === 'chat' ? getRightmostPane(paneBridge.panes) : paneBridge.panes.find(p => p.id === paneBridge.activePaneId);
    const targetPaneId = targetPane?.id ?? paneBridge.activePaneId;

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
      next.forEach((existing, key) => {
        if (key !== bufferId && existing.paneId === targetPaneId) {
          next.set(key, { ...existing, isActive: false });
        }
      });
      next.set(bufferId, newBuffer);
      return next;
    });

    paneBridge.setPanes(prev => prev.map(pane =>
      pane.id === targetPaneId
        ? { ...pane, bufferId }
        : pane
    ));

    paneBridge.setActivePaneId(targetPaneId);
    paneBridge.setActiveBufferId(bufferId);

    return bufferId;
  }, [activateBuffer, getRightmostPane, paneBridge.panes, paneBridge.activePaneId, paneBridge]);

  // Open compare buffer
  const openCompareBuffer = useCallback((options: {
    originalContent: string;
    modifiedContent: string;
    fileName: string;
    aLabel?: string;
    bLabel?: string;
    title?: string;
  }) => {
    const bufferTitle = options.title || `Compare: ${options.fileName}`;
    const bufferPath = `__workspace/compare/${options.fileName}-${Date.now()}`;

    return openWorkspaceBuffer({
      kind: 'compare',
      path: bufferPath,
      title: bufferTitle,
      metadata: {
        originalContent: options.originalContent,
        modifiedContent: options.modifiedContent,
        fileName: options.fileName,
        aLabel: options.aLabel,
        bLabel: options.bLabel,
        title: options.title,
      },
    });
  }, [openWorkspaceBuffer]);

  // Update buffer operations
  const updateBufferMetadata = useCallback((bufferId: string, updates: Record<string, unknown>) => {
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
      const buffer = prev.get(bufferId);
      if (!buffer || buffer.content === content) return prev;
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, { ...buffer, content, isModified: content !== buffer.originalContent });
      return newBuffers;
    });
  }, []);

  const updateBufferCursor = useCallback((bufferId: string, position: { line: number; column: number }) => {
    setBuffers(prev => {
      const buffer = prev.get(bufferId);
      if (!buffer) return prev;
      if (buffer.cursorPosition?.line === position.line && buffer.cursorPosition?.column === position.column) {
        return prev;
      }
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, { ...buffer, cursorPosition: position });
      return newBuffers;
    });
  }, []);

  const updateBufferScroll = useCallback((bufferId: string, position: { top: number; left: number }) => {
    setBuffers(prev => {
      const buffer = prev.get(bufferId);
      if (!buffer) return prev;
      if (buffer.scrollPosition?.top === position.top && buffer.scrollPosition?.left === position.left) {
        return prev;
      }
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, { ...buffer, scrollPosition: position });
      return newBuffers;
    });
  }, []);

  // Buffer property setters
  const setBufferModified = useCallback((bufferId: string, isModified: boolean) => {
    setBuffers(prev => {
      const buffer = prev.get(bufferId);
      if (!buffer || buffer.isModified === isModified) return prev;
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, { ...buffer, isModified });
      return newBuffers;
    });
  }, []);

  const setBufferOriginalContent = useCallback((bufferId: string, originalContent: string) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          originalContent,
          isModified: buffer.content !== originalContent ? buffer.isModified : false,
        });
      }
      return next;
    });
  }, []);

  const setBufferExternallyModified = useCallback((bufferId: string, diskContent: string, mtime?: number) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          externallyModified: true,
          diskContent,
          file: { ...buffer.file, modified: mtime ?? Math.floor(Date.now() / 1000) },
        });
      }
      return next;
    });
  }, []);

  const clearBufferExternallyModified = useCallback((bufferId: string) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          externallyModified: false,
          diskContent: null,
        });
      }
      return next;
    });
  }, []);

  const setBufferLanguageOverride = useCallback((bufferId: string, languageId: string | null) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, languageOverride: languageId });
      }
      return next;
    });
  }, []);

  const toggleBufferPin = useCallback((bufferId: string) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, isPinned: !buffer.isPinned });
      }
      return next;
    });
  }, []);

  const setBufferPinned = useCallback((bufferId: string, isPinned: boolean) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, isPinned });
      }
      return next;
    });
  }, []);

  const setBufferClosable = useCallback((bufferId: string, isClosable: boolean) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, isClosable });
      }
      return next;
    });
  }, []);

  const reloadBufferFromDisk = useCallback((bufferId: string, diskContent: string, mtime?: number) => {
    setBuffers(prev => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          content: diskContent,
          originalContent: diskContent,
          isModified: false,
          externallyModified: false,
          diskContent: null,
          file: { ...buffer.file, modified: mtime ?? Math.floor(Date.now() / 1000) },
        });
      }
      return next;
    });
  }, []);

  // Save buffer
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
        return;
      }

      const trimmedPath = filePath.trim();

      try {
        const response = await writeFileWithFetch(sproutFetch, trimmedPath, buffer.content);
        if (!response.ok) {
          const errorText = await response.text().catch(() => response.statusText);
          throw new Error(errorText || `Failed to save file: ${response.statusText}`);
        }

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
    let contentToSave = buffer.content;
    let formattedContent: string | undefined;
    if (isFormatOnSaveEnabled && isFormattable(buffer.file.path)) {
      try {
        const formatPromise = formatCodeWithConfigDiscovery(
          buffer.content,
          buffer.file.path,
          buffer.file.size,
        );
        const formatTimeout = new Promise<{ formatted: string; error?: string }>((resolve) =>
          setTimeout(() => resolve({ formatted: buffer.content, error: 'Format timed out' }), 2000),
        );
        const result = await Promise.race([formatPromise, formatTimeout]);
        if (!result.error && result.formatted !== buffer.content) {
          contentToSave = result.formatted;
          formattedContent = result.formatted;
        } else if (result.error) {
          debugLog(`[saveBuffer] Format-on-save skipped for ${buffer.file.path}: ${result.error}`);
        }
      } catch {
        debugLog(`[saveBuffer] Format-on-save failed for ${buffer.file.path}, saving unformatted`);
      }
    }

    try {
      const response = await writeFileWithFetch(sproutFetch, buffer.file.path, contentToSave);

      if (response.ok) {
        const data = await response.json();
        if (data.success === false) {
          console.error('Save validation failed:', data);
          throw new Error(data.error || 'Save validation failed');
        }
        if (data.message === 'File saved successfully' || data.success === true) {
          setBuffers(prev => {
            const newBuffers = new Map(prev);
            const buf = newBuffers.get(bufferId);
            if (buf) {
              newBuffers.set(bufferId, { ...buf, originalContent: formattedContent ?? buf.content, isModified: false });
            }
            return newBuffers;
          });
          return { mod_time: typeof data.mod_time === 'number' ? data.mod_time : undefined, formattedContent };
        }
      }
    } catch (error) {
      console.error('Failed to save buffer:', bufferId, error);
      throw error;
    }
  }, [isFormatOnSaveEnabled, sproutFetch]);

  // Save all modified buffers
  const saveAllBuffers = useCallback(async () => {
    const currentBuffers = buffersRef.current;
    const savePromises = Array.from(currentBuffers.entries())
      .filter(([_, buffer]) => buffer.isModified && !buffer.file.path.startsWith('__workspace/'))
      .map(([bufferId, _]) => saveBuffer(bufferId));

    await Promise.all(savePromises);
  }, [saveBuffer]);

  // Close a buffer
  const closeBuffer = useCallback(async (bufferId: string) => {
    const buffer = buffersRef.current.get(bufferId);
    if (!buffer) return;
    if (buffer.isClosable === false) return;

    if (buffer.isModified && isAutoSaveEnabled) {
      try {
        await saveBuffer(bufferId);
      } catch (err) {
        console.error('Failed to save buffer before closing:', bufferId, err);
        return; // Block close on save failure
      }
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
      paneBridge.setPanes(prev => prev.map(pane =>
        pane.id === buffer.paneId
          ? { ...pane, bufferId: nextPaneBuffer?.id || null }
          : pane
      ));
    }

    if (bufferId === paneBridge.activeBufferId) {
      if (nextPaneBuffer) {
        paneBridge.setActiveBufferId(nextPaneBuffer.id);
      } else {
        paneBridge.setActiveBufferId(null);
      }
    }
  }, [isAutoSaveEnabled, saveBuffer, paneBridge]);

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
        isActive: paneBridge.activePaneId === paneId,
      });
      return next;
    });

    paneBridge.moveBufferToPane(bufferId, paneId);

    if (paneBridge.activePaneId === paneId) {
      paneBridge.setActiveBufferId(bufferId);
    }
  }, [paneBridge]);

  // Auto-save interval
  const saveAllBuffersRef = useRef(saveAllBuffers);
  saveAllBuffersRef.current = saveAllBuffers;

  useEffect(() => {
    if (!isAutoSaveEnabled) return;

    const intervalId = setInterval(async () => {
      await saveAllBuffersRef.current();
    }, autoSaveInterval);

    return () => clearInterval(intervalId);
  }, [isAutoSaveEnabled, autoSaveInterval]);

  const value: BufferManagerContextValue = {
    buffers,
    openFile,
    openWorkspaceBuffer,
    openCompareBuffer,
    closeBuffer,
    reorderBuffers,
    moveBufferToPane,
    switchToBuffer,
    updateBufferContent,
    updateBufferCursor,
    updateBufferScroll,
    updateBufferMetadata,
    updateBufferTitle,
    saveBuffer,
    setBufferModified,
    setBufferOriginalContent,
    setBufferExternallyModified,
    clearBufferExternallyModified,
    setBufferLanguageOverride,
    saveAllBuffers,
    toggleBufferPin,
    setBufferPinned,
    setBufferClosable,
    reloadBufferFromDisk,
  };

  return (
    <BufferManagerContext.Provider value={value}>
      {children}
    </BufferManagerContext.Provider>
  );
};
