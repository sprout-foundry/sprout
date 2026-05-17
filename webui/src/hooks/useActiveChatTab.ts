import { useEffect } from 'react';
import type { EditorBuffer } from '../types/editor';

export interface UseActiveChatTabParams {
  activeBufferId: string | null;
  buffersRef: React.RefObject<Map<string, EditorBuffer>>;
  activeChatId: string | null;
  onActiveChatChange?: (id: string) => void;
}

export const useActiveChatTab = ({
  activeBufferId,
  buffersRef,
  activeChatId,
  onActiveChatChange,
}: UseActiveChatTabParams): void => {
  useEffect(() => {
    if (!activeBufferId) return;
    if (!buffersRef.current) return;

    const activeBuf = buffersRef.current.get(activeBufferId);
    if (activeBuf?.kind === 'chat' && activeBuf.metadata?.chatId) {
      const chatId = activeBuf.metadata.chatId as string;
      if (chatId !== activeChatId && onActiveChatChange) {
        onActiveChatChange(chatId);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeBufferId]);
};
