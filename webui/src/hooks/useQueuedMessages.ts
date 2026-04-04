/**
 * Queued message state management.
 *
 * Manages the queue of pending messages and provides CRUD operations:
 * add, remove, edit, reorder, and clear. Also includes the auto-send
 * effect that dispatches the next queued message when processing completes.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppState } from '../types/app';
import { notificationBus } from '../services/notificationBus';

export interface UseQueuedMessagesReturn {
  queuedMessages: string[];
  queuedMessagesRef: MutableRefObject<string[]>;
  setQueuedMessages: Dispatch<SetStateAction<string[]>>;
  handleQueueMessage: (message: string) => void;
  handleRemoveQueuedMessage: (index: number) => void;
  handleEditQueuedMessage: (index: number, newText: string) => void;
  handleReorderQueuedMessages: (fromIndex: number, toIndex: number) => void;
  handleClearQueuedMessages: () => void;
}

export function useQueuedMessages(): UseQueuedMessagesReturn {
  const [queuedMessages, setQueuedMessages] = useState<string[]>([]);
  const queuedMessagesRef = useRef<string[]>([]);

  const handleQueueMessage = useCallback((message: string) => {
    const trimmed = message.trim();
    if (!trimmed) return;
    queuedMessagesRef.current.push(trimmed);
    setQueuedMessages([...queuedMessagesRef.current]);
  }, []);

  const handleRemoveQueuedMessage = useCallback((index: number) => {
    setQueuedMessages((prev) => {
      const next = [...prev];
      next.splice(index, 1);
      queuedMessagesRef.current = next;
      return next;
    });
  }, []);

  const handleEditQueuedMessage = useCallback((index: number, newText: string) => {
    setQueuedMessages((prev) => {
      const next = [...prev];
      next[index] = newText;
      queuedMessagesRef.current = next;
      return next;
    });
  }, []);

  const handleReorderQueuedMessages = useCallback((fromIndex: number, toIndex: number) => {
    setQueuedMessages((prev) => {
      const next = [...prev];
      const [moved] = next.splice(fromIndex, 1);
      next.splice(toIndex, 0, moved);
      queuedMessagesRef.current = next;
      return next;
    });
  }, []);

  const handleClearQueuedMessages = useCallback(() => {
    setQueuedMessages([]);
    queuedMessagesRef.current = [];
  }, []);

  return {
    queuedMessages,
    queuedMessagesRef,
    setQueuedMessages,
    handleQueueMessage,
    handleRemoveQueuedMessage,
    handleEditQueuedMessage,
    handleReorderQueuedMessages,
    handleClearQueuedMessages,
  };
}

/**
 * Hook for the auto-send effect that dispatches queued messages.
 * Separated so it can be placed AFTER handleSendMessage is defined,
 * avoiding a circular dependency between useQueuedMessages and useMessageSending.
 */
export function useQueuedMessagesAutoSend(
  state: AppState,
  activeRequestsRef: MutableRefObject<number>,
  queuedMessagesRef: MutableRefObject<string[]>,
  setQueuedMessages: Dispatch<SetStateAction<string[]>>,
  handleSendMessage: (message: string) => Promise<void>,
  setState: Dispatch<SetStateAction<AppState>>,
): void {
  useEffect(() => {
    if (state.isProcessing || activeRequestsRef.current > 0) {
      return;
    }
    if (queuedMessagesRef.current.length === 0) {
      return;
    }

    const next = queuedMessagesRef.current.shift();
    setQueuedMessages([...queuedMessagesRef.current]);
    if (!next) return;

    handleSendMessage(next).catch((error) => {
      const errorMsg = error instanceof Error ? error.message : 'Failed to send queued message';
      setState((prev) => ({
        ...prev,
        lastError: `Failed to send queued message: ${errorMsg}`,
        messages: [
          ...prev.messages,
          {
            id: Date.now().toString(),
            type: 'assistant',
            content: `[FAIL] Error: ${errorMsg}`,
            timestamp: new Date(),
          },
        ],
      }));
      notificationBus.notify('error', 'Queued Message', 'Failed to send queued message: ' + errorMsg, 8000);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- activeRequestsRef, queuedMessagesRef, setQueuedMessages, and setState are all stable refs/setters, safe to omit
  }, [state.isProcessing, handleSendMessage]);
}
