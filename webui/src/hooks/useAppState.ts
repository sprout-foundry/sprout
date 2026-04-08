/**
 * Core application state initialization with persistence.
 *
 * Loads previously persisted state from localStorage and merges it with
 * runtime-only defaults (connection status, processing flags, etc.).
 */

import { useState } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import type { AppState } from '../types/app';
import { loadPersistedAppState } from '../services/appStatePersistence';

const DEFAULT_APP_STATE: AppState = {
  provider: '',
  model: '',
  sessionId: null,
  queryCount: 0,
  messages: [],
  logs: [],
  currentView: 'chat',
  toolExecutions: [],
  stats: {},
  currentTodos: [],
  fileEdits: [],
  subagentActivities: [],
  isConnected: false,
  isProcessing: false,
  lastError: null,
  queryProgress: null,
  activeChatId: null,
  chatSessions: [],
  perChatCache: {},
  securityApprovalRequest: null,
  securityPromptRequest: null,
};

export interface UseAppStateReturn {
  state: AppState;
  setState: Dispatch<SetStateAction<AppState>>;
}

export function useAppState(): UseAppStateReturn {
  const [state, setState] = useState<AppState>(() => {
    const persisted = loadPersistedAppState();
    return {
      ...DEFAULT_APP_STATE,
      ...persisted,
      // Runtime-only defaults that must never be loaded from storage
      isConnected: false,
      isProcessing: false,
      lastError: null,
      queryProgress: null,
      activeChatId: null,
      chatSessions: [],
      perChatCache: {},
    };
  });

  return { state, setState };
}
