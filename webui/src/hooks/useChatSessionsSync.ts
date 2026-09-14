import { useEffect, useRef } from 'react';
import type { ChatSession } from '../services/chatSessions';
import type { EditorBuffer } from '../types/editor';

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

/**
 * Mirrors chat sessions into workspace chat tabs.
 *
 * Opening is once-per-session-per-mount: a session whose tab the user closed
 * must NOT come back on the next chatSessions update (a WS event, a rename,
 * another create all re-run this effect). Previously every update reopened
 * every unbuffered session, so closed tabs resurrected endlessly — the core
 * "view management doesn't work" complaint. The exclusion list is cleared on
 * session switch (switching back to a chat reopens its tab on purpose).
 */
export const useChatSessionsSync = ({
  chatSessions,
  activeChatId,
  buffersRef,
  updateBufferTitle,
  updateBufferMetadata,
  openWorkspaceBuffer,
}: UseChatSessionsSyncParams): void => {
  const closedChatIdsRef = useRef<Set<string>>(new Set());
  // Track the active chat id across renders to detect switches (which clear
  // the exclusion for the newly-active chat so its tab reopens deliberately).
  const prevActiveChatIdRef = useRef<string | null | undefined>(activeChatId);

  useEffect(() => {
    if (activeChatId !== prevActiveChatIdRef.current) {
      if (activeChatId && closedChatIdsRef.current.has(activeChatId)) {
        closedChatIdsRef.current.delete(activeChatId);
      }
      prevActiveChatIdRef.current = activeChatId;
    }

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
      // Skip sessions whose tab the user explicitly closed this mount.
      if (closedChatIdsRef.current.has(session.id)) return;
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

  // Observe buffer closes to learn which chat tabs the user dismissed.
  // EditorManager emits 'workspace:buffer-closed' with the buffer's metadata,
  // which carries the chatId for chat-kind buffers.
  useEffect(() => {
    const onBufferClosed = (event: Event) => {
      const detail = (event as CustomEvent<{ kind?: string; chatId?: string }>).detail;
      if (detail?.kind === 'chat' && detail.chatId) {
        closedChatIdsRef.current.add(detail.chatId);
      }
    };
    window.addEventListener('workspace:buffer-closed', onBufferClosed);
    return () => window.removeEventListener('workspace:buffer-closed', onBufferClosed);
  }, []);
};
