import type { EditorBuffer } from '../types/editor';
import { useEffect } from 'react';
import type { ChatSession } from '../services/chatSessions';

export interface UseChatSessionsSyncParams {
  chatSessions: ChatSession[] | undefined;
  activeChatId: string | null | undefined;
  buffersRef: React.RefObject<Map<string, EditorBuffer>>;
  updateBufferTitle: (id: string, title: string) => void;
  updateBufferMetadata: (id: string, metadata: Record<string, unknown>) => void;
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
}

export const useChatSessionsSync = ({
  chatSessions,
  activeChatId,
  buffersRef,
  updateBufferTitle,
  updateBufferMetadata,
  openWorkspaceBuffer,
}: UseChatSessionsSyncParams): void => {
  useEffect(() => {
    if (!chatSessions || chatSessions.length === 0) return;
    const currentBuffers = buffersRef.current;
    if (!currentBuffers) return;

    chatSessions.forEach((session) => {
      const existing = Array.from(currentBuffers.values()).find(
        (b) => b.kind === 'chat' && b.metadata?.chatId === session.id,
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
      if (activeChatId && session.id === activeChatId && initialBuf && !initialBuf.metadata?.chatId) {
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
};
