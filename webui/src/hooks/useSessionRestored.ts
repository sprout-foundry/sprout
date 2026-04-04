/**
 * Session-restored event listener.
 *
 * Listens for the custom `ledit:session-restored` DOM event (dispatched
 * by the service worker when a previous editor session is recovered)
 * and hydrates the application state with the restored messages.
 */

import { useEffect } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import type { AppState, Message } from '../types/app';

export interface UseSessionRestoredOptions {
  setState: Dispatch<SetStateAction<AppState>>;
}

export function useSessionRestored({ setState }: UseSessionRestoredOptions): void {
  useEffect(() => {
    const handleSessionRestored = (event: Event) => {
      const customEvent = event as CustomEvent<{
        messages: Array<{ role: string; content: string }>;
      }>;
      const rawMessages = customEvent.detail?.messages;
      if (!Array.isArray(rawMessages)) return;

      const restoredMessages: Message[] = rawMessages
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m, i) => ({
          id: `restored-${i}`,
          type: m.role as 'user' | 'assistant',
          content: typeof m.content === 'string' ? m.content : '',
          timestamp: new Date(),
        }));

      if (restoredMessages.length > 0) {
        setState((prev) => ({
          ...prev,
          messages: restoredMessages,
          toolExecutions: [],
          fileEdits: [],
          subagentActivities: [],
          currentTodos: [],
          queryProgress: null,
          lastError: null,
          isProcessing: false,
        }));
      }
    };

    window.addEventListener('ledit:session-restored', handleSessionRestored);
    return () => window.removeEventListener('ledit:session-restored', handleSessionRestored);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps -- setState is a stable useState setter
}
