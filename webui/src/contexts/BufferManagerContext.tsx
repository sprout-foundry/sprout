import { showThemedPrompt } from '@sprout/ui';
import React, { createContext, useContext, useState, useCallback, useEffect, useRef, type ReactNode } from 'react';
import { useAutoReloadCleanBuffers } from '../hooks/useAutoReloadCleanBuffers';
import { useExternalFileWatcher } from '../hooks/useExternalFileWatcher';
import { useLayoutPersistence } from '../hooks/useLayoutPersistence';
import { formatCodeWithConfigDiscovery, isFormattable } from '../services/formatter';
import { resolveEditorFilePath } from '../services/lspClientService';
import { notificationBus } from '../services/notificationBus';
import type { EditorBuffer, EditorPane, EditorFileEntry, PaneLayout, PaneSize } from '../types/editor';
import { fileEntryAtPath, isWithinPath, movedBufferPath } from './bufferPathSync';
import { debugLog } from '../utils/log';
import { writeFileWithFetch } from './fileWriteHelpers';
import { useSproutFetch } from './SproutAdapterContext';

// ---------------------------------------------------------------------------
// Pane Bridge Interface for cross-context communication
// ---------------------------------------------------------------------------

export interface PaneBridge {
  activePaneId: string | null;
  activeBufferId: string | null;
  panes: EditorPane[];
  /** Split type and sizes, persisted with the layout. */
  paneLayout?: PaneLayout;
  paneSizes?: PaneSize;
  setPaneLayout?: (layout: PaneLayout) => void;
  setPaneSizes?: (sizes: PaneSize) => void;
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
  /** Ref mirror of `buffers` — read latest map inside event handlers without
   *  re-subscribing (used by the beforeunload unsaved-changes guard). */
  buffersRef: React.MutableRefObject<Map<string, EditorBuffer>>;
  openFile: (file: EditorFileEntry) => string;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    activate?: boolean;
    /** Pane to open a new buffer in, when it exists (restoring a saved layout). */
    paneId?: string;
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
  saveBuffer: (
    bufferId: string,
    options?: { force?: boolean },
  ) => Promise<{ mod_time?: number; formattedContent?: string } | void>;
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
  /** Point file tabs at their new location after a file or folder moves. */
  retargetBufferPaths: (oldPath: string, newPath: string) => void;
  /** Close clean tabs under a deleted path; edited tabs stay open so work isn't lost. */
  closeBuffersForDeletedPath: (path: string) => void;
}

const BufferManagerContext = createContext<BufferManagerContextValue | null>(null);

export const useBufferManager = () => {
  const context = useContext(BufferManagerContext);
  if (!context) {
    throw new Error('useBufferManager must be used within BufferManagerProvider');
  }
  return context;
};

// Timestamps alone collide when several buffers open in the same millisecond
// (an agent opening files in a burst), silently replacing the earlier buffer.
let bufferSeq = 0;
function nextBufferId(prefix: string): string {
  bufferSeq += 1;
  return `${prefix}-${Date.now()}-${bufferSeq}`;
}

