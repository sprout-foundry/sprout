/**
 * Message sending and stop-processing handlers.
 *
 * Manages sending user queries to the backend API and stopping
 * an in-progress query. Includes concurrent request tracking and
 * error handling with user-facing error messages.
 */

import { useCallback } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';
import type { AppState } from '../types/app';

export interface UseMessageSendingOptions {
  setState: Dispatch<SetStateAction<AppState>>;
  setInputValue: Dispatch<SetStateAction<string>>;
  activeChatIdRef: MutableRefObject<string | null>;
  activeRequestsRef: MutableRefObject<number>;
}

export interface UseMessageSendingReturn {
  handleSendMessage: (message: string, options?: { allowConcurrent?: boolean }) => Promise<void>;
  handleStopProcessing: () => Promise<void>;
}

export function useMessageSending({
  setState,
  setInputValue,
  activeChatIdRef,
  activeRequestsRef,
}: UseMessageSendingOptions): UseMessageSendingReturn {
  const apiService = ApiService.getInstance();

  const handleSendMessage = useCallback(
    async (message: string, options?: { allowConcurrent?: boolean }) => {
      if (!message.trim()) return;
      const trimmedMessage = message.trim();
      const allowConcurrent = options?.allowConcurrent === true;
      if (!allowConcurrent && activeRequestsRef.current > 0) {
        setState((prev) => ({
          ...prev,
          lastError: null,
          messages: [
            ...prev.messages,
            {
              id: Date.now().toString(),
              type: 'user',
              content: trimmedMessage,
              timestamp: new Date(),
            },
          ],
        }));
        await apiService.steerQuery(trimmedMessage, activeChatIdRef.current ?? undefined);
        setInputValue('');
        return;
      }
      activeRequestsRef.current += 1;

      // Clear any previous errors and set processing state
      setState((prev) => ({
        ...prev,
        isProcessing: true,
        lastError: null,
      }));

      try {
        debugLog('[>>] Sending message:', trimmedMessage);
        await apiService.sendQuery(trimmedMessage, activeChatIdRef.current ?? undefined);
        setInputValue('');
        debugLog('[OK] Message sent successfully');
      } catch (error) {
        console.error('[FAIL] Failed to send message:', error);
        if (activeRequestsRef.current > 0) {
          activeRequestsRef.current -= 1;
        }
        const errorMsg = error instanceof Error ? error.message : 'Failed to send message';
        setState((prev) => ({
          ...prev,
          isProcessing: activeRequestsRef.current > 0,
          lastError: `Failed to send message: ${errorMsg}`,
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
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [apiService],
  );

  const handleStopProcessing = useCallback(async () => {
    try {
      await apiService.stopQuery();
      setState((prev) => ({
        ...prev,
        lastError: null,
      }));
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : 'Failed to stop query';
      setState((prev) => ({
        ...prev,
        lastError: errorMsg,
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
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiService]);

  return { handleSendMessage, handleStopProcessing };
}
