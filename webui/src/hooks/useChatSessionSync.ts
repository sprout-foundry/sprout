import { useEffect, useRef } from 'react';
import type { EditorBuffer } from '../types/editor';

/** Shape of the openWorkspaceBuffer callback from EditorManagerContext */
export type OpenWorkspaceBufferFn = (options: {
  kind: 'chat' | 'diff' | 'review' | 'file';
  path: string;
  title: string;
  content?: string;
  ext?: string;
  isPinned?: boolean;
  isClosable?: boolean;
  metadata?: Record<string, any>;
}) => string;

export interface UseChatSessionSyncOptions {
  chatSessions: Array<{ id: string; name?: string; is_default?: boolean }> | undefined;
  activeChatId: string | null | undefined;
  activeBufferId: string | null;
  buffers: Map<string, EditorBuffer>;
  onActiveChatChange?: (id: string) => void;
  openWorkspaceBuffer: OpenWorkspaceBufferFn;
  updateBufferMetadata: (bufferId: string, updates: Record<string, any>) => void;
  updateBufferTitle: (bufferId: string, title: string) => void;
}

export function useChatSessionSync({
  chatSessions,
  activeChatId,
  activeBufferId,
  buffers,
  onActiveChatChange,
  openWorkspaceBuffer,
  updateBufferMetadata,
  updateBufferTitle,
}: UseChatSessionSyncOptions): void {
  // Keep a stable ref to the current buffers map to avoid infinite loops in effects
  const buffersRef = useRef(buffers);
  useEffect(() => { buffersRef.current = buffers; }, [buffers]);

  // Sync chat sessions → editor buffers: update the initial chat buffer with the active
  // session's ID, and open additional buffers for other sessions.
  useEffect(() => {
    if (!chatSessions || chatSessions.length === 0) return;
    const currentBuffers = buffersRef.current;
    chatSessions.forEach(session => {
      const existing = Array.from(currentBuffers.values()).find(
        b => b.kind === 'chat' && b.metadata?.chatId === session.id
      );
      if (existing) {
        // Update tab title if the session was renamed
        if (existing.file.name !== (session.name || 'Chat')) {
          updateBufferTitle(existing.id, session.name || 'Chat');
        }
        return;
      }
      // If this is the active session and the initial chat buffer has no chatId yet, claim it
      const initialBuf = currentBuffers.get('buffer-chat');
      if (session.id === activeChatId && initialBuf && !initialBuf.metadata?.chatId) {
        updateBufferMetadata('buffer-chat', { chatId: session.id });
        updateBufferTitle('buffer-chat', session.name || 'Chat');
      } else {
        openWorkspaceBuffer({
          kind: 'chat',
          path: `__workspace/chat/${session.id}`,
          title: session.name || 'Chat',
          isPinned: session.is_default ?? false,
          isClosable: !(session.is_default ?? false),
          metadata: { chatId: session.id },
        });
      }
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatSessions, activeChatId]);

  // Detect when the user switches to a different chat tab and notify parent
  useEffect(() => {
    if (!activeBufferId) return;
    const activeBuf = buffersRef.current.get(activeBufferId);
    if (activeBuf?.kind === 'chat' && activeBuf.metadata?.chatId) {
      const chatId = activeBuf.metadata.chatId as string;
      if (chatId !== activeChatId && onActiveChatChange) {
        onActiveChatChange(chatId);
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- activeChatId intentionally excluded: including it would cause an infinite render loop (parent sets activeChatId → effect re-fires → calls onActiveChatChange → parent re-sets). Buffer-change-driven detection via activeBufferId is sufficient.
  }, [activeBufferId]);
}
