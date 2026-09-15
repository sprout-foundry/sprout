import { useEffect, useRef } from 'react';
import type { ChatSession } from '../services/chatSessions';
import type { EditorBuffer } from '../types/editor';

export interface UseChatSessionsSyncParams {
  chatSessions: ChatSession[] | undefined;
  activeChatId: string | null | undefined;
  buffersRef: React.RefObject<Map<string, EditorBuffer>>;
  updateBufferTitle: (id: string, title: string) => void;
  updateBufferMetadata: (id: string, metadata: Record<string, unknown>) => void;
  setBufferPinned: (id: string, isPinned: boolean) => void;
  setBufferClosable: (id: string, isClosable: boolean) => void;
  openWorkspaceBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    activate?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
}

/**
 * Mirrors chat sessions into workspace chat tabs.
 *
 * Exactly one chat tab is ever pinned: the ACTIVE chat's tab. Other sessions
 * open as unpinned, closable, non-activating background tabs so they don't
 * hijack the active chat (an unwanted activation would switch the server-side
 * active chat and, via its session_changed("switch") echo, re-trigger every
 * client — the infinite refresh loop behind "two tabs fighting over one
 * websocket"). Do NOT derive pin state from the wire `is_default` flag: that
 * means "is currently active" (a transient `id == activeChatId` computed on
 * the server), not "user pinned this session". Baking it in permanently
 * pinned every chat that ever happened to be active, and chat tabs expose no
 * unpin or close control when unclosable — a stuck tab with no escape.
 *
 * Opening is once-per-session-per-mount: a session whose tab the user closed
 * must NOT come back on the next chatSessions update (a WS event, a rename,
 * another create all re-run this effect). The exclusion list is cleared on
 * session switch (switching back to a chat reopens its tab on purpose).
 */
export const useChatSessionsSync = ({
  chatSessions,
  activeChatId,
  buffersRef,
  updateBufferTitle,
  updateBufferMetadata,
  setBufferPinned,
  setBufferClosable,
  openWorkspaceBuffer,
}: UseChatSessionsSyncParams): void => {
  const closedChatIdsRef = useRef<Set<string>>(new Set());
  const prevActiveChatIdRef = useRef<string | null | undefined>(activeChatId);

  // Open tabs for sessions that don't have one yet. Runs per-mount except
  // when the active chat changes (which deliberately reopens a closed tab
  // for the now-active chat).
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
        if (existing.file.name !== (session.name || 'Chat')) {
          updateBufferTitle(existing.id, session.name || 'Chat');
        }
        return;
      }
      if (closedChatIdsRef.current.has(session.id)) return;

      const isActive = session.id === activeChatId;
      // The active chat claims the initial pinned chat buffer if it's still
      // unclaimed; otherwise open its tab (pinned, activating — landing the
      // user on the chat they're actually viewing).
      const initialBuf = currentBuffers.get('buffer-chat');
      if (isActive && initialBuf && !initialBuf.metadata?.chatId) {
        updateBufferMetadata('buffer-chat', { chatId: session.id });
        updateBufferTitle('buffer-chat', session.name || 'Chat');
      } else {
        openWorkspaceBuffer({
          kind: 'chat',
          path: `__workspace/chat/${session.id}`,
          title: session.name || 'Chat',
          isPinned: isActive,
          isClosable: !isActive,
          activate: isActive,
          metadata: { chatId: session.id },
        });
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatSessions, activeChatId]);

  // Keep pin state canonical. The ACTIVE chat's tab is the one pinned tab;
  // every other chat tab must be unpinned and closable. This repairs tabs
  // created under the old bug that permanently pinned every formerly-active
  // session (is_default == active), so previously stuck tabs become closable.
  // Idempotent: setBufferPinned/setBufferClosable only fire when state is
  // actually wrong, so steady-state is a no-op (no render loop).
  useEffect(() => {
    if (!chatSessions) return;
    const currentBuffers = buffersRef.current;
    if (!currentBuffers) return;

    for (const session of chatSessions) {
      const buffer = Array.from(currentBuffers.values()).find(
        (b) => b.kind === 'chat' && b.metadata?.chatId === session.id,
      );
      if (!buffer) continue;
      const isActive = session.id === activeChatId;
      if (isActive && (!buffer.isPinned || buffer.isClosable !== false)) {
        setBufferPinned(buffer.id, true);
        setBufferClosable(buffer.id, false);
      } else if (!isActive && (buffer.isPinned || buffer.isClosable === false)) {
        setBufferPinned(buffer.id, false);
        setBufferClosable(buffer.id, true);
      }
    }
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