/** Like useBufferManager, for components that can render outside the editor. */
export const useOptionalBufferManager = () => useContext(BufferManagerContext);

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
        ext: '.chat',
      },
      content: '',
      originalContent: '',
      contentLoaded: true,
      cursorPosition: { line: 0, column: 0 },
      scrollPosition: { top: 0, left: 0 },
      isModified: false,
      isActive: true,
      paneId: 'pane-1',
      isPinned: false,
      isClosable: false,
      metadata: { chatId: null as string | null },
    };

    return new Map([[chatBuffer.id, chatBuffer]]);
  });

  // Keep a ref to the latest buffers Map so async closures don't read stale
  // data. Assigned during render, not in an effect: provider effects run after
  // their children's, so an effect here would overwrite changes the mutators
  // below mirror into the ref from a child's effect in the same commit.
  const buffersRef = useRef(buffers);
  buffersRef.current = buffers;
  // Buffers opened but not yet in committed state, by path. A render between
  // two opens can hand buffersRef the committed map without them; the path
  // lookup in openWorkspaceBuffer checks here too so it never opens a second
  // tab for the same path.
  const pendingOpensRef = useRef(new Map<string, EditorBuffer>());
  for (const [path, pending] of pendingOpensRef.current) {
    if (buffers.has(pending.id)) pendingOpensRef.current.delete(path);
  }

  // Keep a ref to the latest activePaneId so callbacks don't read stale closure values
  const activePaneIdRef = useRef(paneBridge.activePaneId);
  useEffect(() => {
    activePaneIdRef.current = paneBridge.activePaneId;
  }, [paneBridge.activePaneId]);

  // The buffer this manager last focused, ahead of the commit. Adopts the
  // prop only when the prop itself changes, so a render cannot undo a focus
  // change still in flight (closeBuffer right after an open must see the
  // opened buffer as focused, not the one before it).
  const focusedBufferIdRef = useRef(paneBridge.activeBufferId);
  const seenActiveBufferIdRef = useRef(paneBridge.activeBufferId);
  if (seenActiveBufferIdRef.current !== paneBridge.activeBufferId) {
    seenActiveBufferIdRef.current = paneBridge.activeBufferId;
    focusedBufferIdRef.current = paneBridge.activeBufferId;
  }
  const setFocusedBuffer = useCallback(
    (id: string | null) => {
      focusedBufferIdRef.current = id;
      paneBridge.setActiveBufferId(id);
    },
    [paneBridge],
  );

  // Helper to find the rightmost pane for chat placement
  const getRightmostPane = useCallback((paneList: EditorPane[]) => {
    if (paneList.length === 0) return null;
    const positionOrder: Record<string, number> = {
      primary: 0,
      secondary: 1,
      tertiary: 2,
      quaternary: 3,
      quinary: 4,
      senary: 5,
    };
    return paneList.reduce((rightmost, pane) => {
      const rightmostOrder = positionOrder[rightmost.position as string] ?? 0;
      const paneOrder = positionOrder[pane.position as string] ?? 0;
      return paneOrder > rightmostOrder ? pane : rightmost;
    }, paneList[0]);
  }, []);

  // Activate a buffer (display in active pane)
  const activateBuffer = useCallback(
    (bufferId: string) => {
      const currentActivePane = activePaneIdRef.current;
      setFocusedBuffer(bufferId);

      setBuffers((prev) => {
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

      paneBridge.setPanes((prev) => prev.map((pane) => (pane.id === currentActivePane ? { ...pane, bufferId } : pane)));
    },
    [paneBridge, setFocusedBuffer],
  );

  // Switch to a different buffer in the active pane
  const switchToBuffer = useCallback(
    (bufferId: string) => {
      const existingBuffer = buffersRef.current.get(bufferId);
      if (!existingBuffer) {
        return;
      }

      const currentPaneId = activePaneIdRef.current;

      if (existingBuffer.paneId && existingBuffer.paneId !== currentPaneId) {
        paneBridge.setActivePaneId(existingBuffer.paneId);
        setFocusedBuffer(bufferId);
        setBuffers((prev) => {
          const next = new Map(prev);
          Array.from(next.entries()).forEach(([id, buf]) => {
            if (buf.paneId === existingBuffer.paneId) {
              next.set(id, { ...buf, isActive: id === bufferId });
            }
          });
          return next;
        });
        paneBridge.setPanes((prev) =>
          prev.map((pane) => (pane.id === existingBuffer.paneId ? { ...pane, bufferId } : pane)),
        );
        return;
      }

      setFocusedBuffer(bufferId);
      setBuffers((prev) => {
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
      paneBridge.setPanes((prev) => prev.map((pane) => (pane.id === currentPaneId ? { ...pane, bufferId } : pane)));
    },
    [paneBridge, setFocusedBuffer],
  );

  // Open a file in an editor pane
  const openFile = useCallback(
    (file: EditorFileEntry) => {
      // Normalize to an absolute path so the LSP document URI points at the
      // real on-disk file (relative paths break module resolution) and so
      // buffer dedup can't create duplicates for the same file opened via
      // different entry points.
      const filePath = resolveEditorFilePath(file.path);

      const currentBuffers = buffersRef.current;
      const currentActivePane = activePaneIdRef.current;
      const existingBuffer = Array.from(currentBuffers.entries()).find(
        ([_, buffer]) => buffer.kind === 'file' && resolveEditorFilePath(buffer.file.path) === filePath,
      );
      if (existingBuffer) {
        const [bufferId, buffer] = existingBuffer;
        if (buffer.paneId) {
          const pane = paneBridge.panes.find((p) => p.id === buffer.paneId);
          if (pane) {
            switchToBuffer(bufferId);
            return bufferId;
          }
        }
        activateBuffer(bufferId);
        return bufferId;
      }

      const bufferId = nextBufferId('buffer');
      const newBuffer: EditorBuffer = {
        id: bufferId,
        kind: 'file',
        // Store the normalized path so later opens via another path form
        // (tree vs search vs terminal link) find this buffer.
        file: { ...file, path: filePath },
        content: '',
        originalContent: '',
        contentLoaded: false, // fresh buffer — content not yet read from disk
        cursorPosition: { line: 0, column: 0 },
        scrollPosition: { top: 0, left: 0 },
        isModified: false,
        isActive: true,
        paneId: currentActivePane,
      };

      setBuffers((prev) => {
        const newBuffers = new Map(prev);
        newBuffers.forEach((existing, key) => {
          if (key !== bufferId && existing.paneId === currentActivePane) {
            newBuffers.set(key, { ...existing, isActive: false });
          }
        });
        newBuffers.set(bufferId, newBuffer);
        return newBuffers;
      });

      paneBridge.setPanes((prev) => prev.map((pane) => (pane.id === currentActivePane ? { ...pane, bufferId } : pane)));

      setFocusedBuffer(bufferId);

      return bufferId;
    },
    [activateBuffer, switchToBuffer, paneBridge, setFocusedBuffer],
  );

  // Open workspace buffer
  const openWorkspaceBuffer = useCallback(
    (options: {
      kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
      path: string;
      title: string;
      content?: string;
      ext?: string;
      isPinned?: boolean;
      isClosable?: boolean;
      activate?: boolean;
      /** Pane to open a new buffer in, when it exists (restoring a saved layout). */
      paneId?: string;
      metadata?: Record<string, unknown>;
    }) => {
      if (options.kind === 'file') {
        options = { ...options, path: resolveEditorFilePath(options.path) };
      }
      const currentBuffers = buffersRef.current;
      const pendingOpen = pendingOpensRef.current.get(options.path);
      const existingBufferEntry: [string, EditorBuffer] | undefined =
        Array.from(currentBuffers.entries()).find(([_, buffer]) => buffer.file.path === options.path) ??
        (pendingOpen ? [pendingOpen.id, pendingOpen] : undefined);

      if (existingBufferEntry) {
        const [bufferId, buffer] = existingBufferEntry;
        const applyOptions = (b: EditorBuffer): EditorBuffer => ({
          ...b,
          kind: options.kind,
          file: {
            ...b.file,
            name: options.title,
            path: options.path,
            ext: options.ext || b.file.ext,
          },
          content: options.content ?? b.content,
          originalContent: options.content ?? b.originalContent,
          contentLoaded: options.content != null ? true : b.contentLoaded,
          isPinned: options.isPinned ?? b.isPinned,
          isClosable: options.isClosable ?? b.isClosable,
          metadata: options.metadata ?? b.metadata,
        });
        const updated = applyOptions(buffer);
        setBuffers((prev) => {
          const next = new Map(prev);
          next.set(bufferId, applyOptions(prev.get(bufferId) ?? buffer));
          return next;
        });
        buffersRef.current = new Map(buffersRef.current).set(bufferId, updated);
        if (pendingOpensRef.current.has(options.path)) pendingOpensRef.current.set(options.path, updated);
        // A background open (activate: false) refreshes the tab without
        // focusing it — focusing a chat tab switches the conversation.
        if (options.activate === false) return bufferId;
        // Navigate to the buffer's existing pane if it's in a different pane
        if (buffer.paneId && buffer.paneId !== paneBridge.activePaneId) {
          paneBridge.setActivePaneId(buffer.paneId);
          switchToBuffer(bufferId);
        } else {
          activateBuffer(bufferId);
        }
        return bufferId;
      }

      const requestedPane = options.paneId ? paneBridge.panes.find((p) => p.id === options.paneId) : undefined;
      const targetPane =
        requestedPane ??
        (options.kind === 'chat'
          ? getRightmostPane(paneBridge.panes)
          : paneBridge.panes.find((p) => p.id === paneBridge.activePaneId));
      const targetPaneId = targetPane?.id ?? paneBridge.activePaneId;

      const bufferId = nextBufferId(`buffer-${options.kind}`);
      const shouldActivate = options.activate ?? true;
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
        contentLoaded: options.content != null, // explicitly provided content is authoritative
        cursorPosition: { line: 0, column: 0 },
        scrollPosition: { top: 0, left: 0 },
        isModified: false,
        isActive: shouldActivate,
        paneId: targetPaneId,
        isPinned: options.isPinned ?? false,
        isClosable: options.isClosable ?? !options.isPinned,
        metadata: options.metadata ?? {},
      };

      setBuffers((prev) => {
        const next = new Map(prev);
        if (shouldActivate) {
          next.forEach((existing, key) => {
            if (key !== bufferId && existing.paneId === targetPaneId) {
              next.set(key, { ...existing, isActive: false });
            }
          });
        }
        next.set(bufferId, newBuffer);
        return next;
      });
      // Several opens can run before React commits (the session-list sync and
      // a New-chat handler opening the same chat in one tick); the path
      // lookup above must see this buffer or it opens a duplicate tab.
      buffersRef.current = new Map(buffersRef.current).set(bufferId, newBuffer);
      pendingOpensRef.current.set(options.path, newBuffer);

      if (shouldActivate) {
        paneBridge.setPanes((prev) => prev.map((pane) => (pane.id === targetPaneId ? { ...pane, bufferId } : pane)));
        paneBridge.setActivePaneId(targetPaneId);
        setFocusedBuffer(bufferId);
      }

      return bufferId;
    },
    [activateBuffer, getRightmostPane, switchToBuffer, paneBridge, setFocusedBuffer],
  );

  // Open compare buffer
  const openCompareBuffer = useCallback(
    (options: {
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
    },
    [openWorkspaceBuffer],
  );

  // Update buffer operations
  // Mirror a flag/metadata change into buffersRef right away, so lookups later
  // in the same tick (e.g. a close right after making the tab closable) see it.
  const patchBufferRef = useCallback((bufferId: string, patch: (buf: EditorBuffer) => EditorBuffer) => {
    const buf = buffersRef.current.get(bufferId);
    if (buf) buffersRef.current = new Map(buffersRef.current).set(bufferId, patch(buf));
  }, []);

  const updateBufferMetadata = useCallback(
    (bufferId: string, updates: Record<string, unknown>) => {
      patchBufferRef(bufferId, (buf) => ({ ...buf, metadata: { ...buf.metadata, ...updates } }));
      setBuffers((prev) => {
        const buf = prev.get(bufferId);
        if (!buf) return prev;
        const next = new Map(prev);
        next.set(bufferId, { ...buf, metadata: { ...buf.metadata, ...updates } });
        return next;
      });
    },
    [patchBufferRef],
  );

  const updateBufferTitle = useCallback(
    (bufferId: string, title: string) => {
      patchBufferRef(bufferId, (buf) => ({ ...buf, file: { ...buf.file, name: title } }));
      setBuffers((prev) => {
        const buf = prev.get(bufferId);
        if (!buf) return prev;
        const next = new Map(prev);
        next.set(bufferId, { ...buf, file: { ...buf.file, name: title } });
        return next;
      });
    },
    [patchBufferRef],
  );

  const updateBufferContent = useCallback((bufferId: string, content: string) => {
    setBuffers((prev) => {
      const buffer = prev.get(bufferId);
      if (!buffer || buffer.content === content) return prev;
      const newIsModified = content !== buffer.originalContent;
      if (buffer.isModified === newIsModified) {
        // isModified didn't change — mutate in-place to avoid context cascade.
        // Safe because EditorPane reads content from CodeMirror state, not React state.
        // Footer re-renders from selectionInfo changes independently.
        buffer.content = content;
        return prev;
      }
      // isModified transitioned — create new Map to trigger tab indicator update
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, { ...buffer, content, isModified: newIsModified });
      return newBuffers;
    });
  }, []);

  const updateBufferCursor = useCallback((bufferId: string, position: { line: number; column: number }) => {
    setBuffers((prev) => {
      const buffer = prev.get(bufferId);
      if (!buffer) return prev;
      if (buffer.cursorPosition?.line === position.line && buffer.cursorPosition?.column === position.column) {
        return prev;
      }
      // Mutate in-place — cursor position is persisted for tab switching, not React rendering
      buffer.cursorPosition = position;
      return prev;
    });
  }, []);

  const updateBufferScroll = useCallback((bufferId: string, position: { top: number; left: number }) => {
    setBuffers((prev) => {
      const buffer = prev.get(bufferId);
      if (!buffer) return prev;
      if (buffer.scrollPosition?.top === position.top && buffer.scrollPosition?.left === position.left) {
        return prev;
      }
      // Mutate in-place — scroll position is persisted for tab switching, not React rendering
      buffer.scrollPosition = position;
      return prev;
    });
  }, []);

  // Buffer property setters
  const setBufferModified = useCallback((bufferId: string, isModified: boolean) => {
    setBuffers((prev) => {
      const buffer = prev.get(bufferId);
      if (!buffer || buffer.isModified === isModified) return prev;
      const newBuffers = new Map(prev);
      newBuffers.set(bufferId, { ...buffer, isModified });
      return newBuffers;
    });
  }, []);

  const setBufferOriginalContent = useCallback((bufferId: string, originalContent: string) => {
    setBuffers((prev) => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          originalContent,
          contentLoaded: true, // content is now initialized in memory
          isModified: buffer.content !== originalContent ? buffer.isModified : false,
        });
      }
      return next;
    });
  }, []);

  const setBufferExternallyModified = useCallback((bufferId: string, diskContent: string, mtime?: number) => {
    setBuffers((prev) => {
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
    setBuffers((prev) => {
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
    setBuffers((prev) => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, languageOverride: languageId });
      }
      return next;
    });
  }, []);

  const toggleBufferPin = useCallback((bufferId: string) => {
    setBuffers((prev) => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, { ...buffer, isPinned: !buffer.isPinned });
      }
      return next;
    });
  }, []);

  const setBufferPinned = useCallback(
    (bufferId: string, isPinned: boolean) => {
      patchBufferRef(bufferId, (buf) => ({ ...buf, isPinned }));
      setBuffers((prev) => {
        const next = new Map(prev);
        const buffer = next.get(bufferId);
        if (buffer) {
          next.set(bufferId, { ...buffer, isPinned });
        }
        return next;
      });
    },
    [patchBufferRef],
  );

  const setBufferClosable = useCallback(
    (bufferId: string, isClosable: boolean) => {
      patchBufferRef(bufferId, (buf) => ({ ...buf, isClosable }));
      setBuffers((prev) => {
        const next = new Map(prev);
        const buffer = next.get(bufferId);
        if (buffer) {
          next.set(bufferId, { ...buffer, isClosable });
        }
        return next;
      });
    },
    [patchBufferRef],
  );

  const reloadBufferFromDisk = useCallback((bufferId: string, diskContent: string, mtime?: number) => {
    setBuffers((prev) => {
      const next = new Map(prev);
      const buffer = next.get(bufferId);
      if (buffer) {
        next.set(bufferId, {
          ...buffer,
          content: diskContent,
          originalContent: diskContent,
          contentLoaded: true, // reloaded content is authoritative
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
  const saveBuffer = useCallback(
    async (bufferId: string, options?: { force?: boolean }) => {
      const buffer = buffersRef.current.get(bufferId);
      if (!buffer || buffer.kind !== 'file') return;

      // Never let a background save (auto-save, save-all) write over an
      // external change the user has not arbitrated yet — the auto-save
      // interval would otherwise clobber the agent's/build's edit ~30s
      // after the conflict dialog opens. An explicit save (Cmd+S) passes
      // force:true and counts as the user's resolution, so a keep-mine
      // author can still land their version.
      if (buffer.externallyModified && !options?.force) {
        debugLog(`[saveBuffer] Skipped ${buffer.file.path}: external change unresolved`);
        return;
      }

      // Handle virtual workspace buffers (untitled files created via Ctrl+N)
      if (buffer.file.path.startsWith('__workspace/')) {
        const filePath = await showThemedPrompt('Enter a file path for the new file:', {
          title: 'Save As',
          defaultValue: 'untitled',
          placeholder: 'path/to/file.ts',
        });

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

          setBuffers((prev) => {
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
      const savedSourceContent = buffer.content;
      let contentToSave = buffer.content;
      let formattedContent: string | undefined;
      if (isFormatOnSaveEnabled && isFormattable(buffer.file.path)) {
        try {
          const formatPromise = formatCodeWithConfigDiscovery(buffer.content, buffer.file.path, buffer.file.size);
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

      // Announce the save BEFORE the write so the external file watcher
      // learns the new mtime and enters its cooldown before the fsnotify
      // echo / next poll can flag our own write as an external change.
      // (Same contract as useEditorFileIO.handleSave.)
      document.dispatchEvent(
        new CustomEvent('file:editor-saved', {
          detail: { path: buffer.file.path, mtime: Math.floor(Date.now() / 1000) },
        }),
      );

      try {
        const response = await writeFileWithFetch(sproutFetch, buffer.file.path, contentToSave);

        if (response.ok) {
          const data = await response.json();
          if (data.success === false) {
            console.error('Save validation failed:', data);
            throw new Error(data.error || 'Save validation failed');
          }
          if (data.message === 'File saved successfully' || data.success === true) {
            const serverMtime = typeof data.mod_time === 'number' ? data.mod_time : undefined;
            setBuffers((prev) => {
              const newBuffers = new Map(prev);
              const buf = newBuffers.get(bufferId);
              if (buf) {
                // True when the user typed while the HTTP roundtrip was in
                // flight: disk now holds the pre-typing snapshot
                // (contentToSave), the buffer holds the newer text.
                const editedDuringSave = buf.content !== savedSourceContent;
                newBuffers.set(bufferId, {
                  ...buf,
                  // Format-on-save: disk holds the formatted text, so sync
                  // the buffer content with it. Skipping this left the view
                  // holding the unformatted text while originalContent
                  // advanced — isModified computed true forever and
                  // auto-save rewrote the file every interval. Skipped when
                  // the user typed during the save (their newer edits win
                  // and stay flagged modified against the disk snapshot).
                  ...(formattedContent && !editedDuringSave ? { content: formattedContent } : {}),
                  // originalContent is what disk NOW holds (the snapshot we
                  // wrote), not the buffer's latest content — otherwise
                  // keystrokes typed during the roundtrip get marked clean
                  // and silently vanish from the dirty state.
                  originalContent: contentToSave,
                  isModified: editedDuringSave,
                  // A completed save (forced or not) resolves any pending
                  // external-change conflict: disk now holds this buffer's
                  // content by definition.
                  externallyModified: false,
                  diskContent: null,
                  ...(serverMtime != null ? { file: { ...buf.file, modified: serverMtime } } : {}),
                });
              }
              return newBuffers;
            });
            // Re-announce with the authoritative server mtime once the HTTP
            // response lands.
            document.dispatchEvent(
              new CustomEvent('file:editor-saved', {
                detail: { path: buffer.file.path, mtime: serverMtime ?? Math.floor(Date.now() / 1000) },
              }),
            );
            return { mod_time: serverMtime, formattedContent };
          }
        }
      } catch (error) {
        console.error('Failed to save buffer:', bufferId, error);
        throw error;
      }
    },
    [isFormatOnSaveEnabled, sproutFetch],
  );

  // Save all modified buffers
  const saveAllBuffers = useCallback(async () => {
    const currentBuffers = buffersRef.current;
    const savePromises = Array.from(currentBuffers.entries())
      .filter(([_, buffer]) => buffer.isModified && !buffer.file.path.startsWith('__workspace/'))
      .map(([bufferId, _]) => saveBuffer(bufferId));

    await Promise.all(savePromises);
  }, [saveBuffer]);

  // Close a buffer
  const closeBuffer = useCallback(
    async (bufferId: string) => {
      const buffer = buffersRef.current.get(bufferId);
      if (!buffer) return;
      if (buffer.isClosable === false) return;

      // An unresolved external change plus unsaved edits: closing used to
      // auto-save straight through the conflict (clobbering the disk edit
      // the user never arbitrated). Refuse and point at the two exits.
      if (buffer.isModified && buffer.externallyModified) {
        notificationBus.notify(
          'warning',
          'File conflict unresolved',
          `${buffer.file.name} changed on disk. Reload it or save (Cmd+S) to resolve before closing.`,
          6000,
        );
        return;
      }

      if (buffer.isModified && isAutoSaveEnabled) {
        try {
          await saveBuffer(bufferId);
        } catch (err) {
          console.error('Failed to save buffer before closing:', bufferId, err);
          return; // Block close on save failure
        }
      }

      const remain = Array.from(buffersRef.current.values()).filter((candidate) => candidate.id !== bufferId);
      const nextPaneBuffer = buffer.paneId
        ? remain.find((candidate) => candidate.paneId === buffer.paneId) ||
          remain.find((candidate) => !candidate.paneId) ||
          null
        : null;

      const currentActivePane = activePaneIdRef.current;

      const closedWasFocused = bufferId === focusedBufferIdRef.current;

      setBuffers((prev) => {
        const newBuffers = new Map(prev);
        const closedWasShown = prev.get(bufferId)?.isActive;
        newBuffers.delete(bufferId);
        // Hand the pane to another buffer only if the closed one was on show
        // there; closing a background tab must not change what the pane shows.
        if (buffer.paneId && nextPaneBuffer && closedWasShown) {
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
      const withoutClosed = new Map(buffersRef.current);
      withoutClosed.delete(bufferId);
      buffersRef.current = withoutClosed;
      pendingOpensRef.current.delete(buffer.file.path);

      if (buffer.paneId) {
        paneBridge.setPanes((prev) =>
          prev.map((pane) =>
            pane.id === buffer.paneId && pane.bufferId === bufferId
              ? { ...pane, bufferId: nextPaneBuffer?.id || null }
              : pane,
          ),
        );
      }

      if (closedWasFocused) {
        if (nextPaneBuffer) {
          setFocusedBuffer(nextPaneBuffer.id);
        } else {
          setFocusedBuffer(null);
        }
      }

      // Notify listeners (e.g. useChatSessionsSync) which buffer closed so a
      // dismissed chat tab is not resurrected on the next sessions update.
      window.dispatchEvent(
        new CustomEvent('workspace:buffer-closed', {
          detail: { id: bufferId, kind: buffer.kind, chatId: buffer.metadata?.chatId },
        }),
      );
    },
    [isAutoSaveEnabled, saveBuffer, paneBridge, setFocusedBuffer],
  );

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

  const moveBufferToPane = useCallback(
    (bufferId: string, paneId: string) => {
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
        setFocusedBuffer(bufferId);
      }
    },
    [paneBridge, setFocusedBuffer],
  );

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

  // ── External file change detection ─────────────────────────────
  // Polls disk mtimes for open file buffers; on an external write,
  // clean buffers auto-reload from disk and modified buffers get the
  // conflict dialog (via useEditorFileIO's file_externally_modified
  // listener). This layer was silently dropped in the hook-consolidation
  // refactor — without it the editor never learns that the agent or a
  // build changed an open file, and a later save clobbers those edits.
  const retargetBufferPaths = useCallback((oldPath: string, newPath: string) => {
    const from = resolveEditorFilePath(oldPath);
    const to = resolveEditorFilePath(newPath);
    setBuffers((prev) => {
      let next: Map<string, EditorBuffer> | null = null;
      prev.forEach((buffer, id) => {
        if (buffer.kind !== 'file') return;
        const moved = movedBufferPath(resolveEditorFilePath(buffer.file.path), from, to);
        if (!moved) return;
        next ??= new Map(prev);
        next.set(id, { ...buffer, file: fileEntryAtPath(buffer.file, moved) });
      });
      return next ?? prev;
    });
  }, []);

  const closeBuffersForDeletedPath = useCallback(
    (path: string) => {
      const target = resolveEditorFilePath(path);
      for (const [id, buffer] of buffersRef.current) {
        if (buffer.kind !== 'file' || buffer.isModified) continue;
        if (isWithinPath(resolveEditorFilePath(buffer.file.path), target)) void closeBuffer(id);
      }
    },
    [closeBuffer],
  );

  useExternalFileWatcher({ buffers });

  useAutoReloadCleanBuffers({
    buffersRef,
    reloadBufferFromDisk,
    setBufferExternallyModified,
  });

  // Saves and restores open file tabs and the pane layout across reloads,
  // and drops stale file tabs on a workspace switch. Unmounted by an earlier
  // hook consolidation, which left every reload with no open files.
  const panesRef = useRef(paneBridge.panes);
  panesRef.current = paneBridge.panes;
  useLayoutPersistence({
    buffersRef,
    panesRef,
    buffers,
    panes: paneBridge.panes,
    setBuffers,
    setPanes: paneBridge.setPanes,
    activePaneId: paneBridge.activePaneId,
    activeBufferId: paneBridge.activeBufferId,
    setActivePaneId: paneBridge.setActivePaneId,
    setActiveBufferId: paneBridge.setActiveBufferId,
    paneLayout: paneBridge.paneLayout ?? 'single',
    paneSizes: paneBridge.paneSizes ?? {},
    setPaneLayout: paneBridge.setPaneLayout,
    setPaneSizes: paneBridge.setPaneSizes,
  });

  const value = React.useMemo<BufferManagerContextValue>(
    () => ({
      buffers,
      buffersRef,
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
      retargetBufferPaths,
      closeBuffersForDeletedPath,
    }),
    [
      buffers,
      buffersRef,
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
      retargetBufferPaths,
      closeBuffersForDeletedPath,
    ],
  );

  return <BufferManagerContext.Provider value={value}>{children}</BufferManagerContext.Provider>;
};
